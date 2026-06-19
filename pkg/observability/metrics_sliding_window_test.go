package observability

import (
	"testing"
	"time"
)

// TestSlidingWindow_AllExpiredResetsCleanly guards the cleanup regression where a
// fully-expired window left stale entries and decremented counters twice, driving
// them negative and pushing SuccessRate outside [0,1].
func TestSlidingWindow_AllExpiredResetsCleanly(t *testing.T) {
	w := newSlidingWindow(time.Minute)
	w.Record(true)
	w.Record(false)
	w.Record(true)

	// Force every entry past the window, deterministically.
	w.mu.Lock()
	w.cleanupLocked(time.Now().Add(time.Hour))
	successes, failures, n := w.successes, w.failures, len(w.entries)
	w.mu.Unlock()

	if successes != 0 || failures != 0 {
		t.Errorf("after full expiry: successes=%d failures=%d, want 0/0", successes, failures)
	}
	if n != 0 {
		t.Errorf("after full expiry: %d entries remain, want 0", n)
	}

	// A second cleanup must NOT re-decrement into negative counts (the bug).
	w.mu.Lock()
	w.cleanupLocked(time.Now().Add(2 * time.Hour))
	successes, failures = w.successes, w.failures
	w.mu.Unlock()
	if successes < 0 || failures < 0 {
		t.Errorf("second cleanup drove counters negative: successes=%d failures=%d", successes, failures)
	}

	// After re-recording, SuccessRate stays in range.
	w.Record(true)
	w.Record(false)
	if r := w.SuccessRate(); r < 0 || r > 1 {
		t.Errorf("SuccessRate out of range: %v", r)
	}
}
