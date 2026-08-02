package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestWithTimeout(t *testing.T) {
	opts := apply(WithTimeout(45 * time.Second))

	if opts.Timeout != 45*time.Second {
		t.Errorf("expected timeout 45s, got %s", opts.Timeout)
	}
}

func TestWithAllowOptions(t *testing.T) {
	opts := apply(
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)

	if !opts.AllowPrivateIPs {
		t.Error("expected AllowPrivateIPs to be true")
	}
	if !opts.AllowHTTP {
		t.Error("expected AllowHTTP to be true")
	}
}

func TestWithHTTPClientOption(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}
	opts := apply(WithHTTPClient(custom))

	if opts.HTTPClient != custom {
		t.Error("expected HTTPClient to be the custom client")
	}
}

// TestRegisteredFactory exercises the init() registration path via the root
// llms.New constructor, covering the Config -> Option translation in register.go.
func TestRegisteredFactory(t *testing.T) {
	llm, err := llms.New("fireworks", llms.Config{
		APIKey:          "test-key",
		Model:           "accounts/fireworks/models/qwen2p5-72b-instruct",
		BaseURL:         "https://custom.fireworks.ai/v1",
		Timeout:         30 * time.Second,
		HTTPClient:      &http.Client{},
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client, ok := llm.(*Client)
	if !ok {
		t.Fatalf("expected *Client, got %T", llm)
	}

	if client.Provider() != llms.ProviderFireworks {
		t.Errorf("expected provider fireworks, got %s", client.Provider())
	}
	if client.Model() != "accounts/fireworks/models/qwen2p5-72b-instruct" {
		t.Errorf("unexpected model: %s", client.Model())
	}
	if client.options.BaseURL != "https://custom.fireworks.ai/v1" {
		t.Errorf("unexpected base URL: %s", client.options.BaseURL)
	}
	if client.options.Timeout != 30*time.Second {
		t.Errorf("unexpected timeout: %s", client.options.Timeout)
	}
	if !client.options.AllowPrivateIPs {
		t.Error("expected AllowPrivateIPs to be true")
	}
	if !client.options.AllowHTTP {
		t.Error("expected AllowHTTP to be true (set alongside AllowPrivateIPs)")
	}
}

// newModelsTestClient returns a Client wired to an httptest server that serves
// an OpenAI-compatible /models response.
func newModelsTestClient(t *testing.T) *Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("expected /models, got %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"id":      "accounts/fireworks/models/llama-v3p1-70b-instruct",
					"object":  "model",
					"created": int64(1677610602),
				},
				{
					"id":             "accounts/fireworks/models/llama-v3p2-11b-vision-instruct",
					"object":         "model",
					"display_name":   "Llama 3.2 Vision",
					"context_length": 131072,
					"organization":   "Meta",
					"created":        int64(1677610602),
				},
				{
					"id":      "nomic-ai/nomic-embed-text-v1.5",
					"object":  "model",
					"created": int64(0),
				},
				{
					"id":      "accounts/fireworks/models/qwen2p5-coder-32b-instruct",
					"object":  "model",
					"created": int64(1677610602),
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	return client
}

func TestListModels(t *testing.T) {
	client := newModelsTestClient(t)

	result, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}

	if len(result.Models) != 4 {
		t.Fatalf("expected 4 models, got %d", len(result.Models))
	}
	if result.HasMore {
		t.Error("expected HasMore to be false")
	}

	// First model: display name derived from the path tail, org inferred as Meta.
	first := result.Models[0]
	if first.ID != "accounts/fireworks/models/llama-v3p1-70b-instruct" {
		t.Errorf("unexpected first model ID: %s", first.ID)
	}
	if first.DisplayName != "llama-v3p1-70b-instruct" {
		t.Errorf("expected display name derived from path, got %s", first.DisplayName)
	}
	if first.Organization != "Meta" {
		t.Errorf("expected organization Meta, got %s", first.Organization)
	}
	if first.Provider != llms.ProviderFireworks {
		t.Errorf("expected provider fireworks, got %s", first.Provider)
	}
	if first.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set for non-zero created timestamp")
	}

	// Second model: explicit display name and context length preserved.
	second := result.Models[1]
	if second.DisplayName != "Llama 3.2 Vision" {
		t.Errorf("expected explicit display name, got %s", second.DisplayName)
	}
	if second.ContextLength != 131072 {
		t.Errorf("expected context length 131072, got %d", second.ContextLength)
	}
}

func TestListModelsFilterByType(t *testing.T) {
	client := newModelsTestClient(t)

	result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeEmbedding))
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}

	if len(result.Models) != 1 {
		t.Fatalf("expected 1 embedding model, got %d", len(result.Models))
	}
	if result.Models[0].ID != "nomic-ai/nomic-embed-text-v1.5" {
		t.Errorf("unexpected embedding model: %s", result.Models[0].ID)
	}
}

func TestListModelsLimit(t *testing.T) {
	client := newModelsTestClient(t)

	result, err := client.ListModels(context.Background(), llms.WithModelLimit(2))
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}

	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models after limit, got %d", len(result.Models))
	}
}

func TestListModelsContextCancelled(t *testing.T) {
	client := newModelsTestClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ListModels(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestModelInfo(t *testing.T) {
	client := newModelsTestClient(t)

	info, err := client.ModelInfo(context.Background(), "accounts/fireworks/models/qwen2p5-coder-32b-instruct")
	if err != nil {
		t.Fatalf("ModelInfo returned error: %v", err)
	}

	if info.ID != "accounts/fireworks/models/qwen2p5-coder-32b-instruct" {
		t.Errorf("unexpected model ID: %s", info.ID)
	}
	if info.Organization != "Alibaba" {
		t.Errorf("expected organization Alibaba, got %s", info.Organization)
	}
}

func TestModelInfoNotFound(t *testing.T) {
	client := newModelsTestClient(t)

	_, err := client.ModelInfo(context.Background(), "accounts/fireworks/models/does-not-exist")
	if !errors.Is(err, llms.ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound, got %v", err)
	}
}

func TestInferModelTypes(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		expected []llms.ModelType
	}{
		{
			name:     "embedding",
			modelID:  "nomic-ai/nomic-embed-text-v1.5",
			expected: []llms.ModelType{llms.ModelTypeEmbedding},
		},
		{
			name:     "vision",
			modelID:  "accounts/fireworks/models/llama-v3p2-11b-vision-instruct",
			expected: []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		},
		{
			name:     "code",
			modelID:  "accounts/fireworks/models/qwen2p5-coder-32b-instruct",
			expected: []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		},
		{
			name:     "chat default",
			modelID:  "accounts/fireworks/models/llama-v3p1-70b-instruct",
			expected: []llms.ModelType{llms.ModelTypeChat},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferModelTypes(tt.modelID)
			if len(got) != len(tt.expected) {
				t.Fatalf("inferModelTypes(%s) = %v, want %v", tt.modelID, got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("inferModelTypes(%s)[%d] = %v, want %v", tt.modelID, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestInferOrganizationDefaults(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"accounts/fireworks/models/gemma2-9b-it", "Google"},
		{"accounts/fireworks/models/some-unknown-model", "Fireworks AI"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			if got := inferOrganization(tt.modelID); got != tt.expected {
				t.Errorf("inferOrganization(%s) = %s, want %s", tt.modelID, got, tt.expected)
			}
		})
	}
}
