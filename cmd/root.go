// Package cmd implements the CLI command tree for gh-repo-map.
package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mona-actions/gh-repo-map/internal/auth"
	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/model"
	"github.com/mona-actions/gh-repo-map/internal/orchestrator"
	"github.com/mona-actions/gh-repo-map/internal/progress"
	"github.com/spf13/cobra"
)

const version = "0.1.0"

type cliOptions struct {
	ConfigPath        string
	GitHubHost        string
	Token             string
	AppID             int64
	PrivateKeyPath    string
	OrgsFile          string
	DryRun            bool
	Resume            bool
	IncludeTransitive bool
	Anonymize         bool
	CleanCheckpoints  bool
	ShowStatus        bool
	SplitThreshold    int
	Concurrency       int
	MinCoverage       int
	LogLevel          string
}

type configLoader func(path string) (*config.Config, error)
type orgsFileReader func(path string) ([]string, error)
type warningPrinter func(format string, args ...any)
type tokenProviderFactory func(authType, token string, appID int64, privateKeyPath, host string) (auth.TokenProvider, error)
type ptermTrackerFactory func() *progress.PtermTracker
type logTrackerFactory func() *progress.LogTracker

var (
	cfgPath           string
	githubHost        string
	token             string
	appID             int64
	privateKeyPath    string
	orgsFile          string
	dryRun            bool
	resume            bool
	includeTransitive bool
	anonymize         bool
	cleanCheckpoints  bool
	showStatus        bool
	splitThreshold    int
	concurrency       int
	minCoverage       int
	logLevel          string
)

var rootCmd = &cobra.Command{
	Use:   "gh-repo-map",
	Short: "Map inter-repository dependencies across GitHub organizations",
	Long: `gh-repo-map scans repositories across multiple GitHub organizations
to discover and map inter-repository dependencies including packages,
Actions, reusable workflows, submodules, Dockerfiles, Terraform modules,
and build scripts.

Authentication (in priority order):
  1. --token flag or --app-id + --private-key-path flags
  2. GH_TOKEN / GH_APP_ID + GH_APP_PRIVATE_KEY environment variables
  3. gh auth login (automatic if gh CLI is authenticated)

For enterprise scale (5,000+ repos), use GitHub App auth for higher rate limits.`,
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          run,
}

func init() {
	// Config
	rootCmd.Flags().StringVar(&cfgPath, "config", "", "Path to config.yml (optional if using flags)")

	// Auth
	rootCmd.Flags().StringVarP(&token, "token", "t", "", "GitHub personal access token (or set GH_TOKEN)")
	rootCmd.Flags().Int64Var(&appID, "app-id", 0, "GitHub App ID (or set GH_APP_ID)")
	rootCmd.Flags().StringVar(&privateKeyPath, "private-key-path", "", "Path to GitHub App private key PEM file (or set GH_APP_PRIVATE_KEY)")

	// Target
	rootCmd.Flags().StringVar(&githubHost, "github-host", "", "GitHub hostname (default: github.com, set for GHES)")
	rootCmd.Flags().StringVar(&orgsFile, "orgs-file", "", "Path to text file with org names (one per line)")

	// Behavior
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Enumerate repos only, print estimates, and exit")
	rootCmd.Flags().BoolVar(&resume, "resume", false, "Resume from latest checkpoint file")
	rootCmd.Flags().BoolVar(&includeTransitive, "include-transitive", false, "Compute transitive dependencies")
	rootCmd.Flags().BoolVar(&cleanCheckpoints, "clean-checkpoints", false, "Delete checkpoint after success")
	rootCmd.Flags().BoolVar(&showStatus, "status", false, "Show progress from checkpoint and exit")
	rootCmd.Flags().IntVar(&concurrency, "concurrency", 0, "Max concurrent org workers (1-10)")
	rootCmd.Flags().IntVar(&minCoverage, "min-coverage", 0, "Min % repos scanned before output (0-100)")
	rootCmd.Flags().IntVar(&splitThreshold, "split-threshold", 0, "Max repos per output file")

	// Output
	rootCmd.Flags().StringVar(&logLevel, "log-level", "", "Log level: quiet|default|verbose|debug")
}

// Execute is the CLI entrypoint called from main.
func Execute() error {
	return rootCmd.Execute()
}

func run(cmd *cobra.Command, args []string) error {
	_ = cmd
	_ = args

	fmt.Fprintf(os.Stderr, "gh-repo-map v%s\n", version)

	opts := currentCLIOptions()
	cfg, err := loadConfigWithFallback(opts.ConfigPath, "./config.yml", config.Load, fileExists)
	if err != nil {
		return err
	}

	applyCLIOverrides(cfg, opts, readOrgsFile, func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, format, args...)
	})

	// --status: read checkpoint and print progress summary, then exit
	if opts.ShowStatus {
		return printStatus(cfg)
	}

	if err := config.Validate(cfg, opts.Resume); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	host := cfg.GitHubHost
	if host == "" {
		host = "github.com"
	}
	fmt.Fprintf(os.Stderr, "Target: %s | Orgs: %d | Concurrency: %d\n",
		host, len(cfg.Orgs), cfg.Options.Concurrency)

	tp, err := buildTokenProvider(cfg, auth.NewTokenProvider)
	if err != nil {
		return fmt.Errorf("initializing auth: %w", err)
	}

	tracker := selectTracker(cfg.Options.LogLevel, progress.NewPtermTracker, progress.NewLogTracker)

	ctx := context.Background()
	if opts.DryRun {
		return orchestrator.DryRun(ctx, cfg, tp, tracker)
	}

	return orchestrator.Run(ctx, cfg, tp, tracker, opts.Resume, opts.IncludeTransitive, opts.CleanCheckpoints, opts.SplitThreshold)
}

func currentCLIOptions() cliOptions {
	return cliOptions{
		ConfigPath:        cfgPath,
		GitHubHost:        githubHost,
		Token:             token,
		AppID:             appID,
		PrivateKeyPath:    privateKeyPath,
		OrgsFile:          orgsFile,
		DryRun:            dryRun,
		Resume:            resume,
		IncludeTransitive: includeTransitive,
		Anonymize:         anonymize,
		CleanCheckpoints:  cleanCheckpoints,
		ShowStatus:        showStatus,
		SplitThreshold:    splitThreshold,
		Concurrency:       concurrency,
		MinCoverage:       minCoverage,
		LogLevel:          logLevel,
	}
}

// loadConfigWithFallback builds a Config from file and/or CLI flags.
func loadConfigWithFallback(explicitPath, defaultPath string, loadFn configLoader, existsFn func(string) bool) (*config.Config, error) {
	if explicitPath != "" {
		cfg, err := loadFn(explicitPath)
		if err != nil {
			return nil, fmt.Errorf("loading config: %w", err)
		}
		return cfg, nil
	}

	if existsFn(defaultPath) {
		cfg, err := loadFn(defaultPath)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", defaultPath, err)
		}
		return cfg, nil
	}

	cfg := &config.Config{}
	config.ApplyDefaults(cfg)
	return cfg, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// applyCLIOverrides applies CLI flag values onto the loaded config.
// Flags always take precedence over config file values.
func applyCLIOverrides(cfg *config.Config, opts cliOptions, readOrgsFn orgsFileReader, warn warningPrinter) {
	// Auth: --token flag > GH_TOKEN env > config file > gh auth login
	if opts.Token != "" {
		cfg.Auth.Token = opts.Token
		cfg.Auth.Type = "pat"
	}

	// GitHub App flags override config file app settings
	if opts.AppID > 0 {
		cfg.Auth.AppID = opts.AppID
		cfg.Auth.Type = "github-app"
	}
	if opts.PrivateKeyPath != "" {
		cfg.Auth.PrivateKeyPath = opts.PrivateKeyPath
		if cfg.Auth.AppID > 0 {
			cfg.Auth.Type = "github-app"
		}
	}

	if opts.GitHubHost != "" {
		cfg.GitHubHost = opts.GitHubHost
	}

	// Orgs: --orgs-file appends to config orgs
	if opts.OrgsFile != "" {
		fileOrgs, err := readOrgsFn(opts.OrgsFile)
		if err != nil {
			warn("Warning: failed to read orgs file: %v\n", err)
		} else {
			cfg.Orgs = append(cfg.Orgs, fileOrgs...)
		}
	}

	// Deduplicate orgs
	cfg.Orgs = dedup(cfg.Orgs)

	if opts.IncludeTransitive {
		cfg.Output.IncludeTransitive = true
	}
	if opts.Concurrency > 0 {
		cfg.Options.Concurrency = opts.Concurrency
	}
	if opts.MinCoverage > 0 {
		cfg.Options.MinCoverage = opts.MinCoverage
	}
	if opts.LogLevel != "" {
		cfg.Options.LogLevel = opts.LogLevel
	}
}

func buildTokenProvider(cfg *config.Config, factory tokenProviderFactory) (auth.TokenProvider, error) {
	return factory(cfg.Auth.Type, cfg.Auth.Token, cfg.Auth.AppID, cfg.Auth.PrivateKeyPath, cfg.GitHubHost)
}

func selectTracker(logLevel string, ptermFactory ptermTrackerFactory, logFactory logTrackerFactory) progress.Tracker {
	switch logLevel {
	case "verbose", "debug":
		return logFactory()
	default:
		return ptermFactory()
	}
}

// readOrgsFile reads org names from a text file (one per line).
// Lines starting with # are treated as comments. Empty lines are skipped.
func readOrgsFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var orgs []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		orgs = append(orgs, line)
	}
	return orgs, scanner.Err()
}

func dedup(items []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func printStatus(cfg *config.Config) error {
	cpPath := filepath.Join(filepath.Dir(cfg.Output.Path), "checkpoint.json")
	data, err := os.ReadFile(cpPath)
	if err != nil {
		return fmt.Errorf("no checkpoint found at %s — is a scan in progress?", cpPath)
	}

	var cp model.Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return fmt.Errorf("parsing checkpoint: %w", err)
	}

	pending, done, failed, skipped := 0, 0, 0, 0
	for _, repo := range cp.Repos {
		switch {
		case repo.Status.SBOM == model.ScanStatusDone || repo.Status.FileScan == model.ScanStatusDone:
			done++
		case repo.Status.SBOM == model.ScanStatusFailed || repo.Status.FileScan == model.ScanStatusFailed:
			failed++
		case repo.Archived:
			skipped++
		default:
			pending++
		}
	}

	total := len(cp.Repos)
	coverage := 0
	if total > 0 {
		coverage = (done * 100) / total
	}

	fmt.Printf("Checkpoint: %s (v%s)\n", cpPath, cp.Version)
	fmt.Printf("Phase:      %s\n", cp.CurrentPhase)
	fmt.Printf("Host:       %s\n", cp.GitHubHost)
	fmt.Printf("Orgs:       %s\n", strings.Join(cp.Orgs, ", "))
	fmt.Printf("Started:    %s\n", cp.StartedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Printf("Repos:      %d total\n", total)
	fmt.Printf("  Done:     %d (%d%%)\n", done, coverage)
	fmt.Printf("  Pending:  %d\n", pending)
	fmt.Printf("  Failed:   %d\n", failed)
	fmt.Printf("  Skipped:  %d\n", skipped)
	return nil
}
