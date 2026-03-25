package parse

import (
	"testing"

	"github.com/mona-actions/gh-repo-map/internal/model"
)

func TestParseFileDispatchForDependencyEdges(t *testing.T) {
	knownOrgs := map[string]bool{"my-org": true}
	registries := []string{"mycompany.jfrog.io"}
	host := "github.com"

	tests := []struct {
		name         string
		path         string
		content      string
		wantEdgeType model.DependencyType
		wantTarget   string
	}{
		{
			name: "actions workflow",
			path: ".github/workflows/ci.yml",
			content: `
jobs:
  build:
    steps:
      - uses: my-org/build-action@v1
`,
			wantEdgeType: model.DepTypeAction,
			wantTarget:   "my-org/build-action",
		},
		{
			name: "gitmodules",
			path: ".gitmodules",
			content: `[submodule "lib"]
path = lib
url = https://github.com/my-org/shared-lib.git
`,
			wantEdgeType: model.DepTypeSubmodule,
			wantTarget:   "my-org/shared-lib",
		},
		{
			name:         "dockerfile prefix match",
			path:         "Dockerfile.dev",
			content:      "FROM ghcr.io/my-org/base-image:latest",
			wantEdgeType: model.DepTypeDocker,
			wantTarget:   "my-org/base-image",
		},
		{
			name: "docker compose",
			path: "docker-compose.yaml",
			content: `
services:
  app:
    image: ghcr.io/my-org/web-app:latest
`,
			wantEdgeType: model.DepTypeDocker,
			wantTarget:   "my-org/web-app",
		},
		{
			name: "terraform file",
			path: "infra/main.tf",
			content: `
module "vpc" {
  source = "github.com/my-org/tf-modules//vpc?ref=v1.2.0"
}
`,
			wantEdgeType: model.DepTypeTerraform,
			wantTarget:   "my-org/tf-modules",
		},
		{
			name:         "script file",
			path:         "scripts/setup.sh",
			content:      `git clone https://github.com/my-org/tools.git`,
			wantEdgeType: model.DepTypeScript,
			wantTarget:   "my-org/tools",
		},
		{
			name:         "makefile",
			path:         "Makefile",
			content:      `go install github.com/my-org/linter@latest`,
			wantEdgeType: model.DepTypeScript,
			wantTarget:   "my-org/linter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges, published, consumed := ParseFile(tt.path, tt.content, knownOrgs, registries, host)
			if len(edges) != 1 {
				t.Fatalf("got %d edges, want 1", len(edges))
			}
			if edges[0].Type != tt.wantEdgeType {
				t.Fatalf("edge type = %q, want %q", edges[0].Type, tt.wantEdgeType)
			}
			if edges[0].Target != tt.wantTarget {
				t.Fatalf("edge target = %q, want %q", edges[0].Target, tt.wantTarget)
			}
			if len(published) != 0 {
				t.Fatalf("got %d published, want 0", len(published))
			}
			if len(consumed) != 0 {
				t.Fatalf("got %d consumed, want 0", len(consumed))
			}
		})
	}
}

func TestParseFileDispatchForManifestParsers(t *testing.T) {
	knownOrgs := map[string]bool{"my-org": true}
	registries := []string{"mycompany.jfrog.io"}
	host := "github.com"

	tests := []struct {
		name          string
		path          string
		content       string
		wantPublished int
		wantConsumed  int
	}{
		{
			name: "package json",
			path: "package.json",
			content: `{
  "name": "@my-org/service",
  "dependencies": {
    "lodash": "^1.0.0"
  }
}`,
			wantPublished: 1,
			wantConsumed:  1,
		},
		{
			name: "go mod",
			path: "go.mod",
			content: `module github.com/my-org/service
require github.com/my-org/lib v1.2.3
`,
			wantPublished: 1,
			wantConsumed:  1,
		},
		{
			name: "pom xml",
			path: "pom.xml",
			content: `<project>
  <groupId>com.myorg</groupId>
  <artifactId>service</artifactId>
  <dependencies>
    <dependency>
      <groupId>com.myorg</groupId>
      <artifactId>lib</artifactId>
      <version>1.0.0</version>
    </dependency>
  </dependencies>
</project>`,
			wantPublished: 1,
			wantConsumed:  1,
		},
		{
			name: "pyproject",
			path: "pyproject.toml",
			content: `[project]
name = "my-utils"
dependencies = ["requests>=2.0"]
`,
			wantPublished: 1,
			wantConsumed:  1,
		},
		{
			name: "requirements",
			path: "requirements.txt",
			content: `requests>=2.28
click
`,
			wantPublished: 0,
			wantConsumed:  2,
		},
		{
			name: "csproj",
			path: "my-app.csproj",
			content: `<Project>
  <PropertyGroup>
    <PackageId>MyOrg.SharedLib</PackageId>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
  </ItemGroup>
</Project>`,
			wantPublished: 1,
			wantConsumed:  1,
		},
		{
			name: "cargo toml",
			path: "Cargo.toml",
			content: `[package]
name = "my-crate"

[dependencies]
serde = "1.0"
`,
			wantPublished: 1,
			wantConsumed:  1,
		},
		{
			name: "gemfile",
			path: "Gemfile",
			content: `
gem "rails"
`,
			wantPublished: 0,
			wantConsumed:  1,
		},
		{
			name: "gemspec",
			path: "my-lib.gemspec",
			content: `Gem::Specification.new do |spec|
  spec.name = "my-gem"
end`,
			wantPublished: 1,
			wantConsumed:  0,
		},
		{
			name: "composer json",
			path: "composer.json",
			content: `{
  "name": "myorg/auth-service",
  "require": {
    "php": "^8.1",
    "myorg/shared-lib": "^2.0"
  }
}`,
			wantPublished: 1,
			wantConsumed:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges, published, consumed := ParseFile(tt.path, tt.content, knownOrgs, registries, host)
			if len(edges) != 0 {
				t.Fatalf("got %d edges, want 0", len(edges))
			}
			if len(published) != tt.wantPublished {
				t.Fatalf("got %d published, want %d", len(published), tt.wantPublished)
			}
			if len(consumed) != tt.wantConsumed {
				t.Fatalf("got %d consumed, want %d", len(consumed), tt.wantConsumed)
			}
		})
	}
}

func TestParseFileUnknownFile(t *testing.T) {
	knownOrgs := map[string]bool{"my-org": true}

	edges, published, consumed := ParseFile("docs/readme.txt", "no parse rules", knownOrgs, nil, "github.com")
	if len(edges) != 0 || len(published) != 0 || len(consumed) != 0 {
		t.Fatalf("expected empty result, got edges=%d published=%d consumed=%d", len(edges), len(published), len(consumed))
	}
}
