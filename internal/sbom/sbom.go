// Package sbom fetches SBOM data from the GitHub Dependency Graph API.
package sbom

import (
"context"
"log/slog"

"github.com/google/go-github/v84/github"
"github.com/mona-actions/gh-repo-map/internal/auth"
"github.com/mona-actions/gh-repo-map/internal/model"
"github.com/mona-actions/gh-repo-map/internal/purl"
"github.com/mona-actions/gh-repo-map/internal/retry"
)

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

// GitHub's SPDX packages use the name field in purl format
// e.g., "pkg:npm/@scope/package@1.0.0"
name := pkg.GetName()
if ecosystem, pkgName := purl.ParsePurl(name); ecosystem != "" && pkgName != "" {
consumed = append(consumed, model.ConsumedPackage{
Name:      pkgName,
Ecosystem: ecosystem,
Version:   pkg.GetVersionInfo(),
})
} else if name != "" {
// Fallback: use name directly if not in purl format
slog.Debug("SBOM package not in purl format", "name", name)
}
}
}

return consumed, true, nil
}
