package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mona-actions/gh-repo-map/internal/auth"
	"github.com/mona-actions/gh-repo-map/internal/checkpoint"
	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/enumerate"
	"github.com/mona-actions/gh-repo-map/internal/model"
	"github.com/mona-actions/gh-repo-map/internal/progress"
)

// Phase 1: runEnumeratePhase enumerates configured org repos and initializes checkpoint state.
func runEnumeratePhase(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager, tracker progress.Tracker) (progress.EnumerateStats, error) {
	tracker.StartEnumerate()
	allRepos, stats, err := enumerateRepos(ctx, cfg, tp, tracker)
	if err != nil {
		return stats, err
	}
	cpRepos := enumerate.BuildCheckpointRepos(allRepos)
	return stats, cpMgr.Initialize(cfg.GitHubHost, cfg.Orgs, cpRepos)
}

// Phase 1: runEnumerateMissingOrgsPhase enumerates orgs missing from the checkpoint and merges them.
func runEnumerateMissingOrgsPhase(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, cpMgr *checkpoint.Manager, tracker progress.Tracker) (progress.EnumerateStats, error) {
	tracker.StartEnumerate()
	allRepos, stats, err := enumerateRepos(ctx, cfg, tp, tracker)
	if err != nil {
		return stats, err
	}
	cpRepos := enumerate.BuildCheckpointRepos(allRepos)
	added, err := cpMgr.MergeRepos(cfg.Orgs, cpRepos)
	if err != nil {
		return stats, err
	}
	tracker.EndEnumerate(stats)
	slog.Info("merged missing org repos", "orgs", cfg.Orgs, "repos_added", added)
	return stats, nil
}

func enumerateRepos(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, tracker progress.Tracker) ([]model.EnumeratedRepo, progress.EnumerateStats, error) {
	var allRepos []model.EnumeratedRepo
	var stats progress.EnumerateStats

	for _, org := range cfg.Orgs {
		onProgress := func(orgName string, reposSoFar int) {
			tracker.EnumerateProgress(orgName, reposSoFar)
		}
		result, err := enumerate.EnumerateOrg(ctx, tp, org, cfg.Options.IncludeForks, onProgress)
		if err != nil {
			return nil, stats, fmt.Errorf("enumerating org %q: %w", org, err)
		}
		stats.TotalFromAPI += result.TotalFromAPI
		stats.ForksSkipped += result.ForksSkipped
		stats.EmptySkipped += result.EmptySkipped
		if len(result.Repos) == 0 {
			stats.EmptyOrgs = append(stats.EmptyOrgs, org)
		}
		tracker.OrgEnumerated(org, len(result.Repos))
		allRepos = append(allRepos, result.Repos...)
	}

	stats.Enumerated = len(allRepos)
	for _, r := range allRepos {
		if r.Archived {
			stats.Archived++
		}
	}
	stats.Scannable = stats.Enumerated - stats.Archived

	return allRepos, stats, nil
}

// findMissingOrgs returns orgs that are in the config but have no repos in the checkpoint.
func findMissingOrgs(configOrgs, checkpointOrgs []string, repos map[string]*checkpoint.CheckpointRepo) []string {
	if len(configOrgs) == 0 {
		return nil
	}

	orgsWithRepos := make(map[string]bool)
	for name := range repos {
		parts := strings.SplitN(name, "/", 2)
		if len(parts) == 2 {
			orgsWithRepos[parts[0]] = true
		}
	}

	var missing []string
	for _, org := range configOrgs {
		if !orgsWithRepos[org] {
			missing = append(missing, org)
		}
	}
	return missing
}
