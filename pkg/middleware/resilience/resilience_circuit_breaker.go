package resilience

import (
	"errors"
	"sync"
	"time"
)

const (
	stateUnknown = "unknown"
)

// Circuit breaker errors
var (
	ErrCircuitOpen    = errors.New("circuit breaker is open")
	ErrCircuitTimeout = errors.New("circuit breaker timeout")
)

// CircuitState represents the state of a circuit breaker
type CircuitState int

// CircuitState constants define the possible circuit breaker states.
const (
	// CircuitClosed means normal operation, requests allowed.
	CircuitClosed CircuitState = iota
	// CircuitOpen means failure threshold exceeded, requests blocked.
	CircuitOpen
	// CircuitHalfOpen means testing if service recovered.
	CircuitHalfOpen
)

// String returns a human-readable name for the circuit state
// ("closed", "open", or "half-open").
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return stateUnknown
	}
}

// CircuitBreaker prevents cascading failures by stopping requests when
// a service is experiencing problems
type CircuitBreaker struct {
	mu sync.RWMutex

	// Configuration
	maxFailures     int           // Failures before opening
	resetTimeout    time.Duration // Time before trying again
	halfOpenTimeout time.Duration // Maximum time allowed in half-open
	halfOpenSet     bool          // Whether halfOpenTimeout was explicitly configured
	halfOpenMax     int           // Max requests in half-open state
	onStateChange   func(from, to CircuitState)
	onCallbackPanic func(recovered any, from, to CircuitState)

	// State
	state         CircuitState
	failures      int
	successes     int // Successes in half-open state
	halfOpenCount int // Requests currently in flight in half-open
	// inflightClosed counts requests admitted while the breaker was closed.
	// A public Allow/RecordSuccess pair carries no probe token, so a completion
	// in half-open is attributed to a stale closed request while one is
	// outstanding, rather than closing the breaker as if it were a probe.
	//
	// The count is bucketed by generation so Reset can drop the pre-reset
	// requests: a late completion then finds no bucket to decrement instead of
	// consuming a post-reset request's mark. Without that, a stale completion
	// could let the breaker enter half-open while a new request is still
	// outstanding.
	inflightClosed map[uint64]int
	// publicPermits retains grants made through Allow until the matching public
	// RecordSuccess or RecordFailure call. The internal middleware passes its
	// permit directly, but the older public API has no permit parameter. Keeping
	// these tokens preserves the admission generation across Reset.
	publicPermits   []permit
	generation      uint64
	lastFailureTime time.Time
	lastStateChange time.Time

	// callbacks delivers onStateChange callbacks asynchronously in transition
	// order. Its own mutex is independent of cb.mu; callbacks are enqueued under
	// cb.mu but invoked off it. The zero value is ready to use.
	callbacks callbackQueue
}

// CircuitBreakerOption configures a CircuitBreaker
type CircuitBreakerOption func(*CircuitBreaker)

// NewCircuitBreaker creates a new circuit breaker with the given options
func NewCircuitBreaker(opts ...CircuitBreakerOption) *CircuitBreaker {
	cb := &CircuitBreaker{
		maxFailures:     5,
		resetTimeout:    30 * time.Second,
		halfOpenTimeout: 30 * time.Second,
		halfOpenMax:     3,
		state:           CircuitClosed,
		inflightClosed:  make(map[uint64]int),
		lastStateChange: time.Now(),
	}

	for _, opt := range opts {
		opt(cb)
	}

	return cb
}

// WithMaxFailures sets the number of failures before opening the circuit
func WithMaxFailures(n int) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		if n > 0 {
			cb.maxFailures = n
		}
	}
}

// WithResetTimeout sets the time to wait before transitioning from open to half-open
func WithResetTimeout(d time.Duration) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		if d > 0 {
			cb.resetTimeout = d
			if !cb.halfOpenSet {
				cb.halfOpenTimeout = d
			}
		}
	}
}

// WithHalfOpenTimeout sets the maximum time the circuit may remain half-open.
// Nonpositive durations keep the default, which follows the reset timeout.
func WithHalfOpenTimeout(d time.Duration) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		if d > 0 {
			cb.halfOpenTimeout = d
			cb.halfOpenSet = true
		}
	}
}

// WithHalfOpenMax sets the number of requests allowed in half-open state
func WithHalfOpenMax(n int) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		if n > 0 {
			cb.halfOpenMax = n
		}
	}
}

// WithOnStateChange sets a callback for state changes
func WithOnStateChange(fn func(from, to CircuitState)) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		cb.onStateChange = fn
	}
}

// WithOnCallbackPanic sets a callback invoked when the state-change callback
// panics. The recovered panic value and transition states are passed to fn. The
// panic hook runs in the same recovered goroutine as the state-change callback,
// and a panic in fn is also recovered.
func WithOnCallbackPanic(fn func(recovered any, from, to CircuitState)) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		cb.onCallbackPanic = fn
	}
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Allow checks if a request should be allowed through. It retains the grant so
// the next RecordSuccess or RecordFailure call reports that request's outcome.
// Completions are matched to grants in FIFO order, so callers that admit several
// requests and report them out of order should use the middleware path (or a
// dedicated CircuitBreaker per in-flight request) rather than the token-less
// public pair; a concurrent Reset cannot retroactively re-tag an outstanding
// grant.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	admitted, p := cb.acquireLocked()
	if admitted {
		cb.publicPermits = append(cb.publicPermits, p)
	}
	return admitted
}

// permit is the token acquire hands back for a granted request. It records
// whether the grant was a half-open probe and which generation the request was
// admitted in, so a completion can be attributed correctly even across a Reset.
type permit struct {
	halfOpen   bool
	generation uint64
}

// acquire admits a request and returns its permit. The middleware threads the
// permit back into recordSuccess/recordFailure/releasePermit so a request
// admitted while the breaker was closed is not mistaken for a half-open probe
// when it completes after a later transition, and a pre-Reset completion cannot
// retire a post-Reset request's mark.
func (cb *CircuitBreaker) acquire() (admitted bool, p permit) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.acquireLocked()
}

// acquireLocked admits a request. The caller holds cb.mu.
func (cb *CircuitBreaker) acquireLocked() (admitted bool, p permit) {

	switch cb.state {
	case CircuitClosed:
		// The type documents that its zero value is ready to use, so a caller
		// that constructs a bare CircuitBreaker must not panic on the first
		// admission. Lazily create the bucket map.
		if cb.inflightClosed == nil {
			cb.inflightClosed = make(map[uint64]int)
		}
		cb.inflightClosed[cb.generation]++
		return true, permit{generation: cb.generation}

	case CircuitOpen:
		// Check if enough time has passed to try again
		if time.Since(cb.lastFailureTime) >= cb.resetTimeout {
			// A closed-state request admitted before the breaker opened still
			// carries no probe token. Wait for it to settle before entering
			// half-open; otherwise its completion could be mistaken for a probe.
			if cb.hasInflightClosedLocked() {
				return false, permit{}
			}
			cb.transitionTo(CircuitHalfOpen)
			cb.halfOpenCount = 1
			return true, permit{halfOpen: true, generation: cb.generation}
		}
		return false, permit{}

	case CircuitHalfOpen:
		if time.Since(cb.lastStateChange) >= cb.halfOpenTimeout {
			cb.lastFailureTime = time.Now()
			cb.transitionTo(CircuitOpen)
			return false, permit{}
		}
		// Allow limited requests in half-open state
		if cb.halfOpenCount < cb.halfOpenMax {
			cb.halfOpenCount++
			return true, permit{halfOpen: true, generation: cb.generation}
		}
		return false, permit{}

	default:
		return false, permit{}
	}
}

// hasInflightClosedLocked reports whether any generation has a closed-state
// request outstanding. The caller holds cb.mu.
func (cb *CircuitBreaker) hasInflightClosedLocked() bool {
	for _, n := range cb.inflightClosed {
		if n > 0 {
			return true
		}
	}
	return false
}

// retireInflightClosedLocked retires a request admitted in generation gen. A
// completion from a dropped generation finds no bucket and is a no-op. The
// caller holds cb.mu.
func (cb *CircuitBreaker) retireInflightClosedLocked(gen uint64) {
	if n := cb.inflightClosed[gen]; n > 0 {
		if n == 1 {
			delete(cb.inflightClosed, gen)
		} else {
			cb.inflightClosed[gen] = n - 1
		}
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	p, ok := cb.takePublicPermitLocked()
	if !ok {
		// Record methods predate Allow and are also used to report an
		// externally observed health result. Preserve that use when there is no
		// unresolved public grant to match.
		p = permit{halfOpen: !cb.hasInflightClosedLocked(), generation: cb.generation}
	}
	cb.recordSuccessLocked(p)
}

// recordSuccess records a successful request. The permit is half-open for a
// request admitted while the breaker was closed; such a result is stale with
// respect to a later half-open window and must not consume a probe permit or
// count toward closing the breaker.
func (cb *CircuitBreaker) recordSuccess(p permit) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.recordSuccessLocked(p)
}

// recordSuccessLocked implements recordSuccess; the caller holds cb.mu.
func (cb *CircuitBreaker) recordSuccessLocked(p permit) {
	// Any success admitted before a Reset is stale, not just a half-open probe.
	// A closed-state success clears the failure count; letting a pre-reset
	// completion do that would erase failures the fresh breaker accumulated
	// after the operator reset it. (Failures stay asymmetric on purpose: a
	// pre-reset failure is still provider-health evidence and counts.)
	if p.generation != cb.generation {
		return
	}
	switch cb.state {
	case CircuitClosed:
		cb.failures = 0 // Reset failure count on success
		cb.retireInflightClosedLocked(p.generation)

	case CircuitHalfOpen:
		if !p.halfOpen {
			// A request admitted while closed completing in half-open: it holds
			// no probe permit, so it can neither close the breaker nor be
			// counted as a probe. Retire its in-flight mark.
			cb.retireInflightClosedLocked(p.generation)
			return
		}
		if cb.halfOpenCount == 0 {
			return
		}
		cb.halfOpenCount--
		cb.successes++
		// Each successful probe pushes the half-open deadline out. The deadline
		// exists to catch a trial that stalls, not to cap how long collecting
		// halfOpenMax probes may take: a caller making fewer than halfOpenMax
		// requests per halfOpenTimeout would otherwise be forced back open by
		// the watchdog in allowRequest and never close against a healthy
		// provider.
		cb.lastStateChange = time.Now()
		// If we've had enough successes, close the circuit
		if cb.successes >= cb.halfOpenMax {
			cb.transitionTo(CircuitClosed)
		}

	case CircuitOpen:
		// A stale closed-state request can also complete while the breaker is
		// open. Retire its in-flight mark: leaving it set would make the next
		// real half-open probe look stale and unable to close the breaker.
		cb.retireInflightClosedLocked(p.generation)
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	p, ok := cb.takePublicPermitLocked()
	if !ok {
		p = permit{halfOpen: !cb.hasInflightClosedLocked(), generation: cb.generation}
	}
	cb.recordFailureLocked(p)
}

// takePublicPermitLocked returns the oldest unresolved grant made through the
// public Allow API. Reset intentionally leaves old grants queued: their late
// completions must consume their old-generation permit before a post-reset
// completion can be attributed to a new request.
func (cb *CircuitBreaker) takePublicPermitLocked() (permit, bool) {
	if len(cb.publicPermits) == 0 {
		return permit{}, false
	}
	p := cb.publicPermits[0]
	cb.publicPermits[0] = permit{}
	cb.publicPermits = cb.publicPermits[1:]
	return p, true
}

// recordFailure records a failed request. A stale failure from a request
// admitted while the breaker was closed is still provider-health evidence and
// reopens a half-open breaker, but it does not consume a probe permit it never
// held.
func (cb *CircuitBreaker) recordFailure(p permit) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.recordFailureLocked(p)
}

// recordFailureLocked implements recordFailure; the caller holds cb.mu.
func (cb *CircuitBreaker) recordFailureLocked(p permit) {
	if cb.stalePermitLocked(p) {
		return
	}
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failures++
		cb.retireInflightClosedLocked(p.generation)
		if cb.failures >= cb.maxFailures {
			cb.transitionTo(CircuitOpen)
		}

	case CircuitHalfOpen:
		if !p.halfOpen {
			cb.retireInflightClosedLocked(p.generation)
			cb.transitionTo(CircuitOpen)
			return
		}
		if cb.halfOpenCount == 0 {
			return
		}
		cb.halfOpenCount--
		// Any failure in half-open goes back to open
		cb.transitionTo(CircuitOpen)

	case CircuitOpen:
		// Circuit is already open, just update the failure time. A stale
		// closed-state request completing here still retires its in-flight
		// mark, so the next real probe is not mistaken for it.
		cb.retireInflightClosedLocked(p.generation)
	}
}

// stalePermitLocked reports whether p is a half-open probe admitted before a
// Reset. Reset drops the half-open window (and the closed-state marks) by
// advancing the generation, so a probe from the old generation must not close,
// reopen, or consume a permit in the fresh one. A stale closed-state completion
// is still provider-health evidence, which recordFailure keeps. The caller
// holds cb.mu.
func (cb *CircuitBreaker) stalePermitLocked(p permit) bool {
	return p.halfOpen && p.generation != cb.generation
}

// releasePermit abandons an allowed request without treating it as provider
// health evidence. In half-open it returns the in-flight probe permit; a
// closed-state request instead retires its inflightClosed mark, which a later
// half-open window would otherwise misread as a stale request and use to refuse
// a real probe's success.
func (cb *CircuitBreaker) releasePermit(p permit) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if p.halfOpen && cb.state == CircuitHalfOpen && cb.halfOpenCount > 0 {
		cb.halfOpenCount--
		return
	}
	if !p.halfOpen {
		cb.retireInflightClosedLocked(p.generation)
	}
}

// Reset returns the circuit breaker to the closed state with a clean slate.
// The counters are cleared even when the breaker is already closed, because
// transitionTo is a no-op on an unchanged state and an operator resetting a
// closed breaker that sits one failure below the threshold expects the
// accumulated failures to be gone.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionTo(CircuitClosed)
	cb.failures = 0
	cb.successes = 0
	cb.halfOpenCount = 0
	// Drop the pre-reset closed-state marks by advancing the generation. A
	// request admitted before the reset that never settles would otherwise hold
	// the open-to-half-open transition off forever, defeating the operator's
	// escape hatch. A late completion finds no bucket for its generation and is
	// a no-op, so it cannot retire a post-reset request's mark.
	cb.generation++
	cb.inflightClosed = make(map[uint64]int)
}

// stateTransition holds a captured state-change callback and its arguments.
type stateTransition struct {
	callback      func(from, to CircuitState)
	panicCallback func(recovered any, from, to CircuitState)
	oldState      CircuitState
	newState      CircuitState
}

// invoke runs the captured callback with panic recovery. It must be called off
// the circuit-breaker lock (the callbackQueue drain goroutine does so).
func (t stateTransition) invoke() {
	if t.callback == nil {
		return
	}
	// Recover from panics in callbacks to prevent goroutine crashes.
	defer func() {
		if recovered := recover(); recovered != nil && t.panicCallback != nil {
			func() {
				defer func() { _ = recover() }()
				t.panicCallback(recovered, t.oldState, t.newState)
			}()
		}
	}()
	t.callback(t.oldState, t.newState)
}

// callbackQueue delivers state-change callbacks in FIFO transition order.
//
// Transitions are enqueued while the breaker lock is held (see transitionTo), so
// queue order is exactly the order transitions occurred; a single drain
// goroutine then invokes them one at a time OFF the lock. This preserves the
// asynchronous contract (a caller never blocks on a user callback) while fixing
// the prior per-transition-goroutine design, under which two rapid transitions
// could be observed out of order (e.g. Open→HalfOpen before Closed→Open).
//
// The drain goroutine runs only while work is pending and exits when the queue
// empties, so an idle breaker holds no goroutine and the breaker needs no
// Close()/Stop(). A blocking user callback stalls only later callbacks, never
// the breaker itself.
type callbackQueue struct {
	mu      sync.Mutex
	pending []stateTransition
	running bool
}

// enqueue appends a transition and starts the drain goroutine if idle. It is
// called under the breaker lock so append order matches transition order.
func (q *callbackQueue) enqueue(t stateTransition) {
	q.mu.Lock()
	q.pending = append(q.pending, t)
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.mu.Unlock()
	go q.drain()
}

// drain invokes queued callbacks in order until the queue empties, then exits.
func (q *callbackQueue) drain() {
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.pending = nil // release the backing array
			q.running = false
			q.mu.Unlock()
			return
		}
		t := q.pending[0]
		q.pending[0] = stateTransition{} // drop the reference for GC
		q.pending = q.pending[1:]
		q.mu.Unlock()

		t.invoke()
	}
}

// transitionTo changes the circuit breaker state, enqueuing the state-change
// callback (if any) in transition order for asynchronous, ordered delivery.
// It must be called with cb.mu held.
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	if cb.state == newState {
		return
	}

	oldState := cb.state
	cb.state = newState
	cb.lastStateChange = time.Now()

	// Reset counters based on new state
	switch newState {
	case CircuitClosed:
		cb.failures = 0
		cb.successes = 0
		cb.halfOpenCount = 0
	case CircuitOpen:
		cb.successes = 0
		cb.halfOpenCount = 0
	case CircuitHalfOpen:
		cb.successes = 0
		cb.halfOpenCount = 0
	}

	// Enqueue under the lock so callbacks are delivered in transition order.
	if cb.onStateChange != nil {
		cb.callbacks.enqueue(stateTransition{
			callback:      cb.onStateChange,
			panicCallback: cb.onCallbackPanic,
			oldState:      oldState,
			newState:      newState,
		})
	}
}
