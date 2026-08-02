package azure

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// roundTripFunc lets a test rewrite outbound requests to a local httptest server
// while keeping the client's offline (no real network) guarantees.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestWithEmbeddingDeployment(t *testing.T) {
	opts := apply(WithEmbeddingDeployment("text-embedding-ada-002"))

	if opts.EmbeddingDeployment != "text-embedding-ada-002" {
		t.Errorf("EmbeddingDeployment = %q, want text-embedding-ada-002", opts.EmbeddingDeployment)
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}

	opts := apply(WithHTTPClient(custom))

	if opts.HTTPClient != custom {
		t.Error("WithHTTPClient did not set the custom *http.Client")
	}
}

func TestWithTimeout(t *testing.T) {
	opts := apply(WithTimeout(42 * time.Second))

	if opts.Timeout != 42*time.Second {
		t.Errorf("Timeout = %v, want 42s", opts.Timeout)
	}
}

func TestWithAllowPrivateIPs(t *testing.T) {
	opts := apply(WithAllowPrivateIPs())

	if !opts.AllowPrivateIPs {
		t.Error("WithAllowPrivateIPs did not set AllowPrivateIPs to true")
	}
}

func TestWithAllowHTTP(t *testing.T) {
	opts := apply(WithAllowHTTP())

	if !opts.AllowHTTP {
		t.Error("WithAllowHTTP did not set AllowHTTP to true")
	}
}

// TestNewAppliesTransportOptions verifies the construction options that feed the
// underlying openaicompat client are threaded through New without error.
func TestNewAppliesTransportOptions(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithEndpoint("https://myresource.openai.azure.com"),
		WithDeployment("gpt-4"),
		WithEmbeddingDeployment("text-embedding-3-small"),
		WithTimeout(10*time.Second),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
		WithHTTPClient(&http.Client{}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.options.EmbeddingDeployment != "text-embedding-3-small" {
		t.Errorf("EmbeddingDeployment = %q, want text-embedding-3-small", client.options.EmbeddingDeployment)
	}
	if !client.options.AllowPrivateIPs || !client.options.AllowHTTP {
		t.Error("expected AllowPrivateIPs and AllowHTTP to be set on the client options")
	}
}

// newTestClient builds an azure client whose live ListModels call is routed to a
// local httptest server via a custom transport, keeping the test offline.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()

	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = parsed.Scheme
			req.URL.Host = parsed.Host
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	client, err := New(
		WithAPIKey("test-key"),
		WithEndpoint("https://myresource.openai.azure.com"),
		WithDeployment("gpt-4"),
		WithHTTPClient(httpClient),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return client
}

func TestListModels_FromAPIResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(openaicompat.ModelsListResponse{
			Object: "list",
			Data: []openaicompat.ModelResponse{
				{ID: "gpt-4", Created: 1700000000, ContextLength: 128000},
				{ID: "gpt-35-turbo", DisplayName: "GPT-3.5 Turbo"},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)

	result, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result.Models))
	}

	first := result.Models[0]
	if first.ID != "gpt-4" {
		t.Errorf("model[0].ID = %q, want gpt-4", first.ID)
	}
	if first.DisplayName != "gpt-4" {
		t.Errorf("model[0].DisplayName = %q, want gpt-4 (defaulted from ID)", first.DisplayName)
	}
	if first.Provider != llms.ProviderAzure {
		t.Errorf("model[0].Provider = %q, want %q", first.Provider, llms.ProviderAzure)
	}
	if first.Organization != "Microsoft Azure" {
		t.Errorf("model[0].Organization = %q, want Microsoft Azure", first.Organization)
	}
	if first.ContextLength != 128000 {
		t.Errorf("model[0].ContextLength = %d, want 128000", first.ContextLength)
	}
	if first.CreatedAt != time.Unix(1700000000, 0) {
		t.Errorf("model[0].CreatedAt = %v, want %v", first.CreatedAt, time.Unix(1700000000, 0))
	}
	if len(first.Types) != 1 || first.Types[0] != llms.ModelTypeChat {
		t.Errorf("model[0].Types = %v, want [chat]", first.Types)
	}

	// DisplayName is preserved when supplied by the API.
	if result.Models[1].DisplayName != "GPT-3.5 Turbo" {
		t.Errorf("model[1].DisplayName = %q, want GPT-3.5 Turbo", result.Models[1].DisplayName)
	}
}

func TestListModels_LimitAndTypeFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(openaicompat.ModelsListResponse{
			Object: "list",
			Data: []openaicompat.ModelResponse{
				{ID: "gpt-4"},
				{ID: "gpt-35-turbo"},
				{ID: "gpt-4o"},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)

	// Limit caps the returned slice.
	result, err := client.ListModels(context.Background(), llms.WithModelLimit(2))
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("expected limit to cap models to 2, got %d", len(result.Models))
	}

	// Type filter keeps chat models (all converted models are chat).
	filtered, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeChat))
	if err != nil {
		t.Fatalf("ListModels() with type filter error = %v", err)
	}
	if len(filtered.Models) != 3 {
		t.Errorf("expected 3 chat models, got %d", len(filtered.Models))
	}
	if result.HasMore {
		t.Error("HasMore should be false")
	}
}

func TestListModels_FallbackToCurrentDeployment(t *testing.T) {
	// Server returns an error so the live ListModels call fails and the client
	// falls back to reporting the current deployment as the single model.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "no such endpoint", "type": "invalid_request_error"},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)

	result, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(result.Models) != 1 {
		t.Fatalf("expected 1 fallback model, got %d", len(result.Models))
	}
	model := result.Models[0]
	if model.ID != "gpt-4" {
		t.Errorf("fallback model.ID = %q, want gpt-4 (deployment name)", model.ID)
	}
	if model.DisplayName != "gpt-4" {
		t.Errorf("fallback model.DisplayName = %q, want gpt-4", model.DisplayName)
	}
	if model.Provider != llms.ProviderAzure {
		t.Errorf("fallback model.Provider = %q, want %q", model.Provider, llms.ProviderAzure)
	}
	if model.Organization != "Microsoft Azure" {
		t.Errorf("fallback model.Organization = %q, want Microsoft Azure", model.Organization)
	}
}

func TestListModels_FallbackHonorsTypeFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)

	// The fallback deployment is a chat model, so filtering by embedding yields none.
	result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeEmbedding))
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(result.Models) != 0 {
		t.Errorf("expected 0 models after embedding filter on chat-only fallback, got %d", len(result.Models))
	}
}

func TestListModels_ContextCancelled(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ListModels(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestModelInfo_Found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(openaicompat.ModelsListResponse{
			Object: "list",
			Data: []openaicompat.ModelResponse{
				{ID: "gpt-4"},
				{ID: "gpt-4o"},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)

	info, err := client.ModelInfo(context.Background(), "gpt-4o")
	if err != nil {
		t.Fatalf("ModelInfo() error = %v", err)
	}
	if info.ID != "gpt-4o" {
		t.Errorf("ModelInfo().ID = %q, want gpt-4o", info.ID)
	}
}

func TestModelInfo_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(openaicompat.ModelsListResponse{
			Object: "list",
			Data:   []openaicompat.ModelResponse{{ID: "gpt-4"}},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)

	_, err := client.ModelInfo(context.Background(), "does-not-exist")
	if !errors.Is(err, llms.ErrModelNotFound) {
		t.Errorf("expected ErrModelNotFound, got %v", err)
	}
}

// TestRegisterProviderFactory exercises the init()-registered factory via the
// public registry, covering the option-mapping branches in register.go.
func TestRegisterProviderFactory(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	t.Setenv("AZURE_OPENAI_KEY", "")

	llm, err := llms.New("azure", llms.Config{
		APIKey:          "test-key",
		Model:           "gpt-4",
		BaseURL:         "https://myresource.openai.azure.com",
		Timeout:         15 * time.Second,
		HTTPClient:      &http.Client{},
		AllowPrivateIPs: true,
	})
	if err != nil {
		t.Fatalf("llms.New(azure) error = %v", err)
	}

	if llm.Provider() != llms.ProviderAzure {
		t.Errorf("Provider() = %q, want %q", llm.Provider(), llms.ProviderAzure)
	}
}
