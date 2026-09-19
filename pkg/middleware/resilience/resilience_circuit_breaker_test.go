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
