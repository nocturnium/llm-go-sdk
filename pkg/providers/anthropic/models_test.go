package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/testutil"
)

var expectedKnownModelTokenPricing = map[string]llms.Pricing{
	"claude-fable-5":             {Input: 10.00, Output: 50.00},
	"claude-opus-4-8":            {Input: 5.00, Output: 25.00},
	"claude-opus-4-7":            {Input: 5.00, Output: 25.00},
	"claude-opus-4-6":            {Input: 5.00, Output: 25.00},
	"claude-opus-4-5":            {Input: 5.00, Output: 25.00},
	"claude-opus-4-5-20251101":   {Input: 5.00, Output: 25.00},
	"claude-opus-4-1":            {Input: 15.00, Output: 75.00},
	"claude-opus-4-1-20250805":   {Input: 15.00, Output: 75.00},
	"claude-sonnet-4-6":          {Input: 3.00, Output: 15.00},
	"claude-sonnet-4-5":          {Input: 3.00, Output: 15.00},
	"claude-sonnet-4-5-20250929": {Input: 3.00, Output: 15.00},
	"claude-haiku-4-5":           {Input: 1.00, Output: 5.00},
	"claude-haiku-4-5-20251001":  {Input: 1.00, Output: 5.00},
	"claude-3-5-sonnet-latest":   {Input: 3.00, Output: 15.00},
	"claude-3-5-sonnet-20241022": {Input: 3.00, Output: 15.00},
	"claude-3-5-sonnet-20240620": {Input: 3.00, Output: 15.00},
	"claude-3-5-haiku-latest":    {Input: 0.80, Output: 4.00},
	"claude-3-5-haiku-20241022":  {Input: 0.80, Output: 4.00},
	"claude-3-opus-latest":       {Input: 15.00, Output: 75.00},
	"claude-3-opus-20240229":     {Input: 15.00, Output: 75.00},
	"claude-3-sonnet-20240229":   {Input: 3.00, Output: 15.00},
	"claude-3-haiku-20240307":    {Input: 0.25, Output: 1.25},
	"claude-2.1":                 {Input: 8.00, Output: 24.00},
	"claude-2.0":                 {Input: 8.00, Output: 24.00},
	"claude-instant-1.2":         {Input: 0.80, Output: 2.40},
}

func TestKnownModelTokenPricingReconcilesWithDefaultPricing(t *testing.T) {
	testutil.AssertKnownModelTokenPricingReconcilesWithDefaultPricing(
		t,
		llms.ProviderAnthropic,
		knownModelTokenPricing(),
		expectedKnownModelTokenPricing,
	)
}

func knownModelTokenPricing() map[string]*llms.Pricing {
	pricing := make(map[string]*llms.Pricing, len(knownModels))
	for modelID, metadata := range knownModels {
		pricing[modelID] = metadata.pricing
	}
	return pricing
}

func TestConvertAnthropicModel(t *testing.T) {
	tests := []struct {
		name     string
		input    anthropicModelInput
		expected llms.ModelInfo
	}{
		{
			name: "known model claude-3-5-sonnet-latest",
			input: anthropicModelInput{
				ID:          "claude-3-5-sonnet-latest",
				DisplayName: "Claude 3.5 Sonnet",
				CreatedAt:   "2024-10-22T00:00:00Z",
			},
			expected: llms.ModelInfo{
				ID:            "claude-3-5-sonnet-latest",
				DisplayName:   "Claude 3.5 Sonnet",
				Provider:      llms.ProviderAnthropic,
				Organization:  "Anthropic",
				ContextLength: 200000,
				MaxOutput:     8192,
				Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
				Pricing:       &llms.Pricing{Input: 3.00, Output: 15.00},
			},
		},
		{
			name: "known model claude-3-5-haiku-latest",
			input: anthropicModelInput{
				ID:          "claude-3-5-haiku-latest",
				DisplayName: "Claude 3.5 Haiku",
				CreatedAt:   "2024-10-22T00:00:00Z",
			},
			expected: llms.ModelInfo{
				ID:            "claude-3-5-haiku-latest",
				DisplayName:   "Claude 3.5 Haiku",
				Provider:      llms.ProviderAnthropic,
				Organization:  "Anthropic",
				ContextLength: 200000,
				MaxOutput:     8192,
				Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
				Pricing:       &llms.Pricing{Input: 0.80, Output: 4.00},
			},
		},
		{
			name: "known model claude-3-opus-latest",
			input: anthropicModelInput{
				ID:          "claude-3-opus-latest",
				DisplayName: "Claude 3 Opus",
				CreatedAt:   "2024-02-29T00:00:00Z",
			},
			expected: llms.ModelInfo{
				ID:            "claude-3-opus-latest",
				DisplayName:   "Claude 3 Opus",
				Provider:      llms.ProviderAnthropic,
				Organization:  "Anthropic",
				ContextLength: 200000,
				MaxOutput:     4096,
				Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
				Pricing:       &llms.Pricing{Input: 15.00, Output: 75.00},
			},
		},
		{
			name: "known model claude-3-haiku",
			input: anthropicModelInput{
				ID:          "claude-3-haiku-20240307",
				DisplayName: "Claude 3 Haiku",
				CreatedAt:   "2024-03-07T00:00:00Z",
			},
			expected: llms.ModelInfo{
				ID:            "claude-3-haiku-20240307",
				DisplayName:   "Claude 3 Haiku",
				Provider:      llms.ProviderAnthropic,
				Organization:  "Anthropic",
				ContextLength: 200000,
				MaxOutput:     4096,
				Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
				Pricing:       &llms.Pricing{Input: 0.25, Output: 1.25},
			},
		},
		{
			name: "known model claude-2.1",
			input: anthropicModelInput{
				ID:          "claude-2.1",
				DisplayName: "",
				CreatedAt:   "",
			},
			expected: llms.ModelInfo{
				ID:            "claude-2.1",
				DisplayName:   "Claude 2.1",
				Provider:      llms.ProviderAnthropic,
				Organization:  "Anthropic",
				ContextLength: 200000,
				MaxOutput:     4096,
				Types:         []llms.ModelType{llms.ModelTypeChat},
				Pricing:       &llms.Pricing{Input: 8.00, Output: 24.00},
			},
		},
		{
			name: "unknown claude-3 model infers vision",
			input: anthropicModelInput{
				ID:          "claude-3-unknown-20250101",
				DisplayName: "",
				CreatedAt:   "",
			},
			expected: llms.ModelInfo{
				ID:            "claude-3-unknown-20250101",
				DisplayName:   "Claude 3 unknown 20250101", // Generated from formatClaudeModelName
				Provider:      llms.ProviderAnthropic,
				Organization:  "Anthropic",
				ContextLength: 200000,
				Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
			},
		},
		{
			name: "unknown claude-2 model infers chat only",
			input: anthropicModelInput{
				ID:          "claude-2-unknown",
				DisplayName: "",
				CreatedAt:   "",
			},
			expected: llms.ModelInfo{
				ID:            "claude-2-unknown",
				DisplayName:   "Claude.2 unknown", // Generated from formatClaudeModelName
				Provider:      llms.ProviderAnthropic,
				Organization:  "Anthropic",
				ContextLength: 200000,
				Types:         []llms.ModelType{llms.ModelTypeChat},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock anthropicapi.ModelInfo
			result := testConvertModel(tt.input)

			if result.ID != tt.expected.ID {
				t.Errorf("ID = %q, want %q", result.ID, tt.expected.ID)
			}
			if result.DisplayName != tt.expected.DisplayName {
				t.Errorf("DisplayName = %q, want %q", result.DisplayName, tt.expected.DisplayName)
			}
			if result.Provider != tt.expected.Provider {
				t.Errorf("Provider = %q, want %q", result.Provider, tt.expected.Provider)
			}
			if result.Organization != tt.expected.Organization {
				t.Errorf("Organization = %q, want %q", result.Organization, tt.expected.Organization)
			}
			if result.ContextLength != tt.expected.ContextLength {
				t.Errorf("ContextLength = %d, want %d", result.ContextLength, tt.expected.ContextLength)
			}
			if result.MaxOutput != tt.expected.MaxOutput {
				t.Errorf("MaxOutput = %d, want %d", result.MaxOutput, tt.expected.MaxOutput)
			}
			if len(result.Types) != len(tt.expected.Types) {
				t.Errorf("Types length = %d, want %d", len(result.Types), len(tt.expected.Types))
			} else {
				for i, typ := range result.Types {
					if typ != tt.expected.Types[i] {
						t.Errorf("Types[%d] = %q, want %q", i, typ, tt.expected.Types[i])
					}
				}
			}
			if tt.expected.Pricing != nil {
				if result.Pricing == nil {
					t.Error("Pricing is nil, want non-nil")
				} else {
					if result.Pricing.Input != tt.expected.Pricing.Input {
						t.Errorf("Pricing.Input = %f, want %f", result.Pricing.Input, tt.expected.Pricing.Input)
					}
					if result.Pricing.Output != tt.expected.Pricing.Output {
						t.Errorf("Pricing.Output = %f, want %f", result.Pricing.Output, tt.expected.Pricing.Output)
					}
				}
			}
		})
	}
}

// anthropicModelInput is a test helper struct.
type anthropicModelInput struct {
	ID          string
	DisplayName string
	CreatedAt   string
}

// testConvertModel is a test helper that mimics convertAnthropicModel.
func testConvertModel(input anthropicModelInput) llms.ModelInfo {
	info := llms.ModelInfo{
		ID:           input.ID,
		DisplayName:  input.DisplayName,
		Provider:     llms.ProviderAnthropic,
		Organization: "Anthropic",
	}

	// Check if we have known metadata for this model
	if metadata, ok := knownModels[input.ID]; ok {
		if info.DisplayName == "" {
			info.DisplayName = metadata.displayName
		}
		info.ContextLength = metadata.contextLength
		info.MaxOutput = metadata.maxOutput
		info.Types = metadata.types
		info.Pricing = metadata.pricing
	} else {
		// Infer from model ID
		if info.DisplayName == "" {
			info.DisplayName = formatClaudeModelName(input.ID)
		}
		info.Types = inferClaudeModelTypes(input.ID)
		// apply default context length for Claude models
		switch {
		case len(input.ID) >= 8 && input.ID[:8] == "claude-3":
			info.ContextLength = 200000
		case len(input.ID) >= 8 && input.ID[:8] == "claude-2":
			info.ContextLength = 200000
		case len(input.ID) >= 14 && input.ID[:14] == "claude-instant":
			info.ContextLength = 100000
		}
	}

	return info
}

func TestFormatClaudeModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-3-5-sonnet-latest", "Claude 3.5 Sonnet latest"},
		{"claude-3-5-haiku-20241022", "Claude 3.5 Haiku 20241022"},
		{"claude-3-opus-20240229", "Claude 3 Opus 20240229"},
		{"claude-2-1", "Claude.2 1"},                 // version replacement doesn't apply here due to spacing
		{"claude-instant-1-2", "Claude Instant.1 2"}, // version replacement doesn't apply here due to spacing
		{"unknown-model", "unknown model"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := formatClaudeModelName(tt.input)
			if result != tt.expected {
				t.Errorf("formatClaudeModelName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestInferClaudeModelTypes(t *testing.T) {
	tests := []struct {
		input    string
		expected []llms.ModelType
	}{
		{"claude-3-5-sonnet-latest", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}},
		{"claude-3-opus-20240229", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}},
		{"claude-3-haiku-20240307", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}},
		{"claude-2.1", []llms.ModelType{llms.ModelTypeChat}},
		{"claude-2.0", []llms.ModelType{llms.ModelTypeChat}},
		{"claude-instant-1.2", []llms.ModelType{llms.ModelTypeChat}},
		{"unknown-model", []llms.ModelType{llms.ModelTypeChat}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := inferClaudeModelTypes(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("inferClaudeModelTypes(%q) returned %d types, want %d", tt.input, len(result), len(tt.expected))
				return
			}
			for i, typ := range result {
				if typ != tt.expected[i] {
					t.Errorf("inferClaudeModelTypes(%q)[%d] = %q, want %q", tt.input, i, typ, tt.expected[i])
				}
			}
		})
	}
}

func TestKnownModelsMetadata(t *testing.T) {
	// Verify all known models have required fields
	for id, meta := range knownModels {
		t.Run(id, func(t *testing.T) {
			if meta.displayName == "" {
				t.Errorf("model %q has empty displayName", id)
			}
			if meta.contextLength == 0 {
				t.Errorf("model %q has zero contextLength", id)
			}
			if len(meta.types) == 0 {
				t.Errorf("model %q has no types", id)
			}
			// All Claude models should have pricing
			if meta.pricing == nil {
				t.Errorf("model %q has nil pricing", id)
			}
		})
	}
}

func TestKnownModelsCategories(t *testing.T) {
	// Test that Claude 3+ models have vision capability
	claude3Models := []string{
		"claude-3-5-sonnet-latest",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-sonnet-20240620",
		"claude-3-5-haiku-latest",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-latest",
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
	}

	for _, id := range claude3Models {
		t.Run(id+"_has_vision", func(t *testing.T) {
			meta, ok := knownModels[id]
			if !ok {
				t.Fatalf("model %q not found in knownModels", id)
			}
			hasVision := false
			for _, typ := range meta.types {
				if typ == llms.ModelTypeVision {
					hasVision = true
					break
				}
			}
			if !hasVision {
				t.Errorf("model %q should have vision capability", id)
			}
		})
	}

	// Test that Claude 2 models don't have vision
	claude2Models := []string{
		"claude-2.1",
		"claude-2.0",
		"claude-instant-1.2",
	}

	for _, id := range claude2Models {
		t.Run(id+"_no_vision", func(t *testing.T) {
			meta, ok := knownModels[id]
			if !ok {
				t.Fatalf("model %q not found in knownModels", id)
			}
			for _, typ := range meta.types {
				if typ == llms.ModelTypeVision {
					t.Errorf("model %q should not have vision capability", id)
					break
				}
			}
		})
	}
}

func TestClientImplementsModelLister(_ *testing.T) {
	// This is a compile-time check, but we include it as a test for documentation
	var _ llms.ModelLister = (*Client)(nil)
}

func TestModelInfo_ModelNotFoundUsesSentinel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/missing-model" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"missing resource","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowHTTP(),
		WithAllowPrivateIPs(),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.ModelInfo(context.Background(), "missing-model")
	if !errors.Is(err, llms.ErrModelNotFound) {
		t.Fatalf("ModelInfo() error = %v, want ErrModelNotFound", err)
	}
	if strings.Contains(err.Error(), "404") {
		t.Fatalf("ModelInfo() error = %q, want no literal 404", err)
	}
}
