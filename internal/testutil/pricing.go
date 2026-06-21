package testutil

import (
	"math"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

const tokenPricingEpsilon = 1e-12

// AssertKnownModelTokenPricingReconcilesWithDefaultPricing verifies provider
// model pricing against expected values and the central DefaultPricing table.
func AssertKnownModelTokenPricingReconcilesWithDefaultPricing(
	t *testing.T,
	provider llms.Provider,
	knownPricing map[string]*llms.ModelPricing,
	expectedPricing map[string]llms.ModelPricing,
) {
	t.Helper()

	providerPrefix := string(provider)
	for modelID, expected := range expectedPricing {
		pricing, ok := knownPricing[modelID]
		if !ok {
			t.Fatalf("knownModels missing priced model %s", modelID)
		}
		if pricing == nil {
			t.Fatalf("knownModels[%s].pricing is nil", modelID)
		}
		if pricing.Input != expected.Input {
			t.Errorf("%s Input = %f, want %f", modelID, pricing.Input, expected.Input)
		}
		if pricing.Output != expected.Output {
			t.Errorf("%s Output = %f, want %f", modelID, pricing.Output, expected.Output)
		}

		central, ok := llms.DefaultPricing[providerPrefix+":"+modelID]
		if !ok {
			t.Fatalf("DefaultPricing missing %s:%s", providerPrefix, modelID)
		}
		if math.Abs(pricing.Input-central.PromptPerMillion) > tokenPricingEpsilon {
			t.Errorf("%s Input = %f, DefaultPricing PromptPerMillion = %f", modelID, pricing.Input, central.PromptPerMillion)
		}
		if math.Abs(pricing.Output-central.CompletionPerMillion) > tokenPricingEpsilon {
			t.Errorf("%s Output = %f, DefaultPricing CompletionPerMillion = %f", modelID, pricing.Output, central.CompletionPerMillion)
		}
	}

	for modelID, pricing := range knownPricing {
		central, ok := llms.DefaultPricing[providerPrefix+":"+modelID]
		if !ok {
			continue
		}
		if pricing == nil {
			t.Fatalf("knownModels[%s].pricing is nil but DefaultPricing has token pricing", modelID)
		}
		if math.Abs(pricing.Input-central.PromptPerMillion) > tokenPricingEpsilon {
			t.Errorf("%s Input = %f, DefaultPricing PromptPerMillion = %f", modelID, pricing.Input, central.PromptPerMillion)
		}
		if math.Abs(pricing.Output-central.CompletionPerMillion) > tokenPricingEpsilon {
			t.Errorf("%s Output = %f, DefaultPricing CompletionPerMillion = %f", modelID, pricing.Output, central.CompletionPerMillion)
		}
	}
}
