package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mona-actions/gh-repo-map/internal/model"
)

func TestRegisterAndLookup(t *testing.T) {
	reg := New()

	reg.Register("org/auth-service", model.PublishedIdentity{Name: "@org/auth-lib", Ecosystem: "npm"})
	reg.Register("org/go-utils", model.PublishedIdentity{Name: "github.com/org/go-utils", Ecosystem: "go"})
	reg.Register("org/py-tools", model.PublishedIdentity{Name: "My_Tools", Ecosystem: "pypi"})

	tests := []struct {
		name      string
		eco       string
		pkg       string
		wantRepo  string
		wantFound bool
	}{
		{"npm exact", "npm", "@org/auth-lib", "org/auth-service", true},
		{"go via go ecosystem", "go", "github.com/org/go-utils", "org/go-utils", true},
		{"go via golang ecosystem (cross-resolve)", "golang", "github.com/org/go-utils", "org/go-utils", true},
		{"pypi normalized", "pypi", "my-tools", "org/py-tools", true},
		{"pypi original", "pypi", "My_Tools", "org/py-tools", true},
		{"pip resolves to pypi", "pip", "my-tools", "org/py-tools", true},
		{"not found", "npm", "unknown-pkg", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, found := reg.Lookup(tt.eco, tt.pkg)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestLookupPurl(t *testing.T) {
	reg := New()
	reg.Register("org/auth-service", model.PublishedIdentity{Name: "@org/auth-lib", Ecosystem: "npm"})

	repo, found := reg.LookupPurl("pkg:npm/%40org/auth-lib@2.0.0")
	if !found {
		t.Fatal("expected to find repo from purl")
	}
	if repo != "org/auth-service" {
		t.Errorf("repo = %q, want %q", repo, "org/auth-service")
	}
}

func TestApplyOverrides(t *testing.T) {
	reg := New()
	reg.Register("org/utils", model.PublishedIdentity{Name: "my-utils", Ecosystem: "pypi"})

	dir := t.TempDir()
	path := filepath.Join(dir, "overrides.yml")
	overrideContent := `overrides:
  - package: "my-utils"
    ecosystem: "pypi"
    repo: null
  - package: "com.myorg:proto"
    ecosystem: "maven"
    repo: "org/proto-defs"
`
	os.WriteFile(path, []byte(overrideContent), 0644)

	applied, err := reg.ApplyOverrides(path)
	if err != nil {
		t.Fatalf("ApplyOverrides: %v", err)
	}
	if applied != 2 {
		t.Errorf("applied = %d, want 2", applied)
	}

	// my-utils should be removed (external)
	if _, found := reg.Lookup("pypi", "my-utils"); found {
		t.Error("my-utils should have been removed by override")
	}

	// com.myorg:proto should be added
	repo, found := reg.Lookup("maven", "com.myorg:proto")
	if !found || repo != "org/proto-defs" {
		t.Errorf("proto lookup: found=%v, repo=%q", found, repo)
	}
}

func TestMultiplePublishers(t *testing.T) {
	reg := New()
	reg.Register("org/repo-a", model.PublishedIdentity{Name: "shared-lib", Ecosystem: "npm"})
	reg.Register("org/repo-b", model.PublishedIdentity{Name: "shared-lib", Ecosystem: "npm"})

	// Lookup returns the first publisher for backward compat
	repo, found := reg.Lookup("npm", "shared-lib")
	if !found || repo != "org/repo-a" {
		t.Errorf("Lookup: found=%v, repo=%q; want true, %q", found, repo, "org/repo-a")
	}

	// LookupAll returns every publisher
	repos, found := reg.LookupAll("npm", "shared-lib")
	if !found {
		t.Fatal("LookupAll: expected to find shared-lib")
	}
	if len(repos) != 2 {
		t.Fatalf("LookupAll: got %d repos, want 2", len(repos))
	}
	if repos[0] != "org/repo-a" || repos[1] != "org/repo-b" {
		t.Errorf("LookupAll: repos = %v, want [org/repo-a org/repo-b]", repos)
	}
}

func TestRegisterDedup(t *testing.T) {
	reg := New()
	reg.Register("org/repo-a", model.PublishedIdentity{Name: "dup-lib", Ecosystem: "npm"})
	reg.Register("org/repo-a", model.PublishedIdentity{Name: "dup-lib", Ecosystem: "npm"})

	repos, found := reg.LookupAll("npm", "dup-lib")
	if !found {
		t.Fatal("expected to find dup-lib")
	}
	if len(repos) != 1 {
		t.Fatalf("got %d repos, want 1 (no duplicates)", len(repos))
	}
}

func TestApplyOverridesReplacesAllPublishers(t *testing.T) {
	reg := New()
	reg.Register("org/repo-a", model.PublishedIdentity{Name: "multi-pub", Ecosystem: "npm"})
	reg.Register("org/repo-b", model.PublishedIdentity{Name: "multi-pub", Ecosystem: "npm"})

	dir := t.TempDir()
	path := filepath.Join(dir, "overrides.yml")
	content := `overrides:
  - package: "multi-pub"
    ecosystem: "npm"
    repo: "org/override-repo"
`
	os.WriteFile(path, []byte(content), 0644)

	applied, err := reg.ApplyOverrides(path)
	if err != nil {
		t.Fatalf("ApplyOverrides: %v", err)
	}
	if applied != 1 {
		t.Errorf("applied = %d, want 1", applied)
	}

	repos, found := reg.LookupAll("npm", "multi-pub")
	if !found {
		t.Fatal("expected to find multi-pub after override")
	}
	if len(repos) != 1 || repos[0] != "org/override-repo" {
		t.Errorf("repos = %v, want [org/override-repo]", repos)
	}
}

func TestLookupPurlAll(t *testing.T) {
	reg := New()
	reg.Register("org/auth-a", model.PublishedIdentity{Name: "@org/auth-lib", Ecosystem: "npm"})
	reg.Register("org/auth-b", model.PublishedIdentity{Name: "@org/auth-lib", Ecosystem: "npm"})

	repos, found := reg.LookupPurlAll("pkg:npm/%40org/auth-lib@2.0.0")
	if !found {
		t.Fatal("expected to find repos from purl")
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
}
