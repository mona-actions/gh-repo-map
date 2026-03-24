// Package sbom fetches SBOM data from the GitHub Dependency Graph API.
package sbom

import (
	"context"
	"log/slog"
	"strings"

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
	clients, err := tp.Clients(ctx, owner)
	if err != nil {
		return nil, false, err
	}

	sbom, resp, err := clients.REST.DependencyGraph.GetSBOM(ctx, owner, repo)
	if err != nil {
		if resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 403) {
			slog.Debug("SBOM not available", "repo", owner+"/"+repo, "status", resp.StatusCode)
			return nil, false, nil
		}
		if resp != nil && retry.IsRetryableStatus(resp.StatusCode) {
			var retrySBOM *github.SBOM
			retryErr := retry.Do(ctx, "sbom:"+owner+"/"+repo, func() error {
				var retryResp *github.Response
				var err2 error
				retrySBOM, retryResp, err2 = clients.REST.DependencyGraph.GetSBOM(ctx, owner, repo)
				if err2 != nil && retryResp != nil && (retryResp.StatusCode == 404 || retryResp.StatusCode == 403) {
					return nil
				}
				return err2
			}, func(e error) bool { return e != nil })
			if retryErr != nil {
				return nil, false, retryErr
			}
			sbom = retrySBOM
		} else {
			return nil, false, err
		}
	}

	var consumed []model.ConsumedPackage
	if sbom.SBOM != nil {
		for _, pkg := range sbom.SBOM.Packages {
			if pkg.GetSPDXID() == "SPDXRef-DOCUMENT" {
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
	}

	return consumed, true, nil
}
