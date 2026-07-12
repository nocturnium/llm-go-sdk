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
	state           CircuitState
	failures        int
	successes       int // Successes in half-open state
	halfOpenCount   int // Requests currently in flight in half-open
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

// Allow checks if a request should be allowed through
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if enough time has passed to try again
		if time.Since(cb.lastFailureTime) >= cb.resetTimeout {
			cb.transitionTo(CircuitHalfOpen)
			cb.halfOpenCount = 1
			return true
		}
		return false

	case CircuitHalfOpen:
		if time.Since(cb.lastStateChange) >= cb.halfOpenTimeout {
			cb.lastFailureTime = time.Now()
			cb.transitionTo(CircuitOpen)
			return false
		}
		// Allow limited requests in half-open state
		if cb.halfOpenCount < cb.halfOpenMax {
			cb.halfOpenCount++
			return true
		}
		return false

	default:
		return false
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		cb.failures = 0 // Reset failure count on success

	case CircuitHalfOpen:
		if cb.halfOpenCount == 0 {
			return
		}
		cb.halfOpenCount--
		cb.successes++
		// If we've had enough successes, close the circuit
		if cb.successes >= cb.halfOpenMax {
			cb.transitionTo(CircuitClosed)
		}

	case CircuitOpen:
		// No action needed when circuit is open
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failures++
		if cb.failures >= cb.maxFailures {
			cb.transitionTo(CircuitOpen)
		}

	case CircuitHalfOpen:
		if cb.halfOpenCount == 0 {
			return
		}
		cb.halfOpenCount--
		// Any failure in half-open goes back to open
		cb.transitionTo(CircuitOpen)

	case CircuitOpen:
		// Circuit is already open, just update the failure time
	}
}

// release abandons an allowed request without treating it as provider health
// evidence. In half-open it returns the in-flight probe permit.
func (cb *CircuitBreaker) release() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen && cb.halfOpenCount > 0 {
		cb.halfOpenCount--
	}
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionTo(CircuitClosed)
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
