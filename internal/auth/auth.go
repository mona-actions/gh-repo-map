// Package auth provides authenticated GitHub REST and GraphQL clients
// using go-github, githubv4, go-githubauth, and go-github-ratelimit.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	gogh "github.com/cli/go-gh/v2"
	"github.com/gofri/go-github-ratelimit/v2/github_ratelimit"
	"github.com/google/go-github/v84/github"
	"github.com/jferrl/go-githubauth"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// GitHubClients bundles authenticated REST and GraphQL clients.
type GitHubClients struct {
	REST    *github.Client
	GraphQL *githubv4.Client
}

// TokenProvider supplies authenticated GitHub API clients.
type TokenProvider interface {
	// Clients returns authenticated REST + GraphQL clients for the given org.
	Clients(ctx context.Context, org string) (*GitHubClients, error)
	// BaseURL returns the REST API base URL.
	BaseURL() string
}

// --- PAT / gh-auth provider ---

// PATProvider authenticates via personal access token, GH_TOKEN, or gh auth login.
type PATProvider struct {
	token string // explicit token; empty = resolve from env / gh auth
	host  string
}

// NewPATProvider creates a PAT-based provider.
// If token is empty, it resolves from GH_TOKEN env var or `gh auth token`.
func NewPATProvider(token, host string) *PATProvider {
	if host == "" {
		host = "github.com"
	}
	return &PATProvider{token: token, host: host}
}

func (p *PATProvider) resolveToken() (string, error) {
	if p.token != "" {
		return p.token, nil
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t, nil
	}
	stdout, _, err := gogh.Exec("auth", "token", "--hostname", p.host)
	if err != nil {
		return "", fmt.Errorf("not authenticated — run 'gh auth login' or pass --token / GH_TOKEN: %w", err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (p *PATProvider) Clients(_ context.Context, _ string) (*GitHubClients, error) {
	token, err := p.resolveToken()
	if err != nil {
		return nil, err
	}

	ts := githubauth.NewPersonalAccessTokenSource(token)
	httpClient := rateLimitedClient(oauth2.NewClient(context.Background(), ts))

	restClient, err := newRESTClient(httpClient, p.host)
	if err != nil {
		return nil, err
	}

	return &GitHubClients{
		REST:    restClient,
		GraphQL: newGraphQLClient(httpClient, p.host),
	}, nil
}

func (p *PATProvider) BaseURL() string {
	base, _ := buildURLs(p.host)
	return base
}

// --- GitHub App provider ---

// AppProvider authenticates via GitHub App installation tokens (per-org).
type AppProvider struct {
	appID      int64
	privateKey []byte
	host       string
}

// NewAppProvider creates a GitHub App-based provider.
func NewAppProvider(appID int64, privateKeyPath, host string) (*AppProvider, error) {
	keyData, err := loadPrivateKey(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("loading private key: %w", err)
	}
	if host == "" {
		host = "github.com"
	}
	return &AppProvider{appID: appID, privateKey: keyData, host: host}, nil
}

func (p *AppProvider) Clients(ctx context.Context, org string) (*GitHubClients, error) {
	// Create App-level JWT token source
	appTS, err := githubauth.NewApplicationTokenSource(p.appID, p.privateKey)
	if err != nil {
		return nil, fmt.Errorf("creating app token source: %w", err)
	}

	// Find the installation ID for this org using the App JWT
	appClient := github.NewClient(oauth2.NewClient(ctx, appTS))
	if p.host != "github.com" {
		baseURL, _ := buildURLs(p.host)
		appClient, err = appClient.WithEnterpriseURLs(baseURL, baseURL)
		if err != nil {
			return nil, fmt.Errorf("configuring enterprise URLs: %w", err)
		}
	}

	installation, _, err := appClient.Apps.FindOrganizationInstallation(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("finding installation for org %q: %w", org, err)
	}

	// Create installation-scoped token source
	installOpts := []githubauth.InstallationTokenSourceOpt{
		githubauth.WithContext(ctx),
	}
	if p.host != "github.com" {
		baseURL, _ := buildURLs(p.host)
		installOpts = append(installOpts, githubauth.WithEnterpriseURL(baseURL))
	}
	installTS := githubauth.NewInstallationTokenSource(installation.GetID(), appTS, installOpts...)
	httpClient := rateLimitedClient(oauth2.NewClient(ctx, installTS))

	restClient, err := newRESTClient(httpClient, p.host)
	if err != nil {
		return nil, err
	}

	return &GitHubClients{
		REST:    restClient,
		GraphQL: newGraphQLClient(httpClient, p.host),
	}, nil
}

func (p *AppProvider) BaseURL() string {
	base, _ := buildURLs(p.host)
	return base
}

// --- Factory ---

// NewTokenProvider creates the appropriate provider based on auth config.
func NewTokenProvider(authType string, token string, appID int64, privateKeyPath string, host string) (TokenProvider, error) {
	switch authType {
	case "pat", "":
		return NewPATProvider(token, host), nil
	case "github-app":
		return NewAppProvider(appID, privateKeyPath, host)
	default:
		return nil, fmt.Errorf("unknown auth type: %q (must be 'pat' or 'github-app')", authType)
	}
}

// --- Helpers ---

func rateLimitedClient(base *http.Client) *http.Client {
	return github_ratelimit.NewClient(base.Transport)
}

func newRESTClient(httpClient *http.Client, host string) (*github.Client, error) {
	client := github.NewClient(httpClient)
	if host != "" && host != "github.com" {
		baseURL, _ := buildURLs(host)
		var err error
		client, err = client.WithEnterpriseURLs(baseURL, baseURL)
		if err != nil {
			return nil, fmt.Errorf("configuring enterprise URLs: %w", err)
		}
	}
	return client, nil
}

func newGraphQLClient(httpClient *http.Client, host string) *githubv4.Client {
	if host != "" && host != "github.com" {
		_, gqlURL := buildURLs(host)
		return githubv4.NewEnterpriseClient(gqlURL, httpClient)
	}
	return githubv4.NewClient(httpClient)
}

func buildURLs(host string) (baseURL, gqlURL string) {
	if host == "" || host == "github.com" {
		return "https://api.github.com", "https://api.github.com/graphql"
	}
	return fmt.Sprintf("https://%s/api/v3", host), fmt.Sprintf("https://%s/api/graphql", host)
}

func loadPrivateKey(pathOrContent string) ([]byte, error) {
	if strings.HasPrefix(pathOrContent, "-----BEGIN") {
		return []byte(pathOrContent), nil
	}
	return os.ReadFile(pathOrContent)
}
