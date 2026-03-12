package parse

import (
"bufio"
"fmt"
"regexp"
"strings"

"github.com/mona-actions/gh-repo-map/internal/model"
)

// Script dependency patterns
var (
scriptGitClone     = regexp.MustCompile(`git\s+clone\s+[^\s]*github\.com[:/]([^/\s]+)/([^/\s.]+)`)
scriptCurl         = regexp.MustCompile(`curl\s.*raw\.githubusercontent\.com/([^/\s]+)/([^/\s]+)`)
scriptWget         = regexp.MustCompile(`wget\s+[^\s]*github\.com/([^/\s]+)/([^/\s]+)`)
scriptGoInstall    = regexp.MustCompile(`go\s+install\s+github\.com/([^/\s]+)/([^/@\s]+)`)
scriptPipGit       = regexp.MustCompile(`pip\s+install\s+git\+https://github\.com/([^/\s]+)/([^/@\s.]+)`)
)

type scriptPattern struct {
re        *regexp.Regexp
matchType string
}

var scriptPatterns = []scriptPattern{
{scriptGitClone, "git_clone"},
{scriptCurl, "curl"},
{scriptWget, "wget"},
{scriptGoInstall, "go_install"},
{scriptPipGit, "pip_git"},
}

// ParseScript scans Makefile or shell script content for GitHub repository references.
func ParseScript(content, sourceFile, githubHost string, knownOrgs map[string]bool) []model.FileScanEdge {
var edges []model.FileScanEdge
seen := make(map[string]bool)

// For GHES, add host-specific patterns
var extraPatterns []scriptPattern
if githubHost != "" && githubHost != "github.com" {
escaped := regexp.QuoteMeta(githubHost)
extraPatterns = append(extraPatterns,
scriptPattern{
regexp.MustCompile(`git\s+clone\s+[^\s]*` + escaped + `[:/]([^/\s]+)/([^/\s.]+)`),
"git_clone",
},
)
}

allPatterns := append(scriptPatterns, extraPatterns...)

scanner := bufio.NewScanner(strings.NewReader(content))
for scanner.Scan() {
line := scanner.Text()
trimmed := strings.TrimSpace(line)

// Skip comments (# for shell/Makefile)
if strings.HasPrefix(trimmed, "#") {
continue
}

for _, sp := range allPatterns {
if m := sp.re.FindStringSubmatch(line); m != nil {
org, repo := m[1], m[2]
key := fmt.Sprintf("%s/%s:%s", org, repo, sp.matchType)
if knownOrgs[org] && !seen[key] {
seen[key] = true
edges = append(edges, model.FileScanEdge{
Target:     fmt.Sprintf("%s/%s", org, repo),
Type:       model.DepTypeScript,
SourceFile: sourceFile,
Detail:     model.ScriptDetail{Match: strings.TrimSpace(line), MatchType: sp.matchType},
})
}
}
}
}

return edges
}
