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
// must honor its context. Successful completion takes precedence over a deadline
// reached during that callback. A zero policy uses DefaultPollPolicy.
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
		if err := pollContextError(parent, ctx); err != nil {
			return err
		}
		done, err := fn(ctx)
		if done && err == nil {
			return nil
		}
		if ctxErr := pollContextError(parent, ctx); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		timer := time.NewTimer(pollDelay(delay, p))
		select {
		case <-ctx.Done():
			timer.Stop()
			return pollContextError(parent, ctx)
		case <-timer.C:
		}
		delay = time.Duration(math.Min(float64(p.Max), float64(delay)*p.Multiplier))
	}
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
	return time.Duration(math.Min(float64(p.Max), float64(delay)*factor))
}
