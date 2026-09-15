package openrouter

import (
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// This file holds the per-call (llms.CallOption) and model-id helpers for
// OpenRouter service tiers, the capacity grades providers sell for the same
// model. Like the openai package's Responses helpers, these return
// llms.CallOption and are passed to GenerateContent/Call/Stream, unlike the
// client-construction Option values (WithAPIKey, WithModel, …).

// ServiceTier selects the capacity grade a request is routed to. Each tier is a
// separate upstream endpoint with its own pricing, published on the model page.
type ServiceTier string

const (
	// TierDefault pins the standard-tier endpoint. It is worth sending
	// explicitly on a :nitro or :floor model id, where it keeps the variant's
	// sort while refusing the non-default tiers that variant would admit.
	TierDefault ServiceTier = "default"
	// TierFlex is discounted capacity that trades latency and availability for
	// price. Routing never falls back from flex to a default-tier endpoint: a
	// capacity failure surfaces as an error instead of a silent upgrade, so
	// retry without the tier if standard pricing is acceptable. A model with no
	// flex endpoint at all routes normally at standard rates.
	TierFlex ServiceTier = "flex"
	// TierPriority is premium capacity: faster and more reliable, at a higher
	// rate. Matching endpoints are tried first but routing does fall back to
	// others, and billing follows whichever endpoint served the request rather
	// than the tier asked for.
	TierPriority ServiceTier = "priority"
	// TierFast is OpenRouter's alias for TierPriority; both match the same
	// endpoint slug (OpenAI brands it Fast mode, Anthropic speed:"fast"). A
	// served request reports "priority", never "fast".
	TierFast ServiceTier = "fast"
)

// WithServiceTier routes a request to a capacity tier by setting the
// service_tier request field.
//
// The tier that served the request is reported back on
// [llms.Response.ServiceTier] and, for a stream, on the final
// [llms.StreamChunk.ServiceTier] ("default", "flex", "priority", or empty when
// upstream reports nothing). Because priority can fall back and flex can be
// absent, the served tier is not always the tier requested.
//
// This routes only. For what it cost, add [WithUsageAccounting] and read
// Usage.Cost, the charge OpenRouter reports for the endpoint that served. The
// estimate path, [llms.WithPricingMode] fed by [PricingModeFor], resolves to
// unknown here: OpenRouter has no static rate cards in this SDK, since it prices
// per model and per endpoint across its whole catalog.
//
// An empty tier is a no-op, so a configured-but-unset tier sends no field.
//
//	resp, err := client.GenerateContent(ctx, msgs,
//	    openrouter.WithServiceTier(openrouter.TierFlex),
//	    openrouter.WithUsageAccounting())
//	// resp.Usage.Cost is the charge for the tier that served.
func WithServiceTier(tier ServiceTier) llms.CallOption {
	if tier == "" {
		return func(*llms.CallOptions) {}
	}
	return llms.WithExtraBodyParam("service_tier", string(tier))
}

// WithUsageAccounting asks OpenRouter to report what the request cost, filling
// [llms.Usage].Cost with the charge in USD for the endpoint that served it.
//
// It is off by default because the accounting adds a small amount of work
// upstream. Cost arrives on the response, and on a stream's final chunk, so no
// follow-up lookup is needed; [Client.GenerationCost] remains for retrieving a
// cost after the fact from a generation id.
//
// A [llms.CostTracker] banks a reported cost in preference to its own estimate,
// which matters most on a non-default service tier, where the tier that served
// sets the rate.
func WithUsageAccounting() llms.CallOption {
	return llms.WithExtraBodyParam("usage", map[string]any{"include": true})
}

// PricingModeFor maps a served service tier onto the billing lane that prices
// it, for [llms.WithPricingMode] and [llms.CostTracker.RecordMode].
//
// Feed it [llms.Response.ServiceTier], the tier that served, rather than the one
// requested: priority falls back to other endpoints and bills at whichever one
// ran. An unknown or empty tier maps to standard.
//
// OpenRouter models have no published rate cards in this SDK, so a mode resolved
// this way prices at standard rates and reports known=false. It is here for a
// caller who registered cards of their own with [llms.CostTracker.SetModePricing];
// for the real charge use [WithUsageAccounting].
func PricingModeFor(tier ServiceTier) llms.PricingMode {
	switch tier {
	case TierFlex:
		return llms.PricingModeFlex
	case TierPriority, TierFast:
		return llms.PricingModeFast
	default:
		return llms.PricingModeStandard
	}
}

// Model-id variants. OpenRouter publishes no ":flex" or ":priority" variant;
// tier endpoints are admitted into a sort instead, and then have to win it like
// any other endpoint. Setting provider.order disables that admission, because an
// explicit order replaces sorting.
const (
	// VariantNitro sorts by throughput and admits priority endpoints.
	VariantNitro = ":nitro"
	// VariantFloor sorts by price and admits flex endpoints.
	VariantFloor = ":floor"
)

// Nitro returns the model id with the :nitro variant applied, sorting candidate
// endpoints by throughput and admitting priority-tier endpoints into that sort.
// An id that already carries a variant is returned unchanged.
//
//	client.GenerateContent(ctx, msgs, llms.WithModel(openrouter.Nitro("openai/gpt-4o")))
func Nitro(model string) string { return withVariant(model, VariantNitro) }

// Floor returns the model id with the :floor variant applied, sorting candidate
// endpoints by price and admitting flex-tier endpoints into that sort. An id
// that already carries a variant is returned unchanged.
func Floor(model string) string { return withVariant(model, VariantFloor) }

// withVariant appends a variant suffix, leaving an empty id and an id that
// already has a variant alone. Variants are the suffix after the last colon of
// the "author/slug:variant" form.
func withVariant(model, variant string) string {
	if model == "" || strings.Contains(model, ":") {
		return model
	}
	return model + variant
}
