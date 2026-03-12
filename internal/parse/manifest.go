package parse

import (
"encoding/json"
"encoding/xml"
"regexp"
"strings"

"github.com/mona-actions/gh-repo-map/internal/model"
)

// --- npm (package.json) ---

type packageJSON struct {
Name            string            `json:"name"`
Dependencies    map[string]string `json:"dependencies"`
DevDependencies map[string]string `json:"devDependencies"`
}

// ParsePackageJSON extracts consumed packages and publish identity from package.json.
func ParsePackageJSON(content string) (consumed []model.ConsumedPackage, published []model.PublishedIdentity) {
var pkg packageJSON
if err := json.Unmarshal([]byte(content), &pkg); err != nil {
return nil, nil
}

if pkg.Name != "" {
published = append(published, model.PublishedIdentity{Name: pkg.Name, Ecosystem: "npm"})
}

for name, version := range pkg.Dependencies {
consumed = append(consumed, model.ConsumedPackage{Name: name, Ecosystem: "npm", Version: version})
}
for name, version := range pkg.DevDependencies {
consumed = append(consumed, model.ConsumedPackage{Name: name, Ecosystem: "npm", Version: version})
}

return consumed, published
}

// --- Go (go.mod) ---

var (
goModulePattern  = regexp.MustCompile(`(?m)^module\s+(\S+)`)
goRequirePattern = regexp.MustCompile(`(?m)^\s*(\S+)\s+v[\d.]+`)
goRequireBlock   = regexp.MustCompile(`(?s)require\s*\((.*?)\)`)
)

// ParseGoMod extracts consumed modules and publish identity from go.mod.
func ParseGoMod(content string) (consumed []model.ConsumedPackage, published []model.PublishedIdentity) {
if m := goModulePattern.FindStringSubmatch(content); m != nil {
published = append(published, model.PublishedIdentity{Name: m[1], Ecosystem: "go"})
}

// Parse require blocks
for _, block := range goRequireBlock.FindAllStringSubmatch(content, -1) {
for _, req := range goRequirePattern.FindAllStringSubmatch(block[1], -1) {
consumed = append(consumed, model.ConsumedPackage{
Name:      req[1],
Ecosystem: "go",
Version:   "",
})
}
}

// Parse single-line require statements
singleRequire := regexp.MustCompile(`(?m)^require\s+(\S+)\s+v[\d.]+`)
for _, m := range singleRequire.FindAllStringSubmatch(content, -1) {
consumed = append(consumed, model.ConsumedPackage{
Name:      m[1],
Ecosystem: "go",
Version:   "",
})
}

return consumed, published
}

// --- Maven (pom.xml) ---

type pomXML struct {
XMLName      xml.Name       `xml:"project"`
GroupID      string         `xml:"groupId"`
ArtifactID   string        `xml:"artifactId"`
Dependencies pomDependencies `xml:"dependencies"`
}

type pomDependencies struct {
Dependency []pomDependency `xml:"dependency"`
}

type pomDependency struct {
GroupID    string `xml:"groupId"`
ArtifactID string `xml:"artifactId"`
Version    string `xml:"version"`
}

// ParsePomXML extracts consumed packages and publish identity from pom.xml.
func ParsePomXML(content string) (consumed []model.ConsumedPackage, published []model.PublishedIdentity) {
var pom pomXML
if err := xml.Unmarshal([]byte(content), &pom); err != nil {
return nil, nil
}

if pom.GroupID != "" && pom.ArtifactID != "" {
published = append(published, model.PublishedIdentity{
Name:      pom.GroupID + ":" + pom.ArtifactID,
Ecosystem: "maven",
})
}

for _, dep := range pom.Dependencies.Dependency {
consumed = append(consumed, model.ConsumedPackage{
Name:      dep.GroupID + ":" + dep.ArtifactID,
Ecosystem: "maven",
Version:   dep.Version,
})
}

return consumed, published
}

// --- Python (pyproject.toml / requirements.txt) ---

var (
pyprojectName = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
pyprojectDeps = regexp.MustCompile(`(?s)dependencies\s*=\s*\[(.*?)\]`)
pyDepEntry    = regexp.MustCompile(`"([a-zA-Z0-9][a-zA-Z0-9._-]*)`)
requiresTxt   = regexp.MustCompile(`(?m)^([a-zA-Z0-9_.-]+)`)
)

// ParsePyprojectTOML extracts consumed packages and publish identity from pyproject.toml.
func ParsePyprojectTOML(content string) (consumed []model.ConsumedPackage, published []model.PublishedIdentity) {
if m := pyprojectName.FindStringSubmatch(content); m != nil {
published = append(published, model.PublishedIdentity{Name: m[1], Ecosystem: "pypi"})
}

	if block := pyprojectDeps.FindStringSubmatch(content); block != nil {
		for _, dep := range pyDepEntry.FindAllStringSubmatch(block[1], -1) {
name := strings.TrimSpace(dep[1])
if name != "" {
consumed = append(consumed, model.ConsumedPackage{Name: name, Ecosystem: "pypi"})
}
}
}

return consumed, published
}

// ParseRequirementsTxt extracts consumed packages from requirements.txt.
func ParseRequirementsTxt(content string) []model.ConsumedPackage {
var consumed []model.ConsumedPackage
for _, line := range strings.Split(content, "\n") {
line = strings.TrimSpace(line)
if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
continue
}
if m := requiresTxt.FindStringSubmatch(line); m != nil {
consumed = append(consumed, model.ConsumedPackage{
Name:      m[1],
Ecosystem: "pypi",
})
}
}
return consumed
}

// --- NuGet (*.csproj) ---

var (
nugetPackageRef = regexp.MustCompile(`<PackageReference\s+Include="([^"]+)"(?:\s+Version="([^"]*)")?`)
nugetPackageID  = regexp.MustCompile(`<PackageId>([^<]+)</PackageId>`)
)

// ParseCsproj extracts consumed packages and publish identity from .csproj files.
func ParseCsproj(content string) (consumed []model.ConsumedPackage, published []model.PublishedIdentity) {
if m := nugetPackageID.FindStringSubmatch(content); m != nil {
published = append(published, model.PublishedIdentity{Name: m[1], Ecosystem: "nuget"})
}

for _, m := range nugetPackageRef.FindAllStringSubmatch(content, -1) {
version := ""
if len(m) > 2 {
version = m[2]
}
consumed = append(consumed, model.ConsumedPackage{
Name:      m[1],
Ecosystem: "nuget",
Version:   version,
})
}

return consumed, published
}

// --- Rust (Cargo.toml) ---

var (
cargoName    = regexp.MustCompile(`(?m)^\[package\][^[]*?name\s*=\s*"([^"]+)"`)
cargoDeps    = regexp.MustCompile(`(?m)^([a-zA-Z0-9_-]+)\s*=`)
cargoDepsBlock = regexp.MustCompile(`(?s)\[dependencies\](.*?)(?:\[|\z)`)
)

// ParseCargoToml extracts consumed packages and publish identity from Cargo.toml.
func ParseCargoToml(content string) (consumed []model.ConsumedPackage, published []model.PublishedIdentity) {
if m := cargoName.FindStringSubmatch(content); m != nil {
published = append(published, model.PublishedIdentity{Name: m[1], Ecosystem: "cargo"})
}

if block := cargoDepsBlock.FindStringSubmatch(content); block != nil {
for _, m := range cargoDeps.FindAllStringSubmatch(block[1], -1) {
consumed = append(consumed, model.ConsumedPackage{
Name:      m[1],
Ecosystem: "cargo",
})
}
}

return consumed, published
}

// --- Ruby (Gemfile / .gemspec) ---

var (
gemspecName = regexp.MustCompile(`\.name\s*=\s*['"]([^'"]+)['"]`)
gemfileDep  = regexp.MustCompile(`(?m)^\s*gem\s+['"]([^'"]+)['"]`)
)

// ParseGemspec extracts the gem name from a .gemspec file.
func ParseGemspec(content string) []model.PublishedIdentity {
if m := gemspecName.FindStringSubmatch(content); m != nil {
return []model.PublishedIdentity{{Name: m[1], Ecosystem: "gem"}}
}
return nil
}

// ParseGemfile extracts consumed gems from a Gemfile.
func ParseGemfile(content string) []model.ConsumedPackage {
var consumed []model.ConsumedPackage
for _, m := range gemfileDep.FindAllStringSubmatch(content, -1) {
consumed = append(consumed, model.ConsumedPackage{Name: m[1], Ecosystem: "gem"})
}
return consumed
}

// --- PHP (composer.json) ---

type composerJSON struct {
Name    string            `json:"name"`
Require map[string]string `json:"require"`
}

// ParseComposerJSON extracts consumed packages and publish identity from composer.json.
func ParseComposerJSON(content string) (consumed []model.ConsumedPackage, published []model.PublishedIdentity) {
var comp composerJSON
if err := json.Unmarshal([]byte(content), &comp); err != nil {
return nil, nil
}

if comp.Name != "" {
published = append(published, model.PublishedIdentity{Name: comp.Name, Ecosystem: "composer"})
}

for name, version := range comp.Require {
// Skip PHP extensions (ext-*) and php itself
if strings.HasPrefix(name, "ext-") || name == "php" {
continue
}
consumed = append(consumed, model.ConsumedPackage{Name: name, Ecosystem: "composer", Version: version})
}

return consumed, published
}
