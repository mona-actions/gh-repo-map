package sbom

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFetchSBOMAsyncCancellation(t *testing.T) {
	f := newSBOMFixture(t)
	f.pending = 100
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, available, err := f.fetch(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || available {
		t.Errorf("available=%v err=%v, want context deadline exceeded", available, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("cancellation took %v, want less than one second", elapsed)
	}
	f.assertNoLegacy(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.generations != 1 || f.polls == 0 || f.downloads != 0 {
		t.Errorf("generate/poll/download=%d/%d/%d, want pending report canceled before download", f.generations, f.polls, f.downloads)
	}
}

func TestFetchSBOMAsyncAlreadyCanceled(t *testing.T) {
	f := newSBOMFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, available, err := f.fetch(ctx)
	if !errors.Is(err, context.Canceled) || available {
		t.Errorf("available=%v err=%v, want context canceled", available, err)
	}
	f.assertNoLegacy(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.generations != 0 || f.polls != 0 || f.downloads != 0 {
		t.Error("already canceled request performed HTTP calls")
	}
}
