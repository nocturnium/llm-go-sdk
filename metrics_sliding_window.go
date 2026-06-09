package llms

import (
	"sync"
	"time"
)

// slidingWindow tracks success/failure over a time window
type slidingWindow struct {
	mu          sync.Mutex
	successes   int64
	failures    int64
	window      time.Duration
	entries     []windowEntry
	maxCapacity int       // Maximum entries to prevent unbounded growth
	lastCleanup time.Time // Track last cleanup for idle cleanup
}

type windowEntry struct {
	timestamp time.Time
	success   bool
}

// defaultMaxWindowCapacity is the maximum number of entries in the sliding window
const defaultMaxWindowCapacity = 10000

func newSlidingWindow(duration time.Duration) *slidingWindow {
	return &slidingWindow{
		window:      duration,
		entries:     make([]windowEntry, 0, 100),
		maxCapacity: defaultMaxWindowCapacity,
		lastCleanup: time.Now(),
	}
}

// Record adds a success or failure to the sliding window
func (w *slidingWindow) Record(success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()

	// CRITICAL: Clean BEFORE adding to prevent unbounded growth under high load.
	// If cleanup happens after append, the slice can grow faster than cleanup
	// can remove entries during sustained high request rates.
	w.cleanupLocked(now)

	// After cleanup, if we're still at max capacity, drop oldest entry to make room.
	// This ensures the slice never exceeds maxCapacity.
	if len(w.entries) >= w.maxCapacity {
		removed := w.entries[0]
		w.entries = w.entries[1:]
		if removed.success {
			w.successes--
		} else {
			w.failures--
		}
	}

	// Ensure capacity before appending to reduce allocations.
	// If we're at capacity, grow with headroom (but not beyond maxCapacity).
	if len(w.entries) == cap(w.entries) && cap(w.entries) < w.maxCapacity {
		newCap := cap(w.entries) * 2
		if newCap > w.maxCapacity {
			newCap = w.maxCapacity
		}
		newEntries := make([]windowEntry, len(w.entries), newCap)
		copy(newEntries, w.entries)
		w.entries = newEntries
	}

	w.entries = append(w.entries, windowEntry{timestamp: now, success: success})

	if success {
		w.successes++
	} else {
		w.failures++
	}
}

// cleanupLocked removes expired entries. Must be called with lock held.
func (w *slidingWindow) cleanupLocked(now time.Time) {
	cutoff := now.Add(-w.window)
	// Default to "all entries expired": if the loop never finds a live entry, the
	// whole slice is dropped. Using 0 here was the bug — counters were decremented
	// for every expired entry but the slice was never trimmed, so a later cleanup
	// decremented the same entries again into negative counts.
	newStart := len(w.entries)
	for i, e := range w.entries {
		if e.timestamp.After(cutoff) {
			newStart = i
			break
		}
		if e.success {
			w.successes--
		} else {
			w.failures--
		}
	}
	switch {
	case newStart >= len(w.entries):
		// Every entry expired; drop them all (counters already decremented to 0).
		w.entries = w.entries[:0]
	case newStart > 0:
		// Reclaim memory by creating a new slice if we're removing many entries
		remaining := len(w.entries) - newStart
		if remaining < len(w.entries)/2 && remaining > 0 {
			newEntries := make([]windowEntry, remaining, remaining*2)
			copy(newEntries, w.entries[newStart:])
			w.entries = newEntries
		} else {
			w.entries = w.entries[newStart:]
		}
	}

	// If we're at capacity, force more aggressive cleanup
	if len(w.entries) >= w.maxCapacity {
		// Keep only the most recent half
		keepCount := w.maxCapacity / 2
		if keepCount > 0 {
			toRemove := w.entries[:len(w.entries)-keepCount]
			for _, e := range toRemove {
				if e.success {
					w.successes--
				} else {
					w.failures--
				}
			}
			w.entries = w.entries[len(w.entries)-keepCount:]
		}
	}

	w.lastCleanup = now
}

// SuccessRate returns the current success rate in the window
func (w *slidingWindow) SuccessRate() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Perform idle cleanup when reading success rate
	// This ensures old entries are cleaned even if no new records come in
	now := time.Now()
	if now.Sub(w.lastCleanup) > w.window/10 {
		w.cleanupLocked(now)
	}

	total := w.successes + w.failures
	if total == 0 {
		return 1.0 // No requests = 100% success rate
	}
	return float64(w.successes) / float64(total)
}
