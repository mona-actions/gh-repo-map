package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mona-actions/gh-repo-map/internal/auth"
	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/progress"
)

type fakeTokenProvider struct{}

func (f *fakeTokenProvider) Clients(context.Context, string) (*auth.GitHubClients, error) {
	return nil, nil
}

func (f *fakeTokenProvider) BaseURL() string {
	return ""
}

func TestLoadConfigWithFallbackUsesExplicitPath(t *testing.T) {
	t.Helper()

	var gotPath string
	expected := &config.Config{}

	cfg, err := loadConfigWithFallback("./custom.yml", "./config.yml",
		func(path string) (*config.Config, error) {
			gotPath = path
			return expected, nil
		},
		func(string) bool {
			t.Fatal("existsFn should not be called for explicit path")
			return false
		},
	)
	if err != nil {
		t.Fatalf("loadConfigWithFallback returned error: %v", err)
	}
	if cfg != expected {
		t.Fatalf("expected returned config pointer to match loader result")
	}
	if gotPath != "./custom.yml" {
		t.Fatalf("expected loader path ./custom.yml, got %q", gotPath)
	}
}

func TestLoadConfigWithFallbackUsesDefaultPath(t *testing.T) {
	t.Helper()

	var gotPath string
	expected := &config.Config{}

	cfg, err := loadConfigWithFallback("", "./config.yml",
		func(path string) (*config.Config, error) {
			gotPath = path
			return expected, nil
		},
		func(path string) bool {
			return path == "./config.yml"
		},
	)
	if err != nil {
		t.Fatalf("loadConfigWithFallback returned error: %v", err)
	}
	if cfg != expected {
		t.Fatalf("expected returned config pointer to match loader result")
	}
	if gotPath != "./config.yml" {
		t.Fatalf("expected loader path ./config.yml, got %q", gotPath)
	}
}

func TestLoadConfigWithFallbackAppliesDefaultsWithoutFiles(t *testing.T) {
	t.Helper()

	cfg, err := loadConfigWithFallback("", "./config.yml",
		func(string) (*config.Config, error) {
			t.Fatal("loader should not be called when no config file is present")
			return nil, nil
		},
		func(string) bool { return false },
	)
	if err != nil {
		t.Fatalf("loadConfigWithFallback returned error: %v", err)
	}
	if cfg.Options.Concurrency != 4 {
		t.Fatalf("expected default concurrency 4, got %d", cfg.Options.Concurrency)
	}
	if cfg.Output.Path != "./output/dep-graph.json" {
		t.Fatalf("expected default output path, got %q", cfg.Output.Path)
	}
}

func TestApplyCLIOverrides(t *testing.T) {
	t.Helper()

	cfg := &config.Config{
		Auth: config.AuthConfig{
			Type:  "pat",
			Token: "old-token",
		},
		Orgs: []string{"org1", "org2"},
		Options: config.OptionsConfig{
			Concurrency: 4,
			MinCoverage: 80,
			LogLevel:    "default",
		},
	}
	opts := cliOptions{
		Token:             "new-token",
		AppID:             99,
		PrivateKeyPath:    "app.pem",
		GitHubHost:        "ghe.example.com",
		OrgsFile:          "orgs.txt",
		IncludeTransitive: true,
		Concurrency:       8,
		MinCoverage:       90,
		LogLevel:          "debug",
	}

	warned := false
	applyCLIOverrides(cfg, opts,
		func(path string) ([]string, error) {
			if path != "orgs.txt" {
				t.Fatalf("expected orgs file path orgs.txt, got %q", path)
			}
			return []string{"org2", "org3"}, nil
		},
		func(string, ...any) { warned = true },
	)

	if warned {
		t.Fatalf("did not expect warning callback on successful orgs read")
	}
	if cfg.Auth.Type != "github-app" {
		t.Fatalf("expected auth type github-app, got %q", cfg.Auth.Type)
	}
	if cfg.Auth.Token != "new-token" {
		t.Fatalf("expected token override, got %q", cfg.Auth.Token)
	}
	if cfg.Auth.AppID != 99 {
		t.Fatalf("expected app id 99, got %d", cfg.Auth.AppID)
	}
	if cfg.Auth.PrivateKeyPath != "app.pem" {
		t.Fatalf("expected private key path app.pem, got %q", cfg.Auth.PrivateKeyPath)
	}
	if cfg.GitHubHost != "ghe.example.com" {
		t.Fatalf("expected github host override, got %q", cfg.GitHubHost)
	}
	if strings.Join(cfg.Orgs, ",") != "org1,org2,org3" {
		t.Fatalf("expected deduped org list [org1 org2 org3], got %v", cfg.Orgs)
	}
	if !cfg.Output.IncludeTransitive {
		t.Fatalf("expected include transitive override to be true")
	}
	if cfg.Options.Concurrency != 8 {
		t.Fatalf("expected concurrency override to be 8, got %d", cfg.Options.Concurrency)
	}
	if cfg.Options.MinCoverage != 90 {
		t.Fatalf("expected min coverage override to be 90, got %d", cfg.Options.MinCoverage)
	}
	if cfg.Options.LogLevel != "debug" {
		t.Fatalf("expected log level override to be debug, got %q", cfg.Options.LogLevel)
	}
}

func TestApplyCLIOverridesWarnsOnOrgsReadError(t *testing.T) {
	t.Helper()

	cfg := &config.Config{Orgs: []string{"org1"}}
	opts := cliOptions{OrgsFile: "orgs.txt"}

	var warning string
	applyCLIOverrides(cfg, opts,
		func(string) ([]string, error) {
			return nil, errors.New("boom")
		},
		func(format string, args ...any) {
			warning = format
		},
	)

	if warning == "" {
		t.Fatalf("expected warning callback to be called")
	}
	if !strings.Contains(warning, "failed to read orgs file") {
		t.Fatalf("expected warning message to mention orgs file read failure, got %q", warning)
	}
}

func TestBuildTokenProviderUsesConfigValues(t *testing.T) {
	t.Helper()

	cfg := &config.Config{
		Auth:       config.AuthConfig{Type: "github-app", Token: "tok", AppID: 123, PrivateKeyPath: "key.pem"},
		GitHubHost: "ghe.local",
	}

	expected := &fakeTokenProvider{}
	var gotAuthType, gotToken, gotPrivateKeyPath, gotHost string
	var gotAppID int64

	tp, err := buildTokenProvider(cfg, func(authType, token string, appID int64, privateKeyPath, host string) (auth.TokenProvider, error) {
		gotAuthType = authType
		gotToken = token
		gotAppID = appID
		gotPrivateKeyPath = privateKeyPath
		gotHost = host
		return expected, nil
	})
	if err != nil {
		t.Fatalf("buildTokenProvider returned error: %v", err)
	}
	if tp != expected {
		t.Fatalf("expected returned provider from factory")
	}
	if gotAuthType != "github-app" || gotToken != "tok" || gotAppID != 123 || gotPrivateKeyPath != "key.pem" || gotHost != "ghe.local" {
		t.Fatalf("factory arguments mismatch: type=%q token=%q appID=%d key=%q host=%q", gotAuthType, gotToken, gotAppID, gotPrivateKeyPath, gotHost)
	}
}

func TestSelectTracker(t *testing.T) {
	t.Helper()

	logCalled := false
	ptermCalled := false

	tracker := selectTracker("debug",
		func() *progress.PtermTracker {
			ptermCalled = true
			return &progress.PtermTracker{}
		},
		func() *progress.LogTracker {
			logCalled = true
			return &progress.LogTracker{}
		},
	)
	if _, ok := tracker.(*progress.LogTracker); !ok {
		t.Fatalf("expected log tracker for debug log level")
	}
	if !logCalled || ptermCalled {
		t.Fatalf("expected only log tracker factory to be called")
	}

	logCalled = false
	ptermCalled = false
	tracker = selectTracker("default",
		func() *progress.PtermTracker {
			ptermCalled = true
			return &progress.PtermTracker{}
		},
		func() *progress.LogTracker {
			logCalled = true
			return &progress.LogTracker{}
		},
	)
	if _, ok := tracker.(*progress.PtermTracker); !ok {
		t.Fatalf("expected pterm tracker for default log level")
	}
	if !ptermCalled || logCalled {
		t.Fatalf("expected only pterm tracker factory to be called")
	}
}
