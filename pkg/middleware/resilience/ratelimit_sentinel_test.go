package resilience

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

// TestErrRateLimitExceeded_UnifiesWithLLMErrRateLimited verifies the v5
// consolidation: a local rate-limiter rejection (resilience.ErrRateLimitExceeded)
// now satisfies errors.Is(err, llms.ErrRateLimited), so a caller can match the one
// canonical sentinel regardless of whether the limit came from this middleware or
// a provider-reported 429.
func TestErrRateLimitExceeded_UnifiesWithLLMErrRateLimited(t *testing.T) {
	if !errors.Is(ErrRateLimitExceeded, llms.ErrRateLimited) {
		t.Fatal("resilience.ErrRateLimitExceeded must wrap llms.ErrRateLimited so errors.Is unifies across layers")
	}
	// The specific sentinel must still match itself (backward-compatible).
	if !errors.Is(ErrRateLimitExceeded, ErrRateLimitExceeded) {
		t.Fatal("ErrRateLimitExceeded must still match itself")
	}
}
