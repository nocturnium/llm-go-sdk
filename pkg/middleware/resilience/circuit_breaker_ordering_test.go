package resilience

import (
	"sync"
	"testing"
	"time"
)

// TestCircuitBreaker_CallbackOrdering verifies state-change callbacks are
// delivered in the exact order the transitions occurred, even when an earlier
// callback is slow. The prior design spawned one goroutine per transition, so a
// later transition's callback could be observed before an earlier one (e.g.
// Open->HalfOpen recorded before Closed->Open). Ordered delivery must not.
func TestCircuitBreaker_CallbackOrdering(t *testing.T) {
	var mu sync.Mutex
	var order []CircuitState
	record := func(to CircuitState) {
		mu.Lock()
		order = append(order, to)
		mu.Unlock()
	}

	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(time.Millisecond), // let Open->HalfOpen fire promptly
		WithOnStateChange(func(from, to CircuitState) {
			// Delay ONLY the causally-first transition. Under the old
			// per-transition-goroutine design the later (undelayed) callback
			// wins the race and is recorded first; ordered delivery must record
			// Open before HalfOpen regardless of per-callback latency.
			if from == CircuitClosed && to == CircuitOpen {
				time.Sleep(80 * time.Millisecond)
			}
			record(to)
		}),
	)

	cb.RecordFailure() // Closed -> Open (callback enqueued first, then sleeps)
	// Let the reset timeout elapse so the next Allow transitions to half-open.
	time.Sleep(5 * time.Millisecond)
	if !cb.Allow() { // Open -> HalfOpen (callback enqueued behind the first)
		t.Fatal("Allow should transition Open->HalfOpen and return true")
	}

	// Wait for both callbacks to be delivered (generous bound; the first sleeps
	// 80ms). Poll under the mutex to stay race-clean.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(order)
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for callbacks; got %v", order)
		}
		time.Sleep(2 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []CircuitState{CircuitOpen, CircuitHalfOpen}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("callback order = %v, want %v (transitions must be delivered in causal order)", order, want)
	}
}

// TestCircuitBreaker_CallbackQueueDrains verifies the delivery goroutine is
// self-terminating: after a burst of transitions is delivered, no drain
// goroutine lingers (the breaker needs no Close()).
func TestCircuitBreaker_CallbackQueueDrains(t *testing.T) {
	before := waitForStableGoroutineCount(t)

	delivered := make(chan struct{}, 4)
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(time.Millisecond),
		WithOnStateChange(func(from, to CircuitState) {
			delivered <- struct{}{}
		}),
	)

	// Two transitions: Closed->Open, then Open->HalfOpen.
	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)
	cb.Allow()

	for i := 0; i < 2; i++ {
		select {
		case <-delivered:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for callback %d", i+1)
		}
	}

	// The drain goroutine must exit once the queue empties, returning the count
	// to baseline.
	if got := waitForGoroutineDelta(t, before, 0); got != 0 {
		t.Fatalf("goroutine delta after drain = %d, want 0 (drain goroutine must self-terminate)", got)
	}
}
