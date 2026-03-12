package checkpoint

import (
"os"
"path/filepath"
"testing"

"github.com/mona-actions/gh-repo-map/internal/model"
)

func TestInitializeAndLoad(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "checkpoint.json")

mgr := NewManager(path, 5)
repos := map[string]*model.CheckpointRepo{
"org/repo-1": {
Status:        model.CheckpointRepoStatus{Enumerate: model.ScanStatusDone, SBOM: model.ScanStatusPending, FileScan: model.ScanStatusPending},
DefaultBranch: "main",
},
"org/repo-2": {
Status:        model.CheckpointRepoStatus{Enumerate: model.ScanStatusDone, SBOM: model.ScanStatusPending, FileScan: model.ScanStatusPending},
DefaultBranch: "master",
Archived:      true,
},
}

if err := mgr.Initialize("github.com", []string{"org"}, repos); err != nil {
t.Fatalf("Initialize: %v", err)
}

// Verify file exists
if _, err := os.Stat(path); err != nil {
t.Fatalf("checkpoint file not created: %v", err)
}

// Load into a new manager
mgr2 := NewManager(path, 5)
if err := mgr2.Load(); err != nil {
t.Fatalf("Load: %v", err)
}

cp := mgr2.Data()
if cp.Version != "1.0.0" {
t.Errorf("version = %q, want %q", cp.Version, "1.0.0")
}
if len(cp.Repos) != 2 {
t.Errorf("repos count = %d, want 2", len(cp.Repos))
}
if cp.Repos["org/repo-2"].Archived != true {
t.Error("repo-2 should be archived")
}
}

func TestUpdateRepoAndFlush(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "checkpoint.json")

mgr := NewManager(path, 2) // flush every 2 updates
repos := map[string]*model.CheckpointRepo{
"org/repo-1": {Status: model.CheckpointRepoStatus{Enumerate: model.ScanStatusDone}},
"org/repo-2": {Status: model.CheckpointRepoStatus{Enumerate: model.ScanStatusDone}},
}
if err := mgr.Initialize("github.com", []string{"org"}, repos); err != nil {
t.Fatalf("Initialize: %v", err)
}

// Update repo-1
if err := mgr.UpdateRepo("org/repo-1", func(r *model.CheckpointRepo) {
r.Status.SBOM = model.ScanStatusDone
r.SBOMData = &model.SBOMData{SBOMAvailable: true}
}); err != nil {
t.Fatalf("UpdateRepo: %v", err)
}

// Update repo-2 (should trigger flush at interval=2)
if err := mgr.UpdateRepo("org/repo-2", func(r *model.CheckpointRepo) {
r.Status.SBOM = model.ScanStatusFailed
}); err != nil {
t.Fatalf("UpdateRepo: %v", err)
}

// Verify flush happened by loading
mgr2 := NewManager(path, 5)
if err := mgr2.Load(); err != nil {
t.Fatalf("Load: %v", err)
}
cp := mgr2.Data()
if cp.Repos["org/repo-1"].Status.SBOM != model.ScanStatusDone {
t.Error("repo-1 SBOM status should be done")
}
if cp.Repos["org/repo-2"].Status.SBOM != model.ScanStatusFailed {
t.Error("repo-2 SBOM status should be failed")
}
}

func TestUpdateRepoNotFound(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "checkpoint.json")
mgr := NewManager(path, 5)
mgr.Initialize("github.com", []string{}, map[string]*model.CheckpointRepo{})

err := mgr.UpdateRepo("nonexistent", func(r *model.CheckpointRepo) {})
if err == nil {
t.Error("expected error for nonexistent repo")
}
}

func TestAtomicWrite(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "checkpoint.json")
mgr := NewManager(path, 5)

if err := mgr.Initialize("github.com", []string{}, map[string]*model.CheckpointRepo{}); err != nil {
t.Fatalf("Initialize: %v", err)
}

// Verify no .tmp file remains
tmpPath := path + ".tmp"
if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
t.Error("temp file should not exist after successful write")
}
}

func TestExists(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "checkpoint.json")
mgr := NewManager(path, 5)

if mgr.Exists() {
t.Error("should not exist before initialization")
}

mgr.Initialize("github.com", []string{}, map[string]*model.CheckpointRepo{})

if !mgr.Exists() {
t.Error("should exist after initialization")
}
}
