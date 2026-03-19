package parse

import (
"bufio"
"fmt"
"regexp"
"strings"

"github.com/mona-actions/gh-repo-map/internal/model"
)

// githubURLPattern extracts org/repo from GitHub URLs (HTTPS or SSH).
var githubURLPattern = regexp.MustCompile(`github\.com[:/]([^/]+)/([^/.]+?)(?:\.git)?$`)

// ParseGitmodules parses a .gitmodules file and extracts submodule dependencies.
func ParseGitmodules(content, sourceFile, githubHost string, knownOrgs map[string]bool) []model.FileScanEdge {
var edges []model.FileScanEdge

hostPattern := buildHostPattern(githubHost)
scanner := bufio.NewScanner(strings.NewReader(content))

var currentPath string
for scanner.Scan() {
line := strings.TrimSpace(scanner.Text())

if strings.HasPrefix(line, "path") {
if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
currentPath = strings.TrimSpace(parts[1])
}
}

if strings.HasPrefix(line, "url") {
if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
url := strings.TrimSpace(parts[1])
if org, repo := extractGitHubRepo(url, hostPattern); org != "" && knownOrgs[org] {
edges = append(edges, model.FileScanEdge{
Target:     fmt.Sprintf("%s/%s", org, repo),
Type:       model.DepTypeSubmodule,
SourceFile: sourceFile,
Detail:     model.SubmoduleDetail{Type: model.DepTypeSubmodule, URL: url, Path: currentPath},
})
}
}
}
}

return edges
}

func buildHostPattern(githubHost string) *regexp.Regexp {
if githubHost == "" || githubHost == "github.com" {
return githubURLPattern
}
escaped := regexp.QuoteMeta(githubHost)
return regexp.MustCompile(escaped + `[:/]([^/]+)/([^/.]+?)(?:\.git)?$`)
}

func extractGitHubRepo(url string, hostPattern *regexp.Regexp) (org, repo string) {
if m := hostPattern.FindStringSubmatch(url); m != nil {
return m[1], m[2]
}
return "", ""
}
