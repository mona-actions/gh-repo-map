// Package orchestrator coordinates the full scan pipeline:
// Phase 1 (enumerate) → Phase 2A (SBOM) → Phase 2B (file scan) → Phase 3 (cross-ref) → Phase 4 (output).
package orchestrator

import (
"context"
"fmt"
"log/slog"
"os"
"path/filepath"
"strings"
"sync"
"time"

"github.com/mona-actions/gh-repo-map/internal/auth"
"github.com/mona-actions/gh-repo-map/internal/checkpoint"
"github.com/mona-actions/gh-repo-map/internal/config"
"github.com/mona-actions/gh-repo-map/internal/enumerate"
"github.com/mona-actions/gh-repo-map/internal/filescan"
"github.com/mona-actions/gh-repo-map/internal/graph"
"github.com/mona-actions/gh-repo-map/internal/model"
"github.com/mona-actions/gh-repo-map/internal/output"
"github.com/mona-actions/gh-repo-map/internal/parse"
"github.com/mona-actions/gh-repo-map/internal/purl"
"github.com/mona-actions/gh-repo-map/internal/registry"
"github.com/mona-actions/gh-repo-map/internal/sbom"
)

// Run executes the full scan pipeline.
func Run(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, resumeMode bool, includeTransitive bool, cleanCp bool, splitThreshold int) error {
startTime := time.Now()

outputDir := filepath.Dir(cfg.Output.Path)
if err := os.MkdirAll(outputDir, 0o755); err != nil {
return fmt.Errorf("creating output directory: %w", err)
}

cpPath := filepath.Join(outputDir, "checkpoint.json")
cpMgr := checkpoint.NewManager(cpPath, cfg.Options.CheckpointFlushInterval)

if resumeMode {
if err := cpMgr.Load(); err != nil {
return fmt.Errorf("loading checkpoint: %w", err)
}
slog.Info("resumed from checkpoint", "repos", len(cpMgr.Data().Repos))
} else {
if err := runPhase1(ctx, cfg, tp, cpMgr); err != nil {
return fmt.Errorf("phase 1 (enumerate): %w", err)
}
}

cp := cpMgr.Data()
slog.Info("enumeration complete", "total_repos", len(cp.Repos), "orgs", len(cfg.Orgs))

if err := runPhase2(ctx, cfg, tp, cpMgr); err != nil {
return fmt.Errorf("phase 2 (scan): %w", err)
}

cp = cpMgr.Data()
scanned, total := countScanned(cp)
coverage := 0
if total > 0 {
coverage = (scanned * 100) / total
}
slog.Info("scan coverage", "scanned", scanned, "total", total, "coverage_pct", coverage)
if coverage < cfg.Options.MinCoverage {
return fmt.Errorf("coverage %d%% below minimum %d%% — use --resume to continue or --min-coverage 0 to force", coverage, cfg.Options.MinCoverage)
}

g, unresolved := runPhase3(cfg, cp)

if includeTransitive {
g.ComputeTransitive()
}

stats := buildStats(g)
splitInfo := model.SplitInfo{Mode: cfg.Options.OutputSplit, FileIndex: 1, TotalFiles: 1, ThisFileOrgs: cfg.Orgs}
schema := output.GenerateOutput(g.Nodes, unresolved, stats, cfg.Orgs, cfg.GitHubHost, startTime, splitInfo)

if cfg.Options.OutputSplit == "merged" || (cfg.Options.OutputSplit == "auto" && len(g.Nodes) < 500) {
if err := output.WriteJSON(schema, cfg.Output.Path); err != nil {
return fmt.Errorf("writing output: %w", err)
}
} else {
results, err := output.SplitOutput(g.Nodes, unresolved, stats, cfg.Orgs, cfg.GitHubHost, startTime, cfg.Output.Path, cfg.Options.OutputSplit, splitThreshold)
if err != nil {
return fmt.Errorf("splitting output: %w", err)
}
for _, r := range results {
if err := output.WriteJSON(r.Schema, r.Path); err != nil {
return fmt.Errorf("writing split file %s: %w", r.Path, err)
}
}
}

output.PrintSummary(schema)

if cleanCp {
os.Remove(cpPath)
slog.Info("checkpoint cleaned")
}

return nil
}

// DryRun executes Phase 1 only and prints estimates.
func DryRun(ctx context.Context, cfg *config.Config, tp auth.TokenProvider) error {
cpMgr := checkpoint.NewManager("", 0)
if err := runPhase1(ctx, cfg, tp, cpMgr); err != nil {
return err
}

cp := cpMgr.Data()
total := len(cp.Repos)
estimatedCalls := total * 3
rateLimit := 5000
if cfg.Auth.Type == "pat" {
rateLimit = 1000
}
estimatedMinutes := (estimatedCalls * 60) / rateLimit

fmt.Fprintf(os.Stderr, "\n=== Dry Run Summary ===\n")
fmt.Fprintf(os.Stderr, "Orgs: %d\n", len(cfg.Orgs))
fmt.Fprintf(os.Stderr, "Total repos: %d\n", total)
archived := 0
for _, r := range cp.Repos {
if r.Archived {
archived++
}
}
fmt.Fprintf(os.Stderr, "  Scannable: %d\n", total-archived)
fmt.Fprintf(os.Stderr, "  Archived (target only): %d\n", archived)
fmt.Fprintf(os.Stderr, "Estimated API calls: ~%d\n", estimatedCalls)
fmt.Fprintf(os.Stderr, "Estimated time: ~%d minutes (at %d req/hr)\n", estimatedMinutes, rateLimit)
return nil
}

func runPhase1(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager) error {
slog.Info("phase 1: enumerating repositories")
var allRepos []model.EnumeratedRepo
for _, org := range cfg.Orgs {
slog.Info("enumerating org", "org", org)
repos, err := enumerate.EnumerateOrg(ctx, tp, org, cfg.Options.IncludeForks)
if err != nil {
return fmt.Errorf("enumerating org %q: %w", org, err)
}
slog.Info("org enumerated", "org", org, "repos", len(repos))
allRepos = append(allRepos, repos...)
}
cpRepos := enumerate.BuildCheckpointRepos(allRepos)
return cpMgr.Initialize(cfg.GitHubHost, cfg.Orgs, cpRepos)
}

func runPhase2(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager) error {
slog.Info("phase 2: scanning repositories")
cp := cpMgr.Data()
maxWorkers := cfg.Options.Concurrency
if maxWorkers <= 0 {
maxWorkers = 4
}
if maxWorkers > 10 {
maxWorkers = 10
}

orgRepos := make(map[string][]string)
for fullName, repo := range cp.Repos {
if repo.Archived {
continue
}
parts := strings.SplitN(fullName, "/", 2)
if len(parts) != 2 {
continue
}
orgRepos[parts[0]] = append(orgRepos[parts[0]], fullName)
}

sem := make(chan struct{}, maxWorkers)
var wg sync.WaitGroup
var mu sync.Mutex
var firstErr error

for org, repos := range orgRepos {
wg.Add(1)
sem <- struct{}{}
go func(org string, repos []string) {
defer wg.Done()
defer func() { <-sem }()
if err := processOrgRepos(ctx, cfg, tp, cpMgr, org, repos); err != nil {
mu.Lock()
if firstErr == nil {
firstErr = fmt.Errorf("org %s: %w", org, err)
}
mu.Unlock()
slog.Error("org scan failed", "org", org, "error", err)
}
}(org, repos)
}

wg.Wait()
cpMgr.SetPhase("3")
return cpMgr.Flush()
}

func processOrgRepos(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager, org string, repos []string) error {
processed := 0
for _, fullName := range repos {
parts := strings.SplitN(fullName, "/", 2)
if len(parts) != 2 {
continue
}
owner, repoName := parts[0], parts[1]

if err := cpMgr.UpdateRepo(fullName, func(cpRepo *model.CheckpointRepo) {
if cpRepo.Status.SBOM == model.ScanStatusPending {
consumed, available, err := sbom.FetchSBOM(ctx, tp, owner, repoName)
if err != nil {
slog.Warn("SBOM failed", "repo", fullName, "error", err)
cpRepo.Status.SBOM = model.ScanStatusFailed
} else {
cpRepo.Status.SBOM = model.ScanStatusDone
cpRepo.SBOMData = &model.SBOMData{
SBOMAvailable:    available,
ConsumedPackages: consumed,
}
}
}

if cpRepo.Status.FileScan == model.ScanStatusPending {
if err := scanRepoFiles(ctx, cfg, tp, fullName, owner, repoName, cpRepo); err != nil {
slog.Warn("file scan failed", "repo", fullName, "error", err)
cpRepo.Status.FileScan = model.ScanStatusFailed
}
}
}); err != nil {
slog.Error("update repo failed", "repo", fullName, "error", err)
}

processed++
if processed%cfg.Options.CheckpointFlushInterval == 0 {
if err := cpMgr.Flush(); err != nil {
slog.Error("checkpoint flush failed", "error", err)
}
}
slog.Info("repo processed", "repo", fullName, "progress", fmt.Sprintf("%d/%d", processed, len(repos)))
}
return cpMgr.Flush()
}

func scanRepoFiles(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, fullName, owner, repoName string, cpRepo *model.CheckpointRepo) error {
branch := cpRepo.DefaultBranch
if branch == "" {
branch = "main"
}

tree, err := filescan.FetchTree(ctx, tp, owner, repoName, branch)
if err != nil {
return err
}

targetFiles := filescan.FilterTargetFiles(tree.Entries)
if len(targetFiles) == 0 {
cpRepo.Status.FileScan = model.ScanStatusDone
cpRepo.FileScanData = &model.FileScanData{FilesScanned: 0, TreeTruncated: tree.Truncated}
return nil
}

repoFiles := map[string][]string{fullName: targetFiles}
contents, err := filescan.BatchFetchFiles(ctx, tp, owner, repoFiles, cfg.Options.GraphQLBatchSize)
if err != nil {
return fmt.Errorf("batch fetch: %w", err)
}

knownOrgs := make(map[string]bool)
for _, org := range cfg.Orgs {
knownOrgs[org] = true
}

host := "github.com"
if cfg.GitHubHost != "" {
host = cfg.GitHubHost
}

var edges []model.FileScanEdge
var published []model.PublishedIdentity

for _, fc := range contents[fullName] {
fe, fp := parseFile(fc.Path, fc.Content, knownOrgs, cfg.ContainerRegistries, host)
edges = append(edges, fe...)
published = append(published, fp...)
}

cpRepo.Status.FileScan = model.ScanStatusDone
cpRepo.FileScanData = &model.FileScanData{
FilesScanned:  len(contents[fullName]),
TreeTruncated: tree.Truncated,
Edges:         edges,
}

if cpRepo.SBOMData == nil {
cpRepo.SBOMData = &model.SBOMData{}
}
cpRepo.SBOMData.PublishedIdentity = append(cpRepo.SBOMData.PublishedIdentity, published...)
return nil
}

func parseFile(path, content string, knownOrgs map[string]bool, registries []string, host string) ([]model.FileScanEdge, []model.PublishedIdentity) {
var edges []model.FileScanEdge
var published []model.PublishedIdentity

base := filepath.Base(path)
dir := filepath.Dir(path)

switch {
case strings.Contains(dir, ".github/workflows") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")):
e, _ := parse.ParseActionsWorkflow(content, path, knownOrgs)
edges = append(edges, e...)

case base == ".gitmodules":
edges = append(edges, parse.ParseGitmodules(content, path, host, knownOrgs)...)

case strings.HasPrefix(base, "Dockerfile"):
edges = append(edges, parse.ParseDockerfile(content, path, host, registries, knownOrgs)...)

case base == "docker-compose.yml" || base == "docker-compose.yaml":
edges = append(edges, parse.ParseDockerCompose(content, path, host, registries, knownOrgs)...)

case strings.HasSuffix(base, ".tf"):
edges = append(edges, parse.ParseTerraform(content, path, host, knownOrgs)...)

case strings.HasSuffix(base, ".sh") || base == "Makefile":
edges = append(edges, parse.ParseScript(content, path, host, knownOrgs)...)

case base == "package.json":
c, p := parse.ParsePackageJSON(content)
published = append(published, p...)
_ = c // consumed stored elsewhere

case base == "go.mod":
c, p := parse.ParseGoMod(content)
published = append(published, p...)
_ = c

case base == "pom.xml":
c, p := parse.ParsePomXML(content)
published = append(published, p...)
_ = c

case base == "pyproject.toml":
c, p := parse.ParsePyprojectTOML(content)
published = append(published, p...)
_ = c

case base == "requirements.txt":
_ = parse.ParseRequirementsTxt(content)

case strings.HasSuffix(base, ".csproj"):
c, p := parse.ParseCsproj(content)
published = append(published, p...)
_ = c

case base == "Cargo.toml":
c, p := parse.ParseCargoToml(content)
published = append(published, p...)
_ = c

case base == "Gemfile":
_ = parse.ParseGemfile(content)

case strings.HasSuffix(base, ".gemspec"):
published = append(published, parse.ParseGemspec(content)...)

case base == "composer.json":
c, p := parse.ParseComposerJSON(content)
published = append(published, p...)
_ = c
}

return edges, published
}

func runPhase3(cfg *config.Config, cp *model.Checkpoint) (*graph.Graph, map[string][]model.UnresolvedPackage) {
slog.Info("phase 3: cross-referencing dependencies")

reg := registry.New()
if cfg.OverridesPath != "" {
applied, err := reg.ApplyOverrides(cfg.OverridesPath)
if err != nil {
slog.Warn("failed to load overrides", "path", cfg.OverridesPath, "error", err)
} else {
slog.Info("overrides applied", "count", applied)
}
}

for fullName, repo := range cp.Repos {
if repo.SBOMData == nil {
continue
}
for _, pub := range repo.SBOMData.PublishedIdentity {
reg.Register(fullName, pub)
}
}

g := graph.New()
for fullName, repo := range cp.Repos {
g.AddNode(fullName, &model.RepoNode{
ScanStatus: model.RepoScanStatus{
SBOM:     repo.Status.SBOM,
FileScan: repo.Status.FileScan,
},
Annotations: model.RepoAnnotations{
ForkOf:       repo.ForkOf,
TemplateFrom: repo.TemplateFrom,
Archived:     repo.Archived,
},
})
}

unresolved := make(map[string][]model.UnresolvedPackage)

for fullName, repo := range cp.Repos {
if repo.SBOMData != nil {
for _, pkg := range repo.SBOMData.ConsumedPackages {
name := purl.NormalizeManifestName(pkg.Ecosystem, pkg.Name)
target, found := reg.Lookup(pkg.Ecosystem, name)
if found && target != fullName {
edge := model.NewPackageEdge(target, "", pkg.Name, pkg.Ecosystem, pkg.Version, false)
if tr, ok := cp.Repos[target]; ok {
edge.TargetScanned = tr.Status.SBOM == model.ScanStatusDone || tr.Status.FileScan == model.ScanStatusDone
}
g.AddEdge(fullName, edge)
} else if !found {
unresolved[fullName] = append(unresolved[fullName], model.UnresolvedPackage{
PackageName: pkg.Name,
Ecosystem:   pkg.Ecosystem,
Version:     pkg.Version,
Reason:      "no_matching_repo",
})
}
}
}

if repo.FileScanData != nil {
for _, fse := range repo.FileScanData.Edges {
edge := model.DependencyEdge{
Repo:       fse.Target,
Type:       fse.Type,
Confidence: model.ConfidenceHigh,
SourceFile: fse.SourceFile,
}
if tr, ok := cp.Repos[fse.Target]; ok {
edge.TargetScanned = tr.Status.SBOM == model.ScanStatusDone || tr.Status.FileScan == model.ScanStatusDone
}
g.AddEdge(fullName, edge)
}
}
}

return g, unresolved
}

func buildStats(g *graph.Graph) model.OutputStats {
return model.OutputStats{
MostDependedOn:       g.MostDependedOn(10),
DependencyTypeCounts: g.TypeCounts(),
Clusters:             g.FindClusters(),
CircularDeps:         g.DetectCycles(),
OrphanRepos:          g.FindOrphans(),
}
}

func countScanned(cp *model.Checkpoint) (int, int) {
scanned, total := 0, 0
for _, repo := range cp.Repos {
if repo.Archived {
continue
}
total++
if repo.Status.SBOM == model.ScanStatusDone || repo.Status.FileScan == model.ScanStatusDone {
scanned++
}
}
return scanned, total
}
