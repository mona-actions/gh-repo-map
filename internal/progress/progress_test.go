package progress

import (
	"testing"
)

// TestLogTrackerFullLifecycle verifies that LogTracker completes
// the full phase sequence without panicking.
func TestLogTrackerFullLifecycle(t *testing.T) {
	tracker := NewLogTracker()

	// Phase 1
	tracker.StartEnumerate()
	tracker.EnumerateProgress("org-a", 100)
	tracker.EnumerateProgress("org-a", 200)
	tracker.OrgEnumerated("org-a", 250)
	tracker.EnumerateProgress("org-b", 100)
	tracker.OrgEnumerated("org-b", 130)
	tracker.EndEnumerate(EnumerateStats{
		TotalFromAPI: 500,
		Enumerated:   380,
		ForksSkipped: 80,
		EmptySkipped: 40,
		Archived:     50,
		Scannable:    330,
	})

	// Phase 2
	tracker.StartScan(330)
	tracker.RepoScanned("org-a/repo-1", 1, 330)
	tracker.ScanWarning("org-a/repo-2", "SBOM unavailable")
	tracker.RepoScanned("org-a/repo-2", 2, 330)
	tracker.EndScan(325, 330)

	// Phase 3
	tracker.StartCrossRef()
	tracker.EndCrossRef(150, 12)

	// Phase 4
	tracker.StartOutput()
	tracker.EndOutput("./output/dep-graph.json", 330, 150)

	tracker.Stop()
}

// TestPtermTrackerFullLifecycle verifies that PtermTracker completes
// the full phase sequence without panicking (spinners may be no-op
// in non-TTY test environments).
func TestPtermTrackerFullLifecycle(t *testing.T) {
	tracker := NewPtermTracker()

	tracker.StartEnumerate()
	tracker.EnumerateProgress("test-org", 100)
	tracker.EnumerateProgress("test-org", 200)
	tracker.OrgEnumerated("test-org", 250)
	tracker.EndEnumerate(EnumerateStats{
		TotalFromAPI: 300,
		Enumerated:   250,
		ForksSkipped: 30,
		EmptySkipped: 20,
		Archived:     10,
		Scannable:    240,
	})

	tracker.StartScan(10)
	for i := 1; i <= 10; i++ {
		tracker.RepoScanned("test-org/repo", i, 10)
	}
	tracker.EndScan(10, 10)

	tracker.StartCrossRef()
	tracker.EndCrossRef(25, 3)

	tracker.StartOutput()
	tracker.EndOutput("./output/test.json", 10, 25)

	tracker.Stop()
}

// TestTrackerInterface ensures both implementations satisfy the interface.
func TestTrackerInterface(t *testing.T) {
	var _ Tracker = (*PtermTracker)(nil)
	var _ Tracker = (*LogTracker)(nil)
}
