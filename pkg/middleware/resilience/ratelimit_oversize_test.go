package resilience

import (
	"testing"
)

// TestRecordTokens_OversizedUnderestimateStillCharges pins that a token count
// far above the burst is charged. ReserveN refuses any n above the burst and
// charges nothing, so the largest requests used to pass through free.
func TestRecordTokens_OversizedUnderestimateStillCharges(t *testing.T) {
	rl := NewRateLimiter(
		WithTokensPerMinute(10000),
		WithTokenBurst(1000),
		WithTokenEstimate(10),
	)

	before := rl.tokenLim().Tokens()
	rl.RecordTokens(5000)
	after := rl.tokenLim().Tokens()

	if after >= before {
		t.Fatalf("token bucket went from %.0f to %.0f: an oversized request was charged nothing", before, after)
	}
	if drained := before - after; drained < 4000 {
		t.Errorf("drained %.0f tokens, want at least 4000", drained)
	}
}
