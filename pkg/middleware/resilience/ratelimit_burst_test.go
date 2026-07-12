package resilience

import (
	"context"
	"testing"
)

// TestRateLimiter_TokenEstimateExceedingBurstDoesNotWedge covers the self-
// inflicted-outage bug: golang.org/x/time/rate rejects any WaitN/AllowN where n
// exceeds the bucket burst, so a per-request token estimate larger than the burst
// (e.g. WithTokensPerMinute(500) or WithTokenBurst(<1000) against the default
// 1000-token estimate) made every call fail immediately. The Wait paths now cap
// the requested token count to the bucket burst so it throttles instead of
// failing forever; the burst itself is untouched, respecting an explicit
// WithTokenBurst.
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

	// An oversized single-call token count is capped to the burst instead of
	// failing permanently.
	big := NewRateLimiter(WithTokensPerMinute(60000))
	if err := big.WaitN(context.Background(), 1, 10_000_000); err != nil {
		t.Errorf("oversized per-call token request not capped to burst: %v", err)
	}
}
