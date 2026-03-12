package enumerate

import (
"testing"

"github.com/mona-actions/gh-repo-map/internal/model"
)

func TestBuildCheckpointRepos(t *testing.T) {
repos := []model.EnumeratedRepo{
{FullName: "org/active-repo", Owner: "org", Name: "active-repo", DefaultBranch: "main", Archived: false, Size: 100},
{FullName: "org/archived-repo", Owner: "org", Name: "archived-repo", DefaultBranch: "main", Archived: true, Size: 50},
}

result := BuildCheckpointRepos(repos)

if len(result) != 2 {
t.Fatalf("expected 2 repos, got %d", len(result))
}

active := result["org/active-repo"]
if active.Status.Enumerate != model.ScanStatusDone {
t.Errorf("active repo enumerate status: got %q, want %q", active.Status.Enumerate, model.ScanStatusDone)
}
if active.Status.SBOM != model.ScanStatusPending {
t.Errorf("active repo SBOM status: got %q, want %q", active.Status.SBOM, model.ScanStatusPending)
}
if active.DefaultBranch != "main" {
t.Errorf("default branch: got %q, want %q", active.DefaultBranch, "main")
}

archived := result["org/archived-repo"]
if archived.Status.SBOM != model.ScanStatusSkipped {
t.Errorf("archived repo SBOM status: got %q, want %q", archived.Status.SBOM, model.ScanStatusSkipped)
}
if archived.Status.FileScan != model.ScanStatusSkipped {
t.Errorf("archived repo filescan status: got %q, want %q", archived.Status.FileScan, model.ScanStatusSkipped)
}
}
