// Package registry provides an in-memory package-name→repo lookup table
// for resolving which repo produces a given package.
package registry

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/mona-actions/gh-repo-map/internal/model"
	"github.com/mona-actions/gh-repo-map/internal/purl"
	"gopkg.in/yaml.v3"
)

// Registry maps normalized package names to the repo that publishes them.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]string // canonical_name → "org/repo"
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{
		entries: make(map[string]string),
	}
}

// Register adds a published identity to the registry.
// If a package is already registered to a different repo, it logs a warning (ambiguity).
func (r *Registry) Register(repo string, identity model.PublishedIdentity) {
	canonical := purl.NormalizeManifestName(identity.Ecosystem, identity.Name)
	eco := purl.NormalizeEcosystem(identity.Ecosystem)
	key := fmt.Sprintf("%s:%s", eco, canonical)

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.entries[key]; ok && existing != repo {
		slog.Warn("package name collision",
			"package", identity.Name,
			"ecosystem", identity.Ecosystem,
			"existing_repo", existing,
			"new_repo", repo,
		)
		return // keep first registration
	}
	r.entries[key] = repo
}

// Lookup finds the repo that publishes a given package.
// Returns (repo, true) if found, ("", false) if not.
func (r *Registry) Lookup(ecosystem, packageName string) (string, bool) {
	canonical := purl.NormalizeManifestName(ecosystem, packageName)
	eco := purl.NormalizeEcosystem(ecosystem)
	key := fmt.Sprintf("%s:%s", eco, canonical)

	r.mu.RLock()
	defer r.mu.RUnlock()

	repo, ok := r.entries[key]
	return repo, ok
}

// LookupPurl finds the repo using a purl string from SBOM.
func (r *Registry) LookupPurl(purlStr string) (string, bool) {
	ecosystem, name := purl.ParsePurl(purlStr)
	return r.Lookup(ecosystem, name)
}

// ApplyOverrides loads an overrides.yml file and applies it to the registry.
// Override entries take precedence over automated mappings.
func (r *Registry) ApplyOverrides(path string) (applied int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading overrides file: %w", err)
	}

	var overrides model.OverridesFile
	if err := yaml.Unmarshal(data, &overrides); err != nil {
		return 0, fmt.Errorf("parsing overrides file: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, o := range overrides.Overrides {
		canonical := purl.NormalizeManifestName(o.Ecosystem, o.Package)
		eco := purl.NormalizeEcosystem(o.Ecosystem)
		key := fmt.Sprintf("%s:%s", eco, canonical)

		if o.Repo == nil {
			// nil repo = external, remove from registry
			delete(r.entries, key)
		} else {
			r.entries[key] = *o.Repo
		}
		applied++
	}

	return applied, nil
}

// Size returns the number of entries in the registry.
func (r *Registry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
