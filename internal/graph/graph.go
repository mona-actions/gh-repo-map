// Package graph provides dependency graph construction, transitive closure,
// cycle detection, and connected component analysis.
package graph

import (
"sort"

"github.com/mona-actions/gh-repo-map/internal/model"
)

// Graph represents a directed dependency graph.
type Graph struct {
Nodes    map[string]*model.RepoNode
adjList  map[string][]model.DependencyEdge // forward edges
inDegree map[string]int
}

// New creates an empty Graph.
func New() *Graph {
return &Graph{
Nodes:    make(map[string]*model.RepoNode),
adjList:  make(map[string][]model.DependencyEdge),
inDegree: make(map[string]int),
}
}

// AddNode adds a repo node to the graph.
func (g *Graph) AddNode(fullName string, node *model.RepoNode) {
if node.Direct == nil {
node.Direct = []model.DependencyEdge{}
}
g.Nodes[fullName] = node
if _, ok := g.inDegree[fullName]; !ok {
g.inDegree[fullName] = 0
}
}

// AddEdge adds a dependency edge to the graph.
func (g *Graph) AddEdge(from string, edge model.DependencyEdge) {
g.adjList[from] = append(g.adjList[from], edge)
g.inDegree[edge.Repo]++

if node, ok := g.Nodes[from]; ok {
node.Direct = append(node.Direct, edge)
}
}

// ComputeTransitive computes transitive dependencies using BFS for each node.
func (g *Graph) ComputeTransitive() {
for name, node := range g.Nodes {
node.Transitive = g.bfsTransitive(name)
}
}

func (g *Graph) bfsTransitive(start string) []model.TransitiveDep {
var result []model.TransitiveDep
visited := map[string]bool{start: true}

type queueItem struct {
repo  string
via   []string
depth int
depType model.DependencyType
}

var queue []queueItem
for _, edge := range g.adjList[start] {
if !visited[edge.Repo] {
queue = append(queue, queueItem{
repo:    edge.Repo,
via:     []string{},
depth:   1,
depType: edge.Type,
})
visited[edge.Repo] = true
}
}

for len(queue) > 0 {
item := queue[0]
queue = queue[1:]

// Only add transitive deps (depth > 1)
if item.depth > 1 {
result = append(result, model.TransitiveDep{
Repo:  item.repo,
Via:   item.via,
Type:  item.depType,
Depth: item.depth,
})
}

for _, edge := range g.adjList[item.repo] {
if !visited[edge.Repo] {
visited[edge.Repo] = true
newVia := make([]string, len(item.via)+1)
copy(newVia, item.via)
newVia[len(item.via)] = item.repo
queue = append(queue, queueItem{
repo:    edge.Repo,
via:     newVia,
depth:   item.depth + 1,
depType: edge.Type,
})
}
}
}

return result
}

// DetectCycles finds all cycles in the graph using DFS.
func (g *Graph) DetectCycles() [][]string {
var cycles [][]string
visited := make(map[string]int) // 0=unvisited, 1=in-stack, 2=done
path := []string{}

var dfs func(node string)
dfs = func(node string) {
visited[node] = 1
path = append(path, node)

for _, edge := range g.adjList[node] {
switch visited[edge.Repo] {
case 1: // back edge = cycle
// Find where the cycle starts
for i := len(path) - 1; i >= 0; i-- {
if path[i] == edge.Repo {
cycle := make([]string, len(path)-i)
copy(cycle, path[i:])
cycles = append(cycles, cycle)
break
}
}
case 0:
dfs(edge.Repo)
}
}

path = path[:len(path)-1]
visited[node] = 2
}

for name := range g.Nodes {
if visited[name] == 0 {
dfs(name)
}
}

return cycles
}

// FindClusters returns connected components (migration units).
func (g *Graph) FindClusters() []model.Cluster {
visited := make(map[string]bool)
var clusters []model.Cluster
id := 0

// Build undirected adjacency for connected components
undirected := make(map[string]map[string]bool)
for from, edges := range g.adjList {
if undirected[from] == nil {
undirected[from] = make(map[string]bool)
}
for _, edge := range edges {
undirected[from][edge.Repo] = true
if undirected[edge.Repo] == nil {
undirected[edge.Repo] = make(map[string]bool)
}
undirected[edge.Repo][from] = true
}
}

for name := range g.Nodes {
if visited[name] {
continue
}

// BFS to find component
var component []string
queue := []string{name}
visited[name] = true

for len(queue) > 0 {
curr := queue[0]
queue = queue[1:]
component = append(component, curr)

for neighbor := range undirected[curr] {
if !visited[neighbor] {
visited[neighbor] = true
queue = append(queue, neighbor)
}
}
}

if len(component) > 1 {
sort.Strings(component)
id++
clusters = append(clusters, model.Cluster{
ID:    id,
Repos: component,
Size:  len(component),
})
}
}

// Sort clusters by size descending
sort.Slice(clusters, func(i, j int) bool {
return clusters[i].Size > clusters[j].Size
})

return clusters
}

// FindOrphans returns repos with no inbound or outbound edges.
func (g *Graph) FindOrphans() []string {
var orphans []string
for name := range g.Nodes {
if len(g.adjList[name]) == 0 && g.inDegree[name] == 0 {
orphans = append(orphans, name)
}
}
sort.Strings(orphans)
return orphans
}

// MostDependedOn returns the top N repos by inbound dependency count.
func (g *Graph) MostDependedOn(n int) []model.MostDependedOn {
type entry struct {
repo  string
count int
}
var entries []entry
for repo, count := range g.inDegree {
if count > 0 {
entries = append(entries, entry{repo, count})
}
}
sort.Slice(entries, func(i, j int) bool {
return entries[i].count > entries[j].count
})

if n > len(entries) {
n = len(entries)
}

result := make([]model.MostDependedOn, n)
for i := 0; i < n; i++ {
result[i] = model.MostDependedOn{
Repo:             entries[i].repo,
DirectDependents: entries[i].count,
}
}
return result
}

// TypeCounts returns edge counts by dependency type.
// EdgeCount returns the total number of edges in the graph.
func (g *Graph) EdgeCount() int {
	count := 0
	for _, edges := range g.adjList {
		count += len(edges)
	}
	return count
}

func (g *Graph) TypeCounts() map[model.DependencyType]int {
counts := make(map[model.DependencyType]int)
for _, edges := range g.adjList {
for _, edge := range edges {
counts[edge.Type]++
}
}
return counts
}
