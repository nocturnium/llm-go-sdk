package openai

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

// Mock response data matching OpenAI API format.
var mockModelsResponse = `{
	"object": "list",
	"data": [
		{
			"id": "gpt-4o",
			"object": "model",
			"created": 1715367049,
			"owned_by": "system"
		},
		{
			"id": "gpt-4o-mini",
			"object": "model",
			"created": 1721172741,
			"owned_by": "system"
		},
		{
			"id": "gpt-3.5-turbo",
			"object": "model",
			"created": 1677610602,
			"owned_by": "openai"
		},
		{
			"id": "text-embedding-3-small",
			"object": "model",
			"created": 1705948997,
			"owned_by": "system"
		},
		{
			"id": "text-embedding-3-large",
			"object": "model",
			"created": 1705953180,
			"owned_by": "system"
		},
		{
			"id": "dall-e-3",
			"object": "model",
			"created": 1698785189,
			"owned_by": "system"
		},
		{
			"id": "whisper-1",
			"object": "model",
			"created": 1677532384,
			"owned_by": "openai-internal"
		},
		{
			"id": "tts-1",
			"object": "model",
			"created": 1681940951,
			"owned_by": "openai-internal"
		},
		{
			"id": "text-moderation-latest",
			"object": "model",
			"created": 1677532379,
			"owned_by": "openai"
		},
		{
			"id": "ft:gpt-3.5-turbo:my-org:custom:abc123",
			"object": "model",
			"created": 1699999999,
			"owned_by": "user-abc123"
		},
		{
			"id": "davinci-002",
			"object": "model",
			"created": 1649880484,
			"owned_by": "openai"
		}
	]
}`

var expectedKnownModelTokenPricing = map[string]llms.ModelPricing{
	"gpt-5.5":                {Input: 5.00, Output: 30.00},
	"gpt-5.5-pro":            {Input: 30.00, Output: 180.00},
	"gpt-5.4":                {Input: 2.50, Output: 15.00},
	"gpt-5.4-mini":           {Input: 0.75, Output: 4.50},
	"gpt-5.4-nano":           {Input: 0.20, Output: 1.25},
	"gpt-5.4-pro":            {Input: 30.00, Output: 180.00},
	"gpt-5":                  {Input: 1.25, Output: 10.00},
	"gpt-5-mini":             {Input: 0.25, Output: 2.00},
	"gpt-5-nano":             {Input: 0.05, Output: 0.40},
	"gpt-4.1":                {Input: 2.00, Output: 8.00},
	"gpt-4.1-mini":           {Input: 0.40, Output: 1.60},
	"gpt-4.1-nano":           {Input: 0.10, Output: 0.40},
	"gpt-4o":                 {Input: 2.50, Output: 10.00},
	"gpt-4o-mini":            {Input: 0.15, Output: 0.60},
	"gpt-4o-2024-11-20":      {Input: 2.50, Output: 10.00},
	"gpt-4o-2024-08-06":      {Input: 2.50, Output: 10.00},
	"gpt-4o-mini-2024-07-18": {Input: 0.15, Output: 0.60},
	"gpt-4-turbo":            {Input: 10.00, Output: 30.00},
	"gpt-4-turbo-preview":    {Input: 10.00, Output: 30.00},
	"gpt-4":                  {Input: 30.00, Output: 60.00},
	"gpt-4-32k":              {Input: 60.00, Output: 120.00},
	"gpt-3.5-turbo":          {Input: 0.50, Output: 1.50},
	"gpt-3.5-turbo-16k":      {Input: 0.50, Output: 1.50},
	"o4-mini":                {Input: 1.10, Output: 4.40},
	"o3":                     {Input: 2.00, Output: 8.00},
	"o3-mini":                {Input: 1.10, Output: 4.40},
	"o1":                     {Input: 15.00, Output: 60.00},
	"o1-preview":             {Input: 15.00, Output: 60.00},
	"o1-mini":                {Input: 3.00, Output: 12.00},
	"text-embedding-3-large": {Input: 0.13},
	"text-embedding-3-small": {Input: 0.02},
	"text-embedding-ada-002": {Input: 0.10},
}

func TestKnownModelTokenPricingReconcilesWithDefaultPricing(t *testing.T) {
	const epsilon = 1e-12

	for modelID, expected := range expectedKnownModelTokenPricing {
		metadata, ok := knownModels[modelID]
		if !ok {
			t.Fatalf("knownModels missing priced model %s", modelID)
		}
		if metadata.pricing == nil {
			t.Fatalf("knownModels[%s].pricing is nil", modelID)
		}
		if metadata.pricing.Input != expected.Input {
			t.Errorf("%s Input = %f, want %f", modelID, metadata.pricing.Input, expected.Input)
		}
		if metadata.pricing.Output != expected.Output {
			t.Errorf("%s Output = %f, want %f", modelID, metadata.pricing.Output, expected.Output)
		}

		central, ok := llms.DefaultPricing[string(llms.ProviderOpenAI)+":"+modelID]
		if !ok {
			t.Fatalf("DefaultPricing missing openai:%s", modelID)
		}
		if math.Abs(metadata.pricing.Input-central.PromptPerMillion) > epsilon {
			t.Errorf("%s Input = %f, DefaultPricing PromptPerMillion = %f", modelID, metadata.pricing.Input, central.PromptPerMillion)
		}
		if math.Abs(metadata.pricing.Output-central.CompletionPerMillion) > epsilon {
			t.Errorf("%s Output = %f, DefaultPricing CompletionPerMillion = %f", modelID, metadata.pricing.Output, central.CompletionPerMillion)
		}
	}

	for modelID, metadata := range knownModels {
		central, ok := llms.DefaultPricing[string(llms.ProviderOpenAI)+":"+modelID]
		if !ok {
			continue
		}
		if metadata.pricing == nil {
			t.Fatalf("knownModels[%s].pricing is nil but DefaultPricing has token pricing", modelID)
		}
		if math.Abs(metadata.pricing.Input-central.PromptPerMillion) > epsilon {
			t.Errorf("%s Input = %f, DefaultPricing PromptPerMillion = %f", modelID, metadata.pricing.Input, central.PromptPerMillion)
		}
		if math.Abs(metadata.pricing.Output-central.CompletionPerMillion) > epsilon {
			t.Errorf("%s Output = %f, DefaultPricing CompletionPerMillion = %f", modelID, metadata.pricing.Output, central.CompletionPerMillion)
		}
	}
}

func setupMockServer(t *testing.T, handler http.HandlerFunc) *Client {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	return client
}

func TestListModels(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/models" {
			t.Errorf("expected /models, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-api-key" {
			t.Errorf("expected Bearer test-api-key, got %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	result, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 11 {
		t.Errorf("expected 11 models, got %d", len(result.Models))
	}

	// Verify first model (gpt-4o) has known metadata
	var gpt4o *llms.ModelInfo
	for _, m := range result.Models {
		if m.ID == "gpt-4o" {
			gpt4o = &m
			break
		}
	}

	if gpt4o == nil {
		t.Fatal("gpt-4o not found in results")
	}

	if gpt4o.DisplayName != "GPT-4o" {
		t.Errorf("expected display name 'GPT-4o', got %s", gpt4o.DisplayName)
	}
	if gpt4o.Provider != llms.ProviderOpenAI {
		t.Errorf("expected ProviderOpenAI, got %s", gpt4o.Provider)
	}
	if gpt4o.ContextLength != 128000 {
		t.Errorf("expected context length 128000, got %d", gpt4o.ContextLength)
	}
	if !gpt4o.HasType(llms.ModelTypeChat) {
		t.Error("expected gpt-4o to have chat type")
	}
	if !gpt4o.HasType(llms.ModelTypeVision) {
		t.Error("expected gpt-4o to have vision type")
	}
	if gpt4o.Pricing == nil {
		t.Error("expected pricing to be set for known model")
	}

	// Verify HasMore is false
	if result.HasMore {
		t.Error("expected HasMore to be false")
	}
}

func TestListModels_WithTypeFilter(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	t.Run("filter embedding models", func(t *testing.T) {
		result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeEmbedding))
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}

		if len(result.Models) != 2 {
			t.Errorf("expected 2 embedding models, got %d", len(result.Models))
		}
		for _, m := range result.Models {
			if !m.HasType(llms.ModelTypeEmbedding) {
				t.Errorf("model %s should have embedding type", m.ID)
			}
		}
	})

	t.Run("filter chat models", func(t *testing.T) {
		result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeChat))
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}

		// gpt-4o, gpt-4o-mini, gpt-3.5-turbo, and fine-tuned model
		if len(result.Models) < 3 {
			t.Errorf("expected at least 3 chat models, got %d", len(result.Models))
		}
	})

	t.Run("filter image models", func(t *testing.T) {
		result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeImage))
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}

		if len(result.Models) != 1 {
			t.Errorf("expected 1 image model, got %d", len(result.Models))
		}
		if result.Models[0].ID != "dall-e-3" {
			t.Errorf("expected dall-e-3, got %s", result.Models[0].ID)
		}
	})

	t.Run("filter audio models", func(t *testing.T) {
		result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeAudio))
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}

		if len(result.Models) != 2 {
			t.Errorf("expected 2 audio models, got %d", len(result.Models))
		}
	})
}

func TestListModels_WithLimit(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	result, err := client.ListModels(context.Background(), llms.WithModelsLimit(3))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 3 {
		t.Errorf("expected 3 models (limited), got %d", len(result.Models))
	}
}

func TestModelInfo(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	t.Run("known model found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "gpt-4o")
		if err != nil {
			t.Fatalf("ModelInfo failed: %v", err)
		}
		if info == nil {
			t.Fatal("expected model info, got nil")
		}
		if info.ID != "gpt-4o" {
			t.Errorf("expected gpt-4o, got %s", info.ID)
		}
		if info.ContextLength != 128000 {
			t.Errorf("expected context 128000, got %d", info.ContextLength)
		}
	})

	t.Run("unknown model found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "davinci-002")
		if err != nil {
			t.Fatalf("ModelInfo failed: %v", err)
		}
		if info == nil {
			t.Fatal("expected model info, got nil")
		}
		if info.ID != "davinci-002" {
			t.Errorf("expected davinci-002, got %s", info.ID)
		}
		// Should have inferred completion type
		if !info.HasType(llms.ModelTypeCompletion) {
			t.Error("expected completion type for davinci")
		}
	})

	t.Run("not found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "nonexistent-model")
		if !errors.Is(err, llms.ErrModelNotFound) {
			t.Fatalf("ModelInfo error = %v, want %v", err, llms.ErrModelNotFound)
		}
		if info != nil {
			t.Errorf("expected nil for nonexistent model, got %+v", info)
		}
	})
}

func TestListModels_APIError(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "Invalid API key", "type": "authentication_error"}}`))
	})

	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify error is wrapped with provider context
	errStr := err.Error()
	if !contains(errStr, "openai") {
		t.Errorf("expected error to contain provider name, got: %s", errStr)
	}
}

func TestInferModelTypes(t *testing.T) {
	tests := []struct {
		modelID  string
		expected []llms.ModelType
	}{
		{"gpt-4o", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}},
		{"gpt-4o-mini", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}},
		{"gpt-4-turbo", []llms.ModelType{llms.ModelTypeChat}},
		{"gpt-4", []llms.ModelType{llms.ModelTypeChat}},
		{"gpt-3.5-turbo", []llms.ModelType{llms.ModelTypeChat}},
		{"o1-preview", []llms.ModelType{llms.ModelTypeChat}},
		{"o1-mini", []llms.ModelType{llms.ModelTypeChat}},
		{"text-embedding-3-small", []llms.ModelType{llms.ModelTypeEmbedding}},
		{"text-embedding-ada-002", []llms.ModelType{llms.ModelTypeEmbedding}},
		{"dall-e-3", []llms.ModelType{llms.ModelTypeImage}},
		{"dall-e-2", []llms.ModelType{llms.ModelTypeImage}},
		{"whisper-1", []llms.ModelType{llms.ModelTypeAudio}},
		{"tts-1", []llms.ModelType{llms.ModelTypeAudio}},
		{"tts-1-hd", []llms.ModelType{llms.ModelTypeAudio}},
		{"text-moderation-latest", []llms.ModelType{llms.ModelTypeModeration}},
		{"davinci-002", []llms.ModelType{llms.ModelTypeCompletion}},
		{"curie-001", []llms.ModelType{llms.ModelTypeCompletion}},
		{"babbage-002", []llms.ModelType{llms.ModelTypeCompletion}},
		{"ada", []llms.ModelType{llms.ModelTypeCompletion}},
		{"unknown-model", nil},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := inferModelTypes(tt.modelID)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d types, got %d", len(tt.expected), len(result))
				return
			}
			for i, mt := range result {
				if mt != tt.expected[i] {
					t.Errorf("expected %s at index %d, got %s", tt.expected[i], i, mt)
				}
			}
		})
	}
}

func TestInferModelTypes_FineTuned(t *testing.T) {
	// Fine-tuned models should inherit base model types
	result := inferModelTypes("ft:gpt-3.5-turbo:my-org:custom:abc123")
	if len(result) != 1 {
		t.Errorf("expected 1 type, got %d", len(result))
		return
	}
	if result[0] != llms.ModelTypeChat {
		t.Errorf("expected chat type for fine-tuned gpt-3.5-turbo, got %s", result[0])
	}
}

func TestFormatModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"gpt-4o", "GPT-4o"},
		{"gpt-4o-mini", "GPT-4o Mini"},
		{"gpt-4o-2024-08-06", "GPT-4o 2024-08-06"},
		{"gpt-4-turbo", "GPT-4 Turbo"},
		{"gpt-4-turbo-preview", "GPT-4 Turbo preview"},
		{"gpt-4", "GPT-4"},
		{"gpt-4-32k", "GPT-4 32K"},
		{"gpt-3.5-turbo", "GPT-3.5 Turbo"},
		{"gpt-3.5-turbo-16k", "GPT-3.5 Turbo 16K"},
		{"text-embedding-3-small", "Text Embedding 3 Small"},
		{"text-embedding-3-large", "Text Embedding 3 Large"},
		{"dall-e-3", "DALL-E 3"},
		{"dall-e-2", "DALL-E 2"},
		{"whisper-1", "Whisper 1"},
		{"tts-1", "TTS 1"},
		{"tts-1-hd", "TTS 1 HD"},
		{"o1-preview", "o1 Preview"},
		{"o1-mini", "o1 Mini"},
		{"unknown-model", "unknown-model"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := formatModelName(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFormatModelName_FineTuned(t *testing.T) {
	result := formatModelName("ft:gpt-3.5-turbo:my-org:custom:abc123")
	if result != "Fine-tuned GPT-3.5 Turbo" {
		t.Errorf("expected 'Fine-tuned GPT-3.5 Turbo', got %s", result)
	}
}

func TestKnownModelsMetadata(t *testing.T) {
	// Verify critical models have metadata
	criticalModels := []string{
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-4",
		"gpt-3.5-turbo",
		"text-embedding-3-small",
		"text-embedding-3-large",
	}

	for _, modelID := range criticalModels {
		t.Run(modelID, func(t *testing.T) {
			metadata, ok := knownModels[modelID]
			if !ok {
				t.Errorf("model %s not found in knownModels", modelID)
				return
			}
			if metadata.displayName == "" {
				t.Error("display name should be set")
			}
			if len(metadata.types) == 0 {
				t.Error("types should be set")
			}
		})
	}
}

func TestClientImplementsModelLister(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object": "list", "data": []}`))
	})

	var _ llms.ModelLister = client
}

func TestListModels_WithOrganizationHeader(t *testing.T) {
	var capturedOrgHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrgHeader = r.Header.Get("OpenAI-Organization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object": "list", "data": []}`))
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
		WithOrganization("org-test123"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if capturedOrgHeader != "org-test123" {
		t.Errorf("expected org header 'org-test123', got '%s'", capturedOrgHeader)
	}
}

// helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
