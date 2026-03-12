package parse

import (
"fmt"
"regexp"
"strings"

"github.com/mona-actions/gh-repo-map/internal/model"
"gopkg.in/yaml.v3"
)

// workflowUsesPattern matches owner/repo/.github/workflows/file@ref
var workflowUsesPattern = regexp.MustCompile(`^([^/]+)/([^/]+)/\.github/workflows/([^@]+)@(.+)$`)

// actionUsesPattern matches owner/repo@ref or owner/repo/path@ref
var actionUsesPattern = regexp.MustCompile(`^([^/]+)/([^/@]+)(?:/[^@]+)?@(.+)$`)

// workflowFile represents a GitHub Actions workflow YAML structure.
type workflowFile struct {
Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
Uses  string         `yaml:"uses"`
Steps []workflowStep `yaml:"steps"`
}

type workflowStep struct {
Uses string `yaml:"uses"`
}

// ParseActionsWorkflow parses a GitHub Actions workflow YAML and extracts
// dependency references from `uses` fields.
func ParseActionsWorkflow(content, sourceFile string, knownOrgs map[string]bool) ([]model.FileScanEdge, []string) {
var wf workflowFile
if err := yaml.Unmarshal([]byte(content), &wf); err != nil {
return nil, []string{fmt.Sprintf("yaml parse error in %s: %v", sourceFile, err)}
}

var edges []model.FileScanEdge
var unresolvable []string

for _, job := range wf.Jobs {
edges, unresolvable = processUsesRef(job.Uses, sourceFile, knownOrgs, edges, unresolvable)
for _, step := range job.Steps {
edges, unresolvable = processUsesRef(step.Uses, sourceFile, knownOrgs, edges, unresolvable)
}
}

return edges, unresolvable
}

func processUsesRef(uses, sourceFile string, knownOrgs map[string]bool, edges []model.FileScanEdge, unresolvable []string) ([]model.FileScanEdge, []string) {
if uses == "" {
return edges, unresolvable
}

// Skip local actions
if strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "docker://") {
return edges, unresolvable
}

// Skip templated uses
if strings.Contains(uses, "${{") {
unresolvable = append(unresolvable, fmt.Sprintf("templated uses: %s in %s", uses, sourceFile))
return edges, unresolvable
}

// Try reusable workflow pattern first (more specific)
if m := workflowUsesPattern.FindStringSubmatch(uses); m != nil {
org, repo := m[1], m[2]
if knownOrgs[org] {
edges = append(edges, model.FileScanEdge{
Target:     fmt.Sprintf("%s/%s", org, repo),
Type:       model.DepTypeWorkflow,
SourceFile: sourceFile,
Detail:     model.WorkflowDetail{Uses: uses},
})
}
return edges, unresolvable
}

// Try action pattern
if m := actionUsesPattern.FindStringSubmatch(uses); m != nil {
org, repo := m[1], m[2]
if knownOrgs[org] {
edges = append(edges, model.FileScanEdge{
Target:     fmt.Sprintf("%s/%s", org, repo),
Type:       model.DepTypeAction,
SourceFile: sourceFile,
Detail:     model.ActionDetail{Uses: uses},
})
}
return edges, unresolvable
}

return edges, unresolvable
}
