package llms

// PricingMode selects the billing lane a request is priced under.
//
// Providers publish separate rate cards for asynchronous and premium-latency
// processing. The mode is a cost-accounting concept only: setting it does not
// route the request. To send a request to a different lane, use the
// provider's own mechanism (for OpenAI, the service_tier field via
// [WithExtraBodyParam]; for Anthropic, the Batches API).
//
// The zero value is [PricingModeStandard], so an unset mode always prices at
// standard rates and existing code is unaffected.
type PricingMode string

const (
	// PricingModeStandard is interactive, synchronous pricing. It is the zero
	// value, so a request with no mode set is priced normally.
	PricingModeStandard PricingMode = ""
	// PricingModeBatch is asynchronous batch pricing (OpenAI's Batch API,
	// Anthropic's Message Batches API).
	PricingModeBatch PricingMode = "batch"
	// PricingModeFlex is OpenAI's Flex processing tier. Anthropic has no
	// equivalent, so Flex on an Anthropic model resolves to unknown rather than
	// to a guessed discount.
	PricingModeFlex PricingMode = "flex"
	// PricingModeFast is premium-latency pricing (OpenAI's Fast mode, formerly
	// Priority processing; Anthropic's fast mode). Providers do not offer it in
	// combination with batch, and because a request carries exactly one mode the
	// combination cannot be expressed.
	PricingModeFast PricingMode = "fast"
)

// modePricingKey builds the "provider:model:mode" key used by modePricing.
func modePricingKey(provider Provider, model string, mode PricingMode) string {
	return string(provider) + ":" + model + ":" + string(mode)
}

// modePricing holds published per-model rate cards for non-standard billing
// lanes, keyed "provider:model:mode".
//
// Rates are absolute, not multipliers, because that is how providers publish
// them and the ratios are not uniform: OpenAI's Fast mode ranges from about 1.7x
// to 2.5x depending on the model, the cache-write column is absent from some
// tier rows, and not every model appears in every tier's table. Storing a per-provider multiplier would invent precision that the
// published data does not have.
//
// Coverage is intentionally partial, matching DefaultPricing's contract: a model
// with no entry for a mode resolves to unknown (ok=false) and is priced at
// standard rates rather than at a guessed discount. gpt-5.4-nano, for instance,
// has no Fast mode row at all.
//
// Verified against developers.openai.com/api/docs/pricing on 2026-08-02. Values
// are the short-context tier; see the tier note on resolveModePricing.
var modePricing = map[string]Pricing{
	// OpenAI Batch (50% off standard for these models, but transcribed rather
	// than derived, the discount is not uniform provider-wide).
	"openai:gpt-5.6-sol:batch": {Input: 2.00, Output: 10.00, CacheRead: 0.20, CacheWrite: 2.50,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 4.00, Output: 15.00, CacheRead: 0.40, CacheWrite: 5.00}}},
	"openai:gpt-5.6-terra:batch": {Input: 1.00, Output: 6.00, CacheRead: 0.10, CacheWrite: 1.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 2.00, Output: 9.00, CacheRead: 0.20}}},
	"openai:gpt-5.6-luna:batch": {Input: 0.10, Output: 0.60, CacheRead: 0.01, CacheWrite: 0.125,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 0.20, Output: 0.90, CacheRead: 0.02, CacheWrite: 0.25}}},
	"openai:gpt-6.1-sol:batch": {Input: 1.00, Output: 5.00, CacheRead: 0.05, CacheWrite: 1.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 2.00, Output: 7.50, CacheRead: 0.10, CacheWrite: 2.50}}},
	"openai:gpt-6-sol:batch": {Input: 1.00, Output: 5.00, CacheRead: 0.10, CacheWrite: 1.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 2.00, Output: 7.50, CacheRead: 0.20, CacheWrite: 2.50}}},
	"openai:gpt-6-astra:batch": {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 10.00, Output: 37.50, CacheRead: 1.00, CacheWrite: 12.50}}},
	"openai:gpt-6-luna:batch": {Input: 0.05, Output: 0.25, CacheRead: 0.005, CacheWrite: 0.0625,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 0.10, Output: 0.375, CacheRead: 0.01, CacheWrite: 0.125}}},
	"openai:gpt-5.5:batch": {Input: 2.50, Output: 15.00, CacheRead: 0.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 5.00, Output: 22.50, CacheRead: 0.50}}},
	"openai:gpt-5.4:batch": {Input: 1.25, Output: 7.50, CacheRead: 0.13,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 2.50, Output: 11.25, CacheRead: 0.25}}},
	"openai:gpt-5.4-mini:batch": {Input: 0.375, Output: 2.25, CacheRead: 0.0375},
	"openai:gpt-5.4-nano:batch": {Input: 0.10, Output: 0.625, CacheRead: 0.01},

	// OpenAI Flex. Matches Batch for the current families, but is listed
	// separately because Flex covers a different (shorter) model list and the two
	// tiers are not documented as equivalent.
	"openai:gpt-5.6-sol:flex": {Input: 2.00, Output: 10.00, CacheRead: 0.20, CacheWrite: 2.50,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 4.00, Output: 15.00, CacheRead: 0.40, CacheWrite: 5.00}}},
	"openai:gpt-5.6-terra:flex": {Input: 1.00, Output: 6.00, CacheRead: 0.10, CacheWrite: 1.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 2.00, Output: 9.00, CacheRead: 0.20}}},
	"openai:gpt-5.6-luna:flex": {Input: 0.10, Output: 0.60, CacheRead: 0.01, CacheWrite: 0.125,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 0.20, Output: 0.90, CacheRead: 0.02, CacheWrite: 0.25}}},
	"openai:gpt-6.1-sol:flex": {Input: 1.00, Output: 5.00, CacheRead: 0.05, CacheWrite: 1.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 2.00, Output: 7.50, CacheRead: 0.10, CacheWrite: 2.50}}},
	"openai:gpt-6-sol:flex": {Input: 1.00, Output: 5.00, CacheRead: 0.10, CacheWrite: 1.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 2.00, Output: 7.50, CacheRead: 0.20, CacheWrite: 2.50}}},
	"openai:gpt-6-astra:flex": {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 10.00, Output: 37.50, CacheRead: 1.00, CacheWrite: 12.50}}},
	"openai:gpt-6-luna:flex": {Input: 0.05, Output: 0.25, CacheRead: 0.005, CacheWrite: 0.0625,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 0.10, Output: 0.375, CacheRead: 0.01, CacheWrite: 0.125}}},
	"openai:gpt-5.5:flex": {Input: 2.50, Output: 15.00, CacheRead: 0.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 5.00, Output: 22.50, CacheRead: 0.50}}},
	"openai:gpt-5.4:flex": {Input: 1.25, Output: 7.50, CacheRead: 0.13,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold, Input: 2.50, Output: 11.25, CacheRead: 0.25}}},
	"openai:gpt-5.4-mini:flex": {Input: 0.375, Output: 2.25, CacheRead: 0.0375},
	"openai:gpt-5.4-nano:flex": {Input: 0.10, Output: 0.625, CacheRead: 0.01},

	// OpenAI Fast mode (formerly Priority processing). Ratios to standard vary by
	// model (2x on gpt-5.6-sol, 2.5x on gpt-5.5, 2x on gpt-5.4-mini), which is
	// why these are transcribed individually. gpt-5.4-nano has no Fast row.
	"openai:gpt-5.6-sol:fast":   {Input: 8.00, Output: 40.00, CacheRead: 0.80, CacheWrite: 10.00},
	"openai:gpt-5.6-terra:fast": {Input: 4.00, Output: 24.00, CacheRead: 0.40, CacheWrite: 5.00},
	"openai:gpt-5.6-luna:fast":  {Input: 0.40, Output: 2.40, CacheRead: 0.04, CacheWrite: 0.50},
	"openai:gpt-6.1-sol:fast":   {Input: 4.00, Output: 20.00, CacheRead: 0.20, CacheWrite: 5.00},
	"openai:gpt-6-sol:fast":     {Input: 4.00, Output: 20.00, CacheRead: 0.40, CacheWrite: 5.00},
	"openai:gpt-6-astra:fast":   {Input: 20.00, Output: 100.00, CacheRead: 2.00, CacheWrite: 25.00},
	"openai:gpt-6-luna:fast":    {Input: 0.20, Output: 1.00, CacheRead: 0.02, CacheWrite: 0.25},
	"openai:gpt-5.5:fast":       {Input: 12.50, Output: 75.00, CacheRead: 1.25},
	"openai:gpt-5.4:fast":       {Input: 5.00, Output: 30.00, CacheRead: 0.50},
	"openai:gpt-5.4-mini:fast":  {Input: 1.50, Output: 9.00, CacheRead: 0.15},

	// Anthropic fast mode: a published absolute rate card, available on Opus 5
	// and Opus 4.8 only (Opus 4.7 rejects the request; Opus 4.6 silently runs at
	// standard speed and standard rates). Cache rates follow the standard
	// multipliers applied to the fast base rate, per the published rule that
	// prompt-caching multipliers stack on top of fast mode pricing.
	"anthropic:claude-opus-5:fast":   {Input: 10.00, Output: 50.00, CacheRead: 1.00, CacheWrite: 12.50},
	"anthropic:claude-opus-4-8:fast": {Input: 10.00, Output: 50.00, CacheRead: 1.00, CacheWrite: 12.50},
}

// Anthropic publishes a single documented rule for its Batches API, a flat 50%
// discount on input and output, and separately documents that prompt-caching
// multipliers stack on top of other modifiers. Deriving the batch card from the
// standard one therefore follows stated policy rather than inferring a pattern,
// and keeps ~15 models correct without transcribing (and re-transcribing) four
// numbers each.
const (
	anthropicBatchDiscount   = 0.5
	anthropicCacheReadRatio  = 0.1  // cache hits bill at 0.1x the applicable input rate
	anthropicCacheWriteRatio = 1.25 // 5-minute cache writes bill at 1.25x
)

// deriveAnthropicBatchPricing returns the batch rate card implied by a standard
// Anthropic card, or ok=false when no standard card is known.
func deriveAnthropicBatchPricing(base Pricing, known bool) (Pricing, bool) {
	if !known || base.Input == 0 {
		return Pricing{}, false
	}
	input := base.Input * anthropicBatchDiscount
	cacheRead := base.CacheRead * anthropicBatchDiscount
	cacheWrite := base.CacheWrite * anthropicBatchDiscount
	if base.CacheRead == 0 {
		cacheRead = input * anthropicCacheReadRatio
	}
	if base.CacheWrite == 0 {
		cacheWrite = input * anthropicCacheWriteRatio
	}
	derived := Pricing{
		Input:      input,
		Output:     base.Output * anthropicBatchDiscount,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
	}
	// Carry any tiers with the same 50% discount. Anthropic's long-context
	// pricing is flat today (no standard card carries tiers), but a future
	// tiered card must not leak standard-tier rates into the batch card.
	if len(base.Tiers) > 0 {
		derived.Tiers = make([]PricingTier, len(base.Tiers))
		for i, tier := range base.Tiers {
			discounted := PricingTier{
				MinInputTokens: tier.MinInputTokens,
				Input:          tier.Input * anthropicBatchDiscount,
				Output:         tier.Output * anthropicBatchDiscount,
				CacheRead:      tier.CacheRead * anthropicBatchDiscount,
				CacheWrite:     tier.CacheWrite * anthropicBatchDiscount,
			}
			if tier.CacheRead == 0 {
				discounted.CacheRead = discounted.Input * anthropicCacheReadRatio
			}
			if tier.CacheWrite == 0 {
				discounted.CacheWrite = discounted.Input * anthropicCacheWriteRatio
			}
			derived.Tiers[i] = discounted
		}
	}
	return derived, true
}

// resolveModePricing returns the rate card for a provider/model under a billing
// mode, and whether one is known.
//
// Resolution order is: an explicitly published per-model card, then a
// provider-wide rule where the provider states one, then unknown. Callers that
// get ok=false must fall back to standard pricing rather than assume a discount.
//
// A mode card replaces the standard card wholesale. Where the provider publishes
// a separate long-context column for a mode, that tier is transcribed on the mode
// card and resolves at the standard long-context threshold. Models with no
// published long-context row (for example gpt-5.4-mini and gpt-5.4-nano) stay
// flat rather than borrowing another model's tier.
func resolveModePricing(provider Provider, model string, mode PricingMode, standard Pricing, standardKnown bool) (Pricing, bool) {
	if mode == PricingModeStandard {
		return standard, standardKnown
	}
	if p, ok := modePricing[modePricingKey(provider, model, mode)]; ok {
		return p, true
	}
	if provider == ProviderAnthropic && mode == PricingModeBatch {
		return deriveAnthropicBatchPricing(standard, standardKnown)
	}
	return Pricing{}, false
}
