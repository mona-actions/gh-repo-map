package parse

import (
"fmt"
"regexp"
"strings"

"github.com/mona-actions/gh-repo-map/internal/model"
)

// Terraform module source patterns.
// The SSH pattern uses [^/?."]+ to capture repo names with hyphens.
var (
tfGitHubHTTPS = regexp.MustCompile(`source\s*=\s*"(?:git::)?https://github\.com/([^/]+)/([^/?"]+)(?:[^"]*\?ref=([^"]+))?"`)
tfGitHubSSH   = regexp.MustCompile(`source\s*=\s*"git::ssh://git@github\.com/([^/]+)/([^/?".]+?)(?:\.git)?(?:\?ref=([^"]+))?"`)
tfGitHubShort = regexp.MustCompile(`source\s*=\s*"github\.com/([^/]+)/([^/?"]+)(?:[^"]*\?ref=([^"]+))?"`)
)

// ParseTerraform scans Terraform file content for module source references.
func ParseTerraform(content, sourceFile, githubHost string, knownOrgs map[string]bool) []model.FileScanEdge {
var edges []model.FileScanEdge

patterns := []*regexp.Regexp{tfGitHubHTTPS, tfGitHubSSH, tfGitHubShort}

if githubHost != "" && githubHost != "github.com" {
escaped := regexp.QuoteMeta(githubHost)
patterns = append(patterns,
regexp.MustCompile(`source\s*=\s*"(?:git::)?https://`+escaped+`/([^/]+)/([^/?"]+)(?:[^"]*\?ref=([^"]+))?"`),
regexp.MustCompile(`source\s*=\s*"git::ssh://git@`+escaped+`/([^/]+)/([^/?".]+?)(?:\.git)?(?:\?ref=([^"]+))?"`),
)
}

seen := make(map[string]bool)
for _, line := range strings.Split(content, "\n") {
trimmed := strings.TrimSpace(line)
if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
continue
}

for _, pat := range patterns {
if m := pat.FindStringSubmatch(line); m != nil {
org, repo := m[1], strings.TrimSuffix(m[2], ".git")
ref := ""
if len(m) > 3 {
ref = m[3]
}
key := fmt.Sprintf("%s/%s", org, repo)
if knownOrgs[org] && !seen[key] {
seen[key] = true
source := fmt.Sprintf("github.com/%s/%s", org, repo)
edges = append(edges, model.FileScanEdge{
Target:     key,
Type:       model.DepTypeTerraform,
SourceFile: sourceFile,
Detail:     model.TerraformDetail{Source: source, Ref: ref},
})
}
}
}
}

return edges
}
