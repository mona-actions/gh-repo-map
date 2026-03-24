// Package purl handles Package URL normalization for cross-referencing
// SBOM purl-formatted names with raw manifest names.
package purl

import (
"net/url"
"regexp"
"strings"
)

// goMajorVersionSuffix matches /v2, /v3, etc. at the end of Go module paths.
var goMajorVersionSuffix = regexp.MustCompile(`/v\d+$`)

// pypiNormalize replaces hyphens, underscores, and dots with hyphens and lowercases.
var pypiNonAlphanumeric = regexp.MustCompile(`[-_.]+`)

// NormalizeEcosystem maps variant ecosystem names to a canonical purl-standard form.
// This ensures registry keys are consistent regardless of whether the ecosystem
// came from a manifest parser ("go") or a purl ("golang").
func NormalizeEcosystem(eco string) string {
switch strings.ToLower(eco) {
case "go", "golang":
return "golang"
case "pip", "pypi":
return "pypi"
default:
return strings.ToLower(eco)
}
}

// ParsePurl extracts (ecosystem, canonicalName) from a purl string.
// The input MUST be a proper purl (starting with "pkg:").
// Example: "pkg:npm/%40org/auth-lib@2.0.0" → ("npm", "@org/auth-lib")
func ParsePurl(purlStr string) (ecosystem, name string) {
if !strings.HasPrefix(purlStr, "pkg:") {
return "", ""
}

// Strip the "pkg:" prefix
s := strings.TrimPrefix(purlStr, "pkg:")

// Split off version (@...)
if idx := strings.LastIndex(s, "@"); idx > 0 {
// Be careful: npm scoped packages have @ in the name
// The version @ is always the LAST one
s = s[:idx]
}

// Split ecosystem from the rest
parts := strings.SplitN(s, "/", 2)
if len(parts) < 2 {
return s, ""
}
ecosystem = NormalizeEcosystem(parts[0])
rawName := parts[1]

// URL-decode the name (handles %40 → @, etc.)
decoded, err := url.PathUnescape(rawName)
if err != nil {
decoded = rawName
}

return ecosystem, NormalizeManifestName(ecosystem, decoded)
}

// NormalizeManifestName converts a raw manifest package name to its canonical form
// for use as a lookup key. The canonical form must match what ParsePurl produces.
func NormalizeManifestName(ecosystem, rawName string) string {
switch strings.ToLower(ecosystem) {
case "npm":
return normalizeNpm(rawName)
case "maven":
return normalizeMaven(rawName)
case "golang", "go":
return normalizeGo(rawName)
case "pypi", "pip":
return normalizePypi(rawName)
case "nuget":
return normalizeNuget(rawName)
case "cargo":
return rawName // crate names are case-sensitive, no quirks
case "gem":
return strings.ToLower(rawName) // gems are lowercase by convention
case "composer":
return rawName // Composer uses vendor/name consistently
default:
return rawName
}
}

// normalizeNpm handles scoped packages: ensure @scope/name format.
func normalizeNpm(name string) string {
// Already canonical if it starts with @
return name
}

// normalizeMaven converts between groupId/artifactId and groupId:artifactId.
// purl uses / separator, manifest uses : separator. We normalize to groupId:artifactId.
func normalizeMaven(name string) string {
// If it contains /, convert to :
return strings.Replace(name, "/", ":", 1)
}

// normalizeGo strips major version suffixes (github.com/org/repo/v2 → github.com/org/repo).
func normalizeGo(name string) string {
return goMajorVersionSuffix.ReplaceAllString(name, "")
}

// normalizePypi normalizes per PEP 503: lowercase, replace [-_.] with -.
func normalizePypi(name string) string {
normalized := strings.ToLower(name)
normalized = pypiNonAlphanumeric.ReplaceAllString(normalized, "-")
return normalized
}

// normalizeNuget lowercases since NuGet IDs are case-insensitive.
func normalizeNuget(name string) string {
return strings.ToLower(name)
}
