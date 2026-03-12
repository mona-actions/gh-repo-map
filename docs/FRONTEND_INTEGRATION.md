# Frontend Integration Guide

This document describes the JSON output contract produced by `gh-repo-map` and how a frontend application should consume it.

## Overview

`gh-repo-map` produces a self-contained JSON file (or set of split files) that fully describes the inter-repository dependency graph across one or more GitHub organizations. The frontend is a **read-only consumer** — it loads the JSON and visualizes it.

```
┌──────────────┐         ┌────────────┐         ┌──────────────────┐
│ gh repo-map  │──JSON──▶│  Storage   │◀────────│  Web Dashboard   │
│    (CLI)     │         │(file / S3) │         │  (React / Vue)   │
└──────────────┘         └────────────┘         └──────────────────┘
```

## Output Schema (v1.0.0)

The top-level JSON structure:

```jsonc
{
  "schema_version": "1.0.0",
  "metadata": { /* scan context */ },
  "graph": { /* repo → dependencies map */ },
  "unresolved": { /* packages that couldn't be mapped to a repo */ },
  "stats": { /* pre-computed analytics */ }
}
```

---

### `metadata`

Scan context and timing. Use this for dashboard headers and freshness indicators.

| Field | Type | Description |
|-------|------|-------------|
| `generated_at` | `string` (ISO 8601) | When the scan completed |
| `tool_version` | `string` | CLI version that produced this file |
| `github_host` | `string` | GitHub hostname (`""` = github.com) |
| `orgs_scanned` | `string[]` | List of organizations scanned |
| `total_repos` | `number` | Total repos discovered |
| `total_repos_scanned` | `number` | Repos successfully scanned |
| `total_repos_skipped` | `number` | Repos skipped (errors, empty) |
| `total_edges` | `number` | Total dependency edges in the graph |
| `scan_duration_seconds` | `number` | How long the scan took |
| `split_info` | `object` | File splitting details (see below) |

#### `split_info`

When the output is split across multiple files:

```json
{
  "mode": "per-org",
  "file_index": 1,
  "total_files": 5,
  "this_file_orgs": ["my-org"]
}
```

**Modes:** `merged` (single file), `per-org` (one file per org), `auto` (merged if <500 repos, per-org otherwise).

If loading split files, the frontend should merge the `graph` maps from all files. Metadata and stats are per-file.

---

### `graph`

The core data structure. A map of `"org/repo-name"` → `RepoNode`.

```jsonc
{
  "graph": {
    "my-org/api-service": {
      "scan_status": {
        "sbom": "done",
        "filescan": "done"
      },
      "annotations": {
        "fork_of": null,
        "template_from": null,
        "archived": false
      },
      "direct": [
        {
          "repo": "my-org/shared-lib",
          "type": "package",
          "confidence": "high",
          "target_scanned": true,
          "source_file": "package.json",
          "detail": {
            "package_name": "@my-org/shared-lib",
            "ecosystem": "npm",
            "version": "^2.0.0"
          }
        },
        {
          "repo": "my-org/ci-workflows",
          "type": "workflow",
          "confidence": "high",
          "target_scanned": true,
          "source_file": ".github/workflows/ci.yml",
          "detail": {
            "uses": "my-org/ci-workflows/.github/workflows/build.yml@main"
          }
        }
      ],
      "transitive": [
        {
          "repo": "my-org/core-utils",
          "via": ["my-org/shared-lib"],
          "type": "package",
          "depth": 2
        }
      ]
    }
  }
}
```

#### Dependency Types

| `type` | Description | `detail` shape |
|--------|-------------|----------------|
| `package` | Package manager dependency (npm, Go, Maven, etc.) | `{ package_name, ecosystem, version }` |
| `workflow` | GitHub Actions reusable workflow | `{ uses }` |
| `action` | GitHub Actions action | `{ uses }` |
| `submodule` | Git submodule | `{ url, path }` |
| `docker` | Docker image from a known registry | `{ image }` |
| `terraform` | Terraform module source | `{ source, ref? }` |
| `script` | Reference found in build scripts (lower confidence) | `{ match, match_type }` |

#### Confidence Levels

- **`high`** — Parsed from structured files (manifests, YAML, SBOM). Reliable.
- **`low`** — Extracted via regex from scripts/Makefiles. May have false positives.

The frontend should visually distinguish these (e.g., solid vs dashed edges).

#### `target_scanned`

Indicates whether the target repo was also scanned. If `false`, the edge points to a repo outside the scanned orgs or one that failed scanning. Useful for identifying boundary dependencies.

---

### `unresolved`

Packages that were consumed but couldn't be mapped to any scanned repository.

```json
{
  "unresolved": {
    "my-org/api-service": [
      {
        "package_name": "lodash",
        "ecosystem": "npm",
        "version": "^4.17.21",
        "reason": "no_matching_repo"
      }
    ]
  }
}
```

These are typically third-party/external packages. The frontend can show these separately or offer filtering.

---

### `stats`

Pre-computed analytics — the frontend does **not** need to recompute these.

```jsonc
{
  "stats": {
    "most_depended_on": [
      { "repo": "my-org/shared-lib", "direct_dependents": 42 }
    ],
    "dependency_type_counts": {
      "package": 150,
      "workflow": 30,
      "action": 25,
      "docker": 10,
      "submodule": 5,
      "terraform": 3,
      "script": 8
    },
    "clusters": [
      { "id": 1, "repos": ["my-org/a", "my-org/b", "my-org/c"], "size": 3 }
    ],
    "circular_deps": [
      ["my-org/svc-a", "my-org/svc-b"]
    ],
    "orphan_repos": ["my-org/abandoned-project"]
  }
}
```

| Field | Frontend Use Case |
|-------|-------------------|
| `most_depended_on` | Highlight critical repos (blast radius analysis) |
| `dependency_type_counts` | Summary pie/bar chart |
| `clusters` | Connected components — these are **migration units** |
| `circular_deps` | Cycles that need attention before migration |
| `orphan_repos` | Repos with zero connections (easy to migrate independently) |

---

## Frontend Implementation Recommendations

### Loading the Data

```typescript
async function loadRepoMap(file: File): Promise<OutputSchema> {
  const text = await file.text();
  const data = JSON.parse(text);

  if (data.schema_version !== "1.0.0") {
    throw new Error(`Unsupported schema version: ${data.schema_version}`);
  }
  return data;
}

// For split files, merge the graph maps
function mergeFiles(files: OutputSchema[]): OutputSchema {
  const merged = { ...files[0] };
  merged.graph = {};
  merged.unresolved = {};

  for (const file of files) {
    Object.assign(merged.graph, file.graph);
    Object.assign(merged.unresolved, file.unresolved);
  }
  merged.metadata.total_repos = Object.keys(merged.graph).length;
  return merged;
}
```

### Building a Graph Data Structure

Convert the flat map into nodes and edges for your visualization library:

```typescript
interface GraphNode {
  id: string;          // "org/repo"
  org: string;
  archived: boolean;
  directDeps: number;  // outbound edge count
  dependents: number;  // inbound edge count
}

interface GraphEdge {
  source: string;
  target: string;
  type: string;        // "package" | "workflow" | "action" | ...
  confidence: string;  // "high" | "low"
}

function buildGraph(data: OutputSchema) {
  const nodes: GraphNode[] = [];
  const edges: GraphEdge[] = [];
  const inbound = new Map<string, number>();

  for (const [fullName, repo] of Object.entries(data.graph)) {
    const [org] = fullName.split("/");
    nodes.push({
      id: fullName,
      org,
      archived: repo.annotations.archived,
      directDeps: repo.direct?.length ?? 0,
      dependents: 0,
    });

    for (const dep of repo.direct ?? []) {
      edges.push({
        source: fullName,
        target: dep.repo,
        type: dep.type,
        confidence: dep.confidence,
      });
      inbound.set(dep.repo, (inbound.get(dep.repo) ?? 0) + 1);
    }
  }

  for (const node of nodes) {
    node.dependents = inbound.get(node.id) ?? 0;
  }

  return { nodes, edges };
}
```

---

## Suggested Views

| View | Data Source | Description |
|------|------------|-------------|
| **Overview Dashboard** | `metadata` + `stats` | High-level numbers, freshness, scan coverage |
| **Dependency Graph** | `graph` → nodes + edges | Interactive force-directed or hierarchical layout |
| **Cluster View** | `stats.clusters` | Connected components — migration units |
| **Critical Repos** | `stats.most_depended_on` | Repos with highest blast radius |
| **Circular Dependencies** | `stats.circular_deps` | Cycles that block clean migration ordering |
| **Orphan Repos** | `stats.orphan_repos` | Repos with no connections (migrate independently) |
| **Unresolved Packages** | `unresolved` | External dependencies outside scanned orgs |
| **Repo Detail** | `graph[repo]` | Single repo: all inbound + outbound edges |

---

## Recommended Visualization Libraries

| Library | Best For | Scale Limit |
|---------|----------|-------------|
| [Cytoscape.js](https://js.cytoscape.org/) | Large graphs, clustering, filtering | 5,000+ nodes |
| [Sigma.js](https://www.sigmajs.org/) | Very large graphs (WebGL) | 10,000+ nodes |
| [D3-force](https://d3js.org/) | Custom visualizations | ~2,000 nodes |
| [React Flow](https://reactflow.dev/) | React apps, drag-and-drop | ~500 nodes |

For enterprise scale (5,000+ repos), **Cytoscape.js or Sigma.js** are recommended.

---

## Filtering & UX Suggestions

The frontend should support filtering by:

- **Organization** — Show/hide repos by org
- **Dependency type** — Toggle package/workflow/action/docker/etc.
- **Confidence** — Hide low-confidence (script-based) edges
- **Archived** — Dim or hide archived repos
- **Cluster** — Isolate a specific connected component
- **Search** — Find a repo and highlight its N-hop neighborhood

### Color Coding

| Element | Encoding |
|---------|----------|
| Node fill | By organization (categorical palette) |
| Node size | By `dependents` count (larger = more critical) |
| Node border | Scan status (green=done, yellow=partial, red=failed) |
| Edge color | By dependency type |
| Edge style | Solid = high confidence, dashed = low confidence |
| Node opacity | Dim archived repos |

---

## Schema Versioning

The `schema_version` field follows semver:

- **Patch** (1.0.x): Additive fields only, no breaking changes
- **Minor** (1.x.0): New optional sections, existing fields stable
- **Major** (x.0.0): Breaking changes to existing field shapes

The frontend should check `schema_version` on load and warn on unsupported major versions.

---

## Minimal Working Frontend

A minimal frontend needs only:

1. **File input** — upload the JSON
2. **Schema validation** — check `schema_version`
3. **Stats cards** — render `stats` as summary numbers
4. **Graph view** — render `graph` as a node-link diagram
5. **Node click** — show `direct` dependencies for a selected repo

This can be built as a **static SPA with zero backend** — the user uploads the JSON file directly in the browser.
