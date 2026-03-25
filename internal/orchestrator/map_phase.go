package orchestrator

import (
	"log/slog"

	"github.com/mona-actions/gh-repo-map/internal/checkpoint"
	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/graph"
	"github.com/mona-actions/gh-repo-map/internal/model"
	"github.com/mona-actions/gh-repo-map/internal/output"
	"github.com/mona-actions/gh-repo-map/internal/purl"
	"github.com/mona-actions/gh-repo-map/internal/registry"
)

// Phase 3: runMapPhase builds the dependency graph and unresolved package map.
func runMapPhase(cfg *config.Config, cp *checkpoint.Data, results map[string]*checkpoint.RepoResult) (*graph.Graph, map[string][]output.UnresolvedPackage) {
	reg := registry.New()
	if cfg.OverridesPath != "" {
		applied, err := reg.ApplyOverrides(cfg.OverridesPath)
		if err != nil {
			slog.Warn("failed to load overrides", "path", cfg.OverridesPath, "error", err)
		} else {
			slog.Debug("overrides applied", "count", applied)
		}
	}

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

	unresolved := make(map[string][]output.UnresolvedPackage)

	for fullName, result := range results {
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
				unresolved[fullName] = append(unresolved[fullName], output.UnresolvedPackage{
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
