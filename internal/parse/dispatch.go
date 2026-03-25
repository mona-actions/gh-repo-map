package parse

import (
	"path/filepath"
	"strings"

	"github.com/mona-actions/gh-repo-map/internal/model"
)

// ParseFile dispatches parsing based on file path and returns discovered edges,
// published identities, and consumed packages.
func ParseFile(path, content string, knownOrgs map[string]bool, registries []string, host string) ([]model.FileScanEdge, []model.PublishedIdentity, []model.ConsumedPackage) {
	var edges []model.FileScanEdge
	var published []model.PublishedIdentity
	var consumed []model.ConsumedPackage

	base := filepath.Base(path)
	dir := filepath.Dir(path)

	switch {
	case strings.Contains(dir, ".github/workflows") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")):
		e, _ := ParseActionsWorkflow(content, path, knownOrgs)
		edges = append(edges, e...)

	case base == ".gitmodules":
		edges = append(edges, ParseGitmodules(content, path, host, knownOrgs)...)

	case strings.HasPrefix(base, "Dockerfile"):
		edges = append(edges, ParseDockerfile(content, path, host, registries, knownOrgs)...)

	case base == "docker-compose.yml" || base == "docker-compose.yaml":
		edges = append(edges, ParseDockerCompose(content, path, host, registries, knownOrgs)...)

	case strings.HasSuffix(base, ".tf"):
		edges = append(edges, ParseTerraform(content, path, host, knownOrgs)...)

	case strings.HasSuffix(base, ".sh") || base == "Makefile":
		edges = append(edges, ParseScript(content, path, host, knownOrgs)...)

	case base == "package.json":
		c, p := ParsePackageJSON(content)
		published = append(published, p...)
		consumed = append(consumed, c...)

	case base == "go.mod":
		c, p := ParseGoMod(content)
		published = append(published, p...)
		consumed = append(consumed, c...)

	case base == "pom.xml":
		c, p := ParsePomXML(content)
		published = append(published, p...)
		consumed = append(consumed, c...)

	case base == "pyproject.toml":
		c, p := ParsePyprojectTOML(content)
		published = append(published, p...)
		consumed = append(consumed, c...)

	case base == "requirements.txt":
		consumed = append(consumed, ParseRequirementsTxt(content)...)

	case strings.HasSuffix(base, ".csproj"):
		c, p := ParseCsproj(content)
		published = append(published, p...)
		consumed = append(consumed, c...)

	case base == "Cargo.toml":
		c, p := ParseCargoToml(content)
		published = append(published, p...)
		consumed = append(consumed, c...)

	case base == "Gemfile":
		consumed = append(consumed, ParseGemfile(content)...)

	case strings.HasSuffix(base, ".gemspec"):
		published = append(published, ParseGemspec(content)...)

	case base == "composer.json":
		c, p := ParseComposerJSON(content)
		published = append(published, p...)
		consumed = append(consumed, c...)
	}

	return edges, published, consumed
}
