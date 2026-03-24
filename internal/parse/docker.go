package parse

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"

	"github.com/mona-actions/gh-repo-map/internal/model"
	"gopkg.in/yaml.v3"
)

// gitCloneInDockerPattern matches git clone commands with GitHub URLs.
var gitCloneInDockerPattern = regexp.MustCompile(`git\s+clone\s+[^\s]*github\.com[:/]([^/\s]+)/([^/\s.]+)`)

// ParseDockerfile parses a Dockerfile and extracts dependency references
// from FROM instructions and git clone commands.
func ParseDockerfile(content, sourceFile, githubHost string, registries []string, knownOrgs map[string]bool) []model.FileScanEdge {
	var edges []model.FileScanEdge

	registryPrefixes := buildRegistryPrefixes(githubHost, registries)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(strings.ToUpper(line), "FROM ") {
			image := extractFromImage(line)
			if org, repo := matchRegistryImage(image, registryPrefixes, knownOrgs); org != "" {
				edges = append(edges, model.FileScanEdge{
					Target:     fmt.Sprintf("%s/%s", org, repo),
					Type:       model.DepTypeDocker,
					SourceFile: sourceFile,
					Detail:     model.DockerDetail{Type: model.DepTypeDocker, Image: image},
				})
			}
		}

		if m := gitCloneInDockerPattern.FindStringSubmatch(line); m != nil {
			org, repo := m[1], m[2]
			if knownOrgs[org] {
				edges = append(edges, model.FileScanEdge{
					Target:     fmt.Sprintf("%s/%s", org, repo),
					Type:       model.DepTypeDocker,
					SourceFile: sourceFile,
					Detail:     model.DockerDetail{Type: model.DepTypeDocker, Image: fmt.Sprintf("git clone: %s/%s", org, repo)},
				})
			}
		}
	}

	return edges
}

func buildRegistryPrefixes(githubHost string, registries []string) []string {
	prefixes := []string{"ghcr.io"}
	if githubHost != "" && githubHost != "github.com" {
		prefixes = append(prefixes, "ghcr."+githubHost)
	}
	prefixes = append(prefixes, registries...)
	return prefixes
}

func extractFromImage(line string) string {
	parts := strings.Fields(line)
	for i := 1; i < len(parts); i++ {
		if strings.HasPrefix(parts[i], "--") {
			continue
		}
		return parts[i]
	}
	return ""
}

func matchRegistryImage(image string, registries []string, knownOrgs map[string]bool) (org, repo string) {
	for _, reg := range registries {
		prefix := reg + "/"
		if strings.HasPrefix(image, prefix) {
			rest := strings.TrimPrefix(image, prefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) >= 2 {
				org = parts[0]
				repo = strings.SplitN(parts[1], ":", 2)[0]
				if knownOrgs[org] {
					return org, repo
				}
			}
		}
	}
	return "", ""
}

// ParseDockerCompose parses a docker-compose.yml and extracts image references.
func ParseDockerCompose(content, sourceFile, githubHost string, registries []string, knownOrgs map[string]bool) []model.FileScanEdge {
	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}

	if err := yaml.Unmarshal([]byte(content), &compose); err != nil {
		return nil
	}

	registryPrefixes := buildRegistryPrefixes(githubHost, registries)

	var edges []model.FileScanEdge
	for _, svc := range compose.Services {
		if svc.Image != "" {
			if org, repo := matchRegistryImage(svc.Image, registryPrefixes, knownOrgs); org != "" {
				edges = append(edges, model.FileScanEdge{
					Target:     fmt.Sprintf("%s/%s", org, repo),
					Type:       model.DepTypeDocker,
					SourceFile: sourceFile,
					Detail:     model.DockerDetail{Type: model.DepTypeDocker, Image: svc.Image},
				})
			}
		}
	}

	return edges
}
