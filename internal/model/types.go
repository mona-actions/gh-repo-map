// Package model defines all shared types for the gh-repo-map CLI.
// This is the single source of truth — every other package imports these types.
package model

import "time"

// DependencyType enumerates the kinds of inter-repo dependencies we detect.
type DependencyType string

const (
DepTypePackage   DependencyType = "package"
DepTypeWorkflow  DependencyType = "workflow"
DepTypeAction    DependencyType = "action"
DepTypeSubmodule DependencyType = "submodule"
DepTypeDocker    DependencyType = "docker"
DepTypeTerraform DependencyType = "terraform"
DepTypeScript    DependencyType = "script"
)

// Confidence indicates how reliable a detected dependency edge is.
type Confidence string

const (
ConfidenceHigh Confidence = "high"
ConfidenceLow  Confidence = "low"
)

// ScanStatus represents the outcome of scanning a phase for a repo.
type ScanStatus string

const (
ScanStatusDone    ScanStatus = "done"
ScanStatusFailed  ScanStatus = "failed"
ScanStatusSkipped ScanStatus = "skipped"
ScanStatusPending ScanStatus = "pending"
)

// --- Edge Detail Types (Section 0.6 — each type has a fixed shape) ---

// PackageDetail holds detail for type=package edges.
type PackageDetail struct {
PackageName string `json:"package_name"`
Ecosystem   string `json:"ecosystem"`
Version     string `json:"version"`
}

// WorkflowDetail holds detail for type=workflow edges.
type WorkflowDetail struct {
Uses string `json:"uses"`
}

// ActionDetail holds detail for type=action edges.
type ActionDetail struct {
Uses string `json:"uses"`
}

// SubmoduleDetail holds detail for type=submodule edges.
type SubmoduleDetail struct {
URL  string `json:"url"`
Path string `json:"path"`
}

// DockerDetail holds detail for type=docker edges.
type DockerDetail struct {
Image string `json:"image"`
}

// TerraformDetail holds detail for type=terraform edges.
type TerraformDetail struct {
Source string `json:"source"`
Ref    string `json:"ref,omitempty"`
}

// ScriptDetail holds detail for type=script edges.
type ScriptDetail struct {
Match     string `json:"match"`
MatchType string `json:"match_type"` // git_clone, curl, wget, go_install, pip_git
}

// DependencyEdge represents a single dependency from one repo to another.
type DependencyEdge struct {
Repo          string         `json:"repo"`
Type          DependencyType `json:"type"`
Confidence    Confidence     `json:"confidence"`
TargetScanned bool           `json:"target_scanned"`
Detail        interface{}    `json:"detail"`
SourceFile    string         `json:"source_file"`
}

// TransitiveDep represents a transitive dependency.
type TransitiveDep struct {
Repo  string         `json:"repo"`
Via   []string       `json:"via"`
Type  DependencyType `json:"type"`
Depth int            `json:"depth"`
}

// --- Repo-level types ---

// RepoScanStatus tracks scan outcome per phase.
type RepoScanStatus struct {
SBOM     ScanStatus `json:"sbom"`
FileScan ScanStatus `json:"filescan"`
}

// RepoAnnotations holds metadata about a repo.
type RepoAnnotations struct {
ForkOf       *string `json:"fork_of"`
TemplateFrom *string `json:"template_from"`
Archived     bool    `json:"archived"`
}

// RepoNode is the full representation of a repo in the output graph.
type RepoNode struct {
ScanStatus  RepoScanStatus   `json:"scan_status"`
Annotations RepoAnnotations  `json:"annotations"`
Direct      []DependencyEdge `json:"direct"`
Transitive  []TransitiveDep  `json:"transitive,omitempty"`
}

// --- Consumed/Published package types (used during scanning) ---

// ConsumedPackage represents a package dependency found in a repo.
type ConsumedPackage struct {
Name      string `json:"name"`
Ecosystem string `json:"ecosystem"`
Version   string `json:"version"`
}

// PublishedIdentity represents a package that a repo publishes.
type PublishedIdentity struct {
Name      string `json:"name"`
Ecosystem string `json:"ecosystem"`
}

// --- Checkpoint types (Section 1.3) ---

// CheckpointRepoStatus tracks per-phase scan status for a repo.
type CheckpointRepoStatus struct {
Enumerate ScanStatus `json:"enumerate"`
SBOM      ScanStatus `json:"sbom"`
FileScan  ScanStatus `json:"filescan"`
}

// SBOMData holds the collected SBOM data for a repo.
type SBOMData struct {
SBOMAvailable     bool              `json:"sbom_available"`
ConsumedPackages  []ConsumedPackage `json:"consumed_packages"`
PublishedIdentity []PublishedIdentity `json:"published_identity"`
}

// FileScanEdge represents a non-package dependency found during file scanning.
type FileScanEdge struct {
Target     string         `json:"target"`
Type       DependencyType `json:"type"`
SourceFile string         `json:"source_file"`
Detail     interface{}    `json:"detail,omitempty"`
}

// FileScanData holds the collected file scan data for a repo.
type FileScanData struct {
FilesScanned  int            `json:"files_scanned"`
TreeTruncated bool           `json:"tree_truncated"`
Edges         []FileScanEdge `json:"edges"`
Unresolvable  []string       `json:"unresolvable"`
}

// CheckpointRepo holds all state for a single repo in the checkpoint.
type CheckpointRepo struct {
Status        CheckpointRepoStatus `json:"status"`
Archived      bool                 `json:"archived"`
ForkOf        *string              `json:"fork_of"`
TemplateFrom  *string              `json:"template_from"`
DefaultBranch string               `json:"default_branch"`
SBOMData      *SBOMData            `json:"sbom_data"`
FileScanData  *FileScanData        `json:"filescan_data"`
}

// CheckpointStats holds aggregate scan statistics.
type CheckpointStats struct {
Total     int            `json:"total"`
Completed map[string]int `json:"completed"`
Failed    map[string]int `json:"failed"`
Skipped   map[string]int `json:"skipped"`
}

// Checkpoint is the unified checkpoint file that persists all scan state.
type Checkpoint struct {
Version      string                     `json:"version"`
Generation   int                        `json:"generation"`
StartedAt    time.Time                  `json:"started_at"`
GitHubHost   string                     `json:"github_host"`
Orgs         []string                   `json:"orgs"`
CurrentPhase string                     `json:"current_phase"`
Repos        map[string]*CheckpointRepo `json:"repos"`
Stats        CheckpointStats            `json:"stats"`
}

// --- Output schema types (Section 0.8) ---

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
MostDependedOn       []MostDependedOn          `json:"most_depended_on"`
DependencyTypeCounts map[DependencyType]int     `json:"dependency_type_counts"`
Clusters             []Cluster                  `json:"clusters"`
CircularDeps         [][]string                 `json:"circular_deps"`
OrphanRepos          []string                   `json:"orphan_repos"`
}

// OutputSchema is the top-level JSON output structure (CLI ↔ Frontend contract).
type OutputSchema struct {
SchemaVersion string                         `json:"schema_version"`
Metadata      OutputMetadata                 `json:"metadata"`
Graph         map[string]*RepoNode           `json:"graph"`
Unresolved    map[string][]UnresolvedPackage  `json:"unresolved"`
Stats         OutputStats                    `json:"stats"`
}

// --- Override types (Section 0.7) ---

// PackageOverride allows manual correction of package→repo mapping.
type PackageOverride struct {
Package   string  `json:"package" yaml:"package"`
Ecosystem string  `json:"ecosystem" yaml:"ecosystem"`
Repo      *string `json:"repo" yaml:"repo"` // nil means external (not internal)
}

// OverridesFile is the top-level structure of the overrides.yml file.
type OverridesFile struct {
Overrides []PackageOverride `json:"overrides" yaml:"overrides"`
}

// --- Enumerated repo (from Phase 1) ---

// EnumeratedRepo represents a repo discovered during org enumeration.
type EnumeratedRepo struct {
FullName      string  `json:"full_name"`
Owner         string  `json:"owner"`
Name          string  `json:"name"`
DefaultBranch string  `json:"default_branch"`
Archived      bool    `json:"archived"`
Fork          bool    `json:"fork"`
Size          int     `json:"size"` // in KB
ForkOf        *string `json:"fork_of"`
TemplateFrom  *string `json:"template_from"`
}
