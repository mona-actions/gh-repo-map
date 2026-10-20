package sbom

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/go-github/v84/github"
	"github.com/mona-actions/gh-repo-map/internal/auth"
	"github.com/mona-actions/gh-repo-map/internal/model"
	"golang.org/x/oauth2"
)

const (
	testRepoPath = "/api/v3/repos/acme/service/dependency-graph/sbom"
	testReportID = "4bab1a7e-da63-4828-9488-44e0e01a7c1b"
	testSPDX     = `{
		"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3",
		"packages":[
			{"SPDXID":"SPDXRef-DOCUMENT","name":"pkg:npm/ignore@1"},
			{"SPDXID":"SPDXRef-core","name":"core","versionInfo":"1.2.3",
			 "externalRefs":[{"referenceCategory":"PACKAGE-MANAGER","referenceType":"purl","referenceLocator":"pkg:npm/%40acme/core@1.2.3"}]},
			{"SPDXID":"SPDXRef-fallback","name":"pkg:pypi/Example_Lib@2.0","versionInfo":"2.0"},
			{"SPDXID":"SPDXRef-unidentified","name":"no-package-url"}
		]}`
)

type localOnlyTransport struct {
	base http.RoundTripper
}

func (tr localOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ip := net.ParseIP(req.URL.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("test blocked non-loopback request to %s", req.URL.Host)
	}
	return tr.base.RoundTrip(req)
}

type testTokenProvider struct {
	client *github.Client
}

func (p testTokenProvider) Clients(context.Context, string) (*auth.GitHubClients, error) {
	return &auth.GitHubClients{REST: p.client}, nil
}

func (p testTokenProvider) BaseURL() string { return p.client.BaseURL.String() }

type sbomFixture struct {
	t        *testing.T
	api      *httptest.Server
	download *httptest.Server
	provider testTokenProvider

	generateStatus    int
	generateBody      string
	enterpriseVersion string
	legacyStatus      int
	reportURL         string
	pollStatus        int
	pending           int
	transientStage    string
	downloadStatus    int
	document          string

	mu           sync.Mutex
	generations  int
	polls        int
	downloads    int
	legacy       int
	downloadAuth string
	apiAuth      string
	pollTimes    []time.Time
}

func newSBOMFixture(t *testing.T) *sbomFixture {
	t.Helper()
	f := &sbomFixture{
		t: t, generateStatus: http.StatusCreated,
		pollStatus: http.StatusFound, downloadStatus: http.StatusOK, document: testSPDX, legacyStatus: http.StatusGone,
	}
	f.download = httptest.NewTLSServer(http.HandlerFunc(f.serveDownload))
	t.Cleanup(f.download.Close)
	f.api = httptest.NewTLSServer(http.HandlerFunc(f.serveAPI))
	t.Cleanup(f.api.Close)
	f.reportURL = f.api.URL + testRepoPath + "/fetch-report/" + testReportID

	// Both TLS servers use httptest's certificate; all transports reject external hosts.
	transport := localOnlyTransport{base: f.api.Client().Transport}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	client := github.NewClient(&http.Client{Transport: &oauth2.Transport{
		Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "local-test-token"}),
		Base:   transport,
	}})
	baseURL, err := url.Parse(f.api.URL + "/api/v3/")
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL = baseURL
	f.provider = testTokenProvider{client: client}
	return f
}

func (f *sbomFixture) serveAPI(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodGet {
		f.t.Errorf("method = %s, want GET", r.Method)
	}
	f.apiAuth = r.Header.Get("Authorization")
	w.Header().Set("Content-Type", "application/json")
	if f.enterpriseVersion != "" {
		w.Header().Set("X-GitHub-Enterprise-Version", f.enterpriseVersion)
	}
	switch r.URL.Path {
	case testRepoPath:
		f.legacy++
		w.WriteHeader(f.legacyStatus)
		if f.legacyStatus == http.StatusOK {
			fmt.Fprint(w, `{"sbom":`+f.document+`}`)
		} else {
			fmt.Fprint(w, `{"message":"legacy endpoint unavailable (simulated)"}`)
		}
	case testRepoPath + "/generate-report":
		f.generations++
		if f.transientStage == "generate" && f.generations == 1 {
			http.Error(w, `{"message":"try again"}`, http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(f.generateStatus)
		if f.generateBody != "" {
			fmt.Fprint(w, f.generateBody)
		} else {
			json.NewEncoder(w).Encode(map[string]string{"sbom_url": f.reportURL})
		}
	case testRepoPath + "/fetch-report/" + testReportID:
		f.polls++
		f.pollTimes = append(f.pollTimes, time.Now())
		if f.transientStage == "poll" && f.polls == 1 {
			http.Error(w, `{"message":"try again"}`, http.StatusServiceUnavailable)
			return
		}
		if f.polls <= f.pending {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if f.pollStatus == http.StatusFound {
			w.Header().Set("Location", f.download.URL+"/report.spdx.json")
		}
		w.WriteHeader(f.pollStatus)
	default:
		f.t.Errorf("unexpected API path: %s", r.URL.Path)
		http.NotFound(w, r)
	}
}

func (f *sbomFixture) serveDownload(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads++
	f.downloadAuth = r.Header.Get("Authorization")
	if r.URL.Path != "/report.spdx.json" {
		f.t.Errorf("unexpected download path: %s", r.URL.Path)
	}
	if f.transientStage == "download" && f.downloads == 1 {
		http.Error(w, "try again", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.downloadStatus)
	fmt.Fprint(w, f.document)
}

func (f *sbomFixture) fetch(ctx context.Context) ([]model.ConsumedPackage, bool, error) {
	return FetchSBOM(ctx, f.provider, "acme", "service")
}

func (f *sbomFixture) assertNoLegacy(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.legacy != 0 {
		t.Errorf("called retiring endpoint %d time(s)", f.legacy)
	}
}

func TestFetchSBOMAsyncFlow(t *testing.T) {
	f := newSBOMFixture(t)
	f.pending = 1
	packages, available, err := f.fetch(context.Background())
	if err != nil {
		t.Errorf("fetch failed: %v", err)
	}
	want := []model.ConsumedPackage{
		{Name: "@acme/core", Ecosystem: "npm", Version: "1.2.3"},
		{Name: "example-lib", Ecosystem: "pypi", Version: "2.0"},
	}
	if !available || !reflect.DeepEqual(packages, want) {
		t.Errorf("available=%v packages=%+v, want available=true packages=%+v", available, packages, want)
	}
	f.assertNoLegacy(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.generations != 1 || f.polls != 2 || f.downloads != 1 {
		t.Errorf("generate/poll/download=%d/%d/%d, want 1/2/1", f.generations, f.polls, f.downloads)
	}
	if len(f.pollTimes) == 2 && f.pollTimes[1].Sub(f.pollTimes[0]) < 100*time.Millisecond {
		t.Error("pending report was polled without a meaningful delay")
	}
	if f.apiAuth != "Bearer local-test-token" || f.downloadAuth != "" {
		t.Errorf("API auth=%q download auth=%q; want authenticated API and credential-free download", f.apiAuth, f.downloadAuth)
	}
}

func TestFetchSBOMAsyncEmptyDocument(t *testing.T) {
	f := newSBOMFixture(t)
	f.document = `{"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3","packages":[]}`
	packages, available, err := f.fetch(context.Background())
	if err != nil || !available || len(packages) != 0 {
		t.Errorf("got packages=%+v available=%v err=%v, want valid empty SBOM", packages, available, err)
	}
	f.assertNoLegacy(t)
}

func TestFetchSBOMAsyncTransientRecovery(t *testing.T) {
	for _, stage := range []string{"generate", "poll", "download"} {
		t.Run(stage, func(t *testing.T) {
			f := newSBOMFixture(t)
			f.transientStage = stage
			packages, available, err := f.fetch(context.Background())
			if err != nil || !available || len(packages) != 2 {
				t.Errorf("transient %s: packages=%+v available=%v err=%v", stage, packages, available, err)
			}
			f.assertNoLegacy(t)
		})
	}
}
