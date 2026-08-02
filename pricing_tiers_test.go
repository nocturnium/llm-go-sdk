package llms

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// approxEqual compares USD amounts with a tolerance, since tier arithmetic goes
// through float64 division by 1e6.
func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestPricing_UntieredUnaffected pins the backwards-compatibility guarantee: a
// Pricing with no Tiers must bill exactly as it did before tiers existed. Every
// model in DefaultPricing that does not tier depends on this.
func TestPricing_UntieredUnaffected(t *testing.T) {
	p := Pricing{Input: 2.00, Output: 8.00, CacheRead: 0.50, CacheWrite: 2.50}
	usage := Usage{
		PromptTokens: 5_000_000, CompletionTokens: 1_000_000,
		CacheReadTokens: 2_000_000, CacheCreationTokens: 1_000_000,
	}

	want := 5*p.Input + 1*p.Output + 2*p.CacheRead + 1*p.CacheWrite
	if got := p.cost(usage); !approxEqual(got, want) {
		t.Errorf("untiered cost = %v, want %v", got, want)
	}
	// effective must return the receiver unchanged when there are no tiers.
	if e := p.effective(usage); !reflect.DeepEqual(e, p) {
		t.Errorf("effective(no tiers) = %+v, want unchanged %+v", e, p)
	}
}

// TestPricing_TierRepricesWholeRequest verifies the defining semantic of a tier:
// crossing the threshold reprices *every* token in the request, not only the
// tokens past the threshold. Getting this wrong understates long-context cost.
func TestPricing_TierRepricesWholeRequest(t *testing.T) {
	p := Pricing{
		Input: 1.00, Output: 10.00,
		Tiers: []PricingTier{{MinInputTokens: 1_000_000, Input: 2.00, Output: 15.00}},
	}

	// Just under the threshold: base rates.
	under := Usage{PromptTokens: 999_999, CompletionTokens: 1_000_000}
	wantUnder := 0.999999*1.00 + 10.00
	if got := p.cost(under); !approxEqual(got, wantUnder) {
		t.Errorf("below threshold: cost = %v, want %v", got, wantUnder)
	}

	// Exactly at the threshold: MinInputTokens is inclusive, so the tier applies
	// and all 1M input tokens bill at 2.00 — not 1M at 1.00 plus an increment.
	at := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	wantAt := 2.00 + 15.00
	if got := p.cost(at); !approxEqual(got, wantAt) {
		t.Errorf("at threshold: cost = %v, want %v", got, wantAt)
	}
}

// TestPricing_TierThresholdCountsCacheTokens guards the subtlety that makes this
// correct: Usage.PromptTokens excludes cache tokens by contract, but providers
// threshold on total input. A request whose PromptTokens alone is under the
// threshold can still cross it once cache reads/writes are counted.
func TestPricing_TierThresholdCountsCacheTokens(t *testing.T) {
	p := Pricing{
		Input: 1.00, Output: 10.00,
		Tiers: []PricingTier{{MinInputTokens: 1_000_000, Input: 2.00, Output: 15.00}},
	}

	// PromptTokens alone (600k) is below the threshold, but total input is
	// 600k + 300k + 100k = 1M, so the tier must apply.
	usage := Usage{
		PromptTokens: 600_000, CompletionTokens: 1_000_000,
		CacheReadTokens: 300_000, CacheCreationTokens: 100_000,
	}
	if got := totalInputTokens(usage); got != 1_000_000 {
		t.Fatalf("totalInputTokens = %d, want 1000000", got)
	}
	e := p.effective(usage)
	if e.Input != 2.00 {
		t.Errorf("tier not applied: Input = %v, want 2.00 (cache tokens must count "+
			"toward the threshold)", e.Input)
	}
}

// TestPricing_TierCacheRateFallsBackToTierInput pins that an unset cache rate on
// a tier falls back to that *tier's* Input rate, not the base Pricing's — the
// alternative would silently bill long-context cache reads at short-context rates.
func TestPricing_TierCacheRateFallsBackToTierInput(t *testing.T) {
	p := Pricing{
		Input: 1.00, Output: 10.00, CacheRead: 0.10,
		// Tier deliberately omits CacheRead.
		Tiers: []PricingTier{{MinInputTokens: 100, Input: 2.00, Output: 15.00}},
	}
	usage := Usage{PromptTokens: 1_000, CacheReadTokens: 1_000_000}

	e := p.effective(usage)
	if got := e.cacheReadRate(); !approxEqual(got, 2.00) {
		t.Errorf("tier cacheReadRate = %v, want 2.00 (the tier's own Input), not the "+
			"base 0.10", got)
	}
}

// TestPricing_HighestMatchingTierWins verifies multi-tier selection picks the
// highest matching threshold and does not depend on slice ordering.
func TestPricing_HighestMatchingTierWins(t *testing.T) {
	// Deliberately unsorted.
	p := Pricing{
		Input: 1.00,
		Tiers: []PricingTier{
			{MinInputTokens: 2_000_000, Input: 4.00},
			{MinInputTokens: 500_000, Input: 2.00},
		},
	}

	cases := []struct {
		total int
		want  float64
	}{
		{total: 100, want: 1.00},       // below all tiers -> base
		{total: 500_000, want: 2.00},   // lower tier
		{total: 1_999_999, want: 2.00}, // still lower tier
		{total: 2_000_000, want: 4.00}, // higher tier
		{total: 9_000_000, want: 4.00}, // well past the highest
	}
	for _, c := range cases {
		e := p.effective(Usage{PromptTokens: c.total})
		if e.Input != c.want {
			t.Errorf("total=%d: Input = %v, want %v", c.total, e.Input, c.want)
		}
	}
}

// TestPricing_ResolvedTierDoesNotRecurse ensures a resolved tier carries no Tiers
// of its own, so cost computation cannot re-resolve and tiers cannot nest.
func TestPricing_ResolvedTierDoesNotRecurse(t *testing.T) {
	p := Pricing{
		Input: 1.00,
		Tiers: []PricingTier{{MinInputTokens: 10, Input: 2.00}},
	}
	e := p.effective(Usage{PromptTokens: 100})
	if e.Tiers != nil {
		t.Errorf("resolved tier retained Tiers = %+v, want nil", e.Tiers)
	}
}

// TestPricing_TiersSerializeAndOmitWhenEmpty verifies the metadata role: tiers
// round-trip through JSON, and an untiered Pricing emits no "tiers" key so
// existing serialized model metadata is byte-identical to before.
func TestPricing_TiersSerializeAndOmitWhenEmpty(t *testing.T) {
	flat, err := json.Marshal(Pricing{Input: 1.00, Output: 2.00})
	if err != nil {
		t.Fatalf("marshal flat: %v", err)
	}
	if got := string(flat); got != `{"input":1,"output":2}` {
		t.Errorf("flat Pricing JSON = %s, want no tiers key", got)
	}

	orig := Pricing{
		Input: 1.00, Output: 2.00,
		Tiers: []PricingTier{{MinInputTokens: 272_000, Input: 2.00, Output: 3.00}},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal tiered: %v", err)
	}
	var back Pricing
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Tiers) != 1 || back.Tiers[0] != orig.Tiers[0] {
		t.Errorf("round-trip Tiers = %+v, want %+v", back.Tiers, orig.Tiers)
	}
}

// TestDefaultPricing_LongContextTiers checks the shipped table against the
// published long-context rates, using EstimateCost so the whole lookup path is
// exercised. Rates verified against provider pricing pages on 2026-08-02.
func TestDefaultPricing_LongContextTiers(t *testing.T) {
	cases := []struct {
		name              string
		provider          Provider
		model             string
		shortIn, shortOut float64 // expected $ for 1M in + 1M out below threshold
		longIn, longOut   float64 // expected $ for the same split above threshold
		threshold         int
	}{
		{
			name: "gpt-5.5", provider: ProviderOpenAI, model: "gpt-5.5",
			shortIn: 5.00, shortOut: 30.00, longIn: 10.00, longOut: 45.00,
			threshold: openAILongContextThreshold,
		},
		{
			name: "gpt-5.6-sol", provider: ProviderOpenAI, model: "gpt-5.6-sol",
			shortIn: 5.00, shortOut: 30.00, longIn: 10.00, longOut: 45.00,
			threshold: openAILongContextThreshold,
		},
		{
			name: "gpt-5.6-luna", provider: ProviderOpenAI, model: "gpt-5.6-luna",
			shortIn: 0.20, shortOut: 1.20, longIn: 0.40, longOut: 1.80,
			threshold: openAILongContextThreshold,
		},
		{
			name: "gemini-2.5-pro", provider: ProviderGemini, model: "gemini-2.5-pro",
			shortIn: 1.25, shortOut: 10.00, longIn: 2.50, longOut: 15.00,
			threshold: geminiLongContextThreshold,
		},
		{
			name: "gemini-3.1-pro-preview", provider: ProviderGemini, model: "gemini-3.1-pro-preview",
			shortIn: 2.00, shortOut: 12.00, longIn: 4.00, longOut: 18.00,
			threshold: geminiLongContextThreshold,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Below the threshold, scaled to 1M in / 1M out for readable expectations.
			short := Usage{PromptTokens: c.threshold - 1, CompletionTokens: 1_000_000}
			gotShort, ok := EstimateCost(c.provider, c.model, short)
			if !ok {
				t.Fatalf("EstimateCost returned ok=false; model missing from DefaultPricing")
			}
			wantShort := float64(c.threshold-1)/1e6*c.shortIn + c.shortOut
			if !approxEqual(gotShort, wantShort) {
				t.Errorf("below threshold: cost = %v, want %v", gotShort, wantShort)
			}

			// At the threshold the entire request reprices.
			long := Usage{PromptTokens: c.threshold, CompletionTokens: 1_000_000}
			gotLong, ok := EstimateCost(c.provider, c.model, long)
			if !ok {
				t.Fatal("EstimateCost returned ok=false at threshold")
			}
			wantLong := float64(c.threshold)/1e6*c.longIn + c.longOut
			if !approxEqual(gotLong, wantLong) {
				t.Errorf("at threshold: cost = %v, want %v", gotLong, wantLong)
			}

			// One extra input token must not cost less than the request without it.
			if gotLong < gotShort {
				t.Errorf("crossing the threshold decreased cost (%v -> %v)", gotShort, gotLong)
			}
		})
	}
}

// TestDefaultPricing_UntieredModelsHaveNoTiers pins that models without a
// published long-context row stay flat. OpenAI's mini/nano variants publish no
// long-context rate, so inventing one would overstate their cost.
func TestDefaultPricing_UntieredModelsHaveNoTiers(t *testing.T) {
	for _, key := range []string{
		"openai:gpt-5.4-mini", "openai:gpt-5.4-nano",
		"openai:gpt-4o", "anthropic:claude-opus-5", "anthropic:claude-sonnet-5",
		"gemini:gemini-2.5-flash",
	} {
		if p, ok := DefaultPricing[key]; !ok {
			t.Errorf("%s missing from DefaultPricing", key)
		} else if len(p.Tiers) != 0 {
			t.Errorf("%s unexpectedly tiered: %+v", key, p.Tiers)
		}
	}
}
