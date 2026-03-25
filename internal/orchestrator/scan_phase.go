package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mona-actions/gh-repo-map/internal/auth"
	"github.com/mona-actions/gh-repo-map/internal/checkpoint"
	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/filescan"
	"github.com/mona-actions/gh-repo-map/internal/model"
	"github.com/mona-actions/gh-repo-map/internal/parse"
	"github.com/mona-actions/gh-repo-map/internal/progress"
	"github.com/mona-actions/gh-repo-map/internal/sbom"
)

// Phase 2: runScanPhase performs SBOM and file scan collection for repos in the checkpoint.
func runScanPhase(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager, tracker progress.Tracker) error {
	cp := cpMgr.Data()

	var scannable int
	for _, repo := range cp.Repos {
		if !repo.Archived {
			scannable++
		}
	}
	tracker.StartScan(scannable)

	maxWorkers := cfg.Options.Concurrency
	if maxWorkers <= 0 {
		maxWorkers = 4
	}
	if maxWorkers > 10 {
		maxWorkers = 10
	}

	orgRepos := make(map[string][]string)
	for fullName, repo := range cp.Repos {
		if repo.Archived {
			continue
		}
		parts := strings.SplitN(fullName, "/", 2)
		if len(parts) != 2 {
			continue
		}
		orgRepos[parts[0]] = append(orgRepos[parts[0]], fullName)
	}

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	var globalProcessed atomic.Int32

	for _, repo := range cp.Repos {
		if repo.Archived {
			continue
		}
		sbomDone := repo.Status.SBOM == model.ScanStatusDone || repo.Status.SBOM == model.ScanStatusSkipped
		fsDone := repo.Status.FileScan == model.ScanStatusDone || repo.Status.FileScan == model.ScanStatusSkipped
		if sbomDone && fsDone {
			globalProcessed.Add(1)
		}
	}
	if seeded := int(globalProcessed.Load()); seeded > 0 {
		tracker.SeedScanProgress(seeded)
	}

	for org, repos := range orgRepos {
		wg.Add(1)
		sem <- struct{}{}
		go func(org string, repos []string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := processOrgRepos(ctx, cfg, tp, cpMgr, tracker, org, repos, &globalProcessed, scannable); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("org %s: %w", org, err)
				}
				mu.Unlock()
				slog.Error("org scan failed", "org", org, "error", err)
			}
		}(org, repos)
	}

	wg.Wait()
	cpMgr.SetPhase("3")
	return cpMgr.Flush()
}

func processOrgRepos(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager, tracker progress.Tracker, org string, repos []string, globalProcessed *atomic.Int32, totalScannable int) error {
	for _, fullName := range repos {
		parts := strings.SplitN(fullName, "/", 2)
		if len(parts) != 2 {
			continue
		}
		owner, repoName := parts[0], parts[1]

		needSBOM, needFileScan, defaultBranch := cpMgr.RepoScanNeeded(fullName)
		if !needSBOM && !needFileScan {
			continue
		}

		var sbomData *model.SBOMData
		sbomStatus := model.ScanStatusSkipped
		if needSBOM {
			consumed, available, err := sbom.FetchSBOM(ctx, tp, owner, repoName)
			if err != nil {
				tracker.ScanWarning(fullName, fmt.Sprintf("SBOM: %v", err))
				sbomStatus = model.ScanStatusFailed
			} else {
				sbomStatus = model.ScanStatusDone
				sbomData = &model.SBOMData{
					SBOMAvailable:    available,
					ConsumedPackages: consumed,
				}
			}
		}

		var fileScanData *model.FileScanData
		var publishedFromFiles []model.PublishedIdentity
		fileScanStatus := model.ScanStatusSkipped
		if needFileScan {
			fsResult, err := scanRepoFiles(ctx, cfg, tp, fullName, owner, repoName, defaultBranch)
			if err != nil {
				tracker.ScanWarning(fullName, fmt.Sprintf("file scan: %v", err))
				fileScanStatus = model.ScanStatusFailed
			} else {
				fileScanStatus = model.ScanStatusDone
				fileScanData = fsResult.FileScanData
				publishedFromFiles = fsResult.Published
			}
		}

		if err := cpMgr.UpdateRepo(fullName, func(cpRepo *checkpoint.CheckpointRepo) {
			if needSBOM {
				cpRepo.Status.SBOM = sbomStatus
			}
			if needFileScan {
				cpRepo.Status.FileScan = fileScanStatus
			}
		}); err != nil {
			slog.Error("update repo failed", "repo", fullName, "error", err)
		}

		if sbomData != nil || fileScanData != nil {
			result := checkpoint.RepoResult{
				FullName:     fullName,
				SBOMData:     sbomData,
				FileScanData: fileScanData,
			}
			if len(publishedFromFiles) > 0 {
				if result.SBOMData == nil {
					result.SBOMData = &model.SBOMData{}
				}
				result.SBOMData.PublishedIdentity = append(result.SBOMData.PublishedIdentity, publishedFromFiles...)
			}
			if err := cpMgr.AppendResult(result); err != nil {
				slog.Error("append result failed", "repo", fullName, "error", err)
			}
		}

		current := int(globalProcessed.Add(1))
		tracker.RepoScanned(fullName, current, totalScannable)
	}
	return cpMgr.Flush()
}

// fileScanResult holds the results of scanning a repo's files.
type fileScanResult struct {
	FileScanData *model.FileScanData
	Published    []model.PublishedIdentity
}

func scanRepoFiles(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, fullName, owner, repoName, defaultBranch string) (*fileScanResult, error) {
	branch := defaultBranch
	if branch == "" {
		branch = "main"
	}

	tree, err := filescan.FetchTree(ctx, tp, owner, repoName, branch)
	if err != nil {
		return nil, err
	}

	targetFiles := filescan.FilterTargetFiles(tree.Entries, cfg.Scan.VendoredDirs, cfg.Scan.ScriptDirs)
	if len(targetFiles) == 0 {
		return &fileScanResult{
			FileScanData: &model.FileScanData{FilesScanned: 0, TreeTruncated: tree.Truncated},
		}, nil
	}

	repoFiles := map[string][]string{fullName: targetFiles}
	contents, err := filescan.BatchFetchFiles(ctx, tp, owner, repoFiles, cfg.Options.GraphQLBatchSize)
	if err != nil {
		return nil, fmt.Errorf("batch fetch: %w", err)
	}

	knownOrgs := make(map[string]bool)
	for _, org := range cfg.Orgs {
		knownOrgs[org] = true
	}

	host := "github.com"
	if cfg.GitHubHost != "" {
		host = cfg.GitHubHost
	}

	var edges []model.FileScanEdge
	var published []model.PublishedIdentity
	var consumed []model.ConsumedPackage

	for _, fc := range contents[fullName] {
		fe, fp, fc2 := parse.ParseFile(fc.Path, fc.Content, knownOrgs, cfg.ContainerRegistries, host)
		edges = append(edges, fe...)
		published = append(published, fp...)
		consumed = append(consumed, fc2...)
	}

	return &fileScanResult{
		FileScanData: &model.FileScanData{
			FilesScanned:     len(contents[fullName]),
			TreeTruncated:    tree.Truncated,
			Edges:            edges,
			ManifestConsumed: consumed,
		},
		Published: published,
	}, nil
}

func countScanned(cp *checkpoint.Data) (int, int) {
	scanned, total := 0, 0
	for _, repo := range cp.Repos {
		if repo.Archived {
			continue
		}
		total++
		if repo.Status.SBOM == model.ScanStatusDone || repo.Status.FileScan == model.ScanStatusDone {
			scanned++
		}
	}
	return scanned, total
}
