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
if cp.Version != "2.0.0" {
t.Errorf("version = %q, want %q", cp.Version, "2.0.0")
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

// Update repo-1 status only (scan data goes to ResultStore in v2)
if err := mgr.UpdateRepo("org/repo-1", func(r *model.CheckpointRepo) {
r.Status.SBOM = model.ScanStatusDone
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

func TestResultStoreAppendAndLoadAll(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "results.ndjson")

rs, err := NewResultStore(path)
if err != nil {
t.Fatalf("NewResultStore: %v", err)
}

r1 := RepoResult{
FullName: "org/repo-1",
SBOMData: &model.SBOMData{SBOMAvailable: true, ConsumedPackages: []model.ConsumedPackage{
{Name: "lodash", Ecosystem: "npm", Version: "4.17.21"},
}},
}
r2 := RepoResult{
FullName: "org/repo-2",
FileScanData: &model.FileScanData{FilesScanned: 3},
}

if err := rs.Append(r1); err != nil {
t.Fatalf("Append r1: %v", err)
}
if err := rs.Append(r2); err != nil {
t.Fatalf("Append r2: %v", err)
}
rs.Close()

results, err := LoadAll(path)
if err != nil {
t.Fatalf("LoadAll: %v", err)
}
if len(results) != 2 {
t.Fatalf("expected 2 results, got %d", len(results))
}
if !results["org/repo-1"].SBOMData.SBOMAvailable {
t.Error("repo-1 should have SBOMAvailable=true")
}
if results["org/repo-2"].FileScanData.FilesScanned != 3 {
t.Error("repo-2 should have FilesScanned=3")
}
}

func TestResultStoreLastEntryWins(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "results.ndjson")

rs, err := NewResultStore(path)
if err != nil {
t.Fatalf("NewResultStore: %v", err)
}

rs.Append(RepoResult{FullName: "org/repo-1", SBOMData: &model.SBOMData{SBOMAvailable: false}})
rs.Append(RepoResult{FullName: "org/repo-1", SBOMData: &model.SBOMData{SBOMAvailable: true}})
rs.Close()

results, err := LoadAll(path)
if err != nil {
t.Fatalf("LoadAll: %v", err)
}
if len(results) != 1 {
t.Fatalf("expected 1 result (deduped), got %d", len(results))
}
if !results["org/repo-1"].SBOMData.SBOMAvailable {
t.Error("last entry should win (SBOMAvailable=true)")
}
}

func TestResultStoreTruncatedLine(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "results.ndjson")

f, _ := os.Create(path)
f.WriteString("{\"full_name\":\"org/repo-1\",\"sbom_data\":{\"sbom_available\":true}}\n")
f.WriteString("{\"full_name\":\"org/repo-2\",\"sb")
f.Close()

results, err := LoadAll(path)
if err != nil {
t.Fatalf("LoadAll: %v", err)
}
if len(results) != 1 {
t.Fatalf("expected 1 result (truncated line skipped), got %d", len(results))
}
if _, ok := results["org/repo-1"]; !ok {
t.Error("repo-1 should be present")
}
}

func TestLoadAllNonexistent(t *testing.T) {
results, err := LoadAll("/nonexistent/results.ndjson")
if err != nil {
t.Fatalf("LoadAll: %v", err)
}
if len(results) != 0 {
t.Errorf("expected empty map for nonexistent file, got %d", len(results))
}
}

func TestV1Migration(t *testing.T) {
dir := t.TempDir()
cpPath := filepath.Join(dir, "checkpoint.json")

v1Data := "{\"version\":\"1.0.0\",\"generation\":1,\"started_at\":\"2025-01-01T00:00:00Z\",\"github_host\":\"github.com\",\"orgs\":[\"org\"],\"current_phase\":\"scan\",\"repos\":{\"org/repo-1\":{\"status\":{\"enumerate\":\"done\",\"sbom\":\"done\",\"filescan\":\"done\"},\"default_branch\":\"main\",\"sbom_data\":{\"sbom_available\":true,\"consumed_packages\":[{\"name\":\"react\",\"ecosystem\":\"npm\"}],\"published_identity\":null},\"filescan_data\":{\"files_scanned\":5,\"edges\":null,\"unresolvable\":null}}},\"stats\":{\"total\":1,\"completed\":{},\"failed\":{},\"skipped\":{}}}"
os.WriteFile(cpPath, []byte(v1Data), 0644)

mgr := NewManager(cpPath, 5)
if err := mgr.Load(); err != nil {
t.Fatalf("Load v1: %v", err)
}

cp := mgr.Data()
if cp.Version != "2.0.0" {
t.Errorf("version should be migrated to 2.0.0, got %q", cp.Version)
}

if cp.Repos["org/repo-1"].SBOMData != nil {
t.Error("SBOMData should be nil after migration")
}
if cp.Repos["org/repo-1"].FileScanData != nil {
t.Error("FileScanData should be nil after migration")
}

results, err := mgr.LoadResults()
if err != nil {
t.Fatalf("LoadResults: %v", err)
}
if len(results) != 1 {
t.Fatalf("expected 1 migrated result, got %d", len(results))
}
if !results["org/repo-1"].SBOMData.SBOMAvailable {
t.Error("migrated result should have SBOMAvailable=true")
}
if results["org/repo-1"].FileScanData.FilesScanned != 5 {
t.Error("migrated result should have FilesScanned=5")
}
mgr.Close()
}

func TestRepoScanNeeded(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "checkpoint.json")
mgr := NewManager(path, 5)

repos := map[string]*model.CheckpointRepo{
"org/repo-1": {
Status:        model.CheckpointRepoStatus{Enumerate: model.ScanStatusDone, SBOM: model.ScanStatusPending, FileScan: model.ScanStatusPending},
DefaultBranch: "main",
},
"org/repo-2": {
Status:        model.CheckpointRepoStatus{Enumerate: model.ScanStatusDone, SBOM: model.ScanStatusDone, FileScan: model.ScanStatusDone},
DefaultBranch: "develop",
},
}
mgr.Initialize("github.com", []string{"org"}, repos)

needSBOM, needFS, branch := mgr.RepoScanNeeded("org/repo-1")
if !needSBOM || !needFS {
t.Error("repo-1 should need both scans")
}
if branch != "main" {
t.Errorf("branch = %q, want main", branch)
}

needSBOM, needFS, _ = mgr.RepoScanNeeded("org/repo-2")
if needSBOM || needFS {
t.Error("repo-2 should not need any scans")
}

needSBOM, needFS, _ = mgr.RepoScanNeeded("org/nonexistent")
if needSBOM || needFS {
t.Error("nonexistent repo should not need any scans")
}
}
