package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestWithServiceTier(t *testing.T) {
	for _, tc := range []struct {
		name string
		tier ServiceTier
		want any
	}{
		{"flex", TierFlex, "flex"},
		{"priority", TierPriority, "priority"},
		{"fast", TierFast, "fast"},
		{"default", TierDefault, "default"},
		{"empty is a no-op", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := llms.ApplyOptions(WithServiceTier(tc.tier))
			if got := opts.ExtraBody["service_tier"]; got != tc.want {
				t.Fatalf("service_tier = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestServiceTierRoundTrip(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["service_tier"] != "flex" {
			t.Errorf("request body: %v", body)
		}
		fmt.Fprint(w, `{"service_tier":"default","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	})
	resp, err := c.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}}, WithServiceTier(TierFlex))
	if err != nil {
		t.Fatal(err)
	}
	// The served tier is reported back, and is not required to be the one asked for.
	if resp.ServiceTier != "default" {
		t.Fatalf("ServiceTier = %q", resp.ServiceTier)
	}
}

func TestModelVariants(t *testing.T) {
	for _, tc := range []struct {
		model string
		nitro string
		floor string
	}{
		{"openai/gpt-4o", "openai/gpt-4o:nitro", "openai/gpt-4o:floor"},
		{"openai/gpt-4o:free", "openai/gpt-4o:free", "openai/gpt-4o:free"},
		{"", "", ""},
	} {
		if got := Nitro(tc.model); got != tc.nitro {
			t.Errorf("Nitro(%q) = %q, want %q", tc.model, got, tc.nitro)
		}
		if got := Floor(tc.model); got != tc.floor {
			t.Errorf("Floor(%q) = %q, want %q", tc.model, got, tc.floor)
		}
	}
}

func TestPricingModeFor(t *testing.T) {
	for tier, want := range map[ServiceTier]llms.PricingMode{
		TierFlex:     llms.PricingModeFlex,
		TierPriority: llms.PricingModeFast,
		TierFast:     llms.PricingModeFast,
		TierDefault:  llms.PricingModeStandard,
		"":           llms.PricingModeStandard,
		"unknown":    llms.PricingModeStandard,
	} {
		if got := PricingModeFor(tier); got != want {
			t.Errorf("PricingModeFor(%q) = %q, want %q", tier, got, want)
		}
	}
}

// Usage accounting is what makes the served tier's real charge available, so the
// request has to carry it and the reported cost has to reach llms.Usage.
func TestUsageAccountingRoundTrip(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		usage, ok := body["usage"].(map[string]any)
		if !ok || usage["include"] != true {
			t.Errorf("request body: %v", body)
		}
		fmt.Fprint(w, `{"service_tier":"flex","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":4,"total_tokens":5,"cost":5.15e-06,"is_byok":false}}`)
	})
	resp, err := c.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}},
		WithServiceTier(TierFlex), WithUsageAccounting())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage.Cost == nil || *resp.Usage.Cost != 5.15e-06 {
		t.Fatalf("Usage.Cost = %v", resp.Usage.Cost)
	}
	if resp.ServiceTier != "flex" {
		t.Fatalf("ServiceTier = %q", resp.ServiceTier)
	}

	// A reported charge is banked as-is, ahead of any rate card.
	tracker := llms.NewCostTracker()
	tracker.SetPricing(llms.ProviderOpenRouter, c.Model(), llms.Pricing{Input: 1000, Output: 1000})
	cost, known := tracker.Record(llms.ProviderOpenRouter, c.Model(), resp.Usage)
	if !known || cost != 5.15e-06 {
		t.Fatalf("Record = (%v, %v), want the reported cost", cost, known)
	}
}
