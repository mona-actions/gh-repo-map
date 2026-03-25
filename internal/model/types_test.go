package model

import (
	"encoding/json"
	"testing"
)

func TestDependencyEdgeRoundTripEachDetailType(t *testing.T) {
	tests := []struct {
		name   string
		dep    DependencyType
		detail EdgeDetail
		assert func(*testing.T, EdgeDetail)
	}{
		{
			name:   "package",
			dep:    DepTypePackage,
			detail: PackageDetail{Type: DepTypeAction, PackageName: "@org/lib", Ecosystem: "npm", Version: "^1.2.3"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(PackageDetail)
				if !ok {
					t.Fatalf("detail type = %T, want PackageDetail", detail)
				}
				if d.Type != DepTypePackage || d.PackageName != "@org/lib" || d.Ecosystem != "npm" || d.Version != "^1.2.3" {
					t.Fatalf("unexpected package detail: %+v", d)
				}
			},
		},
		{
			name:   "workflow",
			dep:    DepTypeWorkflow,
			detail: WorkflowDetail{Type: DepTypeAction, Uses: "org/workflows/.github/workflows/ci.yml@main"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(WorkflowDetail)
				if !ok {
					t.Fatalf("detail type = %T, want WorkflowDetail", detail)
				}
				if d.Type != DepTypeWorkflow || d.Uses != "org/workflows/.github/workflows/ci.yml@main" {
					t.Fatalf("unexpected workflow detail: %+v", d)
				}
			},
		},
		{
			name:   "action",
			dep:    DepTypeAction,
			detail: ActionDetail{Type: DepTypeWorkflow, Uses: "org/action@v1"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(ActionDetail)
				if !ok {
					t.Fatalf("detail type = %T, want ActionDetail", detail)
				}
				if d.Type != DepTypeAction || d.Uses != "org/action@v1" {
					t.Fatalf("unexpected action detail: %+v", d)
				}
			},
		},
		{
			name:   "submodule",
			dep:    DepTypeSubmodule,
			detail: SubmoduleDetail{Type: DepTypeDocker, URL: "https://github.com/org/shared.git", Path: "vendor/shared"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(SubmoduleDetail)
				if !ok {
					t.Fatalf("detail type = %T, want SubmoduleDetail", detail)
				}
				if d.Type != DepTypeSubmodule || d.URL != "https://github.com/org/shared.git" || d.Path != "vendor/shared" {
					t.Fatalf("unexpected submodule detail: %+v", d)
				}
			},
		},
		{
			name:   "docker",
			dep:    DepTypeDocker,
			detail: DockerDetail{Type: DepTypePackage, Image: "ghcr.io/org/base:latest"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(DockerDetail)
				if !ok {
					t.Fatalf("detail type = %T, want DockerDetail", detail)
				}
				if d.Type != DepTypeDocker || d.Image != "ghcr.io/org/base:latest" {
					t.Fatalf("unexpected docker detail: %+v", d)
				}
			},
		},
		{
			name:   "terraform",
			dep:    DepTypeTerraform,
			detail: TerraformDetail{Type: DepTypeScript, Source: "github.com/org/modules", Ref: "v1.0.0"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(TerraformDetail)
				if !ok {
					t.Fatalf("detail type = %T, want TerraformDetail", detail)
				}
				if d.Type != DepTypeTerraform || d.Source != "github.com/org/modules" || d.Ref != "v1.0.0" {
					t.Fatalf("unexpected terraform detail: %+v", d)
				}
			},
		},
		{
			name:   "script",
			dep:    DepTypeScript,
			detail: ScriptDetail{Type: DepTypeTerraform, Match: "git clone https://github.com/org/scripts.git", MatchType: "git_clone"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(ScriptDetail)
				if !ok {
					t.Fatalf("detail type = %T, want ScriptDetail", detail)
				}
				if d.Type != DepTypeScript || d.Match != "git clone https://github.com/org/scripts.git" || d.MatchType != "git_clone" {
					t.Fatalf("unexpected script detail: %+v", d)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edge := DependencyEdge{
				Repo:          "org/target",
				Type:          tt.dep,
				Confidence:    ConfidenceHigh,
				TargetScanned: true,
				Detail:        tt.detail,
				SourceFile:    "source.file",
			}

			raw, err := json.Marshal(edge)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var payload map[string]json.RawMessage
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			var detail map[string]json.RawMessage
			if err := json.Unmarshal(payload["detail"], &detail); err != nil {
				t.Fatalf("unmarshal detail: %v", err)
			}
			var gotType string
			if err := json.Unmarshal(detail["type"], &gotType); err != nil {
				t.Fatalf("decode detail.type: %v", err)
			}
			if gotType != string(tt.dep) {
				t.Fatalf("detail.type = %q, want %q", gotType, tt.dep)
			}

			var decoded DependencyEdge
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("decode edge: %v", err)
			}
			tt.assert(t, decoded.Detail)
		})
	}
}

func TestDependencyEdgeMarshalJSON_NilDetailWritesDiscriminator(t *testing.T) {
	edge := DependencyEdge{
		Repo:       "org/target",
		Type:       DepTypeWorkflow,
		Confidence: ConfidenceHigh,
		SourceFile: ".github/workflows/ci.yml",
	}

	raw, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	var detail map[string]string
	if err := json.Unmarshal(payload["detail"], &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail["type"] != string(DepTypeWorkflow) {
		t.Fatalf("detail.type = %q, want %q", detail["type"], DepTypeWorkflow)
	}
}

func TestDependencyEdgeUnmarshalJSON_LegacyDetailWithoutType(t *testing.T) {
	raw := []byte(`{
		"repo":"org/base-image",
		"type":"docker",
		"confidence":"high",
		"target_scanned":true,
		"source_file":"Dockerfile",
		"detail":{"image":"ghcr.io/org/base-image:latest"}
	}`)

	var edge DependencyEdge
	if err := json.Unmarshal(raw, &edge); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	d, ok := edge.Detail.(DockerDetail)
	if !ok {
		t.Fatalf("detail type = %T, want DockerDetail", edge.Detail)
	}
	if d.Type != DepTypeDocker || d.Image != "ghcr.io/org/base-image:latest" {
		t.Fatalf("unexpected docker detail: %+v", d)
	}
}

func TestFileScanEdgeRoundTripEachDetailType(t *testing.T) {
	tests := []struct {
		name   string
		dep    DependencyType
		detail EdgeDetail
		assert func(*testing.T, EdgeDetail)
	}{
		{
			name:   "workflow",
			dep:    DepTypeWorkflow,
			detail: WorkflowDetail{Uses: "org/workflows/.github/workflows/ci.yml@main"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(WorkflowDetail)
				if !ok {
					t.Fatalf("detail type = %T, want WorkflowDetail", detail)
				}
				if d.Type != DepTypeWorkflow || d.Uses == "" {
					t.Fatalf("unexpected workflow detail: %+v", d)
				}
			},
		},
		{
			name:   "action",
			dep:    DepTypeAction,
			detail: ActionDetail{Uses: "org/action@v1"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(ActionDetail)
				if !ok {
					t.Fatalf("detail type = %T, want ActionDetail", detail)
				}
				if d.Type != DepTypeAction || d.Uses == "" {
					t.Fatalf("unexpected action detail: %+v", d)
				}
			},
		},
		{
			name:   "submodule",
			dep:    DepTypeSubmodule,
			detail: SubmoduleDetail{URL: "https://github.com/org/shared.git", Path: "vendor/shared"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(SubmoduleDetail)
				if !ok {
					t.Fatalf("detail type = %T, want SubmoduleDetail", detail)
				}
				if d.Type != DepTypeSubmodule || d.URL == "" || d.Path == "" {
					t.Fatalf("unexpected submodule detail: %+v", d)
				}
			},
		},
		{
			name:   "docker",
			dep:    DepTypeDocker,
			detail: DockerDetail{Image: "ghcr.io/org/base:latest"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(DockerDetail)
				if !ok {
					t.Fatalf("detail type = %T, want DockerDetail", detail)
				}
				if d.Type != DepTypeDocker || d.Image == "" {
					t.Fatalf("unexpected docker detail: %+v", d)
				}
			},
		},
		{
			name:   "terraform",
			dep:    DepTypeTerraform,
			detail: TerraformDetail{Source: "github.com/org/modules", Ref: "v1.0.0"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(TerraformDetail)
				if !ok {
					t.Fatalf("detail type = %T, want TerraformDetail", detail)
				}
				if d.Type != DepTypeTerraform || d.Source == "" {
					t.Fatalf("unexpected terraform detail: %+v", d)
				}
			},
		},
		{
			name:   "script",
			dep:    DepTypeScript,
			detail: ScriptDetail{Match: "git clone https://github.com/org/scripts.git", MatchType: "git_clone"},
			assert: func(t *testing.T, detail EdgeDetail) {
				t.Helper()
				d, ok := detail.(ScriptDetail)
				if !ok {
					t.Fatalf("detail type = %T, want ScriptDetail", detail)
				}
				if d.Type != DepTypeScript || d.Match == "" || d.MatchType == "" {
					t.Fatalf("unexpected script detail: %+v", d)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edge := FileScanEdge{
				Target:     "org/target",
				Type:       tt.dep,
				SourceFile: "source.file",
				Detail:     tt.detail,
			}

			raw, err := json.Marshal(edge)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var payload map[string]json.RawMessage
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			var detail map[string]json.RawMessage
			if err := json.Unmarshal(payload["detail"], &detail); err != nil {
				t.Fatalf("unmarshal detail: %v", err)
			}
			var gotType string
			if err := json.Unmarshal(detail["type"], &gotType); err != nil {
				t.Fatalf("decode detail.type: %v", err)
			}
			if gotType != string(tt.dep) {
				t.Fatalf("detail.type = %q, want %q", gotType, tt.dep)
			}

			var decoded FileScanEdge
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("decode edge: %v", err)
			}
			tt.assert(t, decoded.Detail)
		})
	}
}

func TestFileScanEdgeUnmarshalJSON_LegacyDetailWithoutType(t *testing.T) {
	raw := []byte(`{
		"target":"org/workflows",
		"type":"workflow",
		"source_file":".github/workflows/ci.yml",
		"detail":{"uses":"org/workflows/.github/workflows/ci.yml@main"}
	}`)

	var edge FileScanEdge
	if err := json.Unmarshal(raw, &edge); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	d, ok := edge.Detail.(WorkflowDetail)
	if !ok {
		t.Fatalf("detail type = %T, want WorkflowDetail", edge.Detail)
	}
	if d.Type != DepTypeWorkflow || d.Uses != "org/workflows/.github/workflows/ci.yml@main" {
		t.Fatalf("unexpected workflow detail: %+v", d)
	}
}

func TestFileScanEdgeUnmarshalJSON_MissingDetailStaysNil(t *testing.T) {
	raw := []byte(`{
		"target":"org/tools",
		"type":"script",
		"source_file":"scripts/bootstrap.sh"
	}`)

	var edge FileScanEdge
	if err := json.Unmarshal(raw, &edge); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if edge.Detail != nil {
		t.Fatalf("detail = %#v, want nil", edge.Detail)
	}

	encoded, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["detail"]; ok {
		t.Fatalf("detail should be omitted when nil; payload=%s", encoded)
	}
}
