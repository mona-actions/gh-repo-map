package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

func TestLoadValidConfig(t *testing.T) {
	path := writeTestConfig(t, `
auth:
  type: "pat"
  token: "ghp_test123"
orgs:
  - "my-org"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Auth.Type != "pat" {
		t.Errorf("auth.type = %q, want %q", cfg.Auth.Type, "pat")
	}
	if len(cfg.Orgs) != 1 || cfg.Orgs[0] != "my-org" {
		t.Errorf("orgs = %v, want [my-org]", cfg.Orgs)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeTestConfig(t, `
auth:
  type: "pat"
  token: "ghp_test"
orgs:
  - "org1"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Options.GraphQLBatchSize != 15 {
		t.Errorf("graphql_batch_size = %d, want 15", cfg.Options.GraphQLBatchSize)
	}
	if cfg.Options.OutputSplit != "auto" {
		t.Errorf("output_split = %q, want %q", cfg.Options.OutputSplit, "auto")
	}
	if cfg.Options.Concurrency != 4 {
		t.Errorf("concurrency = %d, want 4", cfg.Options.Concurrency)
	}
	if cfg.Options.CheckpointFlushInterval != 10 {
		t.Errorf("checkpoint_flush_interval = %d, want 10", cfg.Options.CheckpointFlushInterval)
	}
	if cfg.Options.LogLevel != "default" {
		t.Errorf("log_level = %q, want %q", cfg.Options.LogLevel, "default")
	}
	if !reflect.DeepEqual(cfg.Scan.VendoredDirs, DefaultVendoredDirs()) {
		t.Errorf("vendored_dirs = %v, want %v", cfg.Scan.VendoredDirs, DefaultVendoredDirs())
	}
	if !reflect.DeepEqual(cfg.Scan.ScriptDirs, DefaultScriptDirs()) {
		t.Errorf("script_dirs = %v, want %v", cfg.Scan.ScriptDirs, DefaultScriptDirs())
	}
}

func TestValidateEmptyOrgs(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{Type: "pat", Token: "ghp_test"},
		Options: OptionsConfig{
			Concurrency: 4,
			OutputSplit: "auto",
			LogLevel:    "default",
		},
	}
	if err := Validate(cfg, false); err == nil {
		t.Error("expected error for empty orgs, got nil")
	}
}

func TestValidateInvalidConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
	}{
		{"too_low", 0},
		{"too_high", 11},
		{"negative", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Auth: AuthConfig{Type: "pat", Token: "ghp_test"},
				Orgs: []string{"org1"},
				Options: OptionsConfig{
					Concurrency: tt.concurrency,
					OutputSplit: "auto",
					LogLevel:    "default",
				},
			}
			if err := Validate(cfg, false); err == nil {
				t.Errorf("expected error for concurrency=%d, got nil", tt.concurrency)
			}
		})
	}
}

func TestValidateInvalidAuthType(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{Type: "invalid"},
		Orgs: []string{"org1"},
		Options: OptionsConfig{
			Concurrency: 4,
			OutputSplit: "auto",
			LogLevel:    "default",
		},
	}
	if err := Validate(cfg, false); err == nil {
		t.Error("expected error for invalid auth type, got nil")
	}
}

func TestValidateGitHubAppMissingFields(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{Type: "github-app"},
		Orgs: []string{"org1"},
		Options: OptionsConfig{
			Concurrency: 4,
			OutputSplit: "auto",
			LogLevel:    "default",
		},
	}
	if err := Validate(cfg, false); err == nil {
		t.Error("expected error for github-app without app_id, got nil")
	}
}

func TestEnvVarOverride(t *testing.T) {
	t.Setenv("GH_TOKEN", "ghp_env_token")
	path := writeTestConfig(t, `
auth:
  type: "pat"
orgs:
  - "org1"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Auth.Token != "ghp_env_token" {
		t.Errorf("token = %q, want %q", cfg.Auth.Token, "ghp_env_token")
	}
}

func TestScanEnvOverrideMergesWithDefaults(t *testing.T) {
	t.Setenv("REPO_MAP_VENDORED_DIRS", " custom_vendor, node_modules, vendor ")
	t.Setenv("REPO_MAP_SCRIPT_DIRS", " custom_scripts, tools ")

	path := writeTestConfig(t, `
auth:
  type: "pat"
orgs:
  - "org1"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantVendored := append(DefaultVendoredDirs(), "custom_vendor")
	if !reflect.DeepEqual(cfg.Scan.VendoredDirs, wantVendored) {
		t.Errorf("vendored_dirs = %v, want %v", cfg.Scan.VendoredDirs, wantVendored)
	}

	wantScripts := append(DefaultScriptDirs(), "custom_scripts")
	if !reflect.DeepEqual(cfg.Scan.ScriptDirs, wantScripts) {
		t.Errorf("script_dirs = %v, want %v", cfg.Scan.ScriptDirs, wantScripts)
	}
}

func TestScanEnvOverrideMergesWithConfiguredValues(t *testing.T) {
	t.Setenv("REPO_MAP_VENDORED_DIRS", "third_party, extras, deps")
	t.Setenv("REPO_MAP_SCRIPT_DIRS", "scripts, ops, bin")

	path := writeTestConfig(t, `
auth:
  type: "pat"
orgs:
  - "org1"
scan:
  vendored_dirs:
    - "vendor"
    - "deps"
  script_dirs:
    - "bin"
    - "."
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantVendored := []string{"vendor", "deps", "third_party", "extras"}
	if !reflect.DeepEqual(cfg.Scan.VendoredDirs, wantVendored) {
		t.Errorf("vendored_dirs = %v, want %v", cfg.Scan.VendoredDirs, wantVendored)
	}

	wantScripts := []string{"bin", ".", "scripts", "ops"}
	if !reflect.DeepEqual(cfg.Scan.ScriptDirs, wantScripts) {
		t.Errorf("script_dirs = %v, want %v", cfg.Scan.ScriptDirs, wantScripts)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := writeTestConfig(t, `{{{invalid`)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
