package llms

import (
	"encoding/json"
	"testing"
)

// TestPricing_UnifiedTypeServesBothRoles verifies the single canonical Pricing
// type (v5 merged the old cost-tracking Pricing and model-metadata ModelPricing)
// still fills both roles: it computes per-token cost from Input/Output/CacheRead/
// CacheWrite, and it serializes as model-metadata with stable JSON keys including
// the new cache and dedicated/fine-tune fields.
func TestPricing_UnifiedTypeServesBothRoles(t *testing.T) {
	p := Pricing{
		Input: 2.50, Output: 10.00, CacheRead: 1.25, CacheWrite: 3.75,
		Hourly: 1.50, Finetune: 5.00, Base: 0.25,
	}

	// Cost role: 1M of each token bucket bills at its own per-million rate.
	got := p.cost(Usage{
		PromptTokens: 1_000_000, CompletionTokens: 1_000_000,
		CacheReadTokens: 1_000_000, CacheCreationTokens: 1_000_000,
	})
	want := p.Input + p.Output + p.CacheRead + p.CacheWrite
	if got != want {
		t.Errorf("cost = %v, want %v", got, want)
	}

	// Metadata role: JSON serialization uses the documented wire keys.
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]float64
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for key, wantVal := range map[string]float64{
		"input": 2.50, "output": 10.00, "cache_read": 1.25, "cache_write": 3.75,
		"hourly": 1.50, "finetune": 5.00, "base": 0.25,
	} {
		if m[key] != wantVal {
			t.Errorf("json key %q = %v, want %v", key, m[key], wantVal)
		}
	}
}

// TestPricing_CacheRatesFallBackToInput verifies the zero-cache-rate fallback is
// preserved after the field rename: an unset CacheRead/CacheWrite bills at Input.
func TestPricing_CacheRatesFallBackToInput(t *testing.T) {
	p := Pricing{Input: 3.00, Output: 15.00} // no explicit cache rates
	if got := p.cacheReadRate(); got != 3.00 {
		t.Errorf("cacheReadRate = %v, want Input 3.00", got)
	}
	if got := p.cacheWriteRate(); got != 3.00 {
		t.Errorf("cacheWriteRate = %v, want Input 3.00", got)
	}
}

// TestModelInfo_PricingUsesUnifiedType verifies ModelInfo.Pricing is the unified
// *Pricing and round-trips the model-discovery wire shape.
func TestModelInfo_PricingUsesUnifiedType(t *testing.T) {
	mi := ModelInfo{ID: "x", Provider: ProviderOpenAI, Pricing: &Pricing{Input: 2.5, Output: 10}}
	data, err := json.Marshal(mi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ModelInfo
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Pricing == nil || out.Pricing.Input != 2.5 || out.Pricing.Output != 10 {
		t.Fatalf("ModelInfo.Pricing round-trip = %+v", out.Pricing)
	}
}
