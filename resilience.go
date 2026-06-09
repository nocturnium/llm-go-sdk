package llms

import (
	"context"
	"time"
)

// ResilientClient wraps an LLM with retry logic and circuit breaker
type ResilientClient struct {
	llm     LLM
	retry   *RetryConfig
	breaker *CircuitBreaker

	// Callbacks
	onRetry func(attempt int, err error, delay time.Duration)
}

// ResilienceOption configures a ResilientClient
type ResilienceOption func(*ResilientClient)

// NewResilientClient creates a new resilient LLM client
func NewResilientClient(llm LLM, opts ...ResilienceOption) *ResilientClient {
	rc := &ResilientClient{
		llm:     llm,
		retry:   DefaultRetryConfig(),
		breaker: NewCircuitBreaker(),
	}

	for _, opt := range opts {
		opt(rc)
	}

	return rc
}

// WithRetryConfig sets the retry configuration
func WithRetryConfig(cfg *RetryConfig) ResilienceOption {
	return func(rc *ResilientClient) {
		if cfg != nil {
			rc.retry = cfg
		}
	}
}

// WithCircuitBreaker sets a custom circuit breaker
func WithCircuitBreaker(cb *CircuitBreaker) ResilienceOption {
	return func(rc *ResilientClient) {
		if cb != nil {
			rc.breaker = cb
		}
	}
}

// WithOnRetry sets a callback that's called before each retry
func WithOnRetry(fn func(attempt int, err error, delay time.Duration)) ResilienceOption {
	return func(rc *ResilientClient) {
		rc.onRetry = fn
	}
}

// WithMaxRetries sets the maximum number of retry attempts
func WithMaxRetries(n int) ResilienceOption {
	return func(rc *ResilientClient) {
		if n >= 0 {
			rc.retry.MaxAttempts = n + 1 // +1 because MaxAttempts includes first try
		}
	}
}

// WithRetryDelay sets the initial retry delay
func WithRetryDelay(d time.Duration) ResilienceOption {
	return func(rc *ResilientClient) {
		rc.retry.InitialDelay = d
	}
}

// Call wraps the LLM's Call method with resilience
func (rc *ResilientClient) Call(ctx context.Context, prompt string, options ...CallOption) (string, error) {
	var result string
	err := rc.execute(ctx, func() error {
		var callErr error
		result, callErr = Call(ctx, rc.llm, prompt, options...)
		return callErr
	})
	return result, err
}

// GenerateContent wraps the LLM's GenerateContent method with resilience
func (rc *ResilientClient) GenerateContent(ctx context.Context, messages []Message, options ...CallOption) (*Response, error) {
	var result *Response
	err := rc.execute(ctx, func() error {
		var genErr error
		result, genErr = rc.llm.GenerateContent(ctx, messages, options...)
		return genErr
	})
	return result, err
}

// Stream wraps the LLM's Stream method with resilience.
//
// Streaming failures surface as a terminal error chunk on the returned channel
// rather than as the synchronous return value, so the circuit breaker is updated
// from the stream's actual outcome (success/failure) once it completes. Mid-stream
// retry is not possible once chunks have been emitted; only the initial connect
// error is handled synchronously.
func (rc *ResilientClient) Stream(ctx context.Context, messages []Message, options ...CallOption) (<-chan StreamChunk, error) {
	if !rc.breaker.Allow() {
		return nil, ErrCircuitOpen
	}
	src, err := rc.llm.Stream(ctx, messages, options...)
	if err != nil {
		if isProviderUnhealthy(err) {
			rc.breaker.RecordFailure()
		}
		return nil, err
	}

	opts := ApplyOptions(options...)
	var termErr error
	return WrapStreamWithFinalizer(ctx, src, opts,
		func(chunk StreamChunk) StreamChunk {
			if chunk.Error != nil {
				termErr = chunk.Error
			}
			return chunk
		},
		func() {
			switch {
			case termErr == nil:
				rc.breaker.RecordSuccess()
			case isProviderUnhealthy(termErr):
				rc.breaker.RecordFailure()
				// Cancellation / client-side errors leave the breaker untouched.
			}
		},
	), nil
}

func (rc *ResilientClient) execute(ctx context.Context, fn func() error) error {
	var lastErr error
	delay := rc.retry.InitialDelay

	for attempt := 0; attempt < rc.retry.MaxAttempts; attempt++ {
		// Check circuit breaker
		if !rc.breaker.Allow() {
			return ErrCircuitOpen
		}

		// Execute the function
		err := fn()
		if err == nil {
			rc.breaker.RecordSuccess()
			return nil
		}

		lastErr = err

		// Only count failures that indicate the provider itself is unhealthy
		// (retryable upstream errors: 429/5xx). Client-side and terminal errors —
		// context cancellation/deadline, 4xx other than 429, and circuit-open —
		// must NOT trip the breaker, otherwise a burst of bad requests or canceled
		// calls would needlessly open the circuit on an otherwise healthy provider.
		// This classification is deliberately independent of rc.retry.ShouldRetry,
		// which callers may override (e.g. to disable retries) without intending to
		// change what counts as a provider-health failure.
		if isProviderUnhealthy(err) {
			rc.breaker.RecordFailure()
		}

		// Check if we should retry
		if !rc.retry.ShouldRetry(err) {
			return err
		}

		// Don't retry if this was the last attempt
		if attempt >= rc.retry.MaxAttempts-1 {
			break
		}

		// Calculate delay with jitter
		jitteredDelay := calculateDelay(delay, rc.retry.Jitter)

		// Callback before retry
		if rc.onRetry != nil {
			rc.onRetry(attempt+1, err, jitteredDelay)
		}

		// Wait before retrying with proper timer cleanup.
		// Using time.NewTimer instead of time.After to prevent goroutine leaks
		// when context is canceled during the wait.
		timer := time.NewTimer(jitteredDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C // Drain the channel if timer already fired
			}
			return ctx.Err()
		case <-timer.C:
		}

		// Increase delay for next attempt
		delay = time.Duration(float64(delay) * rc.retry.BackoffFactor)
		if delay > rc.retry.MaxDelay {
			delay = rc.retry.MaxDelay
		}
	}

	return lastErr
}

// Provider returns the underlying LLM's provider
func (rc *ResilientClient) Provider() Provider {
	return rc.llm.Provider()
}

// Model returns the underlying LLM's model
func (rc *ResilientClient) Model() string {
	return rc.llm.Model()
}

// CircuitBreaker returns the circuit breaker for monitoring
func (rc *ResilientClient) CircuitBreaker() *CircuitBreaker {
	return rc.breaker
}

// Unwrap returns the underlying LLM
func (rc *ResilientClient) Unwrap() LLM {
	return rc.llm
}

// Ensure ResilientClient implements LLM
var _ LLM = (*ResilientClient)(nil)
