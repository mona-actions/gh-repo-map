package orchestrator

import (
	"reflect"
	"testing"

	"github.com/mona-actions/gh-repo-map/internal/checkpoint"
	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/model"
)

// helper to create a *string from a literal.
func strPtr(s string) *string { return &s }

// ---------- resolveCollision unit tests ----------

func TestResolveCollision_ForkWins(t *testing.T) {
	cp := &checkpoint.Data{
		Repos: map[string]*checkpoint.CheckpointRepo{
			"org/parent": {Archived: false, ForkOf: nil},
			"org/fork":   {Archived: false, ForkOf: strPtr("org/parent")},
		},
	}
	candidates := []string{"org/parent", "org/fork"}

	winner, rule := resolveCollision(candidates, cp)
	if winner != "org/parent" {
		t.Errorf("expected winner org/parent, got %q", winner)
	}
	if rule != "fork" {
		t.Errorf("expected rule fork, got %q", rule)
	}
}

func TestResolveCollision_ArchivedWins(t *testing.T) {
	cp := &checkpoint.Data{
		Repos: map[string]*checkpoint.CheckpointRepo{
			"org/active":   {Archived: false},
			"org/archived": {Archived: true},
		},
	}
	candidates := []string{"org/active", "org/archived"}

	winner, rule := resolveCollision(candidates, cp)
	if winner != "org/active" {
		t.Errorf("expected winner org/active, got %q", winner)
	}
	if rule != "archived" {
		t.Errorf("expected rule archived, got %q", rule)
	}
}

func TestResolveCollision_ForkThenArchived(t *testing.T) {
	cp := &checkpoint.Data{
		Repos: map[string]*checkpoint.CheckpointRepo{
			"org/a": {Archived: false, ForkOf: nil},
			"org/b": {Archived: false, ForkOf: strPtr("org/a")},
			"org/c": {Archived: true, ForkOf: nil},
		},
	}
	candidates := []string{"org/a", "org/b", "org/c"}

	winner, rule := resolveCollision(candidates, cp)
	// Fork filtering removes B (fork of A), leaving A and C.
	// Archived filtering removes C, leaving only A.
	if winner != "org/a" {
		t.Errorf("expected winner org/a, got %q", winner)
	}
	if rule != "archived" {
		t.Errorf("expected rule archived, got %q", rule)
	}
}

func TestResolveCollision_Ambiguous(t *testing.T) {
	cp := &checkpoint.Data{
		Repos: map[string]*checkpoint.CheckpointRepo{
			"org/x": {Archived: false},
			"org/y": {Archived: false},
		},
	}
	candidates := []string{"org/x", "org/y"}

	winner, rule := resolveCollision(candidates, cp)
	if winner != "" {
		t.Errorf("expected empty winner, got %q", winner)
	}
	if rule != "" {
		t.Errorf("expected empty rule, got %q", rule)
	}
}

func TestResolveCollision_AllArchived(t *testing.T) {
	cp := &checkpoint.Data{
		Repos: map[string]*checkpoint.CheckpointRepo{
			"org/a1": {Archived: true},
			"org/a2": {Archived: true},
		},
	}
	candidates := []string{"org/a1", "org/a2"}

	winner, rule := resolveCollision(candidates, cp)
	if winner != "" {
		t.Errorf("expected empty winner, got %q", winner)
	}
	if rule != "" {
		t.Errorf("expected empty rule, got %q", rule)
	}
}

// ---------- runMapPhase integration tests ----------

func newTestCheckpointData(repos map[string]*checkpoint.CheckpointRepo) *checkpoint.Data {
	return &checkpoint.Data{
		Version: "2.0.0",
		Repos:   repos,
	}
}

func TestRunMapPhase_SinglePublisher(t *testing.T) {
	cp := newTestCheckpointData(map[string]*checkpoint.CheckpointRepo{
		"org/publisher": {
			Status: checkpoint.CheckpointRepoStatus{
				SBOM:     model.ScanStatusDone,
				FileScan: model.ScanStatusDone,
			},
		},
		"org/consumer": {
			Status: checkpoint.CheckpointRepoStatus{
				SBOM:     model.ScanStatusDone,
				FileScan: model.ScanStatusDone,
			},
		},
	})

	results := map[string]*checkpoint.RepoResult{
		"org/publisher": {
			FullName: "org/publisher",
			SBOMData: &model.SBOMData{
				PublishedIdentity: []model.PublishedIdentity{
					{Ecosystem: "npm", Name: "pkg-a"},
				},
			},
		},
		"org/consumer": {
			FullName: "org/consumer",
			SBOMData: &model.SBOMData{
				ConsumedPackages: []model.ConsumedPackage{
					{Ecosystem: "npm", Name: "pkg-a", Version: "1.0.0"},
				},
			},
		},
	}

	cfg := &config.Config{}
	g, _, collisions := runMapPhase(cfg, cp, results)

	// consumer should have an edge to publisher
	node, ok := g.Nodes["org/consumer"]
	if !ok {
		t.Fatal("expected org/consumer node in graph")
	}
	if len(node.Direct) != 1 {
		t.Fatalf("expected 1 edge from consumer, got %d", len(node.Direct))
	}
	edge := node.Direct[0]
	if edge.Repo != "org/publisher" {
		t.Errorf("expected edge target org/publisher, got %q", edge.Repo)
	}
	if edge.Type != model.DepTypePackage {
		t.Errorf("expected edge type package, got %q", edge.Type)
	}
	if len(collisions) != 0 {
		t.Errorf("expected 0 collisions, got %d", len(collisions))
	}
}

func TestRunMapPhase_MultiPublisher_Ambiguous(t *testing.T) {
	cp := newTestCheckpointData(map[string]*checkpoint.CheckpointRepo{
		"org/pub1": {
			Status: checkpoint.CheckpointRepoStatus{
				SBOM:     model.ScanStatusDone,
				FileScan: model.ScanStatusDone,
			},
		},
		"org/pub2": {
			Status: checkpoint.CheckpointRepoStatus{
				SBOM:     model.ScanStatusDone,
				FileScan: model.ScanStatusDone,
			},
		},
		"org/consumer": {
			Status: checkpoint.CheckpointRepoStatus{
				SBOM:     model.ScanStatusDone,
				FileScan: model.ScanStatusDone,
			},
		},
	})

	results := map[string]*checkpoint.RepoResult{
		"org/pub1": {
			FullName: "org/pub1",
			SBOMData: &model.SBOMData{
				PublishedIdentity: []model.PublishedIdentity{
					{Ecosystem: "npm", Name: "pkg-a"},
				},
			},
		},
		"org/pub2": {
			FullName: "org/pub2",
			SBOMData: &model.SBOMData{
				PublishedIdentity: []model.PublishedIdentity{
					{Ecosystem: "npm", Name: "pkg-a"},
				},
			},
		},
		"org/consumer": {
			FullName: "org/consumer",
			SBOMData: &model.SBOMData{
				ConsumedPackages: []model.ConsumedPackage{
					{Ecosystem: "npm", Name: "pkg-a", Version: "2.0.0"},
				},
			},
		},
	}

	cfg := &config.Config{}
	g, _, collisions := runMapPhase(cfg, cp, results)

	// Consumer should have NO edges because the collision is ambiguous
	node := g.Nodes["org/consumer"]
	if len(node.Direct) != 0 {
		t.Errorf("expected 0 edges from consumer, got %d", len(node.Direct))
	}

	// Should have exactly one collision entry
	if len(collisions) != 1 {
		t.Fatalf("expected 1 collision, got %d", len(collisions))
	}
	c := collisions[0]
	if c.PackageName != "pkg-a" {
		t.Errorf("expected collision package pkg-a, got %q", c.PackageName)
	}
	if c.Ecosystem != "npm" {
		t.Errorf("expected collision ecosystem npm, got %q", c.Ecosystem)
	}
	if c.Consumers != 1 {
		t.Errorf("expected 1 consumer, got %d", c.Consumers)
	}
	expectedPubs := []string{"org/pub1", "org/pub2"}
	if !reflect.DeepEqual(c.Publishers, expectedPubs) {
		t.Errorf("expected publishers %v, got %v", expectedPubs, c.Publishers)
	}
}

func TestRunMapPhase_Deterministic(t *testing.T) {
	cp := newTestCheckpointData(map[string]*checkpoint.CheckpointRepo{
		"org/alpha": {
			Status: checkpoint.CheckpointRepoStatus{
				SBOM:     model.ScanStatusDone,
				FileScan: model.ScanStatusDone,
			},
		},
		"org/beta": {
			Status: checkpoint.CheckpointRepoStatus{
				SBOM:     model.ScanStatusDone,
				FileScan: model.ScanStatusDone,
			},
		},
		"org/gamma": {
			Status: checkpoint.CheckpointRepoStatus{
				SBOM:     model.ScanStatusDone,
				FileScan: model.ScanStatusDone,
			},
		},
	})

	results := map[string]*checkpoint.RepoResult{
		"org/alpha": {
			FullName: "org/alpha",
			SBOMData: &model.SBOMData{
				PublishedIdentity: []model.PublishedIdentity{
					{Ecosystem: "npm", Name: "alpha-lib"},
				},
			},
		},
		"org/beta": {
			FullName: "org/beta",
			SBOMData: &model.SBOMData{
				PublishedIdentity: []model.PublishedIdentity{
					{Ecosystem: "npm", Name: "beta-lib"},
				},
				ConsumedPackages: []model.ConsumedPackage{
					{Ecosystem: "npm", Name: "alpha-lib", Version: "1.0.0"},
				},
			},
		},
		"org/gamma": {
			FullName: "org/gamma",
			SBOMData: &model.SBOMData{
				ConsumedPackages: []model.ConsumedPackage{
					{Ecosystem: "npm", Name: "alpha-lib", Version: "1.0.0"},
					{Ecosystem: "npm", Name: "beta-lib", Version: "2.0.0"},
				},
			},
		},
	}

	cfg := &config.Config{}

	g1, unresolved1, collisions1 := runMapPhase(cfg, cp, results)
	g2, unresolved2, collisions2 := runMapPhase(cfg, cp, results)

	// Graph nodes should be identical
	if len(g1.Nodes) != len(g2.Nodes) {
		t.Fatalf("node count mismatch: %d vs %d", len(g1.Nodes), len(g2.Nodes))
	}
	for name, n1 := range g1.Nodes {
		n2, ok := g2.Nodes[name]
		if !ok {
			t.Fatalf("node %q missing in second run", name)
		}
		if len(n1.Direct) != len(n2.Direct) {
			t.Fatalf("edge count mismatch for %q: %d vs %d", name, len(n1.Direct), len(n2.Direct))
		}
		for i := range n1.Direct {
			if n1.Direct[i].Repo != n2.Direct[i].Repo {
				t.Errorf("edge target mismatch for %q[%d]: %q vs %q", name, i, n1.Direct[i].Repo, n2.Direct[i].Repo)
			}
		}
	}

	if !reflect.DeepEqual(unresolved1, unresolved2) {
		t.Errorf("unresolved mismatch between runs")
	}
	if !reflect.DeepEqual(collisions1, collisions2) {
		t.Errorf("collisions mismatch between runs")
	}
}
