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
