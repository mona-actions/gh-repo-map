// Package output handles JSON output generation and file splitting.
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mona-actions/gh-repo-map/internal/model"
)

const schemaVersion = "1.0.0"
const toolVersion = "0.1.0"

// GenerateOutput builds the complete OutputSchema from graph data.
func GenerateOutput(
	graph map[string]*model.RepoNode,
	unresolved map[string][]UnresolvedPackage,
	stats OutputStats,
	orgs []string,
	host string,
	startTime time.Time,
	splitInfo SplitInfo,
) *OutputSchema {
	totalEdges := 0
	scanned := 0
	skipped := 0
	for _, node := range graph {
		totalEdges += len(node.Direct)
		if node.ScanStatus.SBOM == model.ScanStatusDone || node.ScanStatus.FileScan == model.ScanStatusDone {
			scanned++
		} else {
			skipped++
		}
	}

	return &OutputSchema{
		SchemaVersion: schemaVersion,
		Metadata: OutputMetadata{
			GeneratedAt:       time.Now().UTC(),
			ToolVersion:       toolVersion,
			GitHubHost:        host,
			OrgsScanned:       orgs,
			TotalRepos:        len(graph),
			TotalReposScanned: scanned,
			TotalReposSkipped: skipped,
			TotalEdges:        totalEdges,
			ScanDurationSecs:  time.Since(startTime).Seconds(),
			SplitInfo:         splitInfo,
		},
		Graph:      graph,
		Unresolved: unresolved,
		Stats:      stats,
	}
}

// WriteJSON writes an OutputSchema to the given path.
func WriteJSON(schema *OutputSchema, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}

	return nil
}

// SplitResult holds the file path and schema for a single split file.
type SplitResult struct {
	Path   string
	Schema *OutputSchema
}

// SplitOutput splits the graph into multiple files based on the strategy.
func SplitOutput(
	graph map[string]*model.RepoNode,
	unresolved map[string][]UnresolvedPackage,
	stats OutputStats,
	orgs []string,
	host string,
	startTime time.Time,
	basePath string,
	mode string,
	splitThreshold int,
) ([]SplitResult, error) {
	switch mode {
	case "merged":
		return splitMerged(graph, unresolved, stats, orgs, host, startTime, basePath)
	case "per-org":
		return splitPerOrg(graph, unresolved, stats, orgs, host, startTime, basePath, splitThreshold)
	case "auto":
		return splitAuto(graph, unresolved, stats, orgs, host, startTime, basePath, splitThreshold)
	default:
		return nil, fmt.Errorf("unknown split mode: %q", mode)
	}
}

func splitMerged(graph map[string]*model.RepoNode, unresolved map[string][]UnresolvedPackage, stats OutputStats, orgs []string, host string, startTime time.Time, basePath string) ([]SplitResult, error) {
	splitInfo := SplitInfo{Mode: "merged", FileIndex: 1, TotalFiles: 1, ThisFileOrgs: orgs}
	schema := GenerateOutput(graph, unresolved, stats, orgs, host, startTime, splitInfo)
	return []SplitResult{{Path: basePath, Schema: schema}}, nil
}

func splitPerOrg(graph map[string]*model.RepoNode, unresolved map[string][]UnresolvedPackage, stats OutputStats, orgs []string, host string, startTime time.Time, basePath string, splitThreshold int) ([]SplitResult, error) {
	byOrg := groupByOrg(graph)
	dir := filepath.Dir(basePath)
	ext := filepath.Ext(basePath)
	base := strings.TrimSuffix(filepath.Base(basePath), ext)

	totalFiles := len(byOrg)
	var results []SplitResult
	idx := 0

	for org, orgGraph := range byOrg {
		idx++
		orgUnresolved := filterUnresolved(unresolved, orgGraph)
		splitInfo := SplitInfo{Mode: "per-org", FileIndex: idx, TotalFiles: totalFiles, ThisFileOrgs: []string{org}}

		schema := GenerateOutput(orgGraph, orgUnresolved, stats, []string{org}, host, startTime, splitInfo)
		path := filepath.Join(dir, fmt.Sprintf("%s-%s%s", base, org, ext))
		results = append(results, SplitResult{Path: path, Schema: schema})
	}

	return results, nil
}

func splitAuto(graph map[string]*model.RepoNode, unresolved map[string][]UnresolvedPackage, stats OutputStats, orgs []string, host string, startTime time.Time, basePath string, splitThreshold int) ([]SplitResult, error) {
	if len(graph) < 500 {
		return splitMerged(graph, unresolved, stats, orgs, host, startTime, basePath)
	}
	return splitPerOrg(graph, unresolved, stats, orgs, host, startTime, basePath, splitThreshold)
}

func groupByOrg(graph map[string]*model.RepoNode) map[string]map[string]*model.RepoNode {
	result := make(map[string]map[string]*model.RepoNode)
	for fullName, node := range graph {
		parts := strings.SplitN(fullName, "/", 2)
		org := parts[0]
		if result[org] == nil {
			result[org] = make(map[string]*model.RepoNode)
		}
		result[org][fullName] = node
	}
	return result
}

func filterUnresolved(all map[string][]UnresolvedPackage, graph map[string]*model.RepoNode) map[string][]UnresolvedPackage {
	result := make(map[string][]UnresolvedPackage)
	for repo, pkgs := range all {
		if _, ok := graph[repo]; ok {
			result[repo] = pkgs
		}
	}
	return result
}

// PrintSummary writes scan summary statistics to stderr.
func PrintSummary(schema *OutputSchema) {
	m := schema.Metadata
	fmt.Fprintf(os.Stderr, "\n=== Dependency Graph Summary ===\n")
	fmt.Fprintf(os.Stderr, "Orgs scanned:     %s\n", strings.Join(m.OrgsScanned, ", "))
	fmt.Fprintf(os.Stderr, "Total repos:      %d\n", m.TotalRepos)
	fmt.Fprintf(os.Stderr, "Repos scanned:    %d\n", m.TotalReposScanned)
	fmt.Fprintf(os.Stderr, "Repos skipped:    %d\n", m.TotalReposSkipped)
	fmt.Fprintf(os.Stderr, "Total edges:      %d\n", m.TotalEdges)
	fmt.Fprintf(os.Stderr, "Scan duration:    %.1fs\n", m.ScanDurationSecs)

	s := schema.Stats
	fmt.Fprintf(os.Stderr, "\nEdges by type:\n")
	types := make([]string, 0, len(s.DependencyTypeCounts))
	for t := range s.DependencyTypeCounts {
		types = append(types, string(t))
	}
	sort.Strings(types)
	for _, t := range types {
		fmt.Fprintf(os.Stderr, "  %-12s %d\n", t, s.DependencyTypeCounts[model.DependencyType(t)])
	}

	if len(s.MostDependedOn) > 0 {
		fmt.Fprintf(os.Stderr, "\nMost depended on (top %d):\n", len(s.MostDependedOn))
		for _, dep := range s.MostDependedOn {
			fmt.Fprintf(os.Stderr, "  %-40s %d dependents\n", dep.Repo, dep.DirectDependents)
		}
	}

	fmt.Fprintf(os.Stderr, "\nClusters:         %d\n", len(s.Clusters))
	fmt.Fprintf(os.Stderr, "Circular deps:    %d\n", len(s.CircularDeps))
	fmt.Fprintf(os.Stderr, "Orphan repos:     %d\n", len(s.OrphanRepos))

	totalUnresolved := 0
	for _, pkgs := range schema.Unresolved {
		totalUnresolved += len(pkgs)
	}
	fmt.Fprintf(os.Stderr, "Unresolved pkgs:  %d\n", totalUnresolved)
}
