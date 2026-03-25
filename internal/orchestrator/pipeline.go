package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/mona-actions/gh-repo-map/internal/auth"
	"github.com/mona-actions/gh-repo-map/internal/checkpoint"
	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/progress"
)

// Pipeline coordinates the full scan pipeline:
// Phase 1 (enumerate) → Phase 2A (SBOM) → Phase 2B (file scan) → Phase 3 (cross-ref) → Phase 4 (output).
type Pipeline struct {
	cfg               *config.Config
	tp                auth.TokenProvider
	tracker           progress.Tracker
	resumeMode        bool
	includeTransitive bool
	cleanCp           bool
	splitThreshold    int
}

// NewPipeline creates a Pipeline with runtime dependencies and options.
func NewPipeline(cfg *config.Config, tp auth.TokenProvider, tracker progress.Tracker, resumeMode bool, includeTransitive bool, cleanCp bool, splitThreshold int) *Pipeline {
	return &Pipeline{
		cfg:               cfg,
		tp:                tp,
		tracker:           tracker,
		resumeMode:        resumeMode,
		includeTransitive: includeTransitive,
		cleanCp:           cleanCp,
		splitThreshold:    splitThreshold,
	}
}

// Run executes the full scan pipeline.
func Run(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, tracker progress.Tracker, resumeMode bool, includeTransitive bool, cleanCp bool, splitThreshold int) error {
	return NewPipeline(cfg, tp, tracker, resumeMode, includeTransitive, cleanCp, splitThreshold).Run(ctx)
}

// DryRun executes Phase 1 only and prints estimates.
func DryRun(ctx context.Context, cfg *config.Config, tp auth.TokenProvider, tracker progress.Tracker) error {
	return NewPipeline(cfg, tp, tracker, false, false, false, 0).DryRun(ctx)
}

// Run executes all pipeline phases and writes output.
func (p *Pipeline) Run(ctx context.Context) error {
	startTime := time.Now()
	defer p.tracker.Stop()

	outputDir := filepath.Dir(p.cfg.Output.Path)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	cpPath := filepath.Join(outputDir, "checkpoint.json")
	cpMgr := checkpoint.NewManager(cpPath, p.cfg.Options.CheckpointFlushInterval)

	if p.resumeMode {
		if err := cpMgr.Load(); err != nil {
			return fmt.Errorf("loading checkpoint: %w", err)
		}
		slog.Info("resumed from checkpoint", "repos", len(cpMgr.Data().Repos))

		missingOrgs := findMissingOrgs(p.cfg.Orgs, cpMgr.Data().Orgs, cpMgr.Data().Repos)
		if len(missingOrgs) > 0 {
			slog.Info("enumerating missing orgs", "orgs", missingOrgs)
			missingCfg := *p.cfg
			missingCfg.Orgs = missingOrgs
			enumStats, err := runEnumerateMissingOrgsPhase(ctx, &missingCfg, p.tp, cpMgr, p.tracker)
			if err != nil {
				return fmt.Errorf("enumerating missing orgs: %w", err)
			}
			slog.Info("missing orgs enumerated", "added", enumStats.Enumerated)
		}
	} else {
		enumStats, err := runEnumeratePhase(ctx, p.cfg, p.tp, cpMgr, p.tracker)
		if err != nil {
			return fmt.Errorf("phase 1 (enumerate): %w", err)
		}
		p.tracker.EndEnumerate(enumStats)
	}

	cp := cpMgr.Data()
	if len(cp.Repos) == 0 {
		return fmt.Errorf("no repositories found — check org names and token permissions")
	}

	if err := runScanPhase(ctx, p.cfg, p.tp, cpMgr, p.tracker); err != nil {
		return fmt.Errorf("phase 2 (scan): %w", err)
	}

	cp = cpMgr.Data()
	scanned, total := countScanned(cp)
	coverage := 0
	if total > 0 {
		coverage = (scanned * 100) / total
	}
	p.tracker.EndScan(scanned, total)

	if coverage < p.cfg.Options.MinCoverage {
		return fmt.Errorf("coverage %d%% below minimum %d%% — use --resume to continue or --min-coverage 0 to force", coverage, p.cfg.Options.MinCoverage)
	}

	p.tracker.StartCrossRef()
	results, err := cpMgr.LoadResults()
	if err != nil {
		return fmt.Errorf("loading scan results: %w", err)
	}
	cpMgr.Close()

	g, unresolved := runMapPhase(p.cfg, cp, results)
	if p.includeTransitive {
		g.ComputeTransitive()
	}

	totalEdges := g.EdgeCount()
	totalUnresolved := 0
	for _, u := range unresolved {
		totalUnresolved += len(u)
	}
	p.tracker.EndCrossRef(totalEdges, totalUnresolved)

	p.tracker.StartOutput()
	outputPath, err := runReportPhase(p.cfg, g, unresolved, startTime, p.splitThreshold)
	if err != nil {
		return err
	}
	p.tracker.EndOutput(outputPath, len(g.Nodes), totalEdges)

	if p.cleanCp {
		os.Remove(cpPath)
		slog.Debug("checkpoint cleaned")
	}

	return nil
}

// DryRun executes Phase 1 only and prints estimates.
func (p *Pipeline) DryRun(ctx context.Context) error {
	defer p.tracker.Stop()

	cpMgr := checkpoint.NewManager("", 0)
	enumStats, err := runEnumeratePhase(ctx, p.cfg, p.tp, cpMgr, p.tracker)
	if err != nil {
		return err
	}
	p.tracker.EndEnumerate(enumStats)

	estimatedCalls := enumStats.Scannable * 3
	rateLimit := 5000
	estimatedMinutes := (estimatedCalls * 60) / rateLimit

	fmt.Fprintf(os.Stderr, "\n=== Dry Run Summary ===\n")
	fmt.Fprintf(os.Stderr, "Orgs: %d\n", len(p.cfg.Orgs))
	fmt.Fprintf(os.Stderr, "Total repos (API): %d\n", enumStats.TotalFromAPI)
	fmt.Fprintf(os.Stderr, "  Forks skipped: %d\n", enumStats.ForksSkipped)
	fmt.Fprintf(os.Stderr, "  Empty skipped: %d\n", enumStats.EmptySkipped)
	fmt.Fprintf(os.Stderr, "  Archived (target only): %d\n", enumStats.Archived)
	fmt.Fprintf(os.Stderr, "  Scannable: %d\n", enumStats.Scannable)
	fmt.Fprintf(os.Stderr, "Estimated API calls: ~%d\n", estimatedCalls)
	fmt.Fprintf(os.Stderr, "Estimated time: ~%d minutes (at %d req/hr)\n", estimatedMinutes, rateLimit)
	return nil
}
