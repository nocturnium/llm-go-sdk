package llms

// PricingMode selects the billing lane a request is priced under.
//
// Providers publish separate rate cards for asynchronous and premium-latency
// processing. The mode is a cost-accounting concept only: setting it does not
// route the request. To actually send a request to a different lane, use the
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
// to 2.5x depending on the model, its Batch tier drops cached-input pricing
// entirely for some older models, and not every model appears in every tier's
// table. Storing a per-provider multiplier would invent precision that the
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
	// than derived — the discount is not uniform provider-wide).
	"openai:gpt-5.6-sol:batch":   {Input: 2.50, Output: 15.00, CacheRead: 0.25, CacheWrite: 3.125},
	"openai:gpt-5.6-terra:batch": {Input: 1.00, Output: 6.00, CacheRead: 0.10, CacheWrite: 1.25},
	"openai:gpt-5.6-luna:batch":  {Input: 0.10, Output: 0.60, CacheRead: 0.01, CacheWrite: 0.125},
	"openai:gpt-5.5:batch":       {Input: 2.50, Output: 15.00, CacheRead: 0.25},
	"openai:gpt-5.4:batch":       {Input: 1.25, Output: 7.50, CacheRead: 0.13},
	"openai:gpt-5.4-mini:batch":  {Input: 0.375, Output: 2.25, CacheRead: 0.0375},
	"openai:gpt-5.4-nano:batch":  {Input: 0.10, Output: 0.625, CacheRead: 0.01},

	// OpenAI Flex. Matches Batch for the current families, but is listed
	// separately because Flex covers a different (shorter) model list and the two
	// tiers are not documented as equivalent.
	"openai:gpt-5.6-sol:flex":   {Input: 2.50, Output: 15.00, CacheRead: 0.25, CacheWrite: 3.125},
	"openai:gpt-5.6-terra:flex": {Input: 1.00, Output: 6.00, CacheRead: 0.10, CacheWrite: 1.25},
	"openai:gpt-5.6-luna:flex":  {Input: 0.10, Output: 0.60, CacheRead: 0.01, CacheWrite: 0.125},
	"openai:gpt-5.5:flex":       {Input: 2.50, Output: 15.00, CacheRead: 0.25},
	"openai:gpt-5.4:flex":       {Input: 1.25, Output: 7.50, CacheRead: 0.13},
	"openai:gpt-5.4-mini:flex":  {Input: 0.375, Output: 2.25, CacheRead: 0.0375},
	"openai:gpt-5.4-nano:flex":  {Input: 0.10, Output: 0.625, CacheRead: 0.01},

	// OpenAI Fast mode (formerly Priority processing). Ratios to standard vary by
	// model — 2x on gpt-5.6-sol, 2.5x on gpt-5.5, 2x on gpt-5.4-mini — which is
	// why these are transcribed individually. gpt-5.4-nano has no Fast row.
	"openai:gpt-5.6-sol:fast":   {Input: 10.00, Output: 60.00, CacheRead: 1.00, CacheWrite: 12.50},
	"openai:gpt-5.6-terra:fast": {Input: 4.00, Output: 24.00, CacheRead: 0.40, CacheWrite: 5.00},
	"openai:gpt-5.6-luna:fast":  {Input: 0.40, Output: 2.40, CacheRead: 0.04, CacheWrite: 0.50},
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

// Anthropic publishes a single documented rule for its Batches API — a flat 50%
// discount on input and output — and separately documents that prompt-caching
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
	derived := Pricing{
		Input:      input,
		Output:     base.Output * anthropicBatchDiscount,
		CacheRead:  input * anthropicCacheReadRatio,
		CacheWrite: input * anthropicCacheWriteRatio,
	}
	// Anthropic's long-context pricing is flat, so a standard card carries no
	// tiers; if that ever changes, carry them so the derived card tiers too.
	derived.Tiers = base.Tiers
	return derived, true
}

// resolveModePricing returns the rate card for a provider/model under a billing
// mode, and whether one is known.
//
// Resolution order is: an explicitly published per-model card, then a
// provider-wide rule where the provider states one, then unknown. Callers that
// get ok=false must fall back to standard pricing rather than assume a discount.
//
// Tier caveat: a mode card replaces the standard card wholesale, and the cards
// above carry the providers' short-context rates. OpenAI publishes separate
// long-context columns for its Batch and Flex tiers, which are not transcribed
// here — so a batch request above the long-context threshold is currently priced
// at short-context batch rates and will read low. This mirrors the existing
// long-context caveat on standard pricing.
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
