package orchestrator

import (
	"log/slog"
	"sort"

	"github.com/mona-actions/gh-repo-map/internal/checkpoint"
	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/graph"
	"github.com/mona-actions/gh-repo-map/internal/model"
	"github.com/mona-actions/gh-repo-map/internal/output"
	"github.com/mona-actions/gh-repo-map/internal/purl"
	"github.com/mona-actions/gh-repo-map/internal/registry"
)

// Phase 3: runMapPhase builds the dependency graph, unresolved package map, and ambiguous collisions.
func runMapPhase(cfg *config.Config, cp *checkpoint.Data, results map[string]*checkpoint.RepoResult) (*graph.Graph, map[string][]output.UnresolvedPackage, []output.AmbiguousPackage) {
	reg := registry.New()
	if cfg.OverridesPath != "" {
		applied, err := reg.ApplyOverrides(cfg.OverridesPath)
		if err != nil {
			slog.Warn("failed to load overrides", "path", cfg.OverridesPath, "error", err)
		} else {
			slog.Debug("overrides applied", "count", applied)
		}
	}

	sortedRepoNames := make([]string, 0, len(results))
	for name := range results {
		sortedRepoNames = append(sortedRepoNames, name)
	}
	sort.Strings(sortedRepoNames)

	for _, fullName := range sortedRepoNames {
		result := results[fullName]
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

	type ambiguousKey struct{ name, ecosystem string }
	ambiguousMap := make(map[ambiguousKey]*output.AmbiguousPackage)
	resolvedByFork := 0
	resolvedByArchived := 0

	for _, fullName := range sortedRepoNames {
		result := results[fullName]
		var consumed []model.ConsumedPackage
		if result.SBOMData != nil && len(result.SBOMData.ConsumedPackages) > 0 {
			consumed = result.SBOMData.ConsumedPackages
		} else if result.FileScanData != nil && len(result.FileScanData.ManifestConsumed) > 0 {
			consumed = result.FileScanData.ManifestConsumed
		}

		for _, pkg := range consumed {
			name := purl.NormalizeManifestName(pkg.Ecosystem, pkg.Name)
			publishers, found := reg.LookupAll(pkg.Ecosystem, name)
			if !found {
				unresolved[fullName] = append(unresolved[fullName], output.UnresolvedPackage{
					PackageName: pkg.Name,
					Ecosystem:   pkg.Ecosystem,
					Version:     pkg.Version,
					Reason:      "no_matching_repo",
				})
				continue
			}

			// Filter out self
			var candidates []string
			for _, p := range publishers {
				if p != fullName {
					candidates = append(candidates, p)
				}
			}
			if len(candidates) == 0 {
				continue
			}

			if len(candidates) == 1 {
				target := candidates[0]
				edge := model.NewPackageEdge(target, "", pkg.Name, pkg.Ecosystem, pkg.Version, false)
				if tr, ok := cp.Repos[target]; ok {
					edge.TargetScanned = tr.Status.SBOM == model.ScanStatusDone || tr.Status.FileScan == model.ScanStatusDone
				}
				g.AddEdge(fullName, edge)
			} else {
				target, rule := resolveCollision(candidates, cp)
				if target != "" {
					switch rule {
					case "fork":
						resolvedByFork++
					case "archived":
						resolvedByArchived++
					}
					edge := model.NewPackageEdge(target, "", pkg.Name, pkg.Ecosystem, pkg.Version, false)
					if tr, ok := cp.Repos[target]; ok {
						edge.TargetScanned = tr.Status.SBOM == model.ScanStatusDone || tr.Status.FileScan == model.ScanStatusDone
					}
					g.AddEdge(fullName, edge)
				} else {
					key := ambiguousKey{name: pkg.Name, ecosystem: pkg.Ecosystem}
					if entry, exists := ambiguousMap[key]; exists {
						entry.Consumers++
					} else {
						sortedCandidates := make([]string, len(candidates))
						copy(sortedCandidates, candidates)
						sort.Strings(sortedCandidates)
						ambiguousMap[key] = &output.AmbiguousPackage{
							PackageName: pkg.Name,
							Ecosystem:   pkg.Ecosystem,
							Publishers:  sortedCandidates,
							Consumers:   1,
						}
					}
				}
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

	collisions := make([]output.AmbiguousPackage, 0, len(ambiguousMap))
	for _, v := range ambiguousMap {
		collisions = append(collisions, *v)
	}
	sort.Slice(collisions, func(i, j int) bool {
		if collisions[i].Ecosystem != collisions[j].Ecosystem {
			return collisions[i].Ecosystem < collisions[j].Ecosystem
		}
		return collisions[i].PackageName < collisions[j].PackageName
	})

	slog.Info("collision resolution", "total_collisions", len(collisions), "resolved_by_fork", resolvedByFork, "resolved_by_archived", resolvedByArchived)

	return g, unresolved, collisions
}

// resolveCollision picks a canonical publisher when multiple repos claim the same package.
// Returns the chosen repo and the rule that resolved it, or ("", "") if ambiguous.
func resolveCollision(candidates []string, cp *checkpoint.Data) (string, string) {
	// Rule 1: Filter out forks of other candidates
	nonForks := make([]string, 0, len(candidates))
	candidateSet := make(map[string]bool)
	for _, c := range candidates {
		candidateSet[c] = true
	}
	for _, c := range candidates {
		repo := cp.Repos[c]
		if repo != nil && repo.ForkOf != nil && candidateSet[*repo.ForkOf] {
			continue // skip — this is a fork of another candidate
		}
		nonForks = append(nonForks, c)
	}
	if len(nonForks) == 1 {
		return nonForks[0], "fork"
	}
	if len(nonForks) > 0 {
		candidates = nonForks
	}

	// Rule 2: Filter out archived repos
	var active []string
	for _, c := range candidates {
		repo := cp.Repos[c]
		if repo != nil && repo.Archived {
			continue
		}
		active = append(active, c)
	}
	if len(active) == 1 {
		return active[0], "archived"
	}

	return "", ""
}
