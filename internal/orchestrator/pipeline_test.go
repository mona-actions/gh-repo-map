package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-github/v84/github"
	"github.com/mona-actions/gh-repo-map/internal/auth"
	"github.com/mona-actions/gh-repo-map/internal/config"
	"github.com/mona-actions/gh-repo-map/internal/progress"
)

type dryRunTokenProvider struct {
	client *github.Client
}

func (p dryRunTokenProvider) Clients(context.Context, string) (*auth.GitHubClients, error) {
	return &auth.GitHubClients{REST: p.client}, nil
}

func (p dryRunTokenProvider) BaseURL() string { return p.client.BaseURL.String() }

type dryRunTracker struct {
	*progress.LogTracker
	started bool
	ended   bool
	stopped bool
	stats   progress.EnumerateStats
}

func (t *dryRunTracker) StartEnumerate() { t.started = true }
func (t *dryRunTracker) EndEnumerate(stats progress.EnumerateStats) {
	t.ended = true
	t.stats = stats
}
func (t *dryRunTracker) Stop() { t.stopped = true }

func TestDryRunDoesNotWriteFiles(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		wantStats  progress.EnumerateStats
		wantOutput []string
	}{
		{
			name: "repositories", status: http.StatusOK,
			body: `[{"full_name":"acme/active","size":10},
				{"full_name":"acme/archive","size":10,"archived":true},
				{"full_name":"acme/fork","size":10,"fork":true},
				{"full_name":"acme/empty","size":0}]`,
			wantStats: progress.EnumerateStats{
				TotalFromAPI: 4, Enumerated: 2, ForksSkipped: 1,
				EmptySkipped: 1, Archived: 1, Scannable: 1,
			},
			wantOutput: []string{"Total repos (API): 4", "Forks skipped: 1",
				"Empty skipped: 1", "Archived (target only): 1", "Scannable: 1",
				"Estimated API calls: ~3"},
		},
		{
			name: "empty organization", status: http.StatusOK, body: `[]`,
			wantStats:  progress.EnumerateStats{EmptyOrgs: []string{"acme"}},
			wantOutput: []string{"Total repos (API): 0", "Scannable: 0"},
		},
		{name: "API failure", status: http.StatusForbidden, body: `{"message":"Forbidden"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workdir := t.TempDir()
			t.Chdir(workdir)
			// Keep captured output outside the directory whose contents are checked.
			output, err := os.Create(filepath.Join(t.TempDir(), "stderr.txt"))
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stderr
			os.Stderr = output
			t.Cleanup(func() {
				os.Stderr = previous
				if err := output.Close(); err != nil {
					t.Errorf("closing captured output: %v", err)
				}
			})
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/orgs/acme/repos" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(server.Close)
			client := github.NewClient(server.Client())
			client.BaseURL, err = url.Parse(server.URL + "/")
			if err != nil {
				t.Fatal(err)
			}
			tracker := &dryRunTracker{LogTracker: progress.NewLogTracker()}
			cfg := &config.Config{Orgs: []string{"acme"}}
			cfg.Output.Path = filepath.Join(workdir, "output", "dep-graph.json")
			err = DryRun(context.Background(), cfg, dryRunTokenProvider{client}, tracker)
			if tc.status == http.StatusOK {
				if err != nil {
					t.Errorf("dry run failed: %v", err)
				}
				if !tracker.ended || !reflect.DeepEqual(tracker.stats, tc.wantStats) {
					t.Errorf("enumeration ended=%v stats=%+v, want %+v", tracker.ended, tracker.stats, tc.wantStats)
				}
			} else if err == nil || !strings.Contains(err.Error(), `enumerating org "acme"`) {
				t.Errorf("expected organization API error, got %v", err)
			}
			if !tracker.started || !tracker.stopped || calls != 1 {
				t.Errorf("started=%v stopped=%v API calls=%d", tracker.started, tracker.stopped, calls)
			}
			captured, err := os.ReadFile(output.Name())
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.wantOutput {
				if !strings.Contains(string(captured), want) {
					t.Errorf("summary missing %q: %s", want, captured)
				}
			}
			entries, err := os.ReadDir(workdir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				t.Errorf("dry run created unexpected artifact: %s", entry.Name())
			}
		})
	}
}
