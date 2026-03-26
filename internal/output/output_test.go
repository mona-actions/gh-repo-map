package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mona-actions/gh-repo-map/internal/model"
)

func TestGenerateOutput(t *testing.T) {
	graph := map[string]*model.RepoNode{
		"org/repo-a": {
			ScanStatus:  model.RepoScanStatus{SBOM: model.ScanStatusDone, FileScan: model.ScanStatusDone},
			Annotations: model.RepoAnnotations{},
			Direct: []model.DependencyEdge{
				{Repo: "org/repo-b", Type: model.DepTypeWorkflow, Confidence: model.ConfidenceHigh},
			},
		},
		"org/repo-b": {
			ScanStatus:  model.RepoScanStatus{SBOM: model.ScanStatusDone, FileScan: model.ScanStatusDone},
			Annotations: model.RepoAnnotations{},
		},
	}

	stats := OutputStats{
		DependencyTypeCounts: map[model.DependencyType]int{model.DepTypeWorkflow: 1},
	}
	splitInfo := SplitInfo{Mode: "merged", FileIndex: 1, TotalFiles: 1}

	schema := GenerateOutput(graph, nil, stats, []string{"org"}, "github.com", time.Now(), splitInfo)

	if schema.SchemaVersion != "1.0.0" {
		t.Errorf("schema version: got %q, want %q", schema.SchemaVersion, "1.0.0")
	}
	if schema.Metadata.TotalRepos != 2 {
		t.Errorf("total repos: got %d, want 2", schema.Metadata.TotalRepos)
	}
	if schema.Metadata.TotalReposScanned != 2 {
		t.Errorf("scanned: got %d, want 2", schema.Metadata.TotalReposScanned)
	}
	if schema.Metadata.TotalEdges != 1 {
		t.Errorf("edges: got %d, want 1", schema.Metadata.TotalEdges)
	}
}

func TestWriteJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output", "test.json")

	schema := &OutputSchema{
		SchemaVersion: "1.0.0",
		Graph:         map[string]*model.RepoNode{},
	}

	if err := WriteJSON(schema, path); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	var loaded OutputSchema
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshaling output: %v", err)
	}
	if loaded.SchemaVersion != "1.0.0" {
		t.Errorf("loaded schema version: got %q", loaded.SchemaVersion)
	}
}

func TestSplitOutput_Merged(t *testing.T) {
	graph := map[string]*model.RepoNode{
		"org/repo-a": {ScanStatus: model.RepoScanStatus{SBOM: model.ScanStatusDone}},
	}
	stats := OutputStats{}

	results, err := SplitOutput(graph, nil, stats, []string{"org"}, "", time.Now(), "/tmp/test.json", "merged", 0)
	if err != nil {
		t.Fatalf("SplitOutput merged: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestSplitOutput_Auto_Small(t *testing.T) {
	graph := make(map[string]*model.RepoNode)
	for i := 0; i < 100; i++ {
		graph[fmt.Sprintf("org/repo-%d", i)] = &model.RepoNode{
			ScanStatus: model.RepoScanStatus{SBOM: model.ScanStatusDone},
		}
	}
	stats := OutputStats{}

	results, err := SplitOutput(graph, nil, stats, []string{"org"}, "", time.Now(), "/tmp/test.json", "auto", 0)
	if err != nil {
		t.Fatalf("SplitOutput auto: %v", err)
	}
	// <500 repos = merged = 1 file
	if len(results) != 1 {
		t.Errorf("expected 1 result for <500 repos, got %d", len(results))
	}
}
