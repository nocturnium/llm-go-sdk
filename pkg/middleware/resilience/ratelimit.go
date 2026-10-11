package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"

	"golang.org/x/time/rate"
)

// maxInt returns the largest value an int can hold on this platform.
func maxInt() int { return int(^uint(0) >> 1) }

// Rate limiting errors. ErrRateLimitExceeded wraps the canonical
// llms.ErrRateLimited so a local rate-limiter rejection satisfies
// errors.Is(err, llms.ErrRateLimited) uniformly with a provider-reported 429,
// callers can match on the one sentinel regardless of which layer rate-limited.
var (
	ErrRateLimitExceeded = fmt.Errorf("rate limit exceeded: %w", llms.ErrRateLimited)
	ErrRateLimitTimeout  = errors.New("rate limit wait timeout")
)

// RateLimiter provides client-side rate limiting using token bucket algorithm
type RateLimiter struct {
	mu sync.RWMutex

	requestLimiter *rate.Limiter
	requestsPerMin int
	requestBurst   int

	tokenLimiter  *rate.Limiter
	tokensPerMin  int
	tokenBurst    int // Max tokens allowed to burst at once (0 = a full minute's budget)
	tokenEstimate int // Estimated tokens per request when actual count unknown
	// tokenCredit holds tokens refunded from an overestimate. The standard
	// library limiter has no positive refund operation; a negative ReserveN is
	// rejected as an oversized reservation, so later waits consume this credit
	// separately from the bucket.
	tokenCredit int
	// tokenDebt is the portion of a provider-reported usage count that could not
	// be charged in one RecordTokens call. A huge response leaves the limiter
	// charged in bounded installments; the remainder stays here and is settled
	// before later requests instead of being silently dropped.
	tokenDebt int

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

// maxTokenInstallments bounds how many burst-sized charges RecordTokens will
// make for one request. A provider-reported token count is untrusted input.
const maxTokenInstallments = 64

// tokenBucketBurst returns the configured token burst, defaulting to a full
// minute's budget (tokensPerMin) when WithTokenBurst was not set. A full-minute
// default is required because a single request may legitimately consume many
// tokens; callers who want tighter client-side pacing can lower it via
// WithTokenBurst.
//
// The default is raised to at least the per-request estimate so a configured
// budget below one request's estimate is still charged in full. Otherwise the
// Wait paths clamp the charge to the burst, and a 500/min budget against the
// default 1000-token estimate would bill only 500 tokens per call, letting
// callers consume roughly twice the configured rate. An explicit WithTokenBurst
// is honored as-is; the Wait paths cap the charge to it (n > burst is never
// satisfiable in golang.org/x/time/rate), which is the caller's chosen pacing.
func (rl *RateLimiter) tokenBucketBurst() int {
	if rl.tokenBurst > 0 {
		return rl.tokenBurst
	}
	return max(rl.tokensPerMin, rl.tokenEstimate)
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

// settleTokenDebt charges bounded installments of debt left by an earlier
// provider-reported usage count. It returns the longest reservation delay and
// any amount still owed. The reservations are not cancellable by the admitting
// request: those tokens were already consumed by a previous response.
//
// ctx bounds the work so a caller cancellation is observed between installments.
// A high token rate makes every installment zero-delay, so the loop below would
// otherwise charge the whole (potentially huge) debt while ignoring the wait
// deadline; an expired ctx stops it with the remainder still owed.
func (rl *RateLimiter) settleTokenDebt(ctx context.Context, tokenLimiter *rate.Limiter, now time.Time) (delay time.Duration, remaining int) {
	burst := tokenLimiter.Burst()
	if burst <= 0 {
		return 0, 0
	}
	for range maxTokenInstallments {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		rl.mu.Lock()
		debt := rl.tokenDebt
		if debt <= 0 {
			rl.mu.Unlock()
			break
		}
		credit := min(debt, rl.tokenCredit)
		if credit == debt {
			rl.tokenCredit -= credit
			rl.tokenDebt = 0
			rl.mu.Unlock()
			break
		}
		n := min(debt-credit, burst)
		// Reserve while holding rl.mu so a concurrent refund in RecordTokens
		// never observes the debt already deducted but not yet charged to the
		// bucket, which would cancel it against less debt than is outstanding.
		reservation := tokenLimiter.ReserveN(now, n)
		if !reservation.OK() {
			rl.mu.Unlock()
			break
		}
		// Credit that offset part of the debt leaves the debt with it; only
		// then is the reserved installment removed.
		rl.tokenCredit -= credit
		rl.tokenDebt -= credit + n
		rl.mu.Unlock()
		if d := reservation.DelayFrom(now); d > delay {
			delay = d
		}
	}
	rl.mu.RLock()
	remaining = rl.tokenDebt
	rl.mu.RUnlock()
	return delay, remaining
}

// consumeTokenCredit applies refunded tokens against the next reservation.
// The returned count is subtracted from the tokens to reserve and the credit is
// decremented by exactly that amount so a concurrent caller cannot spend it
// twice.
func (rl *RateLimiter) consumeTokenCredit(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	credit := min(tokens, rl.tokenCredit)
	rl.tokenCredit -= credit
	return credit
}

// restoreTokenCredit returns credit that was consumed for a reservation that
// was then canceled or refused.
func (rl *RateLimiter) restoreTokenCredit(credit int) {
	if credit <= 0 {
		return
	}
	rl.mu.Lock()
	rl.tokenCredit += credit
	rl.mu.Unlock()
}

// Wait blocks until the rate limit allows a request or returns an error
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.WaitN(ctx, 1, rl.tokenEstimate)
}

// WaitN blocks until the rate limit allows n requests with estimated tokens
func (rl *RateLimiter) WaitN(ctx context.Context, requests, tokens int) error {
	if requests <= 0 {
		return fmt.Errorf("rate limit requests must be positive")
	}
	if tokens < 0 {
		return fmt.Errorf("rate limit tokens must not be negative")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !rl.blocking {
		return rl.tryAcquire(ctx, requests, tokens)
	}

	waitCtx, cancel := context.WithTimeout(ctx, rl.waitTimeout)
	defer cancel()

	// Snapshot limiter pointers under the read lock so a concurrent Reset cannot
	// be observed mid-reassignment.
	requestLimiter := rl.requestLim()
	tokenLimiter := rl.tokenLim()
	if tokenLimiter != nil {
		for {
			delay, remaining := rl.settleTokenDebt(waitCtx, tokenLimiter, time.Now())
			if remaining == 0 && delay == 0 {
				break
			}
			if err := waitCtx.Err(); err != nil {
				if failure := rl.waitFailure(ctx, waitCtx, err); failure != nil {
					return failure
				}
				return ErrRateLimitTimeout
			}
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-waitCtx.Done():
					timer.Stop()
					if failure := rl.waitFailure(ctx, waitCtx, waitCtx.Err()); failure != nil {
						return failure
					}
					return ErrRateLimitTimeout
				}
			}
		}
	}

	// A request count above the burst cannot be satisfied by waiting. Do not
	// clamp it: admitting a multi-request call while charging the bucket for one
	// lets callers bypass the configured request rate. Return a local rate-limit
	// error rather than the permanent "exceeds burst" error from x/time/rate.
	if b := requestLimiter.Burst(); b > 0 && requests > b {
		return ErrRateLimitExceeded
	}

	// Reserve both buckets before waiting on either. Reserving the request
	// bucket first and only then discovering the token bucket cannot be met
	// would consume request capacity for a call that never starts, making the
	// next request wait or fail. ReserveN takes the capacity immediately, so a
	// failed or canceled wait must release every reservation it holds.
	now := time.Now()
	reqRes := requestLimiter.ReserveN(now, requests)
	if !reqRes.OK() {
		return ErrRateLimitTimeout
	}
	var tokRes *rate.Reservation
	consumedCredit := 0
	deferredTokens := 0
	if tokenLimiter != nil && tokens > 0 {
		// A refund is capacity already paid for by an earlier request, so it
		// reduces the whole charge. Apply it before splitting burst and debt:
		// offsetting only the burst portion while also carrying the credit into
		// debt would double-count it and overcharge the request.
		consumedCredit = rl.consumeTokenCredit(tokens)
		tokens -= consumedCredit
		// A charge above the burst cannot be satisfied by one reservation. Reserve
		// the burst-sized part now and carry the remainder as debt so the budget is
		// still enforced instead of treating the whole estimate as paid.
		if b := tokenLimiter.Burst(); tokens > b {
			deferredTokens = tokens - b
			tokens = b
		}
		if tokens == 0 {
			goto reserved
		}
		tokRes = tokenLimiter.ReserveN(now, tokens)
		if !tokRes.OK() {
			rl.restoreTokenCredit(consumedCredit)
			reqRes.CancelAt(now)
			return ErrRateLimitTimeout
		}
	}
	// The debt is committed only after the request is admitted: a canceled wait
	// never consumed the tokens, so it must not leave debt behind (and committing
	// early would also let a concurrent waiter settle debt for a request that may
	// yet be refused).

reserved:
	delay := reqRes.Delay()
	if tokRes != nil && tokRes.Delay() > delay {
		delay = tokRes.Delay()
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-waitCtx.Done():
			reqRes.Cancel()
			if tokRes != nil {
				tokRes.Cancel()
			}
			rl.restoreTokenCredit(consumedCredit)
			if failure := rl.waitFailure(ctx, waitCtx, waitCtx.Err()); failure != nil {
				return failure
			}
			return ErrRateLimitTimeout
		}
	}

	// The deadline may have passed while delay was zero; release and report.
	if err := waitCtx.Err(); err != nil {
		reqRes.Cancel()
		if tokRes != nil {
			tokRes.Cancel()
		}
		rl.restoreTokenCredit(consumedCredit)
		if failure := rl.waitFailure(ctx, waitCtx, err); failure != nil {
			return failure
		}
		return ErrRateLimitTimeout
	}
	if deferredTokens > 0 {
		rl.addTokenDebt(deferredTokens)
	}

	return nil
}

// addTokenDebt records the part of an admitted request's token charge that
// could not be reserved in one burst-sized reservation. Later requests settle it
// before they are admitted. Saturating keeps a pathological report from wrapping
// the debt negative, which would make it look settled.
func (rl *RateLimiter) addTokenDebt(n int) {
	if n <= 0 {
		return
	}
	rl.mu.Lock()
	rl.tokenDebt = saturatingAddInt(rl.tokenDebt, n)
	rl.mu.Unlock()
}

// tryAcquire attempts to acquire rate limit tokens without waiting
func (rl *RateLimiter) tryAcquire(ctx context.Context, requests, tokens int) error {
	requestLimiter := rl.requestLim()
	tokenLimiter := rl.tokenLim()
	if tokenLimiter != nil {
		// The non-blocking path cannot wait, but debt settlement must still stop
		// between installments when the caller's context is done rather than
		// charging a huge debt in one call.
		delay, remaining := rl.settleTokenDebt(ctx, tokenLimiter, time.Now())
		if err := ctx.Err(); err != nil {
			return err
		}
		if remaining > 0 || delay > 0 {
			return ErrRateLimitExceeded
		}
	}

	// A request count above the burst cannot be admitted without undercharging
	// the configured request rate. Refuse it instead of clamping to one.
	if b := requestLimiter.Burst(); b > 0 && requests > b {
		return ErrRateLimitExceeded
	}
	if tokenLimiter != nil && tokens > 0 {
		// Apply any refund before the burst check: credit is capacity already
		// paid for, so a charge whose effective size is within the burst must
		// still be admitted.
		credit := rl.consumeTokenCredit(tokens)
		tokens -= credit
		// A charge above the burst cannot be met by one reservation, and
		// x/time/rate refuses any count above the burst outright. Charge the
		// burst-sized part now and carry the remainder as debt, exactly like the
		// blocking path. Refusing the call instead left a limiter with an explicit
		// burst below the estimate rejecting every request forever.
		deferredTokens := 0
		if b := tokenLimiter.Burst(); b > 0 && tokens > b {
			deferredTokens = tokens - b
			tokens = b
		}
		if tokens == 0 {
			now := time.Now()
			reqRes := requestLimiter.ReserveN(now, requests)
			if !reqRes.OK() || reqRes.Delay() > 0 {
				reqRes.CancelAt(now)
				rl.restoreTokenCredit(credit)
				return ErrRateLimitExceeded
			}
			rl.addTokenDebt(deferredTokens)
			return nil
		}
		now := time.Now()
		reqRes := requestLimiter.ReserveN(now, requests)
		if !reqRes.OK() || reqRes.Delay() > 0 {
			reqRes.CancelAt(now)
			rl.restoreTokenCredit(credit)
			return ErrRateLimitExceeded
		}
		tokRes := tokenLimiter.ReserveN(now, tokens)
		if !tokRes.OK() || tokRes.Delay() > 0 {
			tokRes.CancelAt(now)
			rl.restoreTokenCredit(credit)
			reqRes.CancelAt(now)
			return ErrRateLimitExceeded
		}
		rl.addTokenDebt(deferredTokens)
		return nil
	}
	// Reserve the request bucket, then the token bucket. If the token bucket is
	// unavailable, release the request reservation so a call that never started
	// does not consume request capacity.
	now := time.Now()
	reqRes := requestLimiter.ReserveN(now, requests)
	if !reqRes.OK() || reqRes.Delay() > 0 {
		reqRes.CancelAt(now)
		return ErrRateLimitExceeded
	}

	return nil
}

// RecordTokens records actual token usage (for more accurate limiting).
// It settles a request that was admitted with the configured token estimate
// (Wait, or WaitN with tokens equal to the estimate) by charging or refunding
// the difference between that estimate and actualTokens. A request admitted with
// a different explicit count through WaitN must not be settled here, because the
// limiter does not remember per-request charges and would refund tokens it never
// charged.
func (rl *RateLimiter) RecordTokens(actualTokens int) {
	tokenLimiter := rl.tokenLim()
	if tokenLimiter == nil {
		return
	}
	if actualTokens < 0 {
		return
	}

	switch {
	case actualTokens > rl.tokenEstimate:
		// If we underestimated, reserve additional tokens. ReserveN refuses any
		// count above the burst and charges nothing for it, so a large
		// underestimate (a 200k-token request against a smaller burst) used to
		// cost the limiter zero. Charge it in burst-sized installments instead,
		// which is the same clamp the Wait paths document.
		// Compute the overflow as an int64 so a provider-reported count near
		// math.MaxInt cannot wrap negative and skip the charge entirely.
		extra64 := int64(actualTokens) - int64(rl.tokenEstimate)
		if extra64 <= 0 {
			return
		}
		burst := rl.tokenBucketBurst()
		if burst <= 0 {
			return
		}
		// Clamp to the largest value int can represent on this platform; the
		// result stays non-negative even for a report near math.MaxInt.
		if extra64 > int64(maxInt()) {
			extra64 = int64(maxInt())
		}
		extra := int(extra64)
		// ReserveN refuses any count above the burst and charges nothing for
		// it, so a large debt goes in burst-sized installments. Bound the number of
		// calls so one untrusted usage report cannot spin forever, and keep the
		// remainder as debt that later requests settle.
		now := time.Now()
		remainingInstallments := maxTokenInstallments
		for extra > 0 && remainingInstallments > 0 {
			n := min(extra, burst)
			tokenLimiter.ReserveN(now, n)
			extra -= n
			remainingInstallments--
		}
		if extra > 0 {
			rl.mu.Lock()
			rl.tokenDebt = saturatingAddInt(rl.tokenDebt, extra)
			rl.mu.Unlock()
		}
	case actualTokens < rl.tokenEstimate:
		// If we overestimated, refund unused tokens. An estimate above the burst is
		// charged as a burst-sized reservation plus debt for the remainder, so cancel
		// outstanding debt first, then credit whatever is left against the bucket.
		refund := rl.tokenEstimate - actualTokens
		rl.mu.Lock()
		if cancel := min(refund, rl.tokenDebt); cancel > 0 {
			rl.tokenDebt -= cancel
			refund -= cancel
		}
		available := rl.tokenBucketBurst() - int(tokenLimiter.Tokens()) - rl.tokenCredit
		if available < 0 {
			available = 0
		}
		if refund > available {
			refund = available
		}
		if refund > 0 {
			rl.tokenCredit += refund
		}
		rl.mu.Unlock()
	}
}

// saturatingAddInt adds two non-negative ints, clamping at maxInt instead of
// overflowing into a negative value that later requests would treat as settled.
func saturatingAddInt(a, b int) int {
	if a < 0 {
		a = 0
	}
	if b < 0 {
		b = 0
	}
	if a > maxInt()-b {
		return maxInt()
	}
	return a + b
}

// waitFailure classifies a limiter wait failure.
//
// A wait that ran out of time because the caller's own deadline was the
// binding one carries context.DeadlineExceeded as well as the sentinel: a
// retry layer that checks the context is then told not to retry a request
// whose deadline has passed, while one checking the sentinel still matches.
func (rl *RateLimiter) waitFailure(ctx, waitCtx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.Canceled) {
			return ctxErr
		}
		// Both, so a caller matching the sentinel still matches and one
		// checking the context learns the deadline is already past.
		return fmt.Errorf("%w: %w", ErrRateLimitTimeout, ctxErr)
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	// waitCtx carries the earlier of the caller's deadline and the limiter's
	// wait timeout, so comparing the two deadlines says which one bound the
	// wait. Comparing against the raw wait timeout instead would call the
	// limiter's own timeout a caller deadline once an earlier stage had
	// consumed part of the budget.
	callerDeadline, hasCaller := ctx.Deadline()
	waitDeadline, hasWait := waitCtx.Deadline()
	if hasCaller && (!hasWait || !callerDeadline.After(waitDeadline)) {
		return fmt.Errorf("%w: %w", ErrRateLimitTimeout, context.DeadlineExceeded)
	}
	// What is left is the limiter's own wait timeout, or rate's "would exceed
	// context deadline" against that timeout rather than the caller's.
	return ErrRateLimitTimeout
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
	rl.mu.RLock()
	tokens := tokenLimiter.Tokens() + float64(rl.tokenCredit) - float64(rl.tokenDebt)
	rl.mu.RUnlock()
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
	rl.tokenDebt = 0
	rl.tokenCredit = 0
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

	// Record actual tokens for more accurate limiting. A response that reports
	// no usage is left alone: recording zero would read as a gross
	// overestimate and refund the whole reservation, so a provider that never
	// reports usage would pace at zero tokens per request.
	if resp != nil && resp.Usage.TotalTokens > 0 {
		rlc.limiter.RecordTokens(resp.Usage.TotalTokens)
	}

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
		if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
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
		// Synthetic allows 125 requests per 5 hours, about 0.4/min. The field is a
		// whole number of requests per minute, so 1 is the closest it can express and
		// still pace; it remains above the published allowance.
		RequestsPerMinute: 1,
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
