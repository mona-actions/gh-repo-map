package checkpoint

import (
	"time"

	"github.com/mona-actions/gh-repo-map/internal/model"
)

// CheckpointRepoStatus tracks per-phase scan status for a repo.
type CheckpointRepoStatus struct {
	Enumerate model.ScanStatus `json:"enumerate"`
	SBOM      model.ScanStatus `json:"sbom"`
	FileScan  model.ScanStatus `json:"filescan"`
}

// CheckpointRepo holds all state for a single repo in the checkpoint.
// In v2, SBOMData and FileScanData are stored in results.ndjson instead.
// These fields exist only for v1 migration compatibility (omitempty ensures they're not written when nil).
type CheckpointRepo struct {
	Status        CheckpointRepoStatus `json:"status"`
	Archived      bool                 `json:"archived"`
	ForkOf        *string              `json:"fork_of"`
	TemplateFrom  *string              `json:"template_from"`
	DefaultBranch string               `json:"default_branch"`
	SBOMData      *model.SBOMData      `json:"sbom_data,omitempty"`
	FileScanData  *model.FileScanData  `json:"filescan_data,omitempty"`
}

// CheckpointStats holds aggregate scan statistics.
type CheckpointStats struct {
	Total     int            `json:"total"`
	Completed map[string]int `json:"completed"`
	Failed    map[string]int `json:"failed"`
	Skipped   map[string]int `json:"skipped"`
}

// Data is the operational checkpoint state used during scans.
type Data struct {
	Version      string                     `json:"version"`
	Generation   int                        `json:"generation"`
	StartedAt    time.Time                  `json:"started_at"`
	GitHubHost   string                     `json:"github_host"`
	Orgs         []string                   `json:"orgs"`
	CurrentPhase string                     `json:"current_phase"`
	Repos        map[string]*CheckpointRepo `json:"repos"`
	Stats        CheckpointStats            `json:"stats"`
}
