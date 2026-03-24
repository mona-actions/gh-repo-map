package parse

import "testing"

func TestParseDockerfile(t *testing.T) {
	knownOrgs := map[string]bool{"my-org": true, "infra": true}
	registries := []string{"mycompany.jfrog.io"}

	tests := []struct {
		name      string
		content   string
		wantEdges int
		wantFirst string
	}{
		{
			name:      "ghcr.io FROM",
			content:   "FROM ghcr.io/my-org/base-image:latest",
			wantEdges: 1,
			wantFirst: "my-org/base-image",
		},
		{
			name:      "custom registry FROM",
			content:   "FROM mycompany.jfrog.io/infra/build-tools:1.0",
			wantEdges: 1,
			wantFirst: "infra/build-tools",
		},
		{
			name:      "unknown registry ignored",
			content:   "FROM docker.io/library/ubuntu:22.04",
			wantEdges: 0,
		},
		{
			name:      "git clone in RUN",
			content:   "RUN git clone https://github.com/my-org/scripts.git /tmp/scripts",
			wantEdges: 1,
			wantFirst: "my-org/scripts",
		},
		{
			name: "multi-stage with platform",
			content: `FROM --platform=linux/amd64 ghcr.io/my-org/builder:latest AS build
FROM ghcr.io/infra/runtime:slim
`,
			wantEdges: 2,
		},
		{
			name:      "comments skipped",
			content:   "# FROM ghcr.io/my-org/commented:latest",
			wantEdges: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := ParseDockerfile(tt.content, "Dockerfile", "", registries, knownOrgs)
			if len(edges) != tt.wantEdges {
				t.Errorf("got %d edges, want %d", len(edges), tt.wantEdges)
			}
			if tt.wantFirst != "" && len(edges) > 0 && edges[0].Target != tt.wantFirst {
				t.Errorf("edges[0].Target = %q, want %q", edges[0].Target, tt.wantFirst)
			}
		})
	}
}

func TestParseDockerCompose(t *testing.T) {
	knownOrgs := map[string]bool{"my-org": true}

	content := `
services:
  app:
    image: ghcr.io/my-org/web-app:latest
  db:
    image: postgres:15
`
	edges := ParseDockerCompose(content, "docker-compose.yml", "", nil, knownOrgs)
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	if edges[0].Target != "my-org/web-app" {
		t.Errorf("target = %q, want %q", edges[0].Target, "my-org/web-app")
	}
}
