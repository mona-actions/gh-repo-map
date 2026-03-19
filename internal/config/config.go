// Package config handles loading and validating the gh-repo-map configuration.
package config

import (
"fmt"
"os"
"strconv"

"gopkg.in/yaml.v3"
)

// Config is the top-level configuration structure loaded from config.yml.
type Config struct {
Auth                AuthConfig    `yaml:"auth"`
GitHubHost          string        `yaml:"github_host"`
Orgs                []string      `yaml:"orgs"`
ContainerRegistries []string      `yaml:"container_registries"`
Options             OptionsConfig `yaml:"options"`
Output              OutputConfig  `yaml:"output"`
OverridesPath       string        `yaml:"overrides_path"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
Type           string `yaml:"type"` // "github-app" or "pat"
AppID          int64  `yaml:"app_id"`
PrivateKeyPath string `yaml:"private_key_path"`
Token          string `yaml:"token"`
}

// OptionsConfig holds runtime behavior options.
type OptionsConfig struct {
IncludeForks            bool   `yaml:"include_forks"`
SkipSBOMErrors          bool   `yaml:"skip_sbom_errors"`
GraphQLBatchSize        int    `yaml:"graphql_batch_size"`
TreeFallbackRecursive   bool   `yaml:"tree_fallback_recursive"`
OutputSplit             string `yaml:"output_split"`
CheckpointFlushInterval int    `yaml:"checkpoint_flush_interval"`
LogLevel                string `yaml:"log_level"`
LogFile                 string `yaml:"log_file"`
Concurrency             int    `yaml:"concurrency"`
MinCoverage             int    `yaml:"min_coverage"`
}

// OutputConfig holds output file settings.
type OutputConfig struct {
Path              string `yaml:"path"`
IncludeTransitive bool   `yaml:"include_transitive"`
IncludeUnresolved bool   `yaml:"include_unresolved"`
}

// applyDefaults fills in zero-value fields with sensible defaults.
// ApplyDefaults fills zero-value fields with sensible defaults.
func ApplyDefaults(cfg *Config) {
if cfg.Options.GraphQLBatchSize == 0 {
cfg.Options.GraphQLBatchSize = 15
}
if cfg.Options.OutputSplit == "" {
cfg.Options.OutputSplit = "auto"
}
if cfg.Options.CheckpointFlushInterval == 0 {
cfg.Options.CheckpointFlushInterval = 10
}
if cfg.Options.Concurrency == 0 {
cfg.Options.Concurrency = 4
}
if cfg.Options.MinCoverage == 0 && !cfg.Options.IncludeForks {
// Only set default if not explicitly set to 0
cfg.Options.MinCoverage = 80
}
if cfg.Options.LogLevel == "" {
cfg.Options.LogLevel = "default"
}
if cfg.Output.Path == "" {
cfg.Output.Path = "./output/dep-graph.json"
}
if !cfg.Output.IncludeUnresolved {
cfg.Output.IncludeUnresolved = true
}
if cfg.Options.TreeFallbackRecursive == false && cfg.Options.GraphQLBatchSize == 15 {
cfg.Options.TreeFallbackRecursive = true
}
}

// applyEnvOverrides checks environment variables for secret overrides.
func applyEnvOverrides(cfg *Config) {
if token := os.Getenv("GH_TOKEN"); token != "" && cfg.Auth.Token == "" {
cfg.Auth.Token = token
if cfg.Auth.Type == "" {
cfg.Auth.Type = "pat"
}
}
if key := os.Getenv("GH_APP_PRIVATE_KEY"); key != "" && cfg.Auth.PrivateKeyPath == "" {
cfg.Auth.PrivateKeyPath = key
}
if idStr := os.Getenv("GH_APP_ID"); idStr != "" && cfg.Auth.AppID == 0 {
if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
cfg.Auth.AppID = id
if cfg.Auth.Type == "" {
cfg.Auth.Type = "github-app"
}
}
}
}

// Validate checks that the config is internally consistent and complete.
// When resuming is true, orgs are not required (they come from the checkpoint).
func Validate(cfg *Config, resuming bool) error {
if !resuming && len(cfg.Orgs) == 0 {
return fmt.Errorf("config validation: 'orgs' must contain at least one organization")
}
if cfg.Auth.Type != "" && cfg.Auth.Type != "github-app" && cfg.Auth.Type != "pat" {
return fmt.Errorf("config validation: 'auth.type' must be 'github-app' or 'pat', got %q", cfg.Auth.Type)
}
if cfg.Auth.Type == "github-app" {
if cfg.Auth.AppID == 0 {
return fmt.Errorf("config validation: 'auth.app_id' is required when auth.type is 'github-app'")
}
if cfg.Auth.PrivateKeyPath == "" {
return fmt.Errorf("config validation: 'auth.private_key_path' or GH_APP_PRIVATE_KEY env var is required when auth.type is 'github-app'")
}
}
// PAT token is optional — auth package resolves from GH_TOKEN or gh auth login
if cfg.Options.Concurrency < 1 || cfg.Options.Concurrency > 10 {
return fmt.Errorf("config validation: 'options.concurrency' must be between 1 and 10, got %d", cfg.Options.Concurrency)
}
if cfg.Options.MinCoverage < 0 || cfg.Options.MinCoverage > 100 {
return fmt.Errorf("config validation: 'options.min_coverage' must be between 0 and 100, got %d", cfg.Options.MinCoverage)
}
validSplits := map[string]bool{"merged": true, "per-org": true, "auto": true}
if !validSplits[cfg.Options.OutputSplit] {
return fmt.Errorf("config validation: 'options.output_split' must be 'merged', 'per-org', or 'auto', got %q", cfg.Options.OutputSplit)
}
validLogLevels := map[string]bool{"quiet": true, "default": true, "verbose": true, "debug": true}
if !validLogLevels[cfg.Options.LogLevel] {
return fmt.Errorf("config validation: 'options.log_level' must be 'quiet', 'default', 'verbose', or 'debug', got %q", cfg.Options.LogLevel)
}
return nil
}

// Load reads a config file from the given path, applies defaults and env overrides.
func Load(path string) (*Config, error) {
data, err := os.ReadFile(path)
if err != nil {
return nil, fmt.Errorf("reading config file %q: %w", path, err)
}
var cfg Config
if err := yaml.Unmarshal(data, &cfg); err != nil {
return nil, fmt.Errorf("parsing config file %q: %w", path, err)
}
ApplyDefaults(&cfg)
applyEnvOverrides(&cfg)
if err := Validate(&cfg, false); err != nil {
return nil, err
}
return &cfg, nil
}
