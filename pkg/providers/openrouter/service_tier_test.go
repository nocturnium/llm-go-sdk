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
