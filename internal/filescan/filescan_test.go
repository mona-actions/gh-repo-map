package filescan

import (
	"testing"

	"github.com/mona-actions/gh-repo-map/internal/config"
)

func TestFilterTargetFiles(t *testing.T) {
	entries := []TreeEntry{
		{Path: ".github/workflows/ci.yml", Type: "blob"},
		{Path: ".github/workflows/deploy.yaml", Type: "blob"},
		{Path: ".gitmodules", Type: "blob"},
		{Path: "Dockerfile", Type: "blob"},
		{Path: "Dockerfile.prod", Type: "blob"},
		{Path: "docker-compose.yml", Type: "blob"},
		{Path: "infra/main.tf", Type: "blob"},
		{Path: "package.json", Type: "blob"},
		{Path: "go.mod", Type: "blob"},
		{Path: "scripts/build.sh", Type: "blob"},
		{Path: "Makefile", Type: "blob"},
		{Path: "README.md", Type: "blob"},
		{Path: "src/main.go", Type: "blob"},
		{Path: ".github/workflows", Type: "tree"},
		{Path: "nested/package.json", Type: "blob"},
		{Path: "libs/core/pom.xml", Type: "blob"},
		{Path: "src/app.csproj", Type: "blob"},
	}

	targets := FilterTargetFiles(entries, config.DefaultVendoredDirs(), config.DefaultScriptDirs())

	expected := map[string]bool{
		".github/workflows/ci.yml":      true,
		".github/workflows/deploy.yaml": true,
		".gitmodules":                   true,
		"Dockerfile":                    true,
		"Dockerfile.prod":               true,
		"docker-compose.yml":            true,
		"infra/main.tf":                 true,
		"package.json":                  true,
		"go.mod":                        true,
		"scripts/build.sh":              true,
		"Makefile":                      true,
		"nested/package.json":           true,
		"libs/core/pom.xml":             true,
		"src/app.csproj":                true,
	}

	if len(targets) != len(expected) {
		t.Errorf("got %d targets, want %d", len(targets), len(expected))
		for _, f := range targets {
			t.Logf("  got: %s", f)
		}
	}

	for _, f := range targets {
		if !expected[f] {
			t.Errorf("unexpected target: %s", f)
		}
	}
}

func TestFilterTargetFiles_SkipsTreeEntries(t *testing.T) {
	entries := []TreeEntry{
		{Path: ".github/workflows", Type: "tree"},
		{Path: "src", Type: "tree"},
	}

	targets := FilterTargetFiles(entries, config.DefaultVendoredDirs(), config.DefaultScriptDirs())
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for tree entries, got %d", len(targets))
	}
}

func TestIsScriptPath(t *testing.T) {
	tests := []struct {
		dir  string
		want bool
	}{
		{"scripts", true},
		{"build", true},
		{"ci", true},
		{"hack", true},
		{"tools", true},
		{".", true},
		{"src", false},
		{"lib", false},
	}
	for _, tt := range tests {
		got := isScriptPath(tt.dir, toSet(config.DefaultScriptDirs()))
		if got != tt.want {
			t.Errorf("isScriptPath(%q) = %v, want %v", tt.dir, got, tt.want)
		}
	}
}

func TestIsVendoredPath(t *testing.T) {
	tests := []struct {
		dir  string
		want bool
	}{
		{"node_modules/@babel/core", true},
		{"node_modules", true},
		{"app/node_modules/lodash", true},
		{"vendor/github.com/spf13/cobra", true},
		{"vendor", true},
		{"third_party/proto", true},
		{"third-party/lib", true},
		{"bower_components/jquery", true},
		{"jspm_packages/npm", true},
		{"src/components", false},
		{"lib/utils", false},
		{"packages/core", false},
		{".", false},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			got := isVendoredPath(tt.dir, toSet(config.DefaultVendoredDirs()))
			if got != tt.want {
				t.Errorf("isVendoredPath(%q) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}
}

func TestFilterTargetFiles_ExcludesVendored(t *testing.T) {
	entries := []TreeEntry{
		{Path: "package.json", Type: "blob"},
		{Path: "node_modules/@babel/core/package.json", Type: "blob"},
		{Path: "node_modules/lodash/package.json", Type: "blob"},
		{Path: "vendor/github.com/spf13/cobra/go.mod", Type: "blob"},
		{Path: "third_party/proto/Makefile", Type: "blob"},
		{Path: "go.mod", Type: "blob"},
		{Path: "libs/core/package.json", Type: "blob"},
		// Workflows inside vendored dirs should also be excluded
		{Path: "node_modules/@github/markdownlint-github/.github/workflows/ci.yml", Type: "blob"},
		{Path: ".github/workflows/ci.yml", Type: "blob"},
	}

	targets := FilterTargetFiles(entries, config.DefaultVendoredDirs(), config.DefaultScriptDirs())

	expected := map[string]bool{
		"package.json":             true,
		"go.mod":                   true,
		"libs/core/package.json":   true,
		".github/workflows/ci.yml": true,
	}

	if len(targets) != len(expected) {
		t.Errorf("got %d targets, want %d", len(targets), len(expected))
		for _, f := range targets {
			t.Logf("  got: %s", f)
		}
	}
	for _, f := range targets {
		if !expected[f] {
			t.Errorf("unexpected target file (should be excluded): %s", f)
		}
	}
}

func TestFilterTargetFiles_UsesConfiguredScanDirs(t *testing.T) {
	entries := []TreeEntry{
		{Path: "package.json", Type: "blob"},
		{Path: "deps/acme-lib/package.json", Type: "blob"},
		{Path: "node_modules/lodash/package.json", Type: "blob"},
		{Path: "scripts/build.sh", Type: "blob"},
		{Path: "bin/build.sh", Type: "blob"},
	}

	targets := FilterTargetFiles(entries, []string{"deps"}, []string{"bin"})

	expected := map[string]bool{
		"package.json":                     true,
		"node_modules/lodash/package.json": true,
		"bin/build.sh":                     true,
	}

	if len(targets) != len(expected) {
		t.Errorf("got %d targets, want %d", len(targets), len(expected))
		for _, f := range targets {
			t.Logf("  got: %s", f)
		}
	}

	for _, f := range targets {
		if !expected[f] {
			t.Errorf("unexpected target: %s", f)
		}
	}
}
