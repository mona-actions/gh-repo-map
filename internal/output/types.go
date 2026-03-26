package output

import (
	"time"

	"github.com/mona-actions/gh-repo-map/internal/model"
)

// SplitInfo describes the chunking strategy for split output files.
type SplitInfo struct {
	Mode         string   `json:"mode"`
	FileIndex    int      `json:"file_index"`
	TotalFiles   int      `json:"total_files"`
	ThisFileOrgs []string `json:"this_file_orgs"`
}

// OutputMetadata holds scan context and timing for the output file.
type OutputMetadata struct {
	GeneratedAt       time.Time `json:"generated_at"`
	ToolVersion       string    `json:"tool_version"`
	GitHubHost        string    `json:"github_host"`
	OrgsScanned       []string  `json:"orgs_scanned"`
	TotalRepos        int       `json:"total_repos"`
	TotalReposScanned int       `json:"total_repos_scanned"`
	TotalReposSkipped int       `json:"total_repos_skipped"`
	TotalEdges        int       `json:"total_edges"`
	ScanDurationSecs  float64   `json:"scan_duration_seconds"`
	SplitInfo         SplitInfo `json:"split_info"`
}

// UnresolvedPackage represents a package that couldn't be mapped to a repo.
type UnresolvedPackage struct {
	PackageName string `json:"package_name"`
	Ecosystem   string `json:"ecosystem"`
	Version     string `json:"version"`
	Reason      string `json:"reason"`
}

// MostDependedOn summarizes the most-depended-upon repos.
type MostDependedOn struct {
	Repo             string `json:"repo"`
	DirectDependents int    `json:"direct_dependents"`
}

// Cluster represents a connected component of mutually-dependent repos.
type Cluster struct {
	ID    int      `json:"id"`
	Repos []string `json:"repos"`
	Size  int      `json:"size"`
}

// OutputStats holds aggregated statistics for the output.
type OutputStats struct {
	MostDependedOn       []MostDependedOn             `json:"most_depended_on"`
	DependencyTypeCounts map[model.DependencyType]int `json:"dependency_type_counts"`
	Clusters             []Cluster                    `json:"clusters"`
	CircularDeps         [][]string                   `json:"circular_deps"`
	OrphanRepos          []string                     `json:"orphan_repos"`
}

// AmbiguousPackage represents a package published by multiple repos
// that could not be automatically resolved to a single canonical publisher.
type AmbiguousPackage struct {
	PackageName string   `json:"package_name"`
	Ecosystem   string   `json:"ecosystem"`
	Publishers  []string `json:"publishers"`
	Consumers   int      `json:"consumers"`
}

// OutputSchema is the top-level JSON output structure (CLI ↔ Frontend contract).
type OutputSchema struct {
	SchemaVersion string                         `json:"schema_version"`
	Metadata      OutputMetadata                 `json:"metadata"`
	Graph         map[string]*model.RepoNode     `json:"graph"`
	Unresolved    map[string][]UnresolvedPackage `json:"unresolved"`
	Collisions    []AmbiguousPackage             `json:"collisions"`
	Stats         OutputStats                    `json:"stats"`
}
