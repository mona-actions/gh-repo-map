# gh-repo-map

A `gh` CLI extension that maps inter-repository dependencies across GitHub organizations at enterprise scale.

Scans thousands of repositories to discover how they depend on each other — through packages, GitHub Actions workflows, submodules, Docker images, Terraform modules, and build scripts — and produces a JSON dependency graph for visualization and migration planning.

The output files can be uploaded to https://github.com/mona-actions/gh-repomap-dashboard for visualization of your organization's dependency graph, with insights on critical repos.

> [!WARNING]
> This tools is in an technical preview state(Alpha). It is not yet ready for mass consumption and may contain bugs or incomplete features. Use with caution and provide feedback to @amenocal.

## Why

- **Migration planning** — Know which repos must move together and in what order
- **Blast radius analysis** — Understand which repos are critical and what breaks if they change
- **Security posture** — Map how a vulnerability in one repo propagates across your organization

## What It Detects

| Dependency Type | Source | Confidence |
|----------------|--------|------------|
| **Packages** | SBOM API + manifest files (npm, Go, Maven, Python, NuGet, Rust, Ruby, PHP) | High |
| **Reusable Workflows** | `.github/workflows/*.yml` | High |
| **Actions** | `.github/workflows/*.yml` | High |
| **Submodules** | `.gitmodules` | High |
| **Docker Images** | `Dockerfile`, `docker-compose.yml` | High |
| **Terraform Modules** | `*.tf` files | High |
| **Build Scripts** | `Makefile`, `*.sh` (git clone, curl, wget, go install, pip git) | Low |

## Installation

```bash
gh extension install mona-actions/gh-repo-map
```

## Quick Start

**Zero-config** — if you're already authenticated with `gh auth login`:

```bash
# Scan a single org
echo "my-org" > orgs.txt
gh repo-map --orgs-file orgs.txt --dry-run

# Run the full scan
gh repo-map --orgs-file orgs.txt
```

**With a config file** (for advanced options):

```bash
cp config.example.yml config.yml
# Edit config.yml with your orgs and settings
gh repo-map
```

## Authentication

Authentication is resolved automatically in this priority order:

1. CLI flags (`--token` or `--app-id` + `--private-key-path`)
2. Environment variables (`GH_TOKEN` or `GH_APP_ID` + `GH_APP_PRIVATE_KEY`)
3. `gh auth login` (automatic if the `gh` CLI is authenticated)

### Using gh auth (Simplest)

If you've already run `gh auth login`, no extra config is needed:

```bash
gh repo-map --orgs-file orgs.txt
```

### Using a Personal Access Token

```bash
# Via flag
gh repo-map --token ghp_xxxxxxxxxxxx --orgs-file orgs.txt

# Via environment variable
export GH_TOKEN=ghp_xxxxxxxxxxxx
gh repo-map --orgs-file orgs.txt
```

**Required token scopes:** `repo` (for private repos) or `public_repo` (for public only)

### Using a GitHub App (Recommended for Scale)

GitHub Apps get 5,000 requests/hour **per installation** (vs 5,000 total for PATs), making them ideal for scanning thousands of repos.

**Via CLI flags:**

```bash
gh repo-map \
  --app-id 123456 \
  --private-key-path ./my-app.pem \
  --orgs-file orgs.txt
```

**Via environment variables:**

```bash
export GH_APP_ID=123456
export GH_APP_PRIVATE_KEY="$(cat ./my-app.pem)"
gh repo-map --orgs-file orgs.txt
```

**Via config file:**

```yaml
# config.yml
auth:
  type: "github-app"
  app_id: 123456
  private_key_path: "./my-app.pem"
```

> **Note:** `GH_APP_PRIVATE_KEY` accepts the PEM content directly (not a file path), which is useful for CI/CD secrets. The `--private-key-path` flag and `auth.private_key_path` config accept a file path.

**Required App permissions:** Repository contents: `read`, Metadata: `read`, Dependency graph: `read`

**Setup steps:**
1. Create a GitHub App at `https://github.com/settings/apps/new` (or your GHES instance)
2. Set permissions: Repository → Contents: `Read-only`, Metadata: `Read-only`
3. Generate a private key and download the `.pem` file
4. Install the App on each organization you want to scan

## Specifying Organizations

### Via `--orgs-file` (recommended for many orgs)

Create a text file with one org per line:

```
# orgs.txt — lines starting with # are comments
my-org
my-other-org
subsidiary-org
```

```bash
gh repo-map --orgs-file orgs.txt
```

### Via config file

```yaml
# config.yml
orgs:
  - my-org
  - my-other-org
```

Both methods can be combined — `--orgs-file` orgs are appended to config orgs (duplicates are removed).

## GHES Support

Works with GitHub Enterprise Server 3.6+ (required for SBOM API):

```bash
gh repo-map --github-host github.example.com --orgs-file orgs.txt
```

Or in config:

```yaml
github_host: "github.example.com"
```

## Configuration

See [`config.example.yml`](config.example.yml) for the full annotated config. A config file is **optional** — you can use CLI flags for everything.

### Environment Variables

| Variable | Purpose |
|----------|---------|
| `GH_TOKEN` | Personal access token (alternative to `--token`) |
| `GH_APP_ID` | GitHub App ID (alternative to `--app-id`) |
| `GH_APP_PRIVATE_KEY` | GitHub App private key PEM **content** (alternative to `--private-key-path`) |
| `REPO_MAP_VENDORED_DIRS` | Extra vendored directory names for file scan (comma-separated, appended/deduped) |
| `REPO_MAP_SCRIPT_DIRS` | Extra script directory names for `*.sh` detection (comma-separated, appended/deduped) |

### File Scan Target Directories

Use the top-level `scan` config section to control directory matching for file scan:

- `scan.vendored_dirs` — directory names excluded as vendored third-party code
- `scan.script_dirs` — directory names where `*.sh` files are treated as build/script targets

If omitted, defaults match the built-in behavior. `REPO_MAP_VENDORED_DIRS` and `REPO_MAP_SCRIPT_DIRS` append to configured values and remove duplicates.

### Overrides File

Manually correct package→repo mappings when automatic detection fails. See [`overrides.example.yml`](overrides.example.yml).

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-t`, `--token` | *(auto from gh auth)* | GitHub personal access token |
| `--app-id` | | GitHub App ID (for App auth) |
| `--private-key-path` | | Path to GitHub App private key `.pem` file |
| `--orgs-file` | | Path to text file with org names (one per line) |
| `--github-host` | `github.com` | GitHub hostname (set for GHES) |
| `--config` | | Path to config.yml (optional) |
| `--dry-run` | `false` | Enumerate repos and print estimates only |
| `--resume` | `false` | Resume from latest checkpoint |
| `--include-transitive` | `false` | Compute transitive dependency chains |
| `--concurrency` | `4` | Max concurrent org workers (1-10) |
| `--min-coverage` | `80` | Min % repos scanned before output (0-100) |
| `--split-threshold` | `0` | Max repos per output file (0 = unlimited) |
| `--clean-checkpoints` | `false` | Delete checkpoint file after success |
| `--log-level` | `default` | `quiet` \| `default` \| `verbose` \| `debug` |
| `--log-file` | | Write logs to file |

## How It Works

```
Phase 1: Enumerate     List all repos across configured orgs (go-github REST)
    │
Phase 2A: SBOM         Fetch dependency data via GitHub's SBOM API (SPDX)
    │
Phase 2B: File Scan    Discover files via Git Trees API, fetch via githubv4 GraphQL,
    │                  parse workflows, Dockerfiles, Terraform, scripts, manifests
    │
Phase 3: Cross-Ref     Build a publish registry from manifest files,
    │                  match consumed packages to source repos using purl normalization
    │
Phase 4: Output        Write JSON with graph, stats, and unresolved packages
```

### Checkpoint & Resume

Long-running scans are checkpointed to disk every N repos (default: 10). If a scan is interrupted:

```bash
gh repo-map --resume
```

### Adding More Orgs?

Already ran a scan with 2 orgs and need to add more? Just update your org list and resume — no need to re-scan everything:

```bash
# Original scan with 2 orgs
echo -e "org-a\norg-b" > orgs.txt
gh repo-map --orgs-file orgs.txt

# Later: add 2 more orgs to the file
echo -e "org-a\norg-b\norg-c\norg-d" > orgs.txt

# Resume — only scans the new orgs, then re-resolves all cross-org dependencies
gh repo-map --orgs-file orgs.txt --resume
```

The `--resume` flag detects which orgs are new, enumerates and scans only those repos, then re-runs dependency resolution across **all** orgs. This means cross-org dependencies (e.g., org-c consuming a package published by org-a) are automatically discovered without re-scanning org-a and org-b.

### Scale Considerations

| Repos | Estimated Time | Recommendation |
|-------|---------------|----------------|
| < 500 | Minutes | Default settings |
| 500 – 5,000 | 30-60 min | Use GitHub App, `--concurrency 4` |
| 5,000 – 50,000 | Hours | Use GitHub App, `--concurrency 8`, `--resume` on interruption |

Rate limiting is handled automatically by [go-github-ratelimit](https://github.com/gofri/go-github-ratelimit).

## Output

The output is a self-contained JSON file following the [Output Schema v1.0.0](docs/FRONTEND_INTEGRATION.md).

### Example

```jsonc
{
  "schema_version": "1.0.0",
  "metadata": {
    "generated_at": "2025-01-15T10:30:00Z",
    "orgs_scanned": ["my-org"],
    "total_repos": 150,
    "total_edges": 420
  },
  "graph": {
    "my-org/api-service": {
      "direct": [
        {
          "repo": "my-org/shared-lib",
          "type": "package",
          "confidence": "high",
          "detail": { "package_name": "@my-org/shared-lib", "ecosystem": "npm" }
        }
      ]
    }
  },
  "stats": {
    "most_depended_on": [{ "repo": "my-org/shared-lib", "direct_dependents": 42 }],
    "clusters": [{ "id": 1, "repos": ["my-org/a", "my-org/b"], "size": 2 }],
    "circular_deps": [["my-org/svc-a", "my-org/svc-b"]],
    "orphan_repos": ["my-org/standalone-tool"]
  }
}
```

### Consuming the Output

The JSON is designed to be consumed by a separate frontend/dashboard. See [**Frontend Integration Guide**](docs/FRONTEND_INTEGRATION.md) for the full schema reference, TypeScript examples, and visualization recommendations.

## Dependencies

| Package | Purpose |
|---------|---------|
| [google/go-github](https://github.com/google/go-github) | GitHub REST API client |
| [shurcooL/githubv4](https://github.com/shurcooL/githubv4) | GitHub GraphQL API client |
| [jferrl/go-githubauth](https://github.com/jferrl/go-githubauth) | GitHub App JWT + installation token auth |
| [gofri/go-github-ratelimit](https://github.com/gofri/go-github-ratelimit) | Automatic rate limit handling |
| [cli/go-gh](https://github.com/cli/go-gh) | `gh` CLI auth token resolution |
| [spf13/cobra](https://github.com/spf13/cobra) | CLI framework |

## Project Structure

```
gh-repo-map/
├── cmd/root.go                    # CLI entrypoint and flag definitions
├── internal/
│   ├── model/                     # All shared types (single source of truth)
│   ├── config/                    # YAML config loading + validation
│   ├── auth/                      # GitHub auth (go-github, githubv4, go-githubauth)
│   ├── enumerate/                 # Org repo listing via go-github
│   ├── sbom/                      # SBOM API client via go-github
│   ├── filescan/                  # Git Trees + githubv4 batch file fetch
│   ├── parse/                     # Parsers: actions, docker, terraform, scripts, manifests, submodules
│   ├── purl/                      # Package URL normalization (8 ecosystems)
│   ├── registry/                  # In-memory package→repo lookup with overrides
│   ├── graph/                     # Graph construction, BFS transitive, DFS cycles, clusters
│   ├── checkpoint/                # Atomic checkpoint read/write with mutex
│   ├── output/                    # JSON output generation and splitting
│   └── orchestrator/              # Pipeline coordinator (Phase 1→2A→2B→3→4)
├── config.example.yml             # Annotated config template
├── overrides.example.yml          # Package→repo override template
└── docs/
    └── FRONTEND_INTEGRATION.md    # Frontend/dashboard integration guide
```

## License

[MIT](LICENSE)
