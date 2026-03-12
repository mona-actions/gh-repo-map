// Package checkpoint provides atomic checkpoint read/write for scan resumability.
package checkpoint

import (
"encoding/json"
"fmt"
"os"
"path/filepath"
"sync"
"time"

"github.com/mona-actions/gh-repo-map/internal/model"
)

const checkpointVersion = "1.0.0"

// Manager handles reading, writing, and updating the checkpoint file.
type Manager struct {
mu            sync.Mutex
path          string
data          *model.Checkpoint
pendingCount  int
flushInterval int
}

// NewManager creates a new checkpoint manager at the given path.
func NewManager(path string, flushInterval int) *Manager {
if flushInterval <= 0 {
flushInterval = 10
}
return &Manager{
path:          path,
flushInterval: flushInterval,
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

return m.flushLocked()
}

// Load reads an existing checkpoint from disk.
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

m.data = &cp
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

data, err := json.MarshalIndent(m.data, "", "  ")
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

// Delete removes the checkpoint file.
func (m *Manager) Delete() error {
return os.Remove(m.path)
}

// Exists checks if a checkpoint file exists at the configured path.
func (m *Manager) Exists() bool {
_, err := os.Stat(m.path)
return err == nil
}
