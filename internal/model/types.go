// Package model defines all shared types for the gh-repo-map CLI.
// This is the single source of truth — every other package imports these types.
package model

import (
	"encoding/json"
	"time"
)

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

// EdgeDetail is the sealed interface implemented by all dependency edge detail shapes.
// The unexported marker method prevents implementations outside this package.
type EdgeDetail interface {
	edgeDetail()
}

// PackageDetail holds detail for type=package edges.
type PackageDetail struct {
	Type        DependencyType `json:"type"`
	PackageName string         `json:"package_name"`
	Ecosystem   string         `json:"ecosystem"`
	Version     string         `json:"version"`
}

// WorkflowDetail holds detail for type=workflow edges.
type WorkflowDetail struct {
	Type DependencyType `json:"type"`
	Uses string         `json:"uses"`
}

// ActionDetail holds detail for type=action edges.
type ActionDetail struct {
	Type DependencyType `json:"type"`
	Uses string         `json:"uses"`
}

// SubmoduleDetail holds detail for type=submodule edges.
type SubmoduleDetail struct {
	Type DependencyType `json:"type"`
	URL  string         `json:"url"`
	Path string         `json:"path"`
}

// DockerDetail holds detail for type=docker edges.
type DockerDetail struct {
	Type  DependencyType `json:"type"`
	Image string         `json:"image"`
}

// TerraformDetail holds detail for type=terraform edges.
type TerraformDetail struct {
	Type   DependencyType `json:"type"`
	Source string         `json:"source"`
	Ref    string         `json:"ref,omitempty"`
}

// ScriptDetail holds detail for type=script edges.
type ScriptDetail struct {
	Type      DependencyType `json:"type"`
	Match     string         `json:"match"`
	MatchType string         `json:"match_type"` // git_clone, curl, wget, go_install, pip_git
}

type unknownEdgeDetail map[string]json.RawMessage

func (PackageDetail) edgeDetail()     {}
func (WorkflowDetail) edgeDetail()    {}
func (ActionDetail) edgeDetail()      {}
func (SubmoduleDetail) edgeDetail()   {}
func (DockerDetail) edgeDetail()      {}
func (TerraformDetail) edgeDetail()   {}
func (ScriptDetail) edgeDetail()      {}
func (unknownEdgeDetail) edgeDetail() {}

// DependencyEdge represents a single dependency from one repo to another.
type DependencyEdge struct {
	Repo          string         `json:"repo"`
	Type          DependencyType `json:"type"`
	Confidence    Confidence     `json:"confidence"`
	TargetScanned bool           `json:"target_scanned"`
	Detail        EdgeDetail     `json:"detail"`
	SourceFile    string         `json:"source_file"`
}

// MarshalJSON ensures Detail is never null and always has a type discriminator.
func (e DependencyEdge) MarshalJSON() ([]byte, error) {
	type dependencyEdgeJSON struct {
		Repo          string          `json:"repo"`
		Type          DependencyType  `json:"type"`
		Confidence    Confidence      `json:"confidence"`
		TargetScanned bool            `json:"target_scanned"`
		Detail        json.RawMessage `json:"detail"`
		SourceFile    string          `json:"source_file"`
	}

	detail, err := marshalEdgeDetail(e.Type, e.Detail, true)
	if err != nil {
		return nil, err
	}

	return json.Marshal(dependencyEdgeJSON{
		Repo:          e.Repo,
		Type:          e.Type,
		Confidence:    e.Confidence,
		TargetScanned: e.TargetScanned,
		Detail:        detail,
		SourceFile:    e.SourceFile,
	})
}

// UnmarshalJSON decodes detail into a concrete EdgeDetail based on discriminator.
func (e *DependencyEdge) UnmarshalJSON(data []byte) error {
	type dependencyEdgeJSON struct {
		Repo          string          `json:"repo"`
		Type          DependencyType  `json:"type"`
		Confidence    Confidence      `json:"confidence"`
		TargetScanned bool            `json:"target_scanned"`
		Detail        json.RawMessage `json:"detail"`
		SourceFile    string          `json:"source_file"`
	}

	var raw dependencyEdgeJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	detail, err := unmarshalEdgeDetail(raw.Type, raw.Detail, true)
	if err != nil {
		return err
	}

	*e = DependencyEdge{
		Repo:          raw.Repo,
		Type:          raw.Type,
		Confidence:    raw.Confidence,
		TargetScanned: raw.TargetScanned,
		Detail:        detail,
		SourceFile:    raw.SourceFile,
	}
	return nil
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

// --- Checkpoint data types (Section 1.3) ---

// SBOMData holds the collected SBOM data for a repo.
type SBOMData struct {
	SBOMAvailable     bool                `json:"sbom_available"`
	ConsumedPackages  []ConsumedPackage   `json:"consumed_packages"`
	PublishedIdentity []PublishedIdentity `json:"published_identity"`
}

// FileScanEdge represents a non-package dependency found during file scanning.
type FileScanEdge struct {
	Target     string         `json:"target"`
	Type       DependencyType `json:"type"`
	SourceFile string         `json:"source_file"`
	Detail     EdgeDetail     `json:"detail,omitempty"`
}

// MarshalJSON ensures detail discriminator is stable when detail is present.
func (e FileScanEdge) MarshalJSON() ([]byte, error) {
	type fileScanEdgeJSON struct {
		Target     string          `json:"target"`
		Type       DependencyType  `json:"type"`
		SourceFile string          `json:"source_file"`
		Detail     json.RawMessage `json:"detail,omitempty"`
	}

	detail, err := marshalEdgeDetail(e.Type, e.Detail, false)
	if err != nil {
		return nil, err
	}

	return json.Marshal(fileScanEdgeJSON{
		Target:     e.Target,
		Type:       e.Type,
		SourceFile: e.SourceFile,
		Detail:     detail,
	})
}

// UnmarshalJSON decodes detail into a concrete EdgeDetail based on discriminator.
func (e *FileScanEdge) UnmarshalJSON(data []byte) error {
	type fileScanEdgeJSON struct {
		Target     string          `json:"target"`
		Type       DependencyType  `json:"type"`
		SourceFile string          `json:"source_file"`
		Detail     json.RawMessage `json:"detail"`
	}

	var raw fileScanEdgeJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	detail, err := unmarshalEdgeDetail(raw.Type, raw.Detail, false)
	if err != nil {
		return err
	}

	*e = FileScanEdge{
		Target:     raw.Target,
		Type:       raw.Type,
		SourceFile: raw.SourceFile,
		Detail:     detail,
	}
	return nil
}

// FileScanData holds the collected file scan data for a repo.
type FileScanData struct {
	FilesScanned     int               `json:"files_scanned"`
	TreeTruncated    bool              `json:"tree_truncated"`
	Edges            []FileScanEdge    `json:"edges"`
	Unresolvable     []string          `json:"unresolvable"`
	ManifestConsumed []ConsumedPackage `json:"manifest_consumed,omitempty"`
}

type checkpointRepoStatusWire struct {
	Enumerate ScanStatus `json:"enumerate"`
	SBOM      ScanStatus `json:"sbom"`
	FileScan  ScanStatus `json:"filescan"`
}

type checkpointRepoWire struct {
	Status        checkpointRepoStatusWire `json:"status"`
	Archived      bool                     `json:"archived"`
	ForkOf        *string                  `json:"fork_of"`
	TemplateFrom  *string                  `json:"template_from"`
	DefaultBranch string                   `json:"default_branch"`
	SBOMData      *SBOMData                `json:"sbom_data,omitempty"`
	FileScanData  *FileScanData            `json:"filescan_data,omitempty"`
}

type checkpointStatsWire struct {
	Total     int            `json:"total"`
	Completed map[string]int `json:"completed"`
	Failed    map[string]int `json:"failed"`
	Skipped   map[string]int `json:"skipped"`
}

// Checkpoint is the wire format used by cmd --status.
type Checkpoint struct {
	Version      string                         `json:"version"`
	Generation   int                            `json:"generation"`
	StartedAt    time.Time                      `json:"started_at"`
	GitHubHost   string                         `json:"github_host"`
	Orgs         []string                       `json:"orgs"`
	CurrentPhase string                         `json:"current_phase"`
	Repos        map[string]*checkpointRepoWire `json:"repos"`
	Stats        checkpointStatsWire            `json:"stats"`
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

func marshalEdgeDetail(depType DependencyType, detail EdgeDetail, forceTypeWhenNil bool) (json.RawMessage, error) {
	if detail == nil {
		if !forceTypeWhenNil {
			return nil, nil
		}
		if depType == "" {
			return json.RawMessage(`{}`), nil
		}
		return json.Marshal(map[string]DependencyType{"type": depType})
	}

	raw, err := json.Marshal(detail)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		if !forceTypeWhenNil {
			return nil, nil
		}
		if depType == "" {
			return json.RawMessage(`{}`), nil
		}
		return json.Marshal(map[string]DependencyType{"type": depType})
	}

	var detailMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &detailMap); err != nil {
		return nil, err
	}
	if detailMap == nil {
		detailMap = make(map[string]json.RawMessage)
	}

	discriminator := depType
	if discriminator == "" {
		discriminator = edgeDetailType(detail)
	}
	if discriminator != "" {
		typeRaw, err := json.Marshal(discriminator)
		if err != nil {
			return nil, err
		}
		detailMap["type"] = typeRaw
	}

	return json.Marshal(detailMap)
}

func unmarshalEdgeDetail(fallbackType DependencyType, raw json.RawMessage, synthesizeWhenNil bool) (EdgeDetail, error) {
	if len(raw) == 0 || string(raw) == "null" {
		if synthesizeWhenNil {
			return defaultEdgeDetail(fallbackType), nil
		}
		return nil, nil
	}

	var detailMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &detailMap); err != nil {
		return nil, err
	}
	if detailMap == nil {
		detailMap = make(map[string]json.RawMessage)
	}

	depType := fallbackType
	if typeRaw, ok := detailMap["type"]; ok {
		var parsed DependencyType
		if err := json.Unmarshal(typeRaw, &parsed); err == nil && parsed != "" {
			depType = parsed
		}
	}

	if depType != "" {
		typeRaw, err := json.Marshal(depType)
		if err != nil {
			return nil, err
		}
		detailMap["type"] = typeRaw
	}

	detailPtr := newEdgeDetailPtr(depType)
	if detailPtr == nil {
		if len(detailMap) == 0 {
			return nil, nil
		}
		return unknownEdgeDetail(detailMap), nil
	}

	normalized, err := json.Marshal(detailMap)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(normalized, detailPtr); err != nil {
		return nil, err
	}

	return edgeDetailFromPtr(detailPtr), nil
}

func defaultEdgeDetail(depType DependencyType) EdgeDetail {
	detailPtr := newEdgeDetailPtr(depType)
	if detailPtr != nil {
		return edgeDetailFromPtr(detailPtr)
	}
	if depType == "" {
		return nil
	}
	typeRaw, _ := json.Marshal(depType)
	return unknownEdgeDetail{
		"type": typeRaw,
	}
}

func newEdgeDetailPtr(depType DependencyType) interface{} {
	switch depType {
	case DepTypePackage:
		return &PackageDetail{Type: depType}
	case DepTypeWorkflow:
		return &WorkflowDetail{Type: depType}
	case DepTypeAction:
		return &ActionDetail{Type: depType}
	case DepTypeSubmodule:
		return &SubmoduleDetail{Type: depType}
	case DepTypeDocker:
		return &DockerDetail{Type: depType}
	case DepTypeTerraform:
		return &TerraformDetail{Type: depType}
	case DepTypeScript:
		return &ScriptDetail{Type: depType}
	default:
		return nil
	}
}

func edgeDetailFromPtr(detail interface{}) EdgeDetail {
	switch d := detail.(type) {
	case *PackageDetail:
		return *d
	case *WorkflowDetail:
		return *d
	case *ActionDetail:
		return *d
	case *SubmoduleDetail:
		return *d
	case *DockerDetail:
		return *d
	case *TerraformDetail:
		return *d
	case *ScriptDetail:
		return *d
	default:
		return nil
	}
}

func edgeDetailType(detail EdgeDetail) DependencyType {
	switch d := detail.(type) {
	case PackageDetail:
		return d.Type
	case *PackageDetail:
		if d != nil {
			return d.Type
		}
	case WorkflowDetail:
		return d.Type
	case *WorkflowDetail:
		if d != nil {
			return d.Type
		}
	case ActionDetail:
		return d.Type
	case *ActionDetail:
		if d != nil {
			return d.Type
		}
	case SubmoduleDetail:
		return d.Type
	case *SubmoduleDetail:
		if d != nil {
			return d.Type
		}
	case DockerDetail:
		return d.Type
	case *DockerDetail:
		if d != nil {
			return d.Type
		}
	case TerraformDetail:
		return d.Type
	case *TerraformDetail:
		if d != nil {
			return d.Type
		}
	case ScriptDetail:
		return d.Type
	case *ScriptDetail:
		if d != nil {
			return d.Type
		}
	case unknownEdgeDetail:
		if typeRaw, ok := d["type"]; ok {
			var depType DependencyType
			if err := json.Unmarshal(typeRaw, &depType); err == nil {
				return depType
			}
		}
	}
	return ""
}
