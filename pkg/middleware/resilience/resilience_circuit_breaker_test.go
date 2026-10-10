package resilience

import (
	"testing"
	"time"
)

// TestCircuitBreaker_ResetClearsFailuresWhenClosed pins that Reset clears the
// counters on an already-closed breaker, which transitionTo alone skips.
func TestCircuitBreaker_ResetClearsFailuresWhenClosed(t *testing.T) {
	cb := NewCircuitBreaker(WithMaxFailures(3), WithResetTimeout(time.Minute))

	for range 2 {
		cb.RecordFailure()
	}
	if cb.State() != CircuitClosed {
		t.Fatalf("breaker opened early, state = %v", cb.State())
	}

	cb.Reset()
	cb.RecordFailure()
	if state := cb.State(); state != CircuitClosed {
		t.Errorf("after Reset one failure opened the circuit, state = %v, want %v", state, CircuitClosed)
	}
}

// TestCircuitBreaker_SlowProbesStillClose pins that a caller slower than
// halfOpenMax requests per halfOpenTimeout can still close the circuit. The
// half-open watchdog used to measure the whole half-open window from entry, so
// a healthy provider probed once per window never closed.
func TestCircuitBreaker_SlowProbesStillClose(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(time.Millisecond),
		WithHalfOpenTimeout(60*time.Millisecond),
		WithHalfOpenMax(3),
	)

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}
	time.Sleep(5 * time.Millisecond)

	// Three probes spaced so that the last one lands past the original window.
	for i := range 3 {
		if !cb.Allow() {
			t.Fatalf("probe %d refused, state = %v", i, cb.State())
		}
		cb.RecordSuccess()
		time.Sleep(40 * time.Millisecond)
	}

	if state := cb.State(); state != CircuitClosed {
		t.Errorf("state = %v, want %v", state, CircuitClosed)
	}
}

// TestCircuitBreaker_StaleClosedResultDoesNotClose pins that a request admitted
// while the breaker was closed, which then completes after the breaker has
// opened and moved to half-open, is not counted as a half-open probe. With
// halfOpenMax 1 the stale success must not close the circuit on its own.
func TestCircuitBreaker_StaleClosedResultDoesNotClose(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(time.Millisecond),
		WithHalfOpenTimeout(time.Minute),
		WithHalfOpenMax(1),
	)

	// Request admitted while closed.
	admitted, staleProbe := cb.acquire()
	if !admitted || staleProbe {
		t.Fatalf("closed-state acquire = %v,%v; want true,false", admitted, staleProbe)
	}

	// Another request trips the breaker and the watchdog admits a half-open probe.
	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)
	if !cb.Allow() {
		t.Fatalf("half-open probe refused, state = %v", cb.State())
	}

	// The stale closed-state request now succeeds; it must not close the breaker.
	cb.recordSuccess(staleProbe)
	if state := cb.State(); state != CircuitHalfOpen {
		t.Errorf("stale success closed the circuit, state = %v, want %v", state, CircuitHalfOpen)
	}

	// The real probe succeeding closes it.
	cb.RecordSuccess()
	if state := cb.State(); state != CircuitClosed {
		t.Errorf("probe success did not close the circuit, state = %v, want %v", state, CircuitClosed)
	}
}
