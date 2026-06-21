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
	halfOpenMax     int           // Max requests in half-open state
	onStateChange   func(from, to CircuitState)
	onCallbackPanic func(recovered any, from, to CircuitState)

	// State
	state           CircuitState
	failures        int
	successes       int // Successes in half-open state
	halfOpenCount   int // Requests attempted in half-open
	lastFailureTime time.Time
	lastStateChange time.Time
}

// CircuitBreakerOption configures a CircuitBreaker
type CircuitBreakerOption func(*CircuitBreaker)

// NewCircuitBreaker creates a new circuit breaker with the given options
func NewCircuitBreaker(opts ...CircuitBreakerOption) *CircuitBreaker {
	cb := &CircuitBreaker{
		maxFailures:     5,
		resetTimeout:    30 * time.Second,
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
	var transition *stateTransition
	defer func() {
		cb.mu.Unlock()
		// Explicit nil check to prevent panic if transition is nil
		if transition != nil {
			transition.invokeCallback()
		}
	}()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if enough time has passed to try again
		if time.Since(cb.lastFailureTime) >= cb.resetTimeout {
			transition = cb.transitionTo(CircuitHalfOpen)
			cb.halfOpenCount = 1
			return true
		}
		return false

	case CircuitHalfOpen:
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
	var transition *stateTransition
	defer func() {
		cb.mu.Unlock()
		// Explicit nil check to prevent panic if transition is nil
		if transition != nil {
			transition.invokeCallback()
		}
	}()

	switch cb.state {
	case CircuitClosed:
		cb.failures = 0 // Reset failure count on success

	case CircuitHalfOpen:
		cb.successes++
		// If we've had enough successes, close the circuit
		if cb.successes >= cb.halfOpenMax {
			transition = cb.transitionTo(CircuitClosed)
		}

	case CircuitOpen:
		// No action needed when circuit is open
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	var transition *stateTransition
	defer func() {
		cb.mu.Unlock()
		// Explicit nil check to prevent panic if transition is nil
		if transition != nil {
			transition.invokeCallback()
		}
	}()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failures++
		if cb.failures >= cb.maxFailures {
			transition = cb.transitionTo(CircuitOpen)
		}

	case CircuitHalfOpen:
		// Any failure in half-open goes back to open
		transition = cb.transitionTo(CircuitOpen)

	case CircuitOpen:
		// Circuit is already open, just update the failure time
	}
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	transition := cb.transitionTo(CircuitClosed)
	cb.mu.Unlock()
	transition.invokeCallback()
}

// stateTransition holds info about a state transition for callback invocation
type stateTransition struct {
	callback      func(from, to CircuitState)
	panicCallback func(recovered any, from, to CircuitState)
	oldState      CircuitState
	newState      CircuitState
}

// invokeCallback invokes the state change callback if one was captured.
// This should be called after releasing the lock.
//
// Note: Callbacks are invoked asynchronously in a separate goroutine to prevent
// deadlocks and allow the caller to continue immediately. Callbacks should be:
//   - Non-blocking or have their own timeout handling
//   - Safe to call concurrently (multiple transitions may occur rapidly)
//   - Designed to handle their own error recovery (panics will not affect the circuit breaker)
//
// If you need to perform work that requires a context (e.g., logging with trace context),
// capture the context in a closure when setting up the callback.
//
// The circuit breaker cannot cancel callbacks. A blocking callback will retain
// the goroutine running it, so callbacks must not block indefinitely.
func (t *stateTransition) invokeCallback() {
	if t != nil && t.callback != nil {
		go func() {
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
		}()
	}
}

// transitionTo changes the circuit breaker state and returns transition info.
// The caller should invoke the returned transition's callback after releasing the lock.
func (cb *CircuitBreaker) transitionTo(newState CircuitState) *stateTransition {
	if cb.state == newState {
		return nil
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

	// Return transition info for caller to invoke callback after releasing lock
	if cb.onStateChange != nil {
		return &stateTransition{
			callback:      cb.onStateChange,
			panicCallback: cb.onCallbackPanic,
			oldState:      oldState,
			newState:      newState,
		}
	}
	return nil
}
