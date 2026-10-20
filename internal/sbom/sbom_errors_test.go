package sbom

import (
	"context"
	"net/http"
	"testing"
)

func TestFetchSBOMAsyncUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newSBOMFixture(t)
			f.generateStatus = status
			packages, available, err := f.fetch(context.Background())
			if err != nil || available || len(packages) != 0 {
				t.Errorf("packages=%+v available=%v err=%v, want unavailable without error", packages, available, err)
			}
			f.assertNoLegacy(t)
		})
	}
}

func TestFetchSBOMAsyncInvalidResponses(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*sbomFixture)
	}{
		{"missing report URL", func(f *sbomFixture) { f.generateBody = `{}` }},
		{"malformed generation JSON", func(f *sbomFixture) { f.generateBody = `{` }},
		{"unexpected generation status", func(f *sbomFixture) { f.generateStatus = http.StatusOK }},
		{"foreign report URL", func(f *sbomFixture) { f.reportURL = f.download.URL + "/report.spdx.json" }},
		{"wrong report path", func(f *sbomFixture) { f.reportURL = f.api.URL + "/unexpected" }},
		{"poll not found", func(f *sbomFixture) { f.pollStatus = http.StatusNotFound }},
		{"unexpected poll status", func(f *sbomFixture) { f.pollStatus = http.StatusOK }},
		{"download forbidden", func(f *sbomFixture) { f.downloadStatus = http.StatusForbidden }},
		{"malformed document", func(f *sbomFixture) { f.document = `{` }},
		{"missing document identity", func(f *sbomFixture) { f.document = `{"packages":[]}` }},
		{"legacy wrapper", func(f *sbomFixture) { f.document = `{"sbom":` + testSPDX + `}` }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSBOMFixture(t)
			tc.setup(f)
			packages, available, err := f.fetch(context.Background())
			if err == nil || available || len(packages) != 0 {
				t.Errorf("packages=%+v available=%v err=%v, want explicit error", packages, available, err)
			}
			f.assertNoLegacy(t)
			if tc.name == "foreign report URL" || tc.name == "wrong report path" {
				f.mu.Lock()
				if f.polls != 0 || f.downloads != 0 {
					t.Error("invalid report URL was followed")
				}
				f.mu.Unlock()
			}
		})
	}
}

func TestFetchSBOMAsyncGHESCompatibility(t *testing.T) {
	cases := []struct {
		name           string
		generateStatus int
		legacyStatus   int
		pollStatus     int
		wantAvailable  bool
		wantError      bool
		wantLegacy     int
	}{
		{"older GHES", http.StatusNotFound, http.StatusOK, http.StatusFound, true, false, 1},
		{"older GHES unavailable", http.StatusNotFound, http.StatusNotFound, http.StatusFound, false, false, 1},
		{"modern GHES", http.StatusCreated, http.StatusGone, http.StatusFound, true, false, 0},
		{"GHES permission denied", http.StatusForbidden, http.StatusOK, http.StatusFound, false, false, 0},
		{"GHES report missing", http.StatusCreated, http.StatusOK, http.StatusNotFound, false, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSBOMFixture(t)
			f.enterpriseVersion = "3.6.0"
			f.generateStatus = tc.generateStatus
			f.legacyStatus = tc.legacyStatus
			f.pollStatus = tc.pollStatus
			packages, available, err := f.fetch(context.Background())
			if available != tc.wantAvailable || (err != nil) != tc.wantError {
				t.Errorf("available=%v err=%v, want available=%v error=%v", available, err, tc.wantAvailable, tc.wantError)
			}
			if available && len(packages) != 2 {
				t.Errorf("packages=%+v, want preserved package extraction", packages)
			}
			f.mu.Lock()
			if f.legacy != tc.wantLegacy {
				t.Errorf("legacy requests=%d, want %d", f.legacy, tc.wantLegacy)
			}
			f.mu.Unlock()
		})
	}
}
