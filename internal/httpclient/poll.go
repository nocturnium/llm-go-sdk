package httpclient

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"time"
)

// PollPolicy controls polling delays and an optional overall deadline.
type PollPolicy struct {
	// Initial is the first delay after an unfinished poll.
	Initial time.Duration
	// Max is the maximum delay, including jitter.
	Max time.Duration
	// Multiplier grows the delay after each unfinished poll.
	Multiplier float64
	// Jitter is the random fractional variation in [0, 1].
	Jitter float64
	// Timeout bounds the entire operation; zero uses only the caller's context.
	Timeout time.Duration
}

// DefaultPollPolicy returns 1s initial, 10s maximum, 1.5x growth, and 20% jitter.
func DefaultPollPolicy() PollPolicy {
	return PollPolicy{Initial: time.Second, Max: 10 * time.Second, Multiplier: 1.5, Jitter: 0.2}
}

// ErrPollTimeout indicates that the policy's overall timeout elapsed.
var ErrPollTimeout = errors.New("poll timeout")

// Poll calls fn immediately, then with bounded exponential backoff until done.
// Callback errors are returned unchanged. Cancellation returns the caller's
// context error; expiry of Policy.Timeout returns ErrPollTimeout. The callback
// must honor its context. Successful completion takes precedence over
// Policy.Timeout expiring during that callback; the caller's own cancellation or
// deadline does not, so an abandoned operation never reports success. A zero
// policy uses DefaultPollPolicy.
func Poll(ctx context.Context, p PollPolicy, fn func(ctx context.Context) (done bool, err error)) error {
	p = normalizePollPolicy(p)
	parent := ctx
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeoutCause(ctx, p.Timeout, ErrPollTimeout)
		defer cancel()
	}
	delay := p.Initial
	for {
		if err := parent.Err(); err != nil {
			return err
		}
		done, err := fn(ctx)
		if done && err == nil {
			// A callback that ignores its context can report success after the
			// caller canceled. Caller cancellation wins: an abandoned operation
			// must not look successful just because a late callback returned.
			return parent.Err()
		}
		if err != nil {
			// A callback that returns the context error it observed is reporting
			// the policy deadline when that context actually expired; preserve
			// ErrPollTimeout for that case. Otherwise the callback error is the
			// operation's real cause and is returned unchanged, even when it is
			// context.Canceled or context.DeadlineExceeded.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if ctxErr := pollContextError(parent, ctx); ctxErr != nil {
					return ctxErr
				}
			}
			return err
		}
		if ctxErr := pollContextError(parent, ctx); ctxErr != nil {
			return ctxErr
		}
		timer := time.NewTimer(pollDelay(delay, p))
		select {
		case <-ctx.Done():
			timer.Stop()
			return pollContextError(parent, ctx)
		case <-timer.C:
		}
		delay = growDelay(delay, p.Multiplier, p.Max)
	}
}

// maxBackoffDuration is the largest duration a backoff computation may return.
// A float64 conversion of a value at or above 2^63 wraps to a negative
// time.Duration, which would make the timer fire immediately and let polling
// spin instead of respecting the configured maximum. Clamping below 2^63 keeps
// the conversion well-defined.
const maxBackoffDuration = time.Duration(math.MaxInt64)

// growDelay multiplies delay by multiplier, saturating at limit. The float
// product is clamped before the duration conversion so a policy whose Max (or a
// runaway multiplier) would exceed time.Duration's range cannot overflow to a
// negative duration.
func growDelay(delay time.Duration, multiplier float64, limit time.Duration) time.Duration {
	if limit <= 0 {
		limit = maxBackoffDuration
	}
	if limit > maxBackoffDuration {
		limit = maxBackoffDuration
	}
	next := float64(delay) * multiplier
	if math.IsNaN(next) || next <= 0 {
		return delay
	}
	if next >= float64(limit) {
		return limit
	}
	return time.Duration(next)
}

func pollContextError(parent, ctx context.Context) error {
	if err := parent.Err(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return nil
}

func normalizePollPolicy(p PollPolicy) PollPolicy {
	defaults := DefaultPollPolicy()
	if p == (PollPolicy{}) {
		return defaults
	}
	if p.Initial <= 0 {
		p.Initial = defaults.Initial
	}
	if p.Max <= 0 {
		p.Max = defaults.Max
	}
	if p.Initial > p.Max {
		p.Initial = p.Max
	}
	if p.Multiplier < 1 || math.IsNaN(p.Multiplier) || math.IsInf(p.Multiplier, 0) {
		p.Multiplier = defaults.Multiplier
	}
	if math.IsNaN(p.Jitter) {
		p.Jitter = 0
	}
	p.Jitter = math.Max(0, math.Min(1, p.Jitter))
	return p
}

func pollDelay(delay time.Duration, p PollPolicy) time.Duration {
	factor := 1.0
	if p.Jitter > 0 {
		factor += (2*rand.Float64() - 1) * p.Jitter // #nosec G404 -- scheduling jitter, not cryptography
	}
	limit := p.Max
	if limit <= 0 || limit > maxBackoffDuration {
		limit = maxBackoffDuration
	}
	next := float64(delay) * factor
	if next >= float64(limit) {
		return limit
	}
	return time.Duration(next)
}
