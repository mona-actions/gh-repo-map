// Package registry provides an in-memory package-name→repo lookup table
// for resolving which repo produces a given package.
package registry

import (
	"fmt"
	"os"
	"sync"

	"github.com/mona-actions/gh-repo-map/internal/model"
	"github.com/mona-actions/gh-repo-map/internal/purl"
	"gopkg.in/yaml.v3"
)

// Registry maps normalized package names to the repos that publish them.
type Registry struct {
	mu      sync.RWMutex
	entries map[string][]string // canonical_name → ["org/repo-a", "org/repo-b"]
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{
		entries: make(map[string][]string),
	}
}

// Register adds a published identity to the registry.
// If the same repo is already registered for this package, the call is a no-op (dedup).
// Multiple repos may publish the same package; all are recorded.
func (r *Registry) Register(repo string, identity model.PublishedIdentity) {
	canonical := purl.NormalizeManifestName(identity.Ecosystem, identity.Name)
	eco := purl.NormalizeEcosystem(identity.Ecosystem)
	key := fmt.Sprintf("%s:%s", eco, canonical)

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.entries[key] {
		if existing == repo {
			return // already registered
		}
	}
	r.entries[key] = append(r.entries[key], repo)
}

// Lookup finds a repo that publishes a given package.
// Returns the first registered publisher for backward compatibility.
// Use LookupAll to retrieve every publisher.
func (r *Registry) Lookup(ecosystem, packageName string) (string, bool) {
	canonical := purl.NormalizeManifestName(ecosystem, packageName)
	eco := purl.NormalizeEcosystem(ecosystem)
	key := fmt.Sprintf("%s:%s", eco, canonical)

	r.mu.RLock()
	defer r.mu.RUnlock()

	repos := r.entries[key]
	if len(repos) == 0 {
		return "", false
	}
	return repos[0], true
}

// LookupAll returns every repo that publishes a given package.
func (r *Registry) LookupAll(ecosystem, packageName string) ([]string, bool) {
	canonical := purl.NormalizeManifestName(ecosystem, packageName)
	eco := purl.NormalizeEcosystem(ecosystem)
	key := fmt.Sprintf("%s:%s", eco, canonical)

	r.mu.RLock()
	defer r.mu.RUnlock()

	repos := r.entries[key]
	if len(repos) == 0 {
		return nil, false
	}
	return repos, true
}

// LookupPurl finds the repo using a purl string from SBOM.
// Returns the first registered publisher only.
func (r *Registry) LookupPurl(purlStr string) (string, bool) {
	ecosystem, name := purl.ParsePurl(purlStr)
	return r.Lookup(ecosystem, name)
}

// LookupPurlAll returns every repo that publishes the package identified by a purl string.
func (r *Registry) LookupPurlAll(purlStr string) ([]string, bool) {
	ecosystem, name := purl.ParsePurl(purlStr)
	return r.LookupAll(ecosystem, name)
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
			r.entries[key] = []string{*o.Repo}
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
