package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"

	"golang.org/x/time/rate"
)

// Rate limiting errors. ErrRateLimitExceeded wraps the canonical
// llms.ErrRateLimited so a local rate-limiter rejection satisfies
// errors.Is(err, llms.ErrRateLimited) uniformly with a provider-reported 429 —
// callers can match on the one sentinel regardless of which layer rate-limited.
var (
	ErrRateLimitExceeded = fmt.Errorf("rate limit exceeded: %w", llms.ErrRateLimited)
	ErrRateLimitTimeout  = errors.New("rate limit wait timeout")
)

// RateLimiter provides client-side rate limiting using token bucket algorithm
type RateLimiter struct {
	mu sync.RWMutex

	// Request rate limiting
	requestLimiter *rate.Limiter
	requestsPerMin int
	requestBurst   int

	// Token rate limiting (optional)
	tokenLimiter  *rate.Limiter
	tokensPerMin  int
	tokenBurst    int // Max tokens allowed to burst at once (0 = a full minute's budget)
	tokenEstimate int // Estimated tokens per request when actual count unknown

	// Behavior
	blocking    bool          // Wait for rate limit or return error immediately
	waitTimeout time.Duration // Max time to wait for rate limit (if blocking)
}

// RateLimitOption configures a RateLimiter
type RateLimitOption func(*RateLimiter)

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(opts ...RateLimitOption) *RateLimiter {
	rl := &RateLimiter{
		requestsPerMin: 60,               // Default: 60 requests/min
		requestBurst:   1,                // Default: paced requests, no minute-sized burst
		tokensPerMin:   0,                // Disabled by default
		tokenEstimate:  1000,             // Default estimate
		blocking:       true,             // Wait by default
		waitTimeout:    30 * time.Second, // 30s timeout
	}

	for _, opt := range opts {
		opt(rl)
	}

	// Initialize request limiter
	requestRate := rate.Limit(float64(rl.requestsPerMin) / 60.0) // Per second
	rl.requestLimiter = rate.NewLimiter(requestRate, rl.requestBurst)

	// Initialize token limiter if configured
	if rl.tokensPerMin > 0 {
		tokenRate := rate.Limit(float64(rl.tokensPerMin) / 60.0)
		rl.tokenLimiter = rate.NewLimiter(tokenRate, rl.tokenBucketBurst())
	}

	return rl
}

// tokenBucketBurst returns the configured token burst, defaulting to a full
// minute's budget (tokensPerMin) when WithTokenBurst was not set. A full-minute
// default is required because a single request may legitimately consume many
// tokens; callers who want tighter client-side pacing can lower it via
// WithTokenBurst.
//
// NOTE: a per-request token count larger than this burst would make
// golang.org/x/time/rate's WaitN/AllowN reject every call (n > burst is never
// satisfiable). Rather than override an explicit WithTokenBurst here, the Wait
// paths cap the requested token count to the burst — see WaitN/tryAcquire — which
// preserves the caller's chosen burst while avoiding a self-inflicted outage.
func (rl *RateLimiter) tokenBucketBurst() int {
	if rl.tokenBurst > 0 {
		return rl.tokenBurst
	}
	return rl.tokensPerMin
}

// WithRequestsPerMinute sets the maximum requests per minute
func WithRequestsPerMinute(n int) RateLimitOption {
	return func(rl *RateLimiter) {
		if n > 0 {
			rl.requestsPerMin = n
		}
	}
}

// WithRequestBurst sets the maximum number of requests allowed to burst at once.
// The default is 1 so requests are paced instead of allowing an entire minute's
// quota immediately.
func WithRequestBurst(n int) RateLimitOption {
	return func(rl *RateLimiter) {
		if n > 0 {
			rl.requestBurst = n
		}
	}
}

// WithTokensPerMinute sets the maximum tokens per minute
func WithTokensPerMinute(n int) RateLimitOption {
	return func(rl *RateLimiter) {
		if n > 0 {
			rl.tokensPerMin = n
		}
	}
}

// WithTokenBurst sets the maximum number of tokens allowed to burst at once.
// When unset (0) the limiter allows a full minute's token budget to burst, which
// preserves support for large single requests; set a smaller value for tighter
// client-side token pacing.
func WithTokenBurst(n int) RateLimitOption {
	return func(rl *RateLimiter) {
		if n > 0 {
			rl.tokenBurst = n
		}
	}
}

// WithTokenEstimate sets the estimated tokens per request
// Used when actual token count is unknown before the request
func WithTokenEstimate(n int) RateLimitOption {
	return func(rl *RateLimiter) {
		if n > 0 {
			rl.tokenEstimate = n
		}
	}
}

// WithBlocking sets whether to wait for rate limit or return error
func WithBlocking(blocking bool) RateLimitOption {
	return func(rl *RateLimiter) {
		rl.blocking = blocking
	}
}

// WithWaitTimeout sets the maximum time to wait when blocking
func WithWaitTimeout(d time.Duration) RateLimitOption {
	return func(rl *RateLimiter) {
		if d > 0 {
			rl.waitTimeout = d
		}
	}
}

// requestLim returns the current request limiter under a read lock. The pointer
// is read while holding mu.RLock so it is never observed mid-reassignment by
// Reset (which swaps the pointer under mu.Lock). The returned *rate.Limiter is
// itself internally synchronized, so callers may use it after releasing the lock.
func (rl *RateLimiter) requestLim() *rate.Limiter {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.requestLimiter
}

// tokenLim returns the current token limiter (may be nil) under a read lock.
// See requestLim for the locking rationale.
func (rl *RateLimiter) tokenLim() *rate.Limiter {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.tokenLimiter
}

// Wait blocks until the rate limit allows a request or returns an error
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.WaitN(ctx, 1, rl.tokenEstimate)
}

// WaitN blocks until the rate limit allows n requests with estimated tokens
func (rl *RateLimiter) WaitN(ctx context.Context, requests, tokens int) error {
	if !rl.blocking {
		return rl.tryAcquire(requests, tokens)
	}

	// Create timeout context
	waitCtx, cancel := context.WithTimeout(ctx, rl.waitTimeout)
	defer cancel()

	// Snapshot limiter pointers under the read lock so a concurrent Reset cannot
	// be observed mid-reassignment.
	requestLimiter := rl.requestLim()
	tokenLimiter := rl.tokenLim()

	// Wait for request limit
	if err := requestLimiter.WaitN(waitCtx, requests); err != nil {
		// rate.Limiter returns context.DeadlineExceeded or a wrapped error
		// when the wait would exceed the deadline
		if errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return ErrRateLimitTimeout
		}
		// Check for context cancellation
		if errors.Is(err, context.Canceled) {
			return err
		}
		// For rate package specific errors (e.g., "would exceed context deadline")
		// treat as timeout
		return ErrRateLimitTimeout
	}

	// Wait for token limit if configured
	if tokenLimiter != nil && tokens > 0 {
		// Never request more than the bucket can hold, or WaitN rejects it forever.
		if b := tokenLimiter.Burst(); tokens > b {
			tokens = b
		}
		if err := tokenLimiter.WaitN(waitCtx, tokens); err != nil {
			if errors.Is(err, context.DeadlineExceeded) ||
				errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return ErrRateLimitTimeout
			}
			if errors.Is(err, context.Canceled) {
				return err
			}
			return ErrRateLimitTimeout
		}
	}

	return nil
}

// tryAcquire attempts to acquire rate limit tokens without waiting
func (rl *RateLimiter) tryAcquire(requests, tokens int) error {
	requestLimiter := rl.requestLim()
	tokenLimiter := rl.tokenLim()

	if !requestLimiter.AllowN(time.Now(), requests) {
		return ErrRateLimitExceeded
	}

	if tokenLimiter != nil && tokens > 0 {
		if b := tokenLimiter.Burst(); tokens > b {
			tokens = b
		}
		if !tokenLimiter.AllowN(time.Now(), tokens) {
			return ErrRateLimitExceeded
		}
	}

	return nil
}

// RecordTokens records actual token usage (for more accurate limiting)
// This can be called after a request to adjust for the difference between
// estimated and actual tokens
func (rl *RateLimiter) RecordTokens(actualTokens int) {
	tokenLimiter := rl.tokenLim()
	if tokenLimiter == nil {
		return
	}

	switch {
	case actualTokens > rl.tokenEstimate:
		// If we underestimated, reserve additional tokens.
		extra := actualTokens - rl.tokenEstimate
		tokenLimiter.ReserveN(time.Now(), extra)
	case actualTokens < rl.tokenEstimate:
		// If we overestimated, refund unused tokens without exceeding burst.
		refund := rl.tokenEstimate - actualTokens
		if remaining := rl.tokenBucketBurst() - int(tokenLimiter.Tokens()); refund > remaining {
			refund = remaining
		}
		if refund > 0 {
			tokenLimiter.ReserveN(time.Now(), -refund)
		}
	}
}

// RequestsRemaining returns the approximate number of requests that can be made immediately
func (rl *RateLimiter) RequestsRemaining() int {
	tokens := rl.requestLim().Tokens()
	if tokens < 0 {
		return 0
	}
	return int(tokens)
}

// TokensRemaining returns the approximate number of tokens that can be used immediately
func (rl *RateLimiter) TokensRemaining() int {
	tokenLimiter := rl.tokenLim()
	if tokenLimiter == nil {
		return -1 // Not configured
	}
	tokens := tokenLimiter.Tokens()
	if tokens < 0 {
		return 0
	}
	return int(tokens)
}

// Reset resets the rate limiter to full capacity
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Recreate limiters at full burst
	requestRate := rate.Limit(float64(rl.requestsPerMin) / 60.0)
	rl.requestLimiter = rate.NewLimiter(requestRate, rl.requestBurst)

	if rl.tokensPerMin > 0 {
		tokenRate := rate.Limit(float64(rl.tokensPerMin) / 60.0)
		rl.tokenLimiter = rate.NewLimiter(tokenRate, rl.tokenBucketBurst())
	}
}

// RateLimitedClient wraps an LLM with client-side rate limiting
type RateLimitedClient struct {
	llm     llms.LLM
	limiter *RateLimiter
}

// NewRateLimitedClient creates a new rate-limited LLM client
func NewRateLimitedClient(llm llms.LLM, opts ...RateLimitOption) *RateLimitedClient {
	return &RateLimitedClient{
		llm:     llm,
		limiter: NewRateLimiter(opts...),
	}
}

// NewRateLimitedClientWithLimiter creates a rate-limited client with a shared limiter
func NewRateLimitedClientWithLimiter(llm llms.LLM, limiter *RateLimiter) *RateLimitedClient {
	return &RateLimitedClient{
		llm:     llm,
		limiter: limiter,
	}
}

// Call wraps the LLM's Call method with rate limiting
func (rlc *RateLimitedClient) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	if err := rlc.limiter.Wait(ctx); err != nil {
		return "", err
	}
	return llms.Call(ctx, rlc.llm, prompt, options...)
}

// GenerateContent wraps the LLM's GenerateContent method with rate limiting
func (rlc *RateLimitedClient) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	if err := rlc.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	resp, err := rlc.llm.GenerateContent(ctx, messages, options...)
	if err != nil {
		return nil, err
	}

	// Record actual tokens for more accurate limiting
	rlc.limiter.RecordTokens(resp.Usage.TotalTokens)

	return resp, nil
}

// Stream wraps the LLM's Stream method with rate limiting
func (rlc *RateLimitedClient) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	if err := rlc.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	stream, err := rlc.llm.Stream(ctx, messages, options...)
	if err != nil {
		return nil, err
	}

	opts := llms.ApplyOptions(options...)
	return llms.WrapStream(ctx, stream, opts, func(chunk llms.StreamChunk) llms.StreamChunk {
		if chunk.Usage != nil {
			rlc.limiter.RecordTokens(chunk.Usage.TotalTokens)
		}
		return chunk
	}), nil
}

// Provider returns the underlying LLM's provider
func (rlc *RateLimitedClient) Provider() llms.Provider {
	return rlc.llm.Provider()
}

// Model returns the underlying LLM's model
func (rlc *RateLimitedClient) Model() string {
	return rlc.llm.Model()
}

// Limiter returns the rate limiter for monitoring
func (rlc *RateLimitedClient) Limiter() *RateLimiter {
	return rlc.limiter
}

// Unwrap returns the underlying LLM
func (rlc *RateLimitedClient) Unwrap() llms.LLM {
	return rlc.llm
}

// Ensure RateLimitedClient implements LLM
var _ llms.LLM = (*RateLimitedClient)(nil)

// ProviderRateLimits contains default rate limits for known providers
// These are conservative defaults - actual limits may be higher based on tier
var ProviderRateLimits = map[llms.Provider]struct {
	RequestsPerMinute int
	TokensPerMinute   int
}{
	llms.ProviderOpenAI: {
		RequestsPerMinute: 60,    // Tier 1 default
		TokensPerMinute:   60000, // Tier 1 default
	},
	llms.ProviderAnthropic: {
		RequestsPerMinute: 60,    // Default
		TokensPerMinute:   80000, // Default
	},
	llms.ProviderGemini: {
		RequestsPerMinute: 60,      // Free tier
		TokensPerMinute:   1000000, // Very generous
	},
	llms.ProviderTogetherAI: {
		RequestsPerMinute: 60,
		TokensPerMinute:   100000,
	},
	llms.ProviderFeatherless: {
		RequestsPerMinute: 60,     // Conservative default
		TokensPerMinute:   100000, // Conservative default
	},
	llms.ProviderSynthetic: {
		RequestsPerMinute: 25,    // 125 requests per 5 hours = 25/hour = ~0.4/min, use burst
		TokensPerMinute:   50000, // Conservative default (no per-token pricing)
	},
}

// NewProviderRateLimitedClient creates a rate-limited client with provider defaults
func NewProviderRateLimitedClient(llm llms.LLM, opts ...RateLimitOption) *RateLimitedClient {
	provider := llm.Provider()

	// Start with provider defaults if available
	var defaultOpts []RateLimitOption
	if limits, ok := ProviderRateLimits[provider]; ok {
		defaultOpts = append(defaultOpts,
			WithRequestsPerMinute(limits.RequestsPerMinute),
			WithTokensPerMinute(limits.TokensPerMinute),
		)
	}

	// User options override defaults
	defaultOpts = append(defaultOpts, opts...)

	return NewRateLimitedClient(llm, defaultOpts...)
}

// SharedRateLimiter allows multiple clients to share a rate limit
type SharedRateLimiter struct {
	limiters map[string]*RateLimiter
	mu       sync.RWMutex
}

// NewSharedRateLimiter creates a shared rate limiter manager
func NewSharedRateLimiter() *SharedRateLimiter {
	return &SharedRateLimiter{
		limiters: make(map[string]*RateLimiter),
	}
}

// GetLimiter returns or creates a rate limiter for the given key
func (srl *SharedRateLimiter) GetLimiter(key string, opts ...RateLimitOption) *RateLimiter {
	srl.mu.Lock()
	defer srl.mu.Unlock()

	if limiter, exists := srl.limiters[key]; exists {
		return limiter
	}

	limiter := NewRateLimiter(opts...)
	srl.limiters[key] = limiter
	return limiter
}

// GetProviderLimiter returns a rate limiter for a provider with default limits
func (srl *SharedRateLimiter) GetProviderLimiter(provider llms.Provider) *RateLimiter {
	key := string(provider)

	srl.mu.Lock()
	defer srl.mu.Unlock()

	if limiter, exists := srl.limiters[key]; exists {
		return limiter
	}

	var opts []RateLimitOption
	if limits, ok := ProviderRateLimits[provider]; ok {
		opts = append(opts,
			WithRequestsPerMinute(limits.RequestsPerMinute),
			WithTokensPerMinute(limits.TokensPerMinute),
		)
	}

	limiter := NewRateLimiter(opts...)
	srl.limiters[key] = limiter
	return limiter
}

// RemoveLimiter removes a rate limiter
func (srl *SharedRateLimiter) RemoveLimiter(key string) {
	srl.mu.Lock()
	defer srl.mu.Unlock()
	delete(srl.limiters, key)
}

// ResetAll resets all rate limiters
func (srl *SharedRateLimiter) ResetAll() {
	srl.mu.RLock()
	defer srl.mu.RUnlock()

	for _, limiter := range srl.limiters {
		limiter.Reset()
	}
}
