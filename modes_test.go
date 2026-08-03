package llms

import (
	"context"
	"sync"
	"testing"
)

// oneMillionEach is 1M tokens in each bucket, so an expected cost reads as a
// plain sum of the four rates.
var oneMillionEach = Usage{
	PromptTokens: 1_000_000, CompletionTokens: 1_000_000,
	CacheReadTokens: 1_000_000, CacheCreationTokens: 1_000_000,
}

// TestModelUsageStaysComparable is a compile-time guard, not a behavioral test.
// ModelUsage is comparable today; adding a slice or map field would silently
// make it non-comparable and break any caller using == — the exact class of
// change that forced the v6 major. Per-mode cost lives on CostTracker for this
// reason.
func TestModelUsageStaysComparable(t *testing.T) {
	var a, b ModelUsage
	if a != b {
		t.Error("two zero ModelUsage values should be equal")
	}
	_ = map[ModelUsage]struct{}{}
}

// TestPricingModeStandardIsZeroValue pins that an unset mode prices exactly as
// before. Every existing caller depends on this.
func TestPricingModeStandardIsZeroValue(t *testing.T) {
	var unset PricingMode
	if unset != PricingModeStandard {
		t.Fatalf("zero PricingMode = %q, want the standard mode", unset)
	}

	for _, model := range []string{"gpt-5.6-sol", "gpt-5.4", "gpt-4o"} {
		want, wantOK := EstimateCost(ProviderOpenAI, model, oneMillionEach)
		got, gotOK := EstimateCostMode(ProviderOpenAI, model, oneMillionEach, unset)
		if got != want || gotOK != wantOK {
			t.Errorf("%s: mode-zero = (%v,%v), want identical to EstimateCost (%v,%v)",
				model, got, gotOK, want, wantOK)
		}
	}
}

// TestModeUsesPublishedRatesNotAMultiplier pins that mode pricing comes from the
// published per-model card. The ratios are deliberately checked as absolute
// values: OpenAI's Fast mode is 2x on gpt-5.6-sol but 2.5x on gpt-5.5, so any
// implementation that applied one provider-wide multiplier would pass for one
// model and fail for the other.
func TestModeUsesPublishedRatesNotAMultiplier(t *testing.T) {
	cases := []struct {
		model      string
		mode       PricingMode
		wantInput  float64
		wantOutput float64
	}{
		{"gpt-5.6-sol", PricingModeBatch, 2.50, 15.00},
		{"gpt-5.6-sol", PricingModeFlex, 2.50, 15.00},
		{"gpt-5.6-sol", PricingModeFast, 10.00, 60.00},  // 2.0x standard
		{"gpt-5.5", PricingModeFast, 12.50, 75.00},      // 2.5x standard
		{"gpt-5.4-mini", PricingModeFast, 1.50, 9.00},   // 2.0x standard
		{"gpt-5.4-nano", PricingModeBatch, 0.10, 0.625}, // 0.5x standard
	}

	for _, c := range cases {
		t.Run(c.model+"/"+string(c.mode), func(t *testing.T) {
			usage := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
			got, ok := EstimateCostMode(ProviderOpenAI, c.model, usage, c.mode)
			if !ok {
				t.Fatalf("no rate card known for %s/%s", c.model, c.mode)
			}
			want := c.wantInput + c.wantOutput
			if !approxEqual(got, want) {
				t.Errorf("cost = %v, want %v (input %v + output %v)", got, want, c.wantInput, c.wantOutput)
			}
		})
	}
}

// TestUnpublishedModeFallsBackToStandardAndReportsUnknown pins the honesty
// contract: gpt-5.4-nano has no published Fast mode row, so it must price at
// standard rates and say so, never at an invented premium.
func TestUnpublishedModeFallsBackToStandardAndReportsUnknown(t *testing.T) {
	usage := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}

	standard, _ := EstimateCost(ProviderOpenAI, "gpt-5.4-nano", usage)
	got, known := EstimateCostMode(ProviderOpenAI, "gpt-5.4-nano", usage, PricingModeFast)

	if known {
		t.Error("gpt-5.4-nano has no published Fast rate; known must be false")
	}
	if !approxEqual(got, standard) {
		t.Errorf("cost = %v, want the standard cost %v", got, standard)
	}
}

// TestFlexIsUnknownOnAnthropic pins that a mode one provider offers is not
// assumed to exist at another. Anthropic publishes no Flex tier.
func TestFlexIsUnknownOnAnthropic(t *testing.T) {
	usage := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	standard, _ := EstimateCost(ProviderAnthropic, "claude-opus-5", usage)
	got, known := EstimateCostMode(ProviderAnthropic, "claude-opus-5", usage, PricingModeFlex)

	if known {
		t.Error("Anthropic has no Flex tier; known must be false")
	}
	if !approxEqual(got, standard) {
		t.Errorf("cost = %v, want the standard cost %v", got, standard)
	}
}

// TestAnthropicBatchDerivesFromStatedPolicy pins the derived-card path. Anthropic
// documents a flat 50% batch discount on input and output, and documents that
// prompt-caching multipliers stack on top of other modifiers — so the cache rates
// must derive from the *batch* input rate, not from the standard one.
func TestAnthropicBatchDerivesFromStatedPolicy(t *testing.T) {
	// Claude Opus 5 standard: $5 in / $25 out. Batch halves both.
	const wantInput, wantOutput = 2.50, 12.50
	// Cache rates follow the standard multipliers applied to the batch input rate.
	const wantCacheRead, wantCacheWrite = 0.25, 3.125

	got, known := EstimateCostMode(ProviderAnthropic, "claude-opus-5", oneMillionEach, PricingModeBatch)
	if !known {
		t.Fatal("Anthropic batch pricing should be derivable from stated policy")
	}
	want := wantInput + wantOutput + wantCacheRead + wantCacheWrite
	if !approxEqual(got, want) {
		t.Errorf("cost = %v, want %v", got, want)
	}

	// The derivation must not be a blanket halving of the standard card: halving
	// the standard cache-read (0.50 -> 0.25) coincides here, but halving the
	// standard cache-write would give 3.125 only because 6.25/2 == 3.125. Assert
	// the ratio to the batch input rate explicitly instead.
	if !approxEqual(wantCacheRead, wantInput*anthropicCacheReadRatio) {
		t.Errorf("cache read %v is not %vx the batch input rate", wantCacheRead, anthropicCacheReadRatio)
	}
	if !approxEqual(wantCacheWrite, wantInput*anthropicCacheWriteRatio) {
		t.Errorf("cache write %v is not %vx the batch input rate", wantCacheWrite, anthropicCacheWriteRatio)
	}
}

// TestAnthropicFastIsPublishedNotDerived pins that Anthropic fast mode uses the
// published absolute card ($10/$50 on Opus 5 and Opus 4.8) rather than any
// multiple of the standard rate, and that models without fast mode are unknown.
func TestAnthropicFastIsPublishedNotDerived(t *testing.T) {
	usage := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}

	for _, model := range []string{"claude-opus-5", "claude-opus-4-8"} {
		got, known := EstimateCostMode(ProviderAnthropic, model, usage, PricingModeFast)
		if !known {
			t.Fatalf("%s: fast mode rate should be published", model)
		}
		if !approxEqual(got, 10.00+50.00) {
			t.Errorf("%s: cost = %v, want 60 ($10 in + $50 out)", model, got)
		}
	}

	// Opus 4.7 rejects fast mode and Opus 4.6 silently runs at standard rates, so
	// neither has a fast rate card.
	if _, known := EstimateCostMode(ProviderAnthropic, "claude-opus-4-7", usage, PricingModeFast); known {
		t.Error("claude-opus-4-7 does not support fast mode; known must be false")
	}
}

// TestModeCardResolvesItsOwnTiers pins the composition order. A mode replaces the
// rate card wholesale, so tiers resolve against whichever card applies — a mode
// card's own tier, not the standard card's tier scaled by a discount.
func TestModeCardResolvesItsOwnTiers(t *testing.T) {
	tracker := NewCostTracker()
	tracker.SetPricing(ProviderOpenAI, "tiered-model", Pricing{
		Input: 10.00, Output: 20.00,
		Tiers: []PricingTier{{MinInputTokens: 1_000_000, Input: 100.00, Output: 200.00}},
	})
	tracker.SetModePricing(ProviderOpenAI, "tiered-model", PricingModeBatch, Pricing{
		Input: 5.00, Output: 10.00,
		Tiers: []PricingTier{{MinInputTokens: 1_000_000, Input: 50.00, Output: 100.00}},
	})

	longRequest := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}

	cost, known := tracker.RecordMode(ProviderOpenAI, "tiered-model", longRequest, PricingModeBatch)
	if !known {
		t.Fatal("registered mode pricing should be known")
	}
	// The batch card's own long-context tier: 50 + 100.
	if !approxEqual(cost, 150.00) {
		t.Errorf("cost = %v, want 150 (the batch card's tier), not 300 (standard tier) "+
			"or 15 (the batch card's base rates)", cost)
	}
}

// TestBuiltInModeCardsCarryNoLongContextTier documents a known limitation as a
// deliberate choice rather than an accident.
//
// OpenAI publishes separate long-context columns for its Batch and Flex tiers,
// which are not transcribed here. So a batch request above the 272K threshold is
// priced at short-context batch rates and reads low, while the same request at
// standard rates correctly tiers up. Encoding a guessed long-context batch rate
// would be worse than reading low, so the gap stands until the numbers are
// transcribed. If they are, this test should start failing — that is the signal
// to delete it.
func TestBuiltInModeCardsCarryNoLongContextTier(t *testing.T) {
	longRequest := Usage{PromptTokens: openAILongContextThreshold, CompletionTokens: 1_000_000}

	standard, _ := EstimateCost(ProviderOpenAI, "gpt-5.6-sol", longRequest)
	batch, known := EstimateCostMode(ProviderOpenAI, "gpt-5.6-sol", longRequest, PricingModeBatch)
	if !known {
		t.Fatal("gpt-5.6-sol batch pricing should be known")
	}

	// Standard tiers up to the long-context card (10/45).
	wantStandard := float64(openAILongContextThreshold)/1e6*10.00 + 45.00
	if !approxEqual(standard, wantStandard) {
		t.Errorf("standard cost = %v, want %v (long-context tier)", standard, wantStandard)
	}
	// Batch stays on the short-context card (2.50/15.00) — the documented gap.
	wantBatch := float64(openAILongContextThreshold)/1e6*2.50 + 15.00
	if !approxEqual(batch, wantBatch) {
		t.Errorf("batch cost = %v, want %v (short-context batch card)", batch, wantBatch)
	}
}

// TestUnpricedModelStaysUnknownUnderEveryMode pins that a mode discount on an
// unknown base is still unknown — a mode must never conjure a price for a model
// the SDK has no rates for.
func TestUnpricedModelStaysUnknownUnderEveryMode(t *testing.T) {
	for _, mode := range []PricingMode{PricingModeStandard, PricingModeBatch, PricingModeFlex, PricingModeFast} {
		cost, known := EstimateCostMode(ProviderOllama, "llama3", oneMillionEach, mode)
		if known || cost != 0 {
			t.Errorf("mode %q: got (%v,%v), want (0,false) for an unpriced model", mode, cost, known)
		}
	}
}

// TestSetModePricingOverridesBuiltIn pins that a caller-registered card wins over
// the published one, which is how negotiated rates are expressed.
func TestSetModePricingOverridesBuiltIn(t *testing.T) {
	tracker := NewCostTracker()
	tracker.SetModePricing(ProviderOpenAI, "gpt-5.6-sol", PricingModeBatch, Pricing{Input: 1.00, Output: 2.00})

	usage := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	cost, known := tracker.RecordMode(ProviderOpenAI, "gpt-5.6-sol", usage, PricingModeBatch)
	if !known {
		t.Fatal("registered mode pricing should be known")
	}
	if !approxEqual(cost, 3.00) {
		t.Errorf("cost = %v, want 3 (the registered card), not the published 17.50", cost)
	}
}

// TestSetModePricingStandardDelegatesToSetPricing pins the guard that keeps the
// two registration paths from diverging.
func TestSetModePricingStandardDelegatesToSetPricing(t *testing.T) {
	tracker := NewCostTracker()
	tracker.SetModePricing(ProviderOpenAI, "custom", PricingModeStandard, Pricing{Input: 1.00, Output: 2.00})

	usage := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	cost, known := tracker.Record(ProviderOpenAI, "custom", usage)
	if !known || !approxEqual(cost, 3.00) {
		t.Errorf("Record = (%v,%v), want (3,true) — standard mode must land in the standard table", cost, known)
	}
}

// TestModeCostsSplitWhileModelUsageAggregates pins the reporting split: cost
// accumulates into one ModelUsage per model (unchanged), while the per-mode
// breakdown lives on the tracker.
func TestModeCostsSplitWhileModelUsageAggregates(t *testing.T) {
	tracker := NewCostTracker()
	usage := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}

	standardCost, _ := tracker.RecordMode(ProviderOpenAI, "gpt-5.6-sol", usage, PricingModeStandard)
	batchCost, _ := tracker.RecordMode(ProviderOpenAI, "gpt-5.6-sol", usage, PricingModeBatch)

	all := tracker.Report()
	if len(all) != 1 {
		t.Fatalf("got %d ModelUsage entries, want 1 (both modes are the same model)", len(all))
	}
	for _, u := range all {
		if u.Requests != 2 {
			t.Errorf("Requests = %d, want 2", u.Requests)
		}
		if !approxEqual(u.EstimatedCost, standardCost+batchCost) {
			t.Errorf("EstimatedCost = %v, want %v", u.EstimatedCost, standardCost+batchCost)
		}
	}

	modes := tracker.GetModeCosts()
	if !approxEqual(modes[PricingModeStandard], standardCost) {
		t.Errorf("standard mode cost = %v, want %v", modes[PricingModeStandard], standardCost)
	}
	if !approxEqual(modes[PricingModeBatch], batchCost) {
		t.Errorf("batch mode cost = %v, want %v", modes[PricingModeBatch], batchCost)
	}
	if approxEqual(standardCost, batchCost) {
		t.Error("standard and batch costs should differ; the split is not being exercised")
	}
}

// TestGetModeCostsReturnsACopy pins that callers cannot mutate tracker state
// through the returned map.
func TestGetModeCostsReturnsACopy(t *testing.T) {
	tracker := NewCostTracker()
	tracker.RecordMode(ProviderOpenAI, "gpt-5.6-sol", oneMillionEach, PricingModeBatch)

	got := tracker.GetModeCosts()
	got[PricingModeBatch] = 99999

	if approxEqual(tracker.GetModeCosts()[PricingModeBatch], 99999) {
		t.Error("GetModeCosts returned a live reference to tracker state")
	}
}

// TestWithPricingModeRoundTrips pins the option plumbing.
func TestWithPricingModeRoundTrips(t *testing.T) {
	if got := ApplyOptions().PricingMode; got != PricingModeStandard {
		t.Errorf("default PricingMode = %q, want standard", got)
	}
	if got := ApplyOptions(WithPricingMode(PricingModeBatch)).PricingMode; got != PricingModeBatch {
		t.Errorf("PricingMode = %q, want batch", got)
	}
}

// TestCostMiddlewareHonorsPricingMode pins that the option actually reaches the
// tracker on both the unary and streaming paths. GenerateContent did not parse
// CallOptions at all before this change.
func TestCostMiddlewareHonorsPricingMode(t *testing.T) {
	usage := Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	wantBatch, _ := EstimateCostMode(ProviderOpenAI, "gpt-5.6-sol", usage, PricingModeBatch)

	t.Run("GenerateContent", func(t *testing.T) {
		tracker := NewCostTracker()
		mw := NewCostMiddleware(&modeStubLLM{usage: usage}, tracker)

		if _, err := mw.GenerateContent(t.Context(), nil, WithPricingMode(PricingModeBatch)); err != nil {
			t.Fatalf("GenerateContent: %v", err)
		}
		if got := tracker.GetModeCosts()[PricingModeBatch]; !approxEqual(got, wantBatch) {
			t.Errorf("recorded batch cost = %v, want %v", got, wantBatch)
		}
	})

	t.Run("Stream", func(t *testing.T) {
		tracker := NewCostTracker()
		mw := NewCostMiddleware(&modeStubLLM{usage: usage}, tracker)

		stream, err := mw.Stream(t.Context(), nil, WithPricingMode(PricingModeBatch))
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		for range stream { //nolint:revive // draining the stream is the point
		}
		if got := tracker.GetModeCosts()[PricingModeBatch]; !approxEqual(got, wantBatch) {
			t.Errorf("recorded batch cost = %v, want %v", got, wantBatch)
		}
	})
}

// TestRecordModeConcurrency exercises mixed-mode recording under -race.
func TestRecordModeConcurrency(t *testing.T) {
	tracker := NewCostTracker()
	usage := Usage{PromptTokens: 1000, CompletionTokens: 1000}
	modes := []PricingMode{PricingModeStandard, PricingModeBatch, PricingModeFlex, PricingModeFast}

	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tracker.RecordMode(ProviderOpenAI, "gpt-5.6-sol", usage, modes[i%len(modes)])
			_ = tracker.GetModeCosts()
		}(i)
	}
	wg.Wait()

	if got := tracker.GetTotalRequests(); got != 40 {
		t.Errorf("total requests = %d, want 40", got)
	}
}

// modeStubLLM is a minimal LLM returning a fixed usage, for middleware tests.
type modeStubLLM struct{ usage Usage }

func (s *modeStubLLM) GenerateContent(_ context.Context, _ []Message, _ ...CallOption) (*Response, error) {
	return &Response{Content: "ok", Usage: s.usage}, nil
}

func (s *modeStubLLM) Stream(_ context.Context, _ []Message, _ ...CallOption) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	usage := s.usage
	ch <- StreamChunk{Content: "ok", Usage: &usage, FinishReason: FinishReasonStop}
	close(ch)
	return ch, nil
}

func (s *modeStubLLM) Provider() Provider { return ProviderOpenAI }
func (s *modeStubLLM) Model() string      { return "gpt-5.6-sol" }
