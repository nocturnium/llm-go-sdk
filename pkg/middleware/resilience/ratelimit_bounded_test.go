package resilience

import (
	"testing"
	"time"
)

// TestRecordTokens_HugeCountIsBounded pins that a provider-reported token
// count cannot drive unbounded work. Charging in burst-sized installments with
// no cap ran millions of ReserveN calls for one response.
func TestRecordTokens_HugeCountIsBounded(t *testing.T) {
	// A slow refill keeps the bucket still while the charge is measured: at a
	// high rate the tokens the 64 reservations cost come back during the call
	// itself and the assertion reads a wash.
	rl := NewRateLimiter(
		WithTokensPerMinute(60),
		WithTokenBurst(1),
		WithTokenEstimate(0),
	)

	before := rl.tokenLim().Tokens()

	start := time.Now()
	rl.RecordTokens(5_000_000)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("RecordTokens took %v for one response, want it bounded", elapsed)
	}

	// Bounded is not the whole contract: the charge still has to land, and a
	// single ReserveN above the burst would charge nothing at all.
	if drained := before - rl.tokenLim().Tokens(); drained < float64(maxTokenInstallments)-0.5 {
		t.Errorf("drained %.0f tokens, want about %d: the charge did not land", drained, maxTokenInstallments)
	}
}
