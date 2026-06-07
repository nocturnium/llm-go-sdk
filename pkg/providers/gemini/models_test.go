package gemini

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/internal/geminiapi"
)

func TestConvertGeminiModel(t *testing.T) {
	tests := []struct {
		name     string
		input    geminiapi.ModelInfo
		expected llms.ModelInfo
	}{
		{
			name: "known model gemini-1.5-pro",
			input: geminiapi.ModelInfo{
				Name:                       "models/gemini-1.5-pro",
				DisplayName:                "Gemini 1.5 Pro",
				InputTokenLimit:            1000000,
				OutputTokenLimit:           8192,
				SupportedGenerationMethods: []string{"generateContent", "countTokens"},
			},
			expected: llms.ModelInfo{
				ID:            "gemini-1.5-pro",
				DisplayName:   "Gemini 1.5 Pro",
				Provider:      llms.ProviderGemini,
				Organization:  "Google",
				ContextLength: 1000000,
				MaxOutput:     8192,
				Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
				Pricing:       &llms.ModelPricing{Input: 1.25, Output: 5.00},
			},
		},
		{
			name: "known model gemini-1.5-flash",
			input: geminiapi.ModelInfo{
				Name:                       "models/gemini-1.5-flash",
				DisplayName:                "Gemini 1.5 Flash",
				InputTokenLimit:            1000000,
				OutputTokenLimit:           8192,
				SupportedGenerationMethods: []string{"generateContent", "countTokens"},
			},
			expected: llms.ModelInfo{
				ID:            "gemini-1.5-flash",
				DisplayName:   "Gemini 1.5 Flash",
				Provider:      llms.ProviderGemini,
				Organization:  "Google",
				ContextLength: 1000000,
				MaxOutput:     8192,
				Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
				Pricing:       &llms.ModelPricing{Input: 0.075, Output: 0.30},
			},
		},
		{
			name: "known model text-embedding-004",
			input: geminiapi.ModelInfo{
				Name:                       "models/text-embedding-004",
				DisplayName:                "Text Embedding 004",
				InputTokenLimit:            2048,
				SupportedGenerationMethods: []string{"embedContent", "countTokens"},
			},
			expected: llms.ModelInfo{
				ID:            "text-embedding-004",
				DisplayName:   "Text Embedding 004",
				Provider:      llms.ProviderGemini,
				Organization:  "Google",
				ContextLength: 2048,
				MaxOutput:     0,
				Types:         []llms.ModelType{llms.ModelTypeEmbedding},
				Pricing:       &llms.ModelPricing{Input: 0.00},
			},
		},
		{
			name: "known model gemini-1.0-pro",
			input: geminiapi.ModelInfo{
				Name:                       "models/gemini-1.0-pro",
				DisplayName:                "Gemini 1.0 Pro",
				InputTokenLimit:            32760,
				OutputTokenLimit:           8192,
				SupportedGenerationMethods: []string{"generateContent", "countTokens"},
			},
			expected: llms.ModelInfo{
				ID:            "gemini-1.0-pro",
				DisplayName:   "Gemini 1.0 Pro",
				Provider:      llms.ProviderGemini,
				Organization:  "Google",
				ContextLength: 32760,
				MaxOutput:     8192,
				Types:         []llms.ModelType{llms.ModelTypeChat},
				Pricing:       &llms.ModelPricing{Input: 0.50, Output: 1.50},
			},
		},
		{
			name: "unknown gemini-1.5 model infers vision",
			input: geminiapi.ModelInfo{
				Name:                       "models/gemini-1.5-unknown",
				InputTokenLimit:            1000000,
				OutputTokenLimit:           8192,
				SupportedGenerationMethods: []string{"generateContent"},
			},
			expected: llms.ModelInfo{
				ID:            "gemini-1.5-unknown",
				DisplayName:   "Gemini 1.5 unknown", // Generated from formatGeminiModelName
				Provider:      llms.ProviderGemini,
				Organization:  "Google",
				ContextLength: 1000000,
				MaxOutput:     8192,
				Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
			},
		},
		{
			name: "unknown gemini-1.0 model infers chat only",
			input: geminiapi.ModelInfo{
				Name:                       "models/gemini-1.0-unknown",
				InputTokenLimit:            32760,
				OutputTokenLimit:           8192,
				SupportedGenerationMethods: []string{"generateContent"},
			},
			expected: llms.ModelInfo{
				ID:            "gemini-1.0-unknown",
				DisplayName:   "Gemini 1.0 unknown",
				Provider:      llms.ProviderGemini,
				Organization:  "Google",
				ContextLength: 32760,
				MaxOutput:     8192,
				Types:         []llms.ModelType{llms.ModelTypeChat},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertGeminiModel(&tt.input)

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

func TestFormatGeminiModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"gemini-1.5-pro", "Gemini 1.5 Pro"},
		{"gemini-1.5-flash", "Gemini 1.5 Flash"},
		{"gemini-1.0-pro", "Gemini 1.0 Pro"},
		{"gemini-2.0-flash", "Gemini 2.0 Flash"},
		{"text-embedding-004", "text embedding 004"},
		{"unknown-model", "unknown model"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := formatGeminiModelName(tt.input)
			if result != tt.expected {
				t.Errorf("formatGeminiModelName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestInferGeminiModelTypes(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		methods  []string
		expected []llms.ModelType
	}{
		{
			name:     "gemini-1.5-pro has vision",
			id:       "gemini-1.5-pro",
			methods:  []string{"generateContent", "countTokens"},
			expected: []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		},
		{
			name:     "gemini-1.5-flash has vision",
			id:       "gemini-1.5-flash",
			methods:  []string{"generateContent", "countTokens"},
			expected: []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		},
		{
			name:     "gemini-2.0-flash has vision",
			id:       "gemini-2.0-flash",
			methods:  []string{"generateContent", "countTokens"},
			expected: []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		},
		{
			name:     "gemini-1.0-pro chat only",
			id:       "gemini-1.0-pro",
			methods:  []string{"generateContent", "countTokens"},
			expected: []llms.ModelType{llms.ModelTypeChat},
		},
		{
			name:     "gemini-pro-vision has vision",
			id:       "gemini-pro-vision",
			methods:  []string{"generateContent"},
			expected: []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		},
		{
			name:     "text-embedding-004 is embedding",
			id:       "text-embedding-004",
			methods:  []string{"embedContent", "countTokens"},
			expected: []llms.ModelType{llms.ModelTypeEmbedding},
		},
		{
			name:     "embedding-001 is embedding",
			id:       "embedding-001",
			methods:  []string{"embedContent"},
			expected: []llms.ModelType{llms.ModelTypeEmbedding},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferGeminiModelTypes(tt.id, tt.methods)
			if len(result) != len(tt.expected) {
				t.Errorf("inferGeminiModelTypes(%q, %v) returned %d types, want %d", tt.id, tt.methods, len(result), len(tt.expected))
				return
			}
			for i, typ := range result {
				if typ != tt.expected[i] {
					t.Errorf("inferGeminiModelTypes(%q, %v)[%d] = %q, want %q", tt.id, tt.methods, i, typ, tt.expected[i])
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
			if len(meta.types) == 0 {
				t.Errorf("model %q has no types", id)
			}
			// All Gemini models should have pricing (even if zero)
			if meta.pricing == nil {
				t.Errorf("model %q has nil pricing", id)
			}
		})
	}
}

func TestKnownModelsCategories(t *testing.T) {
	// Test that Gemini 1.5+ models have vision capability
	visionModels := []string{
		"gemini-2.0-flash",
		"gemini-2.0-flash-exp",
		"gemini-1.5-pro",
		"gemini-1.5-pro-latest",
		"gemini-1.5-pro-001",
		"gemini-1.5-pro-002",
		"gemini-1.5-flash",
		"gemini-1.5-flash-latest",
		"gemini-1.5-flash-001",
		"gemini-1.5-flash-002",
		"gemini-1.5-flash-8b",
		"gemini-1.0-pro-vision",
		"gemini-pro-vision",
	}

	for _, id := range visionModels {
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

	// Test that Gemini 1.0 Pro (non-vision) doesn't have vision
	chatOnlyModels := []string{
		"gemini-1.0-pro",
		"gemini-1.0-pro-latest",
		"gemini-pro",
	}

	for _, id := range chatOnlyModels {
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

	// Test embedding models
	embeddingModels := []string{
		"text-embedding-004",
		"embedding-001",
	}

	for _, id := range embeddingModels {
		t.Run(id+"_is_embedding", func(t *testing.T) {
			meta, ok := knownModels[id]
			if !ok {
				t.Fatalf("model %q not found in knownModels", id)
			}
			hasEmbedding := false
			for _, typ := range meta.types {
				if typ == llms.ModelTypeEmbedding {
					hasEmbedding = true
					break
				}
			}
			if !hasEmbedding {
				t.Errorf("model %q should have embedding capability", id)
			}
		})
	}
}

func TestClientImplementsModelLister(_ *testing.T) {
	// This is a compile-time check, but we include it as a test for documentation
	var _ llms.ModelLister = (*Client)(nil)
}

func TestModelIDExtraction(t *testing.T) {
	// Test that model IDs are correctly extracted from resource names
	tests := []struct {
		resourceName string
		expectedID   string
	}{
		{"models/gemini-1.5-pro", "gemini-1.5-pro"},
		{"models/gemini-1.5-flash-001", "gemini-1.5-flash-001"},
		{"models/text-embedding-004", "text-embedding-004"},
		{"gemini-1.5-pro", "gemini-1.5-pro"}, // Already just the ID
	}

	for _, tt := range tests {
		t.Run(tt.resourceName, func(t *testing.T) {
			input := geminiapi.ModelInfo{Name: tt.resourceName}
			result := convertGeminiModel(&input)
			if result.ID != tt.expectedID {
				t.Errorf("convertGeminiModel with name %q produced ID %q, want %q", tt.resourceName, result.ID, tt.expectedID)
			}
		})
	}
}
