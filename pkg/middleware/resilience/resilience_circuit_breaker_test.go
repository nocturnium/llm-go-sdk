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

// TestCircuitBreaker_ResetClearsInflightClosed pins that Reset also clears the
// closed-state in-flight count. A request admitted before Reset that never
// settles would otherwise hold the open-to-half-open transition off forever,
// defeating the operator's escape hatch.
func TestCircuitBreaker_ResetClearsInflightClosed(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(time.Millisecond),
		WithHalfOpenTimeout(time.Minute),
		WithHalfOpenMax(1),
	)

	// Two requests admitted while closed. One fails and opens the breaker; the
	// other is still outstanding, so its in-flight mark survives the open.
	for range 2 {
		if admitted, probe := cb.acquire(); !admitted || probe {
			t.Fatalf("closed-state acquire = %v,%v; want true,false", admitted, probe)
		}
	}
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}

	// Reset must clear the leftover in-flight mark: otherwise the stale request
	// holds the open-to-half-open transition off forever.
	cb.Reset()
	if cb.State() != CircuitClosed {
		t.Fatalf("state = %v, want closed", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}
	time.Sleep(5 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("half-open probe refused after Reset cleared the stale in-flight mark")
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

// TestCircuitBreaker_StaleClosedResultDefersHalfOpen pins the admission guard:
// while a request admitted in the closed state is still outstanding, the breaker
// does not enter half-open, so the stale completion cannot be mistaken for a
// probe. Once it settles, a real probe may close the circuit.
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
	if !cb.Allow() {
		t.Fatal("second closed-state request refused")
	}
	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)
	if cb.Allow() {
		t.Fatal("half-open probe admitted while a stale closed request was outstanding")
	}

	// The stale closed-state request settling must not itself close the breaker.
	cb.recordSuccess(staleProbe)
	if state := cb.State(); state != CircuitOpen {
		t.Errorf("stale success changed the circuit, state = %v, want %v", state, CircuitOpen)
	}

	// With every stale request settled, a fresh admission is the probe.
	if !cb.Allow() {
		t.Fatal("half-open probe refused after stale request settled")
	}

	// The real probe succeeding closes it.
	cb.RecordSuccess()
	if state := cb.State(); state != CircuitClosed {
		t.Errorf("probe success did not close the circuit, state = %v, want %v", state, CircuitClosed)
	}
}
