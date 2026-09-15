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
// The tier that actually served the request is reported back on
// [llms.Response.ServiceTier] ("default", "flex", "priority", or empty when
// upstream reports nothing). Because priority can fall back and flex can be
// absent, the served tier is not always the tier requested.
//
// This routes only. Cost accounting is separate: pair it with
// [llms.WithPricingMode] ([llms.PricingModeFlex] or [llms.PricingModeFast]) so
// the recorded cost matches the lane.
//
// An empty tier is a no-op, so a configured-but-unset tier sends no field.
//
//	resp, err := client.GenerateContent(ctx, msgs,
//	    openrouter.WithServiceTier(openrouter.TierFlex),
//	    llms.WithPricingMode(llms.PricingModeFlex))
func WithServiceTier(tier ServiceTier) llms.CallOption {
	if tier == "" {
		return func(*llms.CallOptions) {}
	}
	return llms.WithExtraBodyParam("service_tier", string(tier))
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
