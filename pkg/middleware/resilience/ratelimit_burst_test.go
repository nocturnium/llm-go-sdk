package resilience

import (
	"context"
	"testing"
	"time"
)

// TestRateLimiter_TokenEstimateExceedingBurstDoesNotWedge covers the self-
// inflicted-outage bug: golang.org/x/time/rate rejects any WaitN/AllowN where n
// exceeds the bucket burst, so a per-request token estimate larger than the burst
// (e.g. WithTokensPerMinute(500) or WithTokenBurst(<1000) against the default
// 1000-token estimate) made every call fail immediately. The Wait paths now
// reserve the burst-sized portion and carry the remainder as debt, so calls
// throttle instead of failing forever while the whole estimate is still owed.
func TestRateLimiter_TokenEstimateExceedingBurstDoesNotWedge(t *testing.T) {
	cases := []struct {
		name string
		rl   *RateLimiter
	}{
		{"tokensPerMinute below estimate", NewRateLimiter(WithTokensPerMinute(500))},
		{"explicit burst below estimate", NewRateLimiter(WithTokensPerMinute(60000), WithTokenBurst(100))},
		{"non-blocking below estimate", NewRateLimiter(WithTokensPerMinute(500), WithBlocking(false))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.rl.Wait(context.Background()); err != nil {
				t.Errorf("first request rejected (self-inflicted outage): %v", err)
			}
		})
	}

	// An oversized single-call token count is admitted, but the part above the
	// burst remains debt rather than disappearing.
	big := NewRateLimiter(WithTokensPerMinute(60000))
	if err := big.WaitN(context.Background(), 1, 10_000_000); err != nil {
		t.Errorf("oversized per-call token request should be debt-financed: %v", err)
	}
	if got := big.tokenDebt; got == 0 {
		t.Fatal("oversized token charge left no debt, so the estimate was treated as fully paid")
	}
}

// A wait that is canceled before admission must leave no debt behind: the
// request never consumed the tokens, and a later caller should not pay for it.
func TestRateLimiter_CanceledOversizedWaitLeavesNoDebt(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(60000), WithRequestBurst(1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rl.WaitN(ctx, 1, 10_000_000); err == nil {
		t.Fatal("WaitN with a canceled context succeeded")
	}
	if got := rl.tokenDebt; got != 0 {
		t.Fatalf("canceled wait left tokenDebt=%d, want 0", got)
	}
}

// When actual usage is lower than the estimated burst-plus-debt charge, the
// refund must cancel outstanding debt before adding bucket credit. Otherwise a
// later request still pays for tokens this response never used.
func TestRateLimiter_RecordTokensRefundsOverflowDebt(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(60000), WithTokenBurst(1000), WithTokenEstimate(10_000))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = rl.WaitN(ctx, 1, 10_000)

	rl.mu.RLock()
	debtBefore := rl.tokenDebt
	rl.mu.RUnlock()
	if debtBefore == 0 {
		t.Fatal("setup did not create overflow debt")
	}

	rl.RecordTokens(1_000)
	rl.mu.RLock()
	debtAfter := rl.tokenDebt
	rl.mu.RUnlock()
	if debtAfter != 0 {
		t.Fatalf("tokenDebt after a full refund = %d, want 0", debtAfter)
	}
}

// A partial credit must reduce the whole charge before the burst/debt split.
// With burst 1000, credit 500, and a 2000-token request, an implementation that
// carried the consumed credit into debt as well would charge 2500 tokens.
func TestRateLimiter_PartialCreditDoesNotOverchargeOversizedRequest(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(60000), WithTokenBurst(1000), WithTokenEstimate(10_000))
	rl.mu.Lock()
	rl.tokenCredit = 500
	rl.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rl.WaitN(ctx, 1, 2000); err != nil {
		t.Fatalf("WaitN failed: %v", err)
	}

	rl.mu.RLock()
	debt := rl.tokenDebt
	credit := rl.tokenCredit
	rl.mu.RUnlock()
	if credit != 0 {
		t.Errorf("credit after waiting = %d, want 0", credit)
	}
	if debt != 500 {
		t.Errorf("debt after a 2000-token request with 500 credit = %d, want 500", debt)
	}
}
