// Package enumerate lists repos across GitHub orgs with pagination and filtering.
package enumerate

import (
"context"
"log/slog"

"github.com/google/go-github/v62/github"
"github.com/mona-actions/gh-repo-map/internal/auth"
"github.com/mona-actions/gh-repo-map/internal/model"
)

// EnumerateOrg lists all repos for a given org with pagination.
func EnumerateOrg(ctx context.Context, tp auth.TokenProvider, org string, includeForks bool) ([]model.EnumeratedRepo, error) {
clients, err := tp.Clients(ctx, org)
if err != nil {
return nil, err
}

var allRepos []model.EnumeratedRepo
opts := &github.RepositoryListByOrgOptions{
Type: "all",
ListOptions: github.ListOptions{PerPage: 100},
}

for {
repos, resp, err := clients.REST.Repositories.ListByOrg(ctx, org, opts)
if err != nil {
return nil, err
}

for _, r := range repos {
if r.GetFork() && !includeForks {
slog.Debug("skipping fork", "repo", r.GetFullName())
continue
}
if r.GetSize() == 0 {
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

allRepos = append(allRepos, repo)
}

if resp.NextPage == 0 {
break
}
opts.Page = resp.NextPage
}

return allRepos, nil
}

// BuildCheckpointRepos converts enumerated repos into checkpoint entries.
func BuildCheckpointRepos(repos []model.EnumeratedRepo) map[string]*model.CheckpointRepo {
result := make(map[string]*model.CheckpointRepo, len(repos))
for _, r := range repos {
status := model.CheckpointRepoStatus{
Enumerate: model.ScanStatusDone,
SBOM:      model.ScanStatusPending,
FileScan:  model.ScanStatusPending,
}
if r.Archived {
status.SBOM = model.ScanStatusSkipped
status.FileScan = model.ScanStatusSkipped
}

result[r.FullName] = &model.CheckpointRepo{
Status:        status,
Archived:      r.Archived,
ForkOf:        r.ForkOf,
TemplateFrom:  r.TemplateFrom,
DefaultBranch: r.DefaultBranch,
}
}
return result
}
