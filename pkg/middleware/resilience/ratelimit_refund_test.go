package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRecordTokens_RefundCappedAtWhatWasCharged pins that an estimate above
// the burst is not refunded in full. The wait path clamps its charge to the
// burst, so refunding the whole estimate handed the limiter free tokens.
func TestRecordTokens_RefundCappedAtWhatWasCharged(t *testing.T) {
	rl := NewRateLimiter(
		WithTokensPerMinute(10000),
		WithTokenBurst(100),
		WithTokenEstimate(5000),
	)

	before := rl.tokenLim().Tokens()
	rl.RecordTokens(10)
	after := rl.tokenLim().Tokens()

	if after > before {
		t.Errorf("token bucket went from %.0f to %.0f: the refund exceeded the charge", before, after)
	}
}

// TestWaitN_ReportsCallerDeadline pins that the caller's own expired deadline
// is returned as itself. Reporting it as ErrRateLimitTimeout told a retry
// layer to try again on a request whose deadline had already passed.
func TestWaitN_ReportsCallerDeadline(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(1),
		WithRequestBurst(1),
		WithBlocking(true),
		WithWaitTimeout(time.Minute),
	)

	// Drain the single burst token so the next call has to wait.
	if err := rl.Wait(context.Background()); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := rl.Wait(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}
