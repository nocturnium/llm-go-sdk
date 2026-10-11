package resilience

import (
	"testing"
	"time"
)

// A Wait that was admitted before Reset must not leave debt or credit behind in
// the freshly reset limiter: Reset is the operator's way to start clean.
func TestRateLimiter_ResetDropsStaleDebtAndCredit(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(6000), WithTokenBurst(100))
	_, _, staleGen := rl.snapshotLimiters()

	rl.Reset()

	rl.addTokenDebt(500, staleGen)
	rl.restoreTokenCredit(50, staleGen)

	rl.mu.RLock()
	debt, credit := rl.tokenDebt, rl.tokenCredit
	rl.mu.RUnlock()
	if debt != 0 || credit != 0 {
		t.Fatalf("stale adjustments leaked past Reset: debt=%d credit=%d, want 0 and 0", debt, credit)
	}

	_, _, gen := rl.snapshotLimiters()
	rl.addTokenDebt(7, gen)
	rl.mu.RLock()
	got := rl.tokenDebt
	rl.mu.RUnlock()
	if got != 7 {
		t.Fatalf("current-generation debt = %d, want 7", got)
	}
}

// settleTokenDebt holding a pre-Reset limiter must not charge the old bucket
// against debt recorded after the Reset.
func TestRateLimiter_SettleIgnoresReplacedLimiter(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(6000), WithTokenBurst(100))
	stale := rl.tokenLim()
	rl.Reset()

	rl.mu.Lock()
	rl.tokenDebt = 40
	rl.mu.Unlock()

	rl.settleTokenDebt(nil, stale, time.Now())

	rl.mu.RLock()
	debt := rl.tokenDebt
	rl.mu.RUnlock()
	if debt != 40 {
		t.Fatalf("debt = %d after settling with a replaced limiter, want it untouched at 40", debt)
	}
}
