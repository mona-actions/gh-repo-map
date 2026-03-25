// Package enumerate lists repos across GitHub orgs with pagination and filtering.
package enumerate

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/go-github/v84/github"
	"github.com/mona-actions/gh-repo-map/internal/auth"
	"github.com/mona-actions/gh-repo-map/internal/checkpoint"
	"github.com/mona-actions/gh-repo-map/internal/model"
	"github.com/mona-actions/gh-repo-map/internal/retry"
)

// EnumerateProgressFunc is called after each page of repos is fetched,
// reporting the org name and the cumulative count of repos found so far.
type EnumerateProgressFunc func(org string, reposSoFar int)

// EnumerateResult holds the enumerated repos and filter statistics.
type EnumerateResult struct {
	Repos        []model.EnumeratedRepo
	TotalFromAPI int // raw repo count from API (before filtering)
	ForksSkipped int
	EmptySkipped int
}

// EnumerateOrg lists all repos for a given org with pagination.
// If onProgress is non-nil, it is called after each page with the running total.
func EnumerateOrg(ctx context.Context, tp auth.TokenProvider, org string, includeForks bool, onProgress EnumerateProgressFunc) (*EnumerateResult, error) {
	clients, err := tp.Clients(ctx, org)
	if err != nil {
		return nil, err
	}

	result := &EnumerateResult{}
	opts := &github.RepositoryListByOrgOptions{
		Type:        "all",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := listByOrgWithRetry(ctx, clients.REST, org, opts)
		if err != nil {
			return nil, err
		}

		for _, r := range repos {
			result.TotalFromAPI++

			if r.GetFork() && !includeForks {
				result.ForksSkipped++
				slog.Debug("skipping fork", "repo", r.GetFullName())
				continue
			}
			if r.GetSize() == 0 {
				result.EmptySkipped++
				slog.Debug("skipping empty repo", "repo", r.GetFullName())
				continue
			}

			repo := model.EnumeratedRepo{
				FullName:      r.GetFullName(),
				Owner:         r.GetOwner().GetLogin(),
				Name:          r.GetName(),
				DefaultBranch: r.GetDefaultBranch(),
				Archived:      r.GetArchived(),
				Fork:          r.GetFork(),
				Size:          r.GetSize(),
			}

			if r.GetFork() && r.GetParent() != nil {
				parent := r.GetParent().GetFullName()
				repo.ForkOf = &parent
			}
			if r.GetTemplateRepository() != nil {
				tmpl := r.GetTemplateRepository().GetFullName()
				repo.TemplateFrom = &tmpl
			}

			result.Repos = append(result.Repos, repo)
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage

		if onProgress != nil {
			onProgress(org, len(result.Repos))
		}
	}

	return result, nil
}

// BuildCheckpointRepos converts enumerated repos into checkpoint entries.
func BuildCheckpointRepos(repos []model.EnumeratedRepo) map[string]*checkpoint.CheckpointRepo {
	result := make(map[string]*checkpoint.CheckpointRepo, len(repos))
	for _, r := range repos {
		status := checkpoint.CheckpointRepoStatus{
			Enumerate: model.ScanStatusDone,
			SBOM:      model.ScanStatusPending,
			FileScan:  model.ScanStatusPending,
		}
		if r.Archived {
			status.SBOM = model.ScanStatusSkipped
			status.FileScan = model.ScanStatusSkipped
		}

		result[r.FullName] = &checkpoint.CheckpointRepo{
			Status:        status,
			Archived:      r.Archived,
			ForkOf:        r.ForkOf,
			TemplateFrom:  r.TemplateFrom,
			DefaultBranch: r.DefaultBranch,
		}
	}
	return result
}

const maxRetries = 3

// listByOrgWithRetry wraps ListByOrg with exponential backoff for transient errors.
func listByOrgWithRetry(ctx context.Context, client *github.Client, org string, opts *github.RepositoryListByOrgOptions) ([]*github.Repository, *github.Response, error) {
	var (
		repos   []*github.Repository
		resp    *github.Response
		lastErr error
	)
	for attempt := 0; attempt <= maxRetries; attempt++ {
		repos, resp, lastErr = client.Repositories.ListByOrg(ctx, org, opts)
		if lastErr == nil {
			return repos, resp, nil
		}

		if resp != nil && !retry.IsRetryableStatus(resp.StatusCode) {
			return nil, resp, lastErr
		}

		backoff := time.Duration(1<<uint(attempt)) * time.Second
		slog.Warn("retryable API error, backing off", "org", org, "page", opts.Page, "attempt", attempt+1, "backoff", backoff, "error", lastErr)

		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, nil, lastErr
}
