package groq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

// TestWithEmbeddingModel verifies WithEmbeddingModel sets the embedding model field.
func TestWithEmbeddingModel(t *testing.T) {
	opts := apply(WithEmbeddingModel("nomic-embed-text-v1.5"))
	if opts.EmbeddingModel != "nomic-embed-text-v1.5" {
		t.Errorf("expected embedding model nomic-embed-text-v1.5, got %s", opts.EmbeddingModel)
	}
}

// TestWithHTTPClient verifies WithHTTPClient sets a custom HTTP client.
func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}
	opts := apply(WithHTTPClient(custom))
	if opts.HTTPClient != custom {
		t.Error("expected HTTPClient to be the custom client")
	}
}

// TestWithTimeout verifies WithTimeout sets the timeout field.
func TestWithTimeout(t *testing.T) {
	opts := apply(WithTimeout(42 * time.Second))
	if opts.Timeout != 42*time.Second {
		t.Errorf("expected timeout 42s, got %s", opts.Timeout)
	}
}

// TestWithAllowPrivateIPs verifies WithAllowPrivateIPs flips the field on.
func TestWithAllowPrivateIPs(t *testing.T) {
	opts := apply()
	if opts.AllowPrivateIPs {
		t.Error("expected AllowPrivateIPs to default to false")
	}

	opts = apply(WithAllowPrivateIPs())
	if !opts.AllowPrivateIPs {
		t.Error("expected AllowPrivateIPs to be true")
	}
}

// TestWithAllowHTTP verifies WithAllowHTTP flips the field on.
func TestWithAllowHTTP(t *testing.T) {
	opts := apply()
	if opts.AllowHTTP {
		t.Error("expected AllowHTTP to default to false")
	}

	opts = apply(WithAllowHTTP())
	if !opts.AllowHTTP {
		t.Error("expected AllowHTTP to be true")
	}
}

// TestNewWithAllOptions verifies that all options thread through New into the
// resolved options struct.
func TestNewWithAllOptions(t *testing.T) {
	custom := &http.Client{Timeout: 3 * time.Second}

	client, err := New(
		WithAPIKey("test-key"),
		WithModel("llama-3.1-8b-instant"),
		WithEmbeddingModel("embed-model"),
		WithBaseURL("https://custom.groq.com/v1"),
		WithHTTPClient(custom),
		WithTimeout(11*time.Second),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.options.EmbeddingModel != "embed-model" {
		t.Errorf("expected embedding model embed-model, got %s", client.options.EmbeddingModel)
	}
	if client.options.HTTPClient != custom {
		t.Error("expected custom HTTP client to thread through")
	}
	if client.options.Timeout != 11*time.Second {
		t.Errorf("expected timeout 11s, got %s", client.options.Timeout)
	}
	if !client.options.AllowPrivateIPs {
		t.Error("expected AllowPrivateIPs to be true")
	}
	if !client.options.AllowHTTP {
		t.Error("expected AllowHTTP to be true")
	}
	if client.Model() != "llama-3.1-8b-instant" {
		t.Errorf("expected model llama-3.1-8b-instant, got %s", client.Model())
	}
}

// newModelsServer returns an httptest server that responds to the /models
// endpoint with an OpenAI-format model list.
func newModelsServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"id":       "llama-3.3-70b-versatile",
					"object":   "model",
					"created":  int64(1700000000),
					"owned_by": "Meta",
				},
				{
					"id":      "whisper-large-v3",
					"object":  "model",
					"created": int64(1700000001),
				},
				{
					"id":      "llama-3.2-90b-vision-preview",
					"object":  "model",
					"created": int64(0),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// newGroqTestClient builds a client pointed at the given test server URL.
func newGroqTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(baseURL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

// TestListModels exercises ListModels and convertModelResponse against a mock
// /models endpoint, verifying the unified ModelInfo conversion.
func TestListModels(t *testing.T) {
	server := newModelsServer(t)
	defer server.Close()

	client := newGroqTestClient(t, server.URL)

	result, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(result.Models))
	}
	if result.HasMore {
		t.Error("expected HasMore to be false (Groq does not paginate)")
	}

	// First model: Meta llama, chat type, display name falls back to ID.
	m := result.Models[0]
	if m.ID != "llama-3.3-70b-versatile" {
		t.Errorf("expected first model ID llama-3.3-70b-versatile, got %s", m.ID)
	}
	if m.Provider != llms.ProviderGroq {
		t.Errorf("expected provider groq, got %s", m.Provider)
	}
	if m.DisplayName != "llama-3.3-70b-versatile" {
		t.Errorf("expected display name to fall back to ID, got %s", m.DisplayName)
	}
	if m.Organization != "Meta" {
		t.Errorf("expected organization Meta, got %s", m.Organization)
	}
	if len(m.Types) != 1 || m.Types[0] != llms.ModelTypeChat {
		t.Errorf("expected chat type, got %v", m.Types)
	}
	if m.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt for created>0")
	}

	// Third model has created==0, so CreatedAt must stay zero.
	if !result.Models[2].CreatedAt.IsZero() {
		t.Error("expected zero CreatedAt when created timestamp is 0")
	}
}

// TestListModelsWithTypeFilter verifies the type filter option narrows results.
func TestListModelsWithTypeFilter(t *testing.T) {
	server := newModelsServer(t)
	defer server.Close()

	client := newGroqTestClient(t, server.URL)

	result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeAudio))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 1 {
		t.Fatalf("expected 1 audio model, got %d", len(result.Models))
	}
	if result.Models[0].ID != "whisper-large-v3" {
		t.Errorf("expected whisper-large-v3, got %s", result.Models[0].ID)
	}
}

// TestListModelsWithLimit verifies the limit option truncates results.
func TestListModelsWithLimit(t *testing.T) {
	server := newModelsServer(t)
	defer server.Close()

	client := newGroqTestClient(t, server.URL)

	result, err := client.ListModels(context.Background(), llms.WithModelLimit(2))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models after limit, got %d", len(result.Models))
	}
}

// TestListModelsContextCancelled verifies early cancellation returns the
// context error before any request is made.
func TestListModelsContextCancelled(t *testing.T) {
	server := newModelsServer(t)
	defer server.Close()

	client := newGroqTestClient(t, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ListModels(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestModelInfoFound verifies ModelInfo returns the matching model.
func TestModelInfoFound(t *testing.T) {
	server := newModelsServer(t)
	defer server.Close()

	client := newGroqTestClient(t, server.URL)

	info, err := client.ModelInfo(context.Background(), "whisper-large-v3")
	if err != nil {
		t.Fatalf("ModelInfo failed: %v", err)
	}
	if info.ID != "whisper-large-v3" {
		t.Errorf("expected whisper-large-v3, got %s", info.ID)
	}
	if info.Organization != "OpenAI" {
		t.Errorf("expected organization OpenAI, got %s", info.Organization)
	}
}

// TestModelInfoNotFound verifies ModelInfo returns ErrModelNotFound for an
// unknown model ID.
func TestModelInfoNotFound(t *testing.T) {
	server := newModelsServer(t)
	defer server.Close()

	client := newGroqTestClient(t, server.URL)

	_, err := client.ModelInfo(context.Background(), "does-not-exist")
	if !errors.Is(err, llms.ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound, got %v", err)
	}
}

// TestRegistryFactory exercises the init-registered factory via llms.New,
// covering the provider's register.go option-mapping path.
func TestRegistryFactory(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")

	llm, err := llms.New("groq", llms.Config{
		APIKey:          "test-key",
		Model:           "llama-3.1-8b-instant",
		BaseURL:         "https://custom.groq.com/v1",
		Timeout:         9 * time.Second,
		HTTPClient:      &http.Client{},
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})
	if err != nil {
		t.Fatalf("llms.New(groq) failed: %v", err)
	}

	client, ok := llm.(*Client)
	if !ok {
		t.Fatalf("expected *Client, got %T", llm)
	}

	if client.Provider() != llms.ProviderGroq {
		t.Errorf("expected provider groq, got %s", client.Provider())
	}
	if client.Model() != "llama-3.1-8b-instant" {
		t.Errorf("expected model llama-3.1-8b-instant, got %s", client.Model())
	}
	if client.options.BaseURL != "https://custom.groq.com/v1" {
		t.Errorf("expected base URL to thread through, got %s", client.options.BaseURL)
	}
	if client.options.Timeout != 9*time.Second {
		t.Errorf("expected timeout 9s, got %s", client.options.Timeout)
	}
	if !client.options.AllowPrivateIPs {
		t.Error("expected AllowPrivateIPs to be true via factory")
	}
	if !client.options.AllowHTTP {
		t.Error("expected AllowHTTP to be enabled alongside AllowPrivateIPs via factory")
	}
}
