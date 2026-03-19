// Package progress provides real-time CLI progress feedback using pterm.
// It defines a Tracker interface so the orchestrator stays decoupled from
// the presentation layer, and offers two implementations:
//   - PtermTracker: interactive spinners + progress bars for TTY sessions
//   - LogTracker: plain slog-based output for non-interactive / verbose / debug modes
package progress

import (
"fmt"
"log/slog"
"sync"
"time"

"github.com/pterm/pterm"
)

// EnumerateStats summarizes the outcome of Phase 1 enumeration.
type EnumerateStats struct {
TotalFromAPI int // raw count from the GitHub API
Enumerated   int // repos that passed fork/empty filters
ForksSkipped int
EmptySkipped int
Archived     int // enumerated but won't be scanned
Scannable    int // enumerated - archived
}

// Tracker provides phase-level progress feedback to the user.
type Tracker interface {
// Phase 1: Enumerate
StartEnumerate()
EnumerateProgress(org string, reposSoFar int)
OrgEnumerated(org string, repoCount int)
EndEnumerate(stats EnumerateStats)

// Phase 2: Scan
StartScan(totalRepos int)
SeedScanProgress(done int) // advance bar for already-completed repos on resume
RepoScanned(repo string, current, total int)
ScanWarning(repo, msg string)
EndScan(scanned, total int)

// Phase 3: Cross-reference
StartCrossRef()
EndCrossRef(edgeCount, unresolvedCount int)

// Phase 4: Output
StartOutput()
EndOutput(path string, repoCount, edgeCount int)

// Cleanup — must be called to restore terminal state.
Stop()
}

// ---------- PtermTracker ----------

// PtermTracker renders interactive spinners and a progress bar.
type PtermTracker struct {
mu        sync.Mutex
spinner   *pterm.SpinnerPrinter
bar       *pterm.ProgressbarPrinter
startTime time.Time
warnings  int
}

// NewPtermTracker creates an interactive progress tracker.
func NewPtermTracker() *PtermTracker {
return &PtermTracker{startTime: time.Now()}
}

func (t *PtermTracker) StartEnumerate() {
t.mu.Lock()
defer t.mu.Unlock()
t.spinner, _ = pterm.DefaultSpinner.
WithRemoveWhenDone(true).
Start("Enumerating repositories...")
}

func (t *PtermTracker) EnumerateProgress(org string, reposSoFar int) {
t.mu.Lock()
defer t.mu.Unlock()
if t.spinner != nil {
t.spinner.UpdateText(fmt.Sprintf("Enumerating %s... (%d repos found)", org, reposSoFar))
}
}

func (t *PtermTracker) OrgEnumerated(org string, repoCount int) {
t.mu.Lock()
defer t.mu.Unlock()
if t.spinner != nil {
t.spinner.UpdateText(fmt.Sprintf("Enumerating repositories... %s (%d repos)", org, repoCount))
}
}

func (t *PtermTracker) EndEnumerate(stats EnumerateStats) {
t.mu.Lock()
defer t.mu.Unlock()
if t.spinner != nil {
t.spinner.Success(fmt.Sprintf(
"Enumeration complete: %d total | %d scannable (%d forks, %d archived, %d empty skipped)",
stats.TotalFromAPI, stats.Scannable, stats.ForksSkipped, stats.Archived, stats.EmptySkipped,
))
t.spinner = nil
}
}

func (t *PtermTracker) StartScan(totalRepos int) {
t.mu.Lock()
defer t.mu.Unlock()
t.bar, _ = pterm.DefaultProgressbar.
WithTotal(totalRepos).
WithTitle("Scanning repositories").
WithShowPercentage(true).
WithShowCount(true).
WithShowElapsedTime(true).
WithRemoveWhenDone(true).
Start()
}

func (t *PtermTracker) RepoScanned(repo string, current, total int) {
t.mu.Lock()
defer t.mu.Unlock()
if t.bar != nil {
t.bar.UpdateTitle(fmt.Sprintf("Scanning: %s", repo))
t.bar.Increment()
}
}

func (t *PtermTracker) SeedScanProgress(done int) {
t.mu.Lock()
defer t.mu.Unlock()
if t.bar != nil && done > 0 {
t.bar.Add(done)
}
}

func (t *PtermTracker) ScanWarning(repo, msg string) {
t.mu.Lock()
defer t.mu.Unlock()
t.warnings++
slog.Debug("scan warning", "repo", repo, "msg", msg)
}

func (t *PtermTracker) EndScan(scanned, total int) {
t.mu.Lock()
defer t.mu.Unlock()
if t.bar != nil {
t.bar.Stop()
t.bar = nil
}
pct := 0
if total > 0 {
pct = (scanned * 100) / total
}
msg := fmt.Sprintf("Scan complete: %d/%d repos (%d%% coverage)", scanned, total, pct)
if t.warnings > 0 {
msg += fmt.Sprintf(", %d warnings", t.warnings)
}
pterm.Success.Println(msg)
}

func (t *PtermTracker) StartCrossRef() {
t.mu.Lock()
defer t.mu.Unlock()
t.spinner, _ = pterm.DefaultSpinner.
WithRemoveWhenDone(true).
Start("Cross-referencing dependencies...")
}

func (t *PtermTracker) EndCrossRef(edgeCount, unresolvedCount int) {
t.mu.Lock()
defer t.mu.Unlock()
if t.spinner != nil {
msg := fmt.Sprintf("Cross-reference complete: %d dependency edges found", edgeCount)
if unresolvedCount > 0 {
msg += fmt.Sprintf(", %d unresolved packages", unresolvedCount)
}
t.spinner.Success(msg)
t.spinner = nil
}
}

func (t *PtermTracker) StartOutput() {
t.mu.Lock()
defer t.mu.Unlock()
t.spinner, _ = pterm.DefaultSpinner.
WithRemoveWhenDone(true).
Start("Writing output...")
}

func (t *PtermTracker) EndOutput(path string, repoCount, edgeCount int) {
t.mu.Lock()
defer t.mu.Unlock()
if t.spinner != nil {
t.spinner.Success(fmt.Sprintf("Output written to %s (%d repos, %d edges)", path, repoCount, edgeCount))
t.spinner = nil
}
elapsed := time.Since(t.startTime).Round(time.Second)
pterm.Info.Println(fmt.Sprintf("Total time: %s", elapsed))
}

func (t *PtermTracker) Stop() {
t.mu.Lock()
defer t.mu.Unlock()
if t.spinner != nil {
t.spinner.Stop()
t.spinner = nil
}
if t.bar != nil {
t.bar.Stop()
t.bar = nil
}
}

// ---------- LogTracker ----------

// LogTracker emits plain slog lines — suitable for non-interactive terminals,
// verbose/debug log levels, or when output is piped.
type LogTracker struct{}

// NewLogTracker creates a log-based progress tracker.
func NewLogTracker() *LogTracker { return &LogTracker{} }

func (t *LogTracker) StartEnumerate() {
slog.Info("phase 1: enumerating repositories")
}

func (t *LogTracker) EnumerateProgress(org string, reposSoFar int) {
slog.Info("enumerating", "org", org, "repos_so_far", reposSoFar)
}

func (t *LogTracker) OrgEnumerated(org string, repoCount int) {
slog.Info("org enumerated", "org", org, "repos", repoCount)
}

func (t *LogTracker) EndEnumerate(stats EnumerateStats) {
slog.Info("enumeration complete",
"total_from_api", stats.TotalFromAPI,
"enumerated", stats.Enumerated,
"scannable", stats.Scannable,
"forks_skipped", stats.ForksSkipped,
"empty_skipped", stats.EmptySkipped,
"archived", stats.Archived,
)
}

func (t *LogTracker) StartScan(totalRepos int) {
slog.Info("phase 2: scanning repositories", "total", totalRepos)
}

func (t *LogTracker) SeedScanProgress(done int) {
if done > 0 {
slog.Info("resumed scan progress", "already_done", done)
}
}

func (t *LogTracker) RepoScanned(repo string, current, total int) {
slog.Info("repo scanned", "repo", repo, "progress", fmt.Sprintf("%d/%d", current, total))
}

func (t *LogTracker) ScanWarning(repo, msg string) {
slog.Warn("scan warning", "repo", repo, "msg", msg)
}

func (t *LogTracker) EndScan(scanned, total int) {
pct := 0
if total > 0 {
pct = (scanned * 100) / total
}
slog.Info("scan complete", "scanned", scanned, "total", total, "coverage_pct", pct)
}

func (t *LogTracker) StartCrossRef() {
slog.Info("phase 3: cross-referencing dependencies")
}

func (t *LogTracker) EndCrossRef(edgeCount, unresolvedCount int) {
slog.Info("cross-reference complete", "edges", edgeCount, "unresolved", unresolvedCount)
}

func (t *LogTracker) StartOutput() {
slog.Info("phase 4: writing output")
}

func (t *LogTracker) EndOutput(path string, repoCount, edgeCount int) {
slog.Info("output written", "path", path, "repos", repoCount, "edges", edgeCount)
}

func (t *LogTracker) Stop() {}
