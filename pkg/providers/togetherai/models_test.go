package togetherai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/openaicompat"
)

// Mock response data matching TogetherAI API format
var mockModelsResponse = `{
	"object": "list",
	"data": [
		{
			"id": "meta-llama/Llama-3.3-70B-Instruct-Turbo",
			"object": "model",
			"created": 1733011200,
			"type": "chat",
			"display_name": "Llama 3.3 70B Instruct Turbo",
			"organization": "Meta",
			"context_length": 131072,
			"pricing": {
				"input": 0.88,
				"output": 0.88
			}
		},
		{
			"id": "mistralai/Mixtral-8x7B-Instruct-v0.1",
			"object": "model",
			"created": 1702339200,
			"type": "chat",
			"display_name": "Mixtral 8x7B Instruct",
			"organization": "Mistral AI",
			"context_length": 32768,
			"pricing": {
				"input": 0.60,
				"output": 0.60
			}
		},
		{
			"id": "togethercomputer/m2-bert-80M-8k-retrieval",
			"object": "model",
			"created": 1698883200,
			"type": "embedding",
			"display_name": "M2 BERT 80M 8K Retrieval",
			"organization": "Together",
			"context_length": 8192
		},
		{
			"id": "Salesforce/Llama-Rank-V1",
			"object": "model",
			"created": 1709251200,
			"type": "rerank",
			"display_name": "Llama Rank V1",
			"organization": "Salesforce",
			"context_length": 4096
		},
		{
			"id": "stabilityai/stable-diffusion-xl-base-1.0",
			"object": "model",
			"created": 1690848000,
			"type": "image",
			"display_name": "Stable Diffusion XL Base 1.0",
			"organization": "Stability AI"
		}
	]
}`

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

	if len(result.Models) != 5 {
		t.Errorf("expected 5 models, got %d", len(result.Models))
	}

	// Verify first model
	llama := result.Models[0]
	if llama.ID != "meta-llama/Llama-3.3-70B-Instruct-Turbo" {
		t.Errorf("expected meta-llama/Llama-3.3-70B-Instruct-Turbo, got %s", llama.ID)
	}
	if llama.DisplayName != "Llama 3.3 70B Instruct Turbo" {
		t.Errorf("expected 'Llama 3.3 70B Instruct Turbo', got %s", llama.DisplayName)
	}
	if llama.Provider != llms.ProviderTogetherAI {
		t.Errorf("expected ProviderTogetherAI, got %s", llama.Provider)
	}
	if llama.ContextLength != 131072 {
		t.Errorf("expected context length 131072, got %d", llama.ContextLength)
	}
	if !llama.HasType(llms.ModelTypeChat) {
		t.Error("expected model to have chat type")
	}
	if llama.Pricing == nil {
		t.Error("expected pricing to be set")
	} else if llama.Pricing.Input != 0.88 {
		t.Errorf("expected input price 0.88, got %f", llama.Pricing.Input)
	}

	// Verify HasMore is false (TogetherAI doesn't paginate)
	if result.HasMore {
		t.Error("expected HasMore to be false")
	}
}

func TestListModels_WithTypeFilter(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	// Filter to embedding models only
	result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeEmbedding))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 1 {
		t.Errorf("expected 1 embedding model, got %d", len(result.Models))
	}
	if result.Models[0].ID != "togethercomputer/m2-bert-80M-8k-retrieval" {
		t.Errorf("expected embedding model, got %s", result.Models[0].ID)
	}
}

func TestListModels_WithDedicatedFilter(t *testing.T) {
	var capturedURL string

	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object": "list", "data": []}`))
	})

	_, err := client.ListModels(context.Background(), llms.WithDedicatedModels())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if capturedURL != "/models?dedicated=true" {
		t.Errorf("expected dedicated query param, got URL: %s", capturedURL)
	}
}

func TestListModels_WithLimit(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	result, err := client.ListModels(context.Background(), llms.WithModelsLimit(2))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 2 {
		t.Errorf("expected 2 models (limited), got %d", len(result.Models))
	}
}

func TestModelInfo(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	t.Run("found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "meta-llama/Llama-3.3-70B-Instruct-Turbo")
		if err != nil {
			t.Fatalf("ModelInfo failed: %v", err)
		}
		if info == nil {
			t.Fatal("expected model info, got nil")
		}
		if info.ID != "meta-llama/Llama-3.3-70B-Instruct-Turbo" {
			t.Errorf("expected correct model ID, got %s", info.ID)
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
	if !contains(errStr, "togetherai") {
		t.Errorf("expected error to contain provider name, got: %s", errStr)
	}
}

func TestConvertModelType(t *testing.T) {
	tests := []struct {
		input    string
		expected []llms.ModelType
	}{
		{"chat", []llms.ModelType{llms.ModelTypeChat}},
		{"Chat", []llms.ModelType{llms.ModelTypeChat}},
		{"CHAT", []llms.ModelType{llms.ModelTypeChat}},
		{"language", []llms.ModelType{llms.ModelTypeCompletion}},
		{"code", []llms.ModelType{llms.ModelTypeCode, llms.ModelTypeChat}},
		{"image", []llms.ModelType{llms.ModelTypeImage}},
		{"embedding", []llms.ModelType{llms.ModelTypeEmbedding}},
		{"moderation", []llms.ModelType{llms.ModelTypeModeration}},
		{"rerank", []llms.ModelType{llms.ModelTypeRerank}},
		{"unknown", []llms.ModelType{llms.ModelTypeChat}}, // defaults to chat
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := convertModelType(tt.input)
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

func TestConvertModelResponse(t *testing.T) {
	t.Run("full model", func(t *testing.T) {
		resp := &openaicompat.ModelResponse{
			ID:            "meta-llama/Llama-3.3-70B-Instruct",
			Object:        "model",
			Created:       1733011200,
			Type:          "chat",
			DisplayName:   "Llama 3.3 70B",
			Organization:  "Meta",
			ContextLength: 131072,
			License:       "llama3.3",
			Pricing: &openaicompat.ModelPricing{
				Input:    0.88,
				Output:   0.89,
				Hourly:   1.23,
				Finetune: 2.34,
				Base:     3.45,
			},
		}

		info := convertModelResponse(resp)

		if info.ID != resp.ID {
			t.Errorf("ID mismatch: %s != %s", info.ID, resp.ID)
		}
		if info.DisplayName != resp.DisplayName {
			t.Errorf("DisplayName mismatch: %s != %s", info.DisplayName, resp.DisplayName)
		}
		if info.Organization != resp.Organization {
			t.Errorf("Organization mismatch: %s != %s", info.Organization, resp.Organization)
		}
		if info.ContextLength != resp.ContextLength {
			t.Errorf("ContextLength mismatch: %d != %d", info.ContextLength, resp.ContextLength)
		}
		if info.License != resp.License {
			t.Errorf("License mismatch: %s != %s", info.License, resp.License)
		}
		if info.Pricing == nil {
			t.Error("expected pricing to be set")
		} else {
			if info.Pricing != resp.Pricing {
				t.Error("expected pricing pointer to pass through")
			}
			if info.Pricing.Input != 0.88 {
				t.Errorf("expected input price 0.88, got %f", info.Pricing.Input)
			}
			if info.Pricing.Output != 0.89 {
				t.Errorf("expected output price 0.89, got %f", info.Pricing.Output)
			}
			if info.Pricing.Hourly != 1.23 {
				t.Errorf("expected hourly price 1.23, got %f", info.Pricing.Hourly)
			}
			if info.Pricing.Finetune != 2.34 {
				t.Errorf("expected finetune price 2.34, got %f", info.Pricing.Finetune)
			}
			if info.Pricing.Base != 3.45 {
				t.Errorf("expected base price 3.45, got %f", info.Pricing.Base)
			}
		}
		if info.FromCache {
			t.Error("expected FromCache to be false")
		}
		if info.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set")
		}
	})

	t.Run("minimal model", func(t *testing.T) {
		resp := &openaicompat.ModelResponse{
			ID:   "some-org/some-model",
			Type: "chat",
		}

		info := convertModelResponse(resp)

		if info.ID != "some-org/some-model" {
			t.Errorf("ID mismatch: %s", info.ID)
		}
		// Should extract display name from ID
		if info.DisplayName != "some-model" {
			t.Errorf("expected display name 'some-model', got %s", info.DisplayName)
		}
		// Should extract organization from ID
		if info.Organization != "some-org" {
			t.Errorf("expected organization 'some-org', got %s", info.Organization)
		}
		if info.Pricing != nil {
			t.Error("expected pricing to be nil")
		}
	})

	t.Run("model without slash in ID", func(t *testing.T) {
		resp := &openaicompat.ModelResponse{
			ID:   "simple-model",
			Type: "embedding",
		}

		info := convertModelResponse(resp)

		if info.DisplayName != "simple-model" {
			t.Errorf("expected display name 'simple-model', got %s", info.DisplayName)
		}
		if info.Organization != "" {
			t.Errorf("expected empty organization, got %s", info.Organization)
		}
	})
}

func TestClientImplementsModelLister(t *testing.T) {
	// This is a compile-time check, but we can also verify at runtime
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object": "list", "data": []}`))
	})

	var _ llms.ModelLister = client
}

// helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Test that we can parse real-world TogetherAI response format
func TestParseRealWorldResponse(t *testing.T) {
	// This is a sample from actual TogetherAI API docs
	realResponse := `{
		"object": "list",
		"data": [
			{
				"id": "Austism/chronos-hermes-13b",
				"object": "model",
				"created": 1699394170,
				"type": "chat",
				"display_name": "Chronos Hermes 13B",
				"organization": "Austism",
				"link": "https://huggingface.co/Austism/chronos-hermes-13b",
				"license": "llama2",
				"context_length": 4096,
				"pricing": {
					"hourly": 0,
					"input": 0.2,
					"output": 0.2,
					"base": 0,
					"finetune": 0
				}
			}
		]
	}`

	var resp openaicompat.ModelsListResponse
	if err := json.Unmarshal([]byte(realResponse), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Data))
	}

	m := resp.Data[0]
	if m.ID != "Austism/chronos-hermes-13b" {
		t.Errorf("unexpected ID: %s", m.ID)
	}
	if m.ContextLength != 4096 {
		t.Errorf("unexpected context length: %d", m.ContextLength)
	}
	if m.Pricing == nil {
		t.Fatal("expected pricing")
	}
	if m.Pricing.Input != 0.2 {
		t.Errorf("unexpected input price: %f", m.Pricing.Input)
	}

	// Convert and verify
	info := convertModelResponse(&m)
	if info.Provider != llms.ProviderTogetherAI {
		t.Errorf("expected ProviderTogetherAI, got %s", info.Provider)
	}
	if !info.HasType(llms.ModelTypeChat) {
		t.Error("expected chat type")
	}
}
