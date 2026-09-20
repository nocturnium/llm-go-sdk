package resilience

import (
	"testing"
	"time"
)

// TestRecordTokens_HugeCountIsBounded pins that a provider-reported token
// count cannot drive unbounded work. Charging in burst-sized installments with
// no cap ran millions of ReserveN calls for one response.
func TestRecordTokens_HugeCountIsBounded(t *testing.T) {
	rl := NewRateLimiter(
		WithTokensPerMinute(10_000_000),
		WithTokenBurst(1),
		WithTokenEstimate(0),
	)

	start := time.Now()
	rl.RecordTokens(5_000_000)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("RecordTokens took %v for one response, want it bounded", elapsed)
	}
}
