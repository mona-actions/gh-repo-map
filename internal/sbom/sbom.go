// Package sbom fetches SBOM data from the GitHub Dependency Graph API.
package sbom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v84/github"
	"github.com/mona-actions/gh-repo-map/internal/auth"
	"github.com/mona-actions/gh-repo-map/internal/model"
	"github.com/mona-actions/gh-repo-map/internal/purl"
	"github.com/mona-actions/gh-repo-map/internal/retry"
)

// extractPurlFromRefs finds the purl locator from SPDX ExternalRefs.
// Returns "" if no PACKAGE-MANAGER purl reference exists.
func extractPurlFromRefs(refs []*github.PackageExternalRef) string {
	for _, ref := range refs {
		if ref != nil && ref.ReferenceCategory == "PACKAGE-MANAGER" && ref.ReferenceType == "purl" {
			return ref.ReferenceLocator
		}
	}
	return ""
}

// FetchSBOM retrieves the SBOM for a repository via the Dependency Graph API.
// Returns consumed packages and whether the SBOM was available.
func FetchSBOM(ctx context.Context, tp auth.TokenProvider, owner, repo string) ([]model.ConsumedPackage, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	clients, err := tp.Clients(ctx, owner)
	if err != nil {
		return nil, false, err
	}

	document, available, err := fetchDocument(ctx, clients.REST, owner, repo)
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if err != nil || !available {
		return nil, false, err
	}
	if document == nil || document.GetSPDXID() != "SPDXRef-DOCUMENT" ||
		!validSPDXVersion(document.GetSPDXVersion()) {
		return nil, false, errors.New("SBOM response is not a valid SPDX document")
	}

	var consumed []model.ConsumedPackage
	for _, pkg := range document.Packages {
		if pkg == nil || pkg.GetSPDXID() == "SPDXRef-DOCUMENT" {
			continue
		}

		// Prefer the actual purl from ExternalRefs (accurate ecosystem + name).
		// Fall back to the Name field only if it's already in purl format.
		purlStr := extractPurlFromRefs(pkg.ExternalRefs)
		if purlStr == "" {
			// Some SPDX packages embed the purl directly in the Name field
			name := pkg.GetName()
			if strings.HasPrefix(name, "pkg:") {
				purlStr = name
			}
		}

		if purlStr != "" {
			if ecosystem, pkgName := purl.ParsePurl(purlStr); ecosystem != "" && pkgName != "" {
				consumed = append(consumed, model.ConsumedPackage{
					Name:      pkgName,
					Ecosystem: ecosystem,
					Version:   pkg.GetVersionInfo(),
				})
			}
		} else {
			slog.Debug("SBOM package has no purl", "name", pkg.GetName(), "repo", owner+"/"+repo)
		}
	}

	return consumed, true, nil
}

const maxSBOMBody = 32 << 20

func validSPDXVersion(version string) bool {
	// SBOMInfo models SPDX 2.x JSON, not SPDX 3's different document format.
	minor, err := strconv.Atoi(strings.TrimPrefix(version, "SPDX-2."))
	return strings.HasPrefix(version, "SPDX-2.") && err == nil && minor >= 0 &&
		version == fmt.Sprintf("SPDX-2.%d", minor)
}

func fetchDocument(ctx context.Context, client *github.Client, owner, repo string) (*github.SBOMInfo, bool, error) {
	path := fmt.Sprintf("repos/%s/%s/dependency-graph/sbom", url.PathEscape(owner), url.PathEscape(repo))
	req, err := client.NewRequest(http.MethodGet, path+"/generate-report", nil)
	if err != nil {
		return nil, false, errors.New("cannot create SBOM generation request")
	}
	// Use the SDK's authenticated transport, but handle 202 and 302 ourselves:
	// Client.Do treats both as errors, and following 302 would expose credentials.
	api := client.Client()
	api.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := sbomRequest(ctx, api, req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound &&
		resp.Header.Get("X-GitHub-Enterprise-Version") != "" &&
		!strings.EqualFold(client.BaseURL.Hostname(), "github.com") &&
		!strings.EqualFold(client.BaseURL.Hostname(), "api.github.com") {
		resp.Body.Close()
		return fetchLegacyDocument(ctx, client, api, owner, repo)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		slog.Debug("SBOM not available", "repo", owner+"/"+repo, "status", resp.StatusCode)
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, false, fmt.Errorf("SBOM generation returned HTTP %d", resp.StatusCode)
	}
	var generated struct {
		URL string `json:"sbom_url"`
	}
	if err := decodeSBOMJSON(resp.Body, &generated); err != nil {
		return nil, false, err
	}
	resp.Body.Close()
	report, err := url.Parse(generated.URL)
	prefix := strings.TrimSuffix(req.URL.Path, "generate-report") + "fetch-report/"
	if err != nil || !validSBOMURL(report) ||
		!strings.EqualFold(report.Host, client.BaseURL.Host) ||
		!strings.EqualFold(report.Scheme, client.BaseURL.Scheme) ||
		report.RawQuery != "" || report.ForceQuery ||
		!strings.HasPrefix(report.Path, prefix) {
		return nil, false, errors.New("invalid SBOM polling URL")
	}
	id := strings.TrimPrefix(report.Path, prefix)
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") ||
		report.EscapedPath() != report.Path {
		return nil, false, errors.New("invalid SBOM report ID")
	}
	pollDelay := time.Second
	for {
		req, err = client.NewRequest(http.MethodGet, report.String(), nil)
		if err != nil {
			return nil, false, errors.New("cannot create SBOM polling request")
		}
		resp, err = sbomRequest(ctx, api, req)
		if err != nil {
			return nil, false, err
		}
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusCreated, http.StatusAccepted:
			delay := max(pollDelay, sbomRetryAfter(resp.Header))
			if err := waitSBOM(ctx, delay); err != nil {
				return nil, false, err
			}
			pollDelay = min(2*pollDelay, 5*time.Second)
		case http.StatusFound:
			document, err := downloadDocument(ctx, resp.Header.Get("Location"))
			return document, err == nil, err
		default:
			return nil, false, fmt.Errorf("SBOM polling returned HTTP %d", resp.StatusCode)
		}
	}
}

func validSBOMURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Hostname() != "" &&
		u.User == nil && u.Fragment == "" && !strings.Contains(u.String(), "#") && u.Opaque == ""
}

func downloadDocument(ctx context.Context, location string) (*github.SBOMInfo, error) {
	u, err := url.Parse(location)
	if err != nil || !validSBOMURL(u) {
		return nil, errors.New("invalid SBOM download URL")
	}
	// Deliberately do not reuse the authenticated API transport or its cookie jar.
	client := &http.Client{
		Transport: http.DefaultTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !validSBOMURL(req.URL) {
				return errors.New("invalid SBOM download redirect")
			}
			req.Header.Del("Referer")
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("cannot create SBOM download request")
	}
	resp, err := sbomRequest(ctx, client, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SBOM download returned HTTP %d", resp.StatusCode)
	}
	var document *github.SBOMInfo
	if err := decodeSBOMJSON(resp.Body, &document); err != nil {
		return nil, err
	}
	return document, nil
}

func decodeSBOMJSON(body io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxSBOMBody+1))
	if err != nil {
		return errors.New("cannot read SBOM response")
	}
	if len(data) > maxSBOMBody {
		return errors.New("SBOM response exceeds size limit")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("invalid SBOM JSON response")
	}
	return nil
}

func waitSBOM(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sbomRetryAfter(header http.Header) time.Duration {
	if seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64); err == nil && seconds > 0 {
		return time.Duration(min(seconds, int64(300))) * time.Second
	}
	if date, err := http.ParseTime(header.Get("Retry-After")); err == nil {
		return max(0, time.Until(date))
	}
	return 0
}

func sbomRateLimited(resp *http.Response) bool {
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
		return true
	}
	var failure struct {
		Message string `json:"message"`
		URL     string `json:"documentation_url"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&failure)
	return strings.Contains(strings.ToLower(failure.Message), "rate limit") ||
		strings.Contains(failure.URL, "rate-limits")
}

// Keep retries separate from pending polls, and never include response bodies or
// request URLs in errors: download URLs can contain signed credentials.
func sbomRequest(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := client.Do(req.Clone(ctx))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("SBOM HTTP request failed")
		}
		if !retry.IsRetryableStatus(resp.StatusCode) && !sbomRateLimited(resp) {
			return resp, nil
		}
		resp.Body.Close()
		if attempt >= retry.MaxRetries {
			return nil, fmt.Errorf("SBOM HTTP %d after %d retries", resp.StatusCode, retry.MaxRetries)
		}
		if err := waitSBOM(ctx, max(time.Second<<uint(attempt), sbomRetryAfter(resp.Header))); err != nil {
			return nil, err
		}
	}
}

func fetchLegacyDocument(ctx context.Context, original *github.Client, api *http.Client, owner, repo string) (*github.SBOMInfo, bool, error) {
	path := fmt.Sprintf("repos/%s/%s/dependency-graph/sbom", url.PathEscape(owner), url.PathEscape(repo))
	req, err := original.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, false, errors.New("cannot create legacy SBOM request")
	}
	resp, err := sbomRequest(ctx, api, req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		slog.Debug("SBOM not available", "repo", owner+"/"+repo, "status", resp.StatusCode)
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("legacy SBOM request returned HTTP %d", resp.StatusCode)
	}
	var document github.SBOM
	if err := decodeSBOMJSON(resp.Body, &document); err != nil {
		return nil, false, err
	}
	if document.SBOM == nil {
		return nil, false, errors.New("missing legacy SBOM document")
	}
	return document.SBOM, true, nil
}
