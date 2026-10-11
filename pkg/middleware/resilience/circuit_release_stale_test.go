package resilience

import "testing"

// A half-open probe admitted before Reset holds no slot in the fresh window.
// Releasing it after the breaker re-enters half-open must not decrement the
// new generation's probe count.
func TestCircuitBreaker_ReleaseStaleProbeKeepsNewProbeCount(t *testing.T) {
	cb := NewCircuitBreaker(WithHalfOpenMax(2))

	cb.mu.Lock()
	cb.state = CircuitHalfOpen
	cb.halfOpenCount = 1
	stale := permit{halfOpen: true, generation: cb.generation}
	cb.mu.Unlock()

	cb.Reset()

	cb.mu.Lock()
	cb.state = CircuitHalfOpen
	cb.halfOpenCount = 1 // the new generation's own probe
	cb.mu.Unlock()

	cb.releasePermit(stale)

	cb.mu.Lock()
	got := cb.halfOpenCount
	cb.mu.Unlock()
	if got != 1 {
		t.Fatalf("halfOpenCount = %d after releasing a stale probe, want 1", got)
	}
}
