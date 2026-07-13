package llms

import (
	"testing"
	"time"
)

func TestCacheOptions(t *testing.T) {
	if opts := ApplyOptions(WithCache()); opts.Cache == nil || opts.Cache.Disabled || opts.Cache.TTL != 0 {
		t.Errorf("WithCache: got %+v", opts.Cache)
	}
	if opts := ApplyOptions(WithCacheTTL(time.Hour)); opts.Cache == nil || opts.Cache.TTL != time.Hour {
		t.Errorf("WithCacheTTL: got %+v", opts.Cache)
	}
	if opts := ApplyOptions(WithoutCache()); opts.Cache == nil || !opts.Cache.Disabled {
		t.Errorf("WithoutCache: got %+v", opts.Cache)
	}
	if opts := ApplyOptions(); opts.Cache != nil {
		t.Errorf("no option: expected nil Cache, got %+v", opts.Cache)
	}
}

func TestPricingCost_CacheRates(t *testing.T) {
	// 1M of each kind of token, with explicit cache rates.
	p := Pricing{
		Input:      3.00,
		Output:     15.00,
		CacheRead:  0.30,
		CacheWrite: 3.75,
	}
	usage := Usage{
		PromptTokens:        1_000_000,
		CompletionTokens:    1_000_000,
		CacheReadTokens:     1_000_000,
		CacheCreationTokens: 1_000_000,
	}
	got := p.cost(usage)
	want := 3.00 + 15.00 + 0.30 + 3.75
	if got != want {
		t.Errorf("cost = %v, want %v", got, want)
	}
}

func TestPricingCost_CacheRateFallback(t *testing.T) {
	// With no cache-specific rates, cache tokens fall back to the prompt rate.
	p := Pricing{Input: 2.00, Output: 8.00}
	usage := Usage{CacheReadTokens: 1_000_000, CacheCreationTokens: 1_000_000}
	got := p.cost(usage)
	want := 2.00 + 2.00 // both fall back to prompt rate
	if got != want {
		t.Errorf("cost = %v, want %v", got, want)
	}
}

func TestEstimateCost_DiscountsCacheReads(t *testing.T) {
	// A cache-heavy request should cost less than billing every input token at the
	// full prompt rate, because cache reads are discounted.
	model := "claude-3-5-sonnet-20241022"
	cached := Usage{PromptTokens: 100, CompletionTokens: 100, CacheReadTokens: 9000}
	uncached := Usage{PromptTokens: 9100, CompletionTokens: 100}

	cachedCost, _ := EstimateCost(ProviderAnthropic, model, cached)
	uncachedCost, _ := EstimateCost(ProviderAnthropic, model, uncached)
	if cachedCost >= uncachedCost {
		t.Errorf("expected cached cost (%v) < uncached cost (%v)", cachedCost, uncachedCost)
	}
	if cachedCost == 0 {
		t.Error("expected cache reads to contribute a non-zero cost")
	}
}
