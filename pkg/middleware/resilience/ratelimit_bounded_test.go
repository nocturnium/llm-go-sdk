package resilience

import (
	"context"
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

// TestRecordTokens_RemainderIsCharged pins that the installment cap limits the
// number of ReserveN calls without dropping tokens. The remainder is carried as
// debt and settled before a later request, so a huge response eventually pays
// its full cost even though it cannot be covered by one reservation.
func TestRecordTokens_RemainderIsCharged(t *testing.T) {
	rl := NewRateLimiter(
		WithTokensPerMinute(60000),
		WithTokenBurst(1),
		WithTokenEstimate(0),
	)

	actual := maxTokenInstallments + 7
	rl.RecordTokens(actual)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("Wait after oversized usage: %v", err)
	}
	if remaining := rl.TokensRemaining(); remaining < 0 {
		t.Fatalf("TokensRemaining = %d, want non-negative", remaining)
	}
}
