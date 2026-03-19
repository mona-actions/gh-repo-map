// Package checkpoint provides atomic checkpoint read/write for scan resumability.
package checkpoint

import (
"encoding/json"
"fmt"
"log/slog"
"os"
"path/filepath"
"sync"
"time"

"github.com/mona-actions/gh-repo-map/internal/model"
)

const checkpointVersion = "2.0.0"

// Manager handles reading, writing, and updating the checkpoint file.
type Manager struct {
mu            sync.Mutex
path          string
data          *model.Checkpoint
pendingCount  int
flushInterval int
results       *ResultStore
resultsPath   string
}

// NewManager creates a new checkpoint manager at the given path.
func NewManager(path string, flushInterval int) *Manager {
if flushInterval <= 0 {
flushInterval = 10
}
dir := filepath.Dir(path)
return &Manager{
path:          path,
flushInterval: flushInterval,
resultsPath:   filepath.Join(dir, "results.ndjson"),
}
}

// Initialize creates a new checkpoint with enumerated repos.
func (m *Manager) Initialize(host string, orgs []string, repos map[string]*model.CheckpointRepo) error {
m.mu.Lock()
defer m.mu.Unlock()

m.data = &model.Checkpoint{
Version:      checkpointVersion,
Generation:   1,
StartedAt:    time.Now().UTC(),
GitHubHost:   host,
Orgs:         orgs,
CurrentPhase: "enumerate",
Repos:        repos,
Stats: model.CheckpointStats{
Total:     len(repos),
Completed: map[string]int{},
Failed:    map[string]int{},
Skipped:   map[string]int{},
},
}

if err := m.openResults(); err != nil {
return err
}

return m.flushLocked()
}

// Load reads an existing checkpoint from disk. If it's a v1 checkpoint,
// scan data is migrated to results.ndjson and the checkpoint is rewritten as v2.
func (m *Manager) Load() error {
m.mu.Lock()
defer m.mu.Unlock()

data, err := os.ReadFile(m.path)
if err != nil {
return fmt.Errorf("reading checkpoint: %w", err)
}

var cp model.Checkpoint
if err := json.Unmarshal(data, &cp); err != nil {
return fmt.Errorf("parsing checkpoint: %w", err)
}

// v1 → v2 migration: extract scan data from checkpoint into results.ndjson
if cp.Version == "1.0.0" {
slog.Info("migrating v1 checkpoint to v2", "repos", len(cp.Repos))
if err := m.openResults(); err != nil {
return fmt.Errorf("opening results for migration: %w", err)
}
for fullName, repo := range cp.Repos {
if repo.SBOMData != nil || repo.FileScanData != nil {
if err := m.results.Append(RepoResult{
FullName:     fullName,
SBOMData:     repo.SBOMData,
FileScanData: repo.FileScanData,
}); err != nil {
return fmt.Errorf("migrating results for %s: %w", fullName, err)
}
repo.SBOMData = nil
repo.FileScanData = nil
}
}
cp.Version = checkpointVersion
slog.Info("v1 migration complete")
}

m.data = &cp

// Open results store if not already open (resumed v2 checkpoint)
if m.results == nil {
if err := m.openResults(); err != nil {
return err
}
}

return m.flushLocked()
}

func (m *Manager) openResults() error {
dir := filepath.Dir(m.path)
if err := os.MkdirAll(dir, 0755); err != nil {
return fmt.Errorf("creating results directory: %w", err)
}
rs, err := NewResultStore(m.resultsPath)
if err != nil {
return err
}
m.results = rs
return nil
}

// Data returns the current checkpoint data (read-only snapshot).
func (m *Manager) Data() *model.Checkpoint {
m.mu.Lock()
defer m.mu.Unlock()
return m.data
}

// UpdateRepo updates a repo's data and optionally flushes to disk.
func (m *Manager) UpdateRepo(fullName string, updater func(repo *model.CheckpointRepo)) error {
m.mu.Lock()
defer m.mu.Unlock()

repo, ok := m.data.Repos[fullName]
if !ok {
return fmt.Errorf("repo %q not found in checkpoint", fullName)
}

updater(repo)
m.pendingCount++

if m.pendingCount >= m.flushInterval {
return m.flushLocked()
}
return nil
}

// SetPhase updates the current phase marker.
func (m *Manager) SetPhase(phase string) {
m.mu.Lock()
defer m.mu.Unlock()
m.data.CurrentPhase = phase
}

// MergeRepos adds new repos to an existing checkpoint without overwriting existing entries.
// Also adds any new orgs to the orgs list. Returns the number of repos added.
func (m *Manager) MergeRepos(orgs []string, repos map[string]*model.CheckpointRepo) (int, error) {
m.mu.Lock()
defer m.mu.Unlock()

// Add new orgs
existingOrgs := make(map[string]bool)
for _, o := range m.data.Orgs {
existingOrgs[o] = true
}
for _, o := range orgs {
if !existingOrgs[o] {
m.data.Orgs = append(m.data.Orgs, o)
}
}

// Merge repos — only add if not already present
added := 0
for name, repo := range repos {
if _, exists := m.data.Repos[name]; !exists {
m.data.Repos[name] = repo
added++
}
}
m.data.Stats.Total = len(m.data.Repos)

return added, m.flushLocked()
}

// UpdateStats recalculates aggregate stats from repo statuses.
func (m *Manager) UpdateStats() {
m.mu.Lock()
defer m.mu.Unlock()

completed := map[string]int{}
failed := map[string]int{}
skipped := map[string]int{}

for _, repo := range m.data.Repos {
for _, phase := range []struct {
name   string
status model.ScanStatus
}{
{"sbom", repo.Status.SBOM},
{"filescan", repo.Status.FileScan},
} {
switch phase.status {
case model.ScanStatusDone:
completed[phase.name]++
case model.ScanStatusFailed:
failed[phase.name]++
case model.ScanStatusSkipped:
skipped[phase.name]++
}
}
if repo.Archived {
skipped["archived"]++
}
}

m.data.Stats.Completed = completed
m.data.Stats.Failed = failed
m.data.Stats.Skipped = skipped
}

// Flush writes the checkpoint to disk using atomic rename.
func (m *Manager) Flush() error {
m.mu.Lock()
defer m.mu.Unlock()
return m.flushLocked()
}

func (m *Manager) flushLocked() error {
m.pendingCount = 0

data, err := json.Marshal(m.data)
if err != nil {
return fmt.Errorf("marshaling checkpoint: %w", err)
}

// Ensure output directory exists
dir := filepath.Dir(m.path)
if err := os.MkdirAll(dir, 0755); err != nil {
return fmt.Errorf("creating checkpoint directory: %w", err)
}

tmpPath := m.path + ".tmp"
if err := os.WriteFile(tmpPath, data, 0644); err != nil {
return fmt.Errorf("writing temp checkpoint: %w", err)
}

if err := os.Rename(tmpPath, m.path); err != nil {
return fmt.Errorf("atomic rename checkpoint: %w", err)
}

return nil
}

// Delete removes the checkpoint and results files.
func (m *Manager) Delete() error {
if m.results != nil {
_ = m.results.Close()
_ = os.Remove(m.resultsPath)
}
return os.Remove(m.path)
}

// Exists checks if a checkpoint file exists at the configured path.
func (m *Manager) Exists() bool {
_, err := os.Stat(m.path)
return err == nil
}

// AppendResult writes a scan result to the NDJSON results file.
func (m *Manager) AppendResult(result RepoResult) error {
if m.results == nil {
return fmt.Errorf("results store not initialized")
}
return m.results.Append(result)
}

// LoadResults reads all scan results from the NDJSON file.
func (m *Manager) LoadResults() (map[string]*RepoResult, error) {
return LoadAll(m.resultsPath)
}

// Close closes the results store. Call when scanning is complete.
func (m *Manager) Close() {
if m.results != nil {
_ = m.results.Close()
}
}

// RepoScanNeeded checks if a repo needs SBOM and/or filescan, returning status + default branch.
// This acquires the lock briefly, so callers can perform network I/O without holding it.
func (m *Manager) RepoScanNeeded(fullName string) (needSBOM bool, needFileScan bool, defaultBranch string) {
m.mu.Lock()
defer m.mu.Unlock()

repo, ok := m.data.Repos[fullName]
if !ok {
return false, false, ""
}
return repo.Status.SBOM == model.ScanStatusPending || repo.Status.SBOM == model.ScanStatusFailed,
repo.Status.FileScan == model.ScanStatusPending || repo.Status.FileScan == model.ScanStatusFailed,
repo.DefaultBranch
}
