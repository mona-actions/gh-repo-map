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
"sync/atomic"
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
"github.com/mona-actions/gh-repo-map/internal/progress"
"github.com/mona-actions/gh-repo-map/internal/purl"
"github.com/mona-actions/gh-repo-map/internal/registry"
"github.com/mona-actions/gh-repo-map/internal/sbom"
)

// Run executes the full scan pipeline.
func Run(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, tracker progress.Tracker, resumeMode bool, includeTransitive bool, cleanCp bool, splitThreshold int) error {
startTime := time.Now()
defer tracker.Stop()

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

// Check if any configured orgs are missing from the checkpoint
missingOrgs := findMissingOrgs(cfg.Orgs, cpMgr.Data().Orgs, cpMgr.Data().Repos)
if len(missingOrgs) > 0 {
slog.Info("enumerating missing orgs", "orgs", missingOrgs)
missingCfg := *cfg
missingCfg.Orgs = missingOrgs
enumStats, err := runPhase1Partial(ctx, &missingCfg, tp, cpMgr, tracker)
if err != nil {
return fmt.Errorf("enumerating missing orgs: %w", err)
}
slog.Info("missing orgs enumerated", "added", enumStats.Enumerated)
}
} else {
enumStats, err := runPhase1(ctx, cfg, tp, cpMgr, tracker)
if err != nil {
return fmt.Errorf("phase 1 (enumerate): %w", err)
}
tracker.EndEnumerate(enumStats)
}

cp := cpMgr.Data()

// Bail early if no repos to scan
if len(cp.Repos) == 0 {
return fmt.Errorf("no repositories found — check org names and token permissions")
}

if err := runPhase2(ctx, cfg, tp, cpMgr, tracker); err != nil {
return fmt.Errorf("phase 2 (scan): %w", err)
}

cp = cpMgr.Data()
scanned, total := countScanned(cp)
coverage := 0
if total > 0 {
coverage = (scanned * 100) / total
}
tracker.EndScan(scanned, total)

if coverage < cfg.Options.MinCoverage {
return fmt.Errorf("coverage %d%% below minimum %d%% — use --resume to continue or --min-coverage 0 to force", coverage, cfg.Options.MinCoverage)
}

tracker.StartCrossRef()
results, err := cpMgr.LoadResults()
if err != nil {
return fmt.Errorf("loading scan results: %w", err)
}
cpMgr.Close()
g, unresolved := runPhase3(cfg, cp, results)

if includeTransitive {
g.ComputeTransitive()
}

totalEdges := g.EdgeCount()
totalUnresolved := 0
for _, u := range unresolved {
totalUnresolved += len(u)
}
tracker.EndCrossRef(totalEdges, totalUnresolved)

tracker.StartOutput()
stats := buildStats(g)
splitInfo := model.SplitInfo{Mode: cfg.Options.OutputSplit, FileIndex: 1, TotalFiles: 1, ThisFileOrgs: cfg.Orgs}
schema := output.GenerateOutput(g.Nodes, unresolved, stats, cfg.Orgs, cfg.GitHubHost, startTime, splitInfo)

outputPath := cfg.Output.Path
if cfg.Options.OutputSplit == "merged" || (cfg.Options.OutputSplit == "auto" && len(g.Nodes) < 500) {
if err := output.WriteJSON(schema, outputPath); err != nil {
return fmt.Errorf("writing output: %w", err)
}
} else {
results, err := output.SplitOutput(g.Nodes, unresolved, stats, cfg.Orgs, cfg.GitHubHost, startTime, outputPath, cfg.Options.OutputSplit, splitThreshold)
if err != nil {
return fmt.Errorf("splitting output: %w", err)
}
for _, r := range results {
if err := output.WriteJSON(r.Schema, r.Path); err != nil {
return fmt.Errorf("writing split file %s: %w", r.Path, err)
}
}
if len(results) > 0 {
outputPath = filepath.Dir(results[0].Path) + "/ (" + fmt.Sprintf("%d files", len(results)) + ")"
}
}

tracker.EndOutput(outputPath, len(g.Nodes), totalEdges)

if cleanCp {
os.Remove(cpPath)
slog.Debug("checkpoint cleaned")
}

return nil
}

// DryRun executes Phase 1 only and prints estimates.
func DryRun(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, tracker progress.Tracker) error {
defer tracker.Stop()

cpMgr := checkpoint.NewManager("", 0)
enumStats, err := runPhase1(ctx, cfg, tp, cpMgr, tracker)
if err != nil {
return err
}
tracker.EndEnumerate(enumStats)

estimatedCalls := enumStats.Scannable * 3
rateLimit := 5000
estimatedMinutes := (estimatedCalls * 60) / rateLimit

fmt.Fprintf(os.Stderr, "\n=== Dry Run Summary ===\n")
fmt.Fprintf(os.Stderr, "Orgs: %d\n", len(cfg.Orgs))
fmt.Fprintf(os.Stderr, "Total repos (API): %d\n", enumStats.TotalFromAPI)
fmt.Fprintf(os.Stderr, "  Forks skipped: %d\n", enumStats.ForksSkipped)
fmt.Fprintf(os.Stderr, "  Empty skipped: %d\n", enumStats.EmptySkipped)
fmt.Fprintf(os.Stderr, "  Archived (target only): %d\n", enumStats.Archived)
fmt.Fprintf(os.Stderr, "  Scannable: %d\n", enumStats.Scannable)
fmt.Fprintf(os.Stderr, "Estimated API calls: ~%d\n", estimatedCalls)
fmt.Fprintf(os.Stderr, "Estimated time: ~%d minutes (at %d req/hr)\n", estimatedMinutes, rateLimit)
return nil
}

func runPhase1(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager, tracker progress.Tracker) (progress.EnumerateStats, error) {
tracker.StartEnumerate()
var allRepos []model.EnumeratedRepo
var stats progress.EnumerateStats
for _, org := range cfg.Orgs {
onProgress := func(orgName string, reposSoFar int) {
tracker.EnumerateProgress(orgName, reposSoFar)
}
result, err := enumerate.EnumerateOrg(ctx, tp, org, cfg.Options.IncludeForks, onProgress)
if err != nil {
return stats, fmt.Errorf("enumerating org %q: %w", org, err)
}
stats.TotalFromAPI += result.TotalFromAPI
stats.ForksSkipped += result.ForksSkipped
stats.EmptySkipped += result.EmptySkipped
if len(result.Repos) == 0 {
stats.EmptyOrgs = append(stats.EmptyOrgs, org)
}
tracker.OrgEnumerated(org, len(result.Repos))
allRepos = append(allRepos, result.Repos...)
}
stats.Enumerated = len(allRepos)
for _, r := range allRepos {
if r.Archived {
stats.Archived++
}
}
stats.Scannable = stats.Enumerated - stats.Archived
cpRepos := enumerate.BuildCheckpointRepos(allRepos)
return stats, cpMgr.Initialize(cfg.GitHubHost, cfg.Orgs, cpRepos)
}

func runPhase2(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager, tracker progress.Tracker) error {
cp := cpMgr.Data()

// Count scannable (non-archived) repos
var scannable int
for _, repo := range cp.Repos {
if !repo.Archived {
scannable++
}
}
tracker.StartScan(scannable)

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
var globalProcessed atomic.Int32

// Seed counter with already-scanned repos so resume progress is accurate
for _, repo := range cp.Repos {
if repo.Archived {
continue
}
sbomDone := repo.Status.SBOM == model.ScanStatusDone || repo.Status.SBOM == model.ScanStatusSkipped
fsDone := repo.Status.FileScan == model.ScanStatusDone || repo.Status.FileScan == model.ScanStatusSkipped
if sbomDone && fsDone {
globalProcessed.Add(1)
}
}
if seeded := int(globalProcessed.Load()); seeded > 0 {
tracker.SeedScanProgress(seeded)
}

for org, repos := range orgRepos {
wg.Add(1)
sem <- struct{}{}
go func(org string, repos []string) {
defer wg.Done()
defer func() { <-sem }()
if err := processOrgRepos(ctx, cfg, tp, cpMgr, tracker, org, repos, &globalProcessed, scannable); err != nil {
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

func processOrgRepos(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager, tracker progress.Tracker, org string, repos []string, globalProcessed *atomic.Int32, totalScannable int) error {
for _, fullName := range repos {
parts := strings.SplitN(fullName, "/", 2)
if len(parts) != 2 {
continue
}
owner, repoName := parts[0], parts[1]

// Read status with brief lock — no network I/O
needSBOM, needFileScan, defaultBranch := cpMgr.RepoScanNeeded(fullName)

// Skip repos already fully scanned (resume case)
if !needSBOM && !needFileScan {
continue
}

// Perform all network I/O outside the checkpoint lock
var sbomData *model.SBOMData
sbomStatus := model.ScanStatusSkipped
if needSBOM {
consumed, available, err := sbom.FetchSBOM(ctx, tp, owner, repoName)
if err != nil {
tracker.ScanWarning(fullName, fmt.Sprintf("SBOM: %v", err))
sbomStatus = model.ScanStatusFailed
} else {
sbomStatus = model.ScanStatusDone
sbomData = &model.SBOMData{
SBOMAvailable:    available,
ConsumedPackages: consumed,
}
}
}

var fileScanData *model.FileScanData
var publishedFromFiles []model.PublishedIdentity
fileScanStatus := model.ScanStatusSkipped
if needFileScan {
fsResult, err := scanRepoFiles(ctx, cfg, tp, fullName, owner, repoName, defaultBranch)
if err != nil {
tracker.ScanWarning(fullName, fmt.Sprintf("file scan: %v", err))
fileScanStatus = model.ScanStatusFailed
} else {
fileScanStatus = model.ScanStatusDone
fileScanData = fsResult.FileScanData
publishedFromFiles = fsResult.Published
}
}

// Update checkpoint status with brief lock — no scan data stored here
if err := cpMgr.UpdateRepo(fullName, func(cpRepo *model.CheckpointRepo) {
if needSBOM {
cpRepo.Status.SBOM = sbomStatus
}
if needFileScan {
cpRepo.Status.FileScan = fileScanStatus
}
}); err != nil {
slog.Error("update repo failed", "repo", fullName, "error", err)
}

// Append scan results to NDJSON — separate from checkpoint
if sbomData != nil || fileScanData != nil {
result := checkpoint.RepoResult{
FullName:     fullName,
SBOMData:     sbomData,
FileScanData: fileScanData,
}
if len(publishedFromFiles) > 0 {
if result.SBOMData == nil {
result.SBOMData = &model.SBOMData{}
}
result.SBOMData.PublishedIdentity = append(result.SBOMData.PublishedIdentity, publishedFromFiles...)
}
if err := cpMgr.AppendResult(result); err != nil {
slog.Error("append result failed", "repo", fullName, "error", err)
}
}

current := int(globalProcessed.Add(1))
tracker.RepoScanned(fullName, current, totalScannable)
}
return cpMgr.Flush()
}

// fileScanResult holds the results of scanning a repo's files.
type fileScanResult struct {
FileScanData *model.FileScanData
Published    []model.PublishedIdentity
Consumed     []model.ConsumedPackage
}

func scanRepoFiles(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, fullName, owner, repoName, defaultBranch string) (*fileScanResult, error) {
branch := defaultBranch
if branch == "" {
branch = "main"
}

tree, err := filescan.FetchTree(ctx, tp, owner, repoName, branch)
if err != nil {
return nil, err
}

targetFiles := filescan.FilterTargetFiles(tree.Entries)
if len(targetFiles) == 0 {
return &fileScanResult{
FileScanData: &model.FileScanData{FilesScanned: 0, TreeTruncated: tree.Truncated},
}, nil
}

repoFiles := map[string][]string{fullName: targetFiles}
contents, err := filescan.BatchFetchFiles(ctx, tp, owner, repoFiles, cfg.Options.GraphQLBatchSize)
if err != nil {
return nil, fmt.Errorf("batch fetch: %w", err)
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
var consumed []model.ConsumedPackage

for _, fc := range contents[fullName] {
fe, fp, fc2 := parseFile(fc.Path, fc.Content, knownOrgs, cfg.ContainerRegistries, host)
edges = append(edges, fe...)
published = append(published, fp...)
consumed = append(consumed, fc2...)
}

return &fileScanResult{
FileScanData: &model.FileScanData{
FilesScanned:     len(contents[fullName]),
TreeTruncated:    tree.Truncated,
Edges:            edges,
ManifestConsumed: consumed,
},
Published: published,
Consumed:  consumed,
}, nil
}

func parseFile(path, content string, knownOrgs map[string]bool, registries []string, host string) ([]model.FileScanEdge, []model.PublishedIdentity, []model.ConsumedPackage) {
var edges []model.FileScanEdge
var published []model.PublishedIdentity
var consumed []model.ConsumedPackage

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
consumed = append(consumed, c...)

case base == "go.mod":
c, p := parse.ParseGoMod(content)
published = append(published, p...)
consumed = append(consumed, c...)

case base == "pom.xml":
c, p := parse.ParsePomXML(content)
published = append(published, p...)
consumed = append(consumed, c...)

case base == "pyproject.toml":
c, p := parse.ParsePyprojectTOML(content)
published = append(published, p...)
consumed = append(consumed, c...)

case base == "requirements.txt":
consumed = append(consumed, parse.ParseRequirementsTxt(content)...)

case strings.HasSuffix(base, ".csproj"):
c, p := parse.ParseCsproj(content)
published = append(published, p...)
consumed = append(consumed, c...)

case base == "Cargo.toml":
c, p := parse.ParseCargoToml(content)
published = append(published, p...)
consumed = append(consumed, c...)

case base == "Gemfile":
consumed = append(consumed, parse.ParseGemfile(content)...)

case strings.HasSuffix(base, ".gemspec"):
published = append(published, parse.ParseGemspec(content)...)

case base == "composer.json":
c, p := parse.ParseComposerJSON(content)
published = append(published, p...)
consumed = append(consumed, c...)
}

return edges, published, consumed
}

func runPhase3(cfg *config.Config, cp *model.Checkpoint, results map[string]*checkpoint.RepoResult) (*graph.Graph, map[string][]model.UnresolvedPackage) {
reg := registry.New()
if cfg.OverridesPath != "" {
applied, err := reg.ApplyOverrides(cfg.OverridesPath)
if err != nil {
slog.Warn("failed to load overrides", "path", cfg.OverridesPath, "error", err)
} else {
slog.Debug("overrides applied", "count", applied)
}
}

// Register published identities from scan results
for fullName, result := range results {
if result.SBOMData == nil {
continue
}
for _, pub := range result.SBOMData.PublishedIdentity {
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

for fullName, result := range results {
// Determine consumed packages: prefer SBOM, fall back to manifest parsing
var consumed []model.ConsumedPackage
if result.SBOMData != nil && len(result.SBOMData.ConsumedPackages) > 0 {
consumed = result.SBOMData.ConsumedPackages
} else if result.FileScanData != nil && len(result.FileScanData.ManifestConsumed) > 0 {
consumed = result.FileScanData.ManifestConsumed
}

for _, pkg := range consumed {
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

if result.FileScanData != nil {
for _, fse := range result.FileScanData.Edges {
edge := model.DependencyEdge{
Repo:       fse.Target,
Type:       fse.Type,
Confidence: model.ConfidenceHigh,
SourceFile: fse.SourceFile,
Detail:     fse.Detail,
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

// findMissingOrgs returns orgs that are in the config but have no repos in the checkpoint.
func findMissingOrgs(configOrgs, checkpointOrgs []string, repos map[string]*model.CheckpointRepo) []string {
if len(configOrgs) == 0 {
return nil
}

// Build set of orgs that actually have repos in the checkpoint
orgsWithRepos := make(map[string]bool)
for name := range repos {
parts := strings.SplitN(name, "/", 2)
if len(parts) == 2 {
orgsWithRepos[parts[0]] = true
}
}

var missing []string
for _, org := range configOrgs {
if !orgsWithRepos[org] {
missing = append(missing, org)
}
}
return missing
}

// runPhase1Partial enumerates only the specified orgs and merges into the existing checkpoint.
func runPhase1Partial(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager, tracker progress.Tracker) (progress.EnumerateStats, error) {
tracker.StartEnumerate()
var allRepos []model.EnumeratedRepo
var stats progress.EnumerateStats
for _, org := range cfg.Orgs {
onProgress := func(orgName string, reposSoFar int) {
tracker.EnumerateProgress(orgName, reposSoFar)
}
result, err := enumerate.EnumerateOrg(ctx, tp, org, cfg.Options.IncludeForks, onProgress)
if err != nil {
return stats, fmt.Errorf("enumerating org %q: %w", org, err)
}
stats.TotalFromAPI += result.TotalFromAPI
stats.ForksSkipped += result.ForksSkipped
stats.EmptySkipped += result.EmptySkipped
if len(result.Repos) == 0 {
stats.EmptyOrgs = append(stats.EmptyOrgs, org)
}
tracker.OrgEnumerated(org, len(result.Repos))
allRepos = append(allRepos, result.Repos...)
}
stats.Enumerated = len(allRepos)
for _, r := range allRepos {
if r.Archived {
stats.Archived++
}
}
stats.Scannable = stats.Enumerated - stats.Archived
cpRepos := enumerate.BuildCheckpointRepos(allRepos)
added, err := cpMgr.MergeRepos(cfg.Orgs, cpRepos)
if err != nil {
return stats, err
}
tracker.EndEnumerate(stats)
slog.Info("merged missing org repos", "orgs", cfg.Orgs, "repos_added", added)
return stats, nil
}
