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
		if admitted, p := cb.acquire(); !admitted || p.halfOpen {
			t.Fatalf("closed-state acquire = %v,%+v; want true,non-probe", admitted, p)
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

// TestCircuitBreaker_ResetGenerationsIsolateCompletions pins that a completion
// from before Reset cannot retire a mark recorded after it. If the counters
// were a single shared integer, the stale completion would decrement the new
// request's mark and let the breaker enter half-open while that request was
// still outstanding.
func TestCircuitBreaker_ResetGenerationsIsolateCompletions(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(time.Millisecond),
		WithHalfOpenTimeout(time.Minute),
		WithHalfOpenMax(1),
	)

	admitted, stale := cb.acquire()
	if !admitted || stale.halfOpen {
		t.Fatalf("pre-reset acquire = %v,%+v", admitted, stale)
	}
	cb.Reset()

	// One post-reset request stays outstanding; a second trips the breaker.
	if admitted, p := cb.acquire(); !admitted || p.halfOpen {
		t.Fatalf("post-reset acquire = %v,%+v", admitted, p)
	}
	admitted, tripping := cb.acquire()
	if !admitted || tripping.halfOpen {
		t.Fatalf("post-reset acquire = %v,%+v", admitted, tripping)
	}
	cb.recordFailure(tripping) // trips the breaker open
	if cb.State() != CircuitOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}

	// The pre-reset request settling must not retire the post-reset request's
	// mark, so the open-to-half-open transition stays deferred.
	cb.recordSuccess(stale)
	time.Sleep(5 * time.Millisecond)
	if cb.Allow() {
		t.Fatal("half-open probe admitted while the post-reset request was outstanding")
	}
}

// TestCircuitBreaker_SlowProbesStillClose pins that a caller slower than
// TestCircuitBreaker_ResetDropsStaleHalfOpenProbe pins that a half-open probe
// admitted before Reset cannot reopen or consume a permit in the fresh
// generation once Reset has returned the breaker to a clean closed state.
func TestCircuitBreaker_ResetDropsStaleHalfOpenProbe(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(time.Millisecond),
		WithHalfOpenTimeout(time.Minute),
		WithHalfOpenMax(1),
	)
	cb.RecordFailure() // open
	time.Sleep(5 * time.Millisecond)
	admitted, probe := cb.acquire()
	if !admitted || !probe.halfOpen {
		t.Fatalf("probe acquire = %v,%+v", admitted, probe)
	}

	cb.Reset()
	if cb.State() != CircuitClosed {
		t.Fatalf("state after Reset = %v, want closed", cb.State())
	}

	// The stale probe failing must not reopen the fresh, clean breaker.
	cb.recordFailure(probe)
	if state := cb.State(); state != CircuitClosed {
		t.Errorf("stale probe failure reopened the breaker, state = %v, want closed", state)
	}
	if cb.failures != 0 {
		t.Errorf("stale probe failure counted as %d failures, want 0", cb.failures)
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
	admitted, stalePermit := cb.acquire()
	if !admitted || stalePermit.halfOpen {
		t.Fatalf("closed-state acquire = %v,%+v; want true,non-probe", admitted, stalePermit)
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
	cb.recordSuccess(stalePermit)
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

// TestCircuitBreaker_ZeroValueIsUsable pins that a bare CircuitBreaker, with no
// NewCircuitBreaker call, does not panic on its first admission. The type
// documents that its zero value is ready to use.
func TestCircuitBreaker_ZeroValueIsUsable(t *testing.T) {
	var cb CircuitBreaker
	if !cb.Allow() {
		t.Fatal("zero-value breaker refused a request")
	}
	cb.RecordSuccess()
	cb.RecordFailure()
	cb.Reset()
}

// TestCircuitBreaker_PublicCompletionCannotRetirePostResetRequest pins that a
// request admitted through the public Allow API before Reset keeps its own
// generation. If RecordSuccess synthesized the current generation, the stale
// completion would retire a post-Reset request's in-flight mark and let the
// breaker enter half-open while that request was still outstanding.
func TestCircuitBreaker_PublicCompletionCannotRetirePostResetRequest(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(time.Millisecond),
		WithHalfOpenTimeout(time.Minute),
		WithHalfOpenMax(1),
	)

	// A request admitted through Allow before Reset, still outstanding.
	if !cb.Allow() {
		t.Fatal("pre-reset Allow refused")
	}
	cb.Reset()

	// A post-reset request stays outstanding. It must keep its mark when the
	// pre-reset request's completion arrives below.
	if !cb.Allow() {
		t.Fatal("post-reset Allow refused")
	}

	// The pre-reset request fails, which opens the breaker. Its completion
	// carries the pre-reset generation and must not retire the post-reset mark.
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}

	time.Sleep(5 * time.Millisecond)
	if cb.Allow() {
		t.Fatal("half-open probe admitted while the post-reset request was outstanding")
	}
}
