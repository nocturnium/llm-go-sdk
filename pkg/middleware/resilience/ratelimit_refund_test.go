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

	// Drain the bucket first: a full bucket is capped by the pre-existing
	// remaining-room clamp, which would hide the charged clamp this test is
	// about.
	rl.tokenLim().ReserveN(time.Now(), 100)

	before := rl.tokenLim().Tokens()
	rl.RecordTokens(10)
	after := rl.tokenLim().Tokens()

	// The wait path could only have charged the burst (100), so at most 90
	// tokens may come back, not the 4990 the raw estimate implies.
	if gained := after - before; gained > 90.5 {
		t.Errorf("refund returned %.0f tokens, want at most 90: the clamp used the estimate, not the charge", gained)
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
