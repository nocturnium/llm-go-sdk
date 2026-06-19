package resilience

import (
	"context"
	"errors"
	"fmt"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

// ResilientClient wraps an LLM with retry logic and circuit breaker
type ResilientClient struct {
	llm     llms.LLM
	retry   *RetryConfig
	breaker *CircuitBreaker

	// Callbacks
	onRetry func(attempt int, err error, delay time.Duration)
}

// ResilienceOption configures a ResilientClient
type ResilienceOption func(*ResilientClient)

// NewResilientClient creates a new resilient LLM client
func NewResilientClient(llm llms.LLM, opts ...ResilienceOption) *ResilientClient {
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
			rc.retry = copyRetryConfig(cfg)
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
			rc.retry = copyRetryConfig(rc.retry)
			rc.retry.MaxAttempts = n + 1 // +1 because MaxAttempts includes first try
		}
	}
}

// WithRetryDelay sets the initial retry delay
func WithRetryDelay(d time.Duration) ResilienceOption {
	return func(rc *ResilientClient) {
		rc.retry = copyRetryConfig(rc.retry)
		rc.retry.InitialDelay = d
	}
}

func copyRetryConfig(cfg *RetryConfig) *RetryConfig {
	if cfg == nil {
		return DefaultRetryConfig()
	}
	copied := *cfg
	return &copied
}

// Call wraps the LLM's Call method with resilience
func (rc *ResilientClient) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	var result string
	err := rc.execute(ctx, func() error {
		var callErr error
		result, callErr = llms.Call(ctx, rc.llm, prompt, options...)
		return callErr
	})
	return result, err
}

// GenerateContent wraps the LLM's GenerateContent method with resilience
func (rc *ResilientClient) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	var result *llms.Response
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
func (rc *ResilientClient) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
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

	opts := llms.ApplyOptions(options...)
	var termErr error
	return llms.WrapStreamWithFinalizer(ctx, src, opts,
		func(chunk llms.StreamChunk) llms.StreamChunk {
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
	maxAttempts := rc.retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Check circuit breaker
		if !rc.breaker.Allow() {
			if lastErr != nil {
				return fmt.Errorf("%w: %w", ErrCircuitOpen, lastErr)
			}
			return ErrCircuitOpen
		}

		// Execute the function
		err := fn()
		if err == nil {
			rc.breaker.RecordSuccess()
			return nil
		}

		lastErr = err

		// Only terminal failures that indicate the provider itself is unhealthy
		// (retryable upstream errors: 429/5xx) count against the breaker.
		// Client-side and terminal errors —
		// context cancellation/deadline, 4xx other than 429, and circuit-open —
		// must NOT trip the breaker, otherwise a burst of bad requests or canceled
		// calls would needlessly open the circuit on an otherwise healthy provider.
		// This classification is deliberately independent of rc.retry.ShouldRetry,
		// which callers may override (e.g. to disable retries) without intending to
		// change what counts as a provider-health failure.
		providerUnhealthy := isProviderUnhealthy(err)

		// Check if we should retry
		if rc.retry.ShouldRetry == nil || !rc.retry.ShouldRetry(err) {
			if providerUnhealthy {
				rc.breaker.RecordFailure()
			}
			return err
		}

		// Don't retry if this was the last attempt
		if attempt >= maxAttempts-1 {
			if providerUnhealthy {
				rc.breaker.RecordFailure()
			}
			break
		}

		// Calculate delay with jitter
		jitteredDelay := calculateDelay(delay, rc.retry.Jitter)
		retryDelay := retryDelayForError(err, jitteredDelay, rc.retry.MaxDelay)

		// Callback before retry
		if rc.onRetry != nil {
			rc.onRetry(attempt+1, err, retryDelay)
		}

		// Wait before retrying with proper timer cleanup.
		// Using time.NewTimer instead of time.After to prevent goroutine leaks
		// when context is canceled during the wait.
		timer := time.NewTimer(retryDelay)
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

func retryDelayForError(err error, jitteredDelay, maxDelay time.Duration) time.Duration {
	retryDelay := jitteredDelay
	var apiErr *llms.APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > retryDelay {
		retryDelay = apiErr.RetryAfter
	}
	if maxDelay > 0 && retryDelay > maxDelay {
		return maxDelay
	}
	return retryDelay
}

// Provider returns the underlying LLM's provider
func (rc *ResilientClient) Provider() llms.Provider {
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
func (rc *ResilientClient) Unwrap() llms.LLM {
	return rc.llm
}

// Ensure ResilientClient implements LLM
var _ llms.LLM = (*ResilientClient)(nil)
