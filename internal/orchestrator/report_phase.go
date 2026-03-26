package orchestrator

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/graph"
	"github.com/mona-actions/gh-repo-map/internal/output"
)

// Phase 4: runReportPhase generates and writes output files.
func runReportPhase(cfg *config.Config, g *graph.Graph, unresolved map[string][]output.UnresolvedPackage, collisions []output.AmbiguousPackage, startTime time.Time, splitThreshold int) (string, error) {
	stats := buildStats(g)
	splitInfo := output.SplitInfo{Mode: cfg.Options.OutputSplit, FileIndex: 1, TotalFiles: 1, ThisFileOrgs: cfg.Orgs}
	schema := output.GenerateOutput(g.Nodes, unresolved, stats, cfg.Orgs, cfg.GitHubHost, startTime, splitInfo)
	schema.Collisions = collisions

	outputPath := cfg.Output.Path
	if cfg.Options.OutputSplit == "merged" || (cfg.Options.OutputSplit == "auto" && len(g.Nodes) < 500) {
		if err := output.WriteJSON(schema, outputPath); err != nil {
			return "", fmt.Errorf("writing output: %w", err)
		}
		return outputPath, nil
	}

	results, err := output.SplitOutput(g.Nodes, unresolved, stats, cfg.Orgs, cfg.GitHubHost, startTime, outputPath, cfg.Options.OutputSplit, splitThreshold)
	if err != nil {
		return "", fmt.Errorf("splitting output: %w", err)
	}
	for _, r := range results {
		if err := output.WriteJSON(r.Schema, r.Path); err != nil {
			return "", fmt.Errorf("writing split file %s: %w", r.Path, err)
		}
	}
	if len(results) > 0 {
		outputPath = filepath.Dir(results[0].Path) + "/ (" + fmt.Sprintf("%d files", len(results)) + ")"
	}

	return outputPath, nil
}

func buildStats(g *graph.Graph) output.OutputStats {
	return output.OutputStats{
		MostDependedOn:       g.MostDependedOn(10),
		DependencyTypeCounts: g.TypeCounts(),
		Clusters:             g.FindClusters(),
		CircularDeps:         g.DetectCycles(),
		OrphanRepos:          g.FindOrphans(),
	}
}
