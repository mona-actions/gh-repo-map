package graph

import (
	"testing"

	"github.com/mona-actions/gh-repo-map/internal/model"
)

func makeTestGraph() *Graph {
	g := New()

	// A → B → C (chain)
	// A → D (direct)
	// D → B (creates cluster {A,B,C,D})
	// E (orphan)
	for _, name := range []string{"org/a", "org/b", "org/c", "org/d", "org/e"} {
		g.AddNode(name, &model.RepoNode{
			ScanStatus: model.RepoScanStatus{SBOM: model.ScanStatusDone, FileScan: model.ScanStatusDone},
		})
	}

	g.AddEdge("org/a", model.NewPackageEdge("org/b", "package.json", "b-lib", "npm", "^1.0", true))
	g.AddEdge("org/b", model.NewPackageEdge("org/c", "package.json", "c-lib", "npm", "^2.0", true))
	g.AddEdge("org/a", model.NewActionEdge("org/d", ".github/workflows/ci.yml", "org/d@v1", true))
	g.AddEdge("org/d", model.NewPackageEdge("org/b", "go.mod", "b-mod", "go", "v1.0", true))

	return g
}

func TestAddNodeAndEdge(t *testing.T) {
	g := makeTestGraph()
	if len(g.Nodes) != 5 {
		t.Errorf("nodes = %d, want 5", len(g.Nodes))
	}
	if len(g.Nodes["org/a"].Direct) != 2 {
		t.Errorf("org/a direct edges = %d, want 2", len(g.Nodes["org/a"].Direct))
	}
}

func TestComputeTransitive(t *testing.T) {
	g := makeTestGraph()
	g.ComputeTransitive()

	// org/a should have transitive dep on org/c (via org/b)
	aTransitive := g.Nodes["org/a"].Transitive
	found := false
	for _, td := range aTransitive {
		if td.Repo == "org/c" && td.Depth == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Error("org/a should have transitive dep on org/c at depth 2")
	}
}

func TestDetectCycles(t *testing.T) {
	g := New()
	g.AddNode("org/x", &model.RepoNode{})
	g.AddNode("org/y", &model.RepoNode{})
	g.AddEdge("org/x", model.NewPackageEdge("org/y", "p.json", "y", "npm", "1", true))
	g.AddEdge("org/y", model.NewPackageEdge("org/x", "p.json", "x", "npm", "1", true))

	cycles := g.DetectCycles()
	if len(cycles) == 0 {
		t.Error("expected at least one cycle between org/x and org/y")
	}
}

func TestFindClusters(t *testing.T) {
	g := makeTestGraph()
	clusters := g.FindClusters()

	// org/e is an orphan, so there should be 1 cluster with A,B,C,D
	if len(clusters) != 1 {
		t.Fatalf("clusters = %d, want 1", len(clusters))
	}
	if clusters[0].Size != 4 {
		t.Errorf("cluster size = %d, want 4", clusters[0].Size)
	}
}

func TestFindOrphans(t *testing.T) {
	g := makeTestGraph()
	orphans := g.FindOrphans()
	if len(orphans) != 1 || orphans[0] != "org/e" {
		t.Errorf("orphans = %v, want [org/e]", orphans)
	}
}

func TestMostDependedOn(t *testing.T) {
	g := makeTestGraph()
	top := g.MostDependedOn(3)
	if len(top) == 0 {
		t.Fatal("expected at least one entry")
	}
	// org/b has the most inbound edges (from A and D)
	if top[0].Repo != "org/b" {
		t.Errorf("most depended on = %q, want %q", top[0].Repo, "org/b")
	}
	if top[0].DirectDependents != 2 {
		t.Errorf("dependents = %d, want 2", top[0].DirectDependents)
	}
}

func TestTypeCounts(t *testing.T) {
	g := makeTestGraph()
	counts := g.TypeCounts()
	if counts[model.DepTypePackage] != 3 {
		t.Errorf("package count = %d, want 3", counts[model.DepTypePackage])
	}
	if counts[model.DepTypeAction] != 1 {
		t.Errorf("action count = %d, want 1", counts[model.DepTypeAction])
	}
}
