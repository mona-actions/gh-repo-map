// Package filescan discovers and fetches target files from repos using
// the Git Trees API (go-github) and GraphQL batch file fetching (githubv4).
package filescan

import (
"context"
"fmt"
"log/slog"
"path/filepath"
"strings"

"github.com/mona-actions/gh-repo-map/internal/auth"
"github.com/mona-actions/gh-repo-map/internal/retry"
"github.com/shurcooL/githubv4"
)

// TreeResult holds the result of fetching a repo's file tree.
type TreeResult struct {
Entries   []TreeEntry
Truncated bool
}

// TreeEntry represents a file in the Git tree.
type TreeEntry struct {
Path string
Type string // "blob" or "tree"
SHA  string
}

// FetchTree retrieves the file tree for a repository's default branch.
func FetchTree(ctx context.Context, tp auth.TokenProvider, owner, repo, branch string) (*TreeResult, error) {
clients, err := tp.Clients(ctx, owner)
if err != nil {
return nil, err
}

tree, resp, err := clients.REST.Git.GetTree(ctx, owner, repo, branch, true)
if err != nil {
if resp != nil && resp.StatusCode == 409 {
return &TreeResult{}, nil
}
if resp != nil && retry.IsRetryableStatus(resp.StatusCode) {
retryErr := retry.Do(ctx, "tree:"+owner+"/"+repo, func() error {
var err2 error
tree, resp, err2 = clients.REST.Git.GetTree(ctx, owner, repo, branch, true)
if err2 != nil && resp != nil && resp.StatusCode == 409 {
return nil
}
return err2
}, func(e error) bool { return e != nil })
if retryErr != nil {
return nil, fmt.Errorf("fetching tree: %w", retryErr)
}
} else {
return nil, fmt.Errorf("fetching tree: %w", err)
}
}

var entries []TreeEntry
for _, e := range tree.Entries {
entries = append(entries, TreeEntry{
Path: e.GetPath(),
Type: e.GetType(),
SHA:  e.GetSHA(),
})
}

if tree.GetTruncated() {
slog.Warn("tree truncated", "repo", owner+"/"+repo)
}

return &TreeResult{
Entries:   entries,
Truncated: tree.GetTruncated(),
}, nil
}

// FilterTargetFiles filters tree entries to only target file patterns.
func FilterTargetFiles(entries []TreeEntry) []string {
var targets []string
for _, entry := range entries {
if entry.Type != "blob" {
continue
}
if isTargetFile(entry.Path) {
targets = append(targets, entry.Path)
}
}
return targets
}

func isTargetFile(path string) bool {
base := filepath.Base(path)
dir := filepath.Dir(path)

// Skip vendored/third-party dependency directories — these are consumed,
// not published by the repo. Their manifest files would falsely register
// the repo as the publisher of public packages.
if isVendoredPath(dir) {
return false
}

if strings.Contains(dir, ".github/workflows") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")) {
return true
}

switch base {
case ".gitmodules", "Makefile",
"package.json", "go.mod", "pom.xml",
"pyproject.toml", "requirements.txt",
"Cargo.toml", "Gemfile", "composer.json",
"docker-compose.yml", "docker-compose.yaml":
return true
}

if strings.HasPrefix(base, "Dockerfile") {
return true
}
if strings.HasSuffix(base, ".tf") {
return true
}
if strings.HasSuffix(base, ".sh") && isScriptPath(dir) {
return true
}
if strings.HasSuffix(base, ".csproj") || strings.HasSuffix(base, ".gemspec") {
return true
}

return false
}

// isVendoredPath returns true if the path is inside a vendored dependency directory.
// Files in these directories belong to third-party packages, not the repo itself.
func isVendoredPath(dir string) bool {
parts := strings.Split(dir, "/")
for _, p := range parts {
switch p {
case "node_modules", // npm/yarn/pnpm
"vendor",       // Go, PHP Composer, Ruby Bundler
"third_party",  // common convention
"third-party",  // common convention
"bower_components", // Bower (legacy)
"jspm_packages":    // jspm
return true
}
}
return false
}

func isScriptPath(dir string) bool {
parts := strings.Split(dir, "/")
for _, p := range parts {
switch p {
case "scripts", "build", "ci", "hack", "tools", ".":
return true
}
}
return dir == "."
}

// FileContent holds fetched file content from GraphQL.
type FileContent struct {
Path    string
Content string
}

// BatchFetchFiles fetches file contents via githubv4 GraphQL queries.
// It fetches files for multiple repos in a single org in batches.
func BatchFetchFiles(ctx context.Context, tp auth.TokenProvider, org string, repoFiles map[string][]string, batchSize int) (map[string][]FileContent, error) {
clients, err := tp.Clients(ctx, org)
if err != nil {
return nil, err
}

result := make(map[string][]FileContent)

// Process one repo at a time using githubv4 for file content
for fullName, files := range repoFiles {
parts := strings.SplitN(fullName, "/", 2)
if len(parts) != 2 {
continue
}
owner, repoName := parts[0], parts[1]

contents, err := fetchRepoFiles(ctx, clients.GraphQL, owner, repoName, files, batchSize)
if err != nil {
slog.Warn("failed to fetch files", "repo", fullName, "error", err)
continue
}
if len(contents) > 0 {
result[fullName] = contents
}
}

return result, nil
}

// fetchRepoFiles fetches file contents one at a time using githubv4.
func fetchRepoFiles(ctx context.Context, gql *githubv4.Client, owner, repo string, files []string, batchSize int) ([]FileContent, error) {
var results []FileContent

for i := 0; i < len(files); i += batchSize {
end := i + batchSize
if end > len(files) {
end = len(files)
}
batch := files[i:end]

for _, file := range batch {
var query struct {
Repository struct {
Object struct {
Blob struct {
Text *string
} `graphql:"... on Blob"`
} `graphql:"object(expression: $expr)"`
} `graphql:"repository(owner: $owner, name: $name)"`
}

variables := map[string]interface{}{
"owner": githubv4.String(owner),
"name":  githubv4.String(repo),
"expr":  githubv4.String("HEAD:" + file),
}

if err := gql.Query(ctx, &query, variables); err != nil {
slog.Debug("graphql file fetch failed", "repo", owner+"/"+repo, "file", file, "error", err)
continue
}

if query.Repository.Object.Blob.Text != nil {
results = append(results, FileContent{
Path:    file,
Content: *query.Repository.Object.Blob.Text,
})
}
}
}

return results, nil
}
