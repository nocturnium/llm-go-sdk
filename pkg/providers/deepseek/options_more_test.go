package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

// TestOptionSetters exercises every WithX option setter against the options
// struct via apply, asserting each sets its expected field.
func TestOptionSetters(t *testing.T) {
	customHTTP := &http.Client{Timeout: 5 * time.Second}

	tests := []struct {
		name   string
		opt    Option
		assert func(t *testing.T, o *options)
	}{
		{
			name: "WithAPIKey",
			opt:  WithAPIKey("secret-key"),
			assert: func(t *testing.T, o *options) {
				if o.APIKey != "secret-key" {
					t.Errorf("APIKey = %q, want secret-key", o.APIKey)
				}
			},
		},
		{
			name: "WithModel",
			opt:  WithModel("deepseek-reasoner"),
			assert: func(t *testing.T, o *options) {
				if o.Model != "deepseek-reasoner" {
					t.Errorf("Model = %q, want deepseek-reasoner", o.Model)
				}
			},
		},
		{
			name: "WithBaseURL",
			opt:  WithBaseURL("https://example.test/v1"),
			assert: func(t *testing.T, o *options) {
				if o.BaseURL != "https://example.test/v1" {
					t.Errorf("BaseURL = %q, want https://example.test/v1", o.BaseURL)
				}
			},
		},
		{
			name: "WithEmbeddingModel",
			opt:  WithEmbeddingModel("custom-embed"),
			assert: func(t *testing.T, o *options) {
				if o.EmbeddingModel != "custom-embed" {
					t.Errorf("EmbeddingModel = %q, want custom-embed", o.EmbeddingModel)
				}
			},
		},
		{
			name: "WithHTTPClient",
			opt:  WithHTTPClient(customHTTP),
			assert: func(t *testing.T, o *options) {
				if o.HTTPClient != customHTTP {
					t.Errorf("HTTPClient = %v, want custom client", o.HTTPClient)
				}
			},
		},
		{
			name: "WithTimeout",
			opt:  WithTimeout(42 * time.Second),
			assert: func(t *testing.T, o *options) {
				if o.Timeout != 42*time.Second {
					t.Errorf("Timeout = %s, want 42s", o.Timeout)
				}
			},
		},
		{
			name: "WithAllowPrivateIPs",
			opt:  WithAllowPrivateIPs(),
			assert: func(t *testing.T, o *options) {
				if !o.AllowPrivateIPs {
					t.Error("AllowPrivateIPs = false, want true")
				}
			},
		},
		{
			name: "WithAllowHTTP",
			opt:  WithAllowHTTP(),
			assert: func(t *testing.T, o *options) {
				if !o.AllowHTTP {
					t.Error("AllowHTTP = false, want true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := apply(tt.opt)
			tt.assert(t, o)
		})
	}
}

// TestNewAppliesOptions verifies that options threaded through New are honored
// on the constructed client.
func TestNewAppliesOptions(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("deepseek-coder"),
		WithEmbeddingModel("embed-x"),
		WithBaseURL("https://example.test/v1"),
		WithTimeout(10*time.Second),
		WithHTTPClient(&http.Client{}),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if client.Model() != "deepseek-coder" {
		t.Errorf("Model() = %q, want deepseek-coder", client.Model())
	}
	if client.Provider() != llms.ProviderDeepSeek {
		t.Errorf("Provider() = %q, want deepseek", client.Provider())
	}
	if client.options.EmbeddingModel != "embed-x" {
		t.Errorf("EmbeddingModel = %q, want embed-x", client.options.EmbeddingModel)
	}
	if client.options.BaseURL != "https://example.test/v1" {
		t.Errorf("BaseURL = %q, want https://example.test/v1", client.options.BaseURL)
	}
	if !client.options.AllowPrivateIPs || !client.options.AllowHTTP {
		t.Error("expected AllowPrivateIPs and AllowHTTP to be true")
	}
}

// modelsHandler returns an httptest server responding to the OpenAI-compatible
// /models endpoint with a fixed model list.
func modelsHandler(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "deepseek-chat", "object": "model", "created": 1700000000},
				{"id": "deepseek-coder", "object": "model", "created": 1700000001},
				{"id": "deepseek-reasoner", "object": "model"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(serverURL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func TestListModels(t *testing.T) {
	server := modelsHandler(t)
	defer server.Close()

	client := newTestClient(t, server.URL)

	result, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(result.Models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(result.Models))
	}
	if result.HasMore {
		t.Error("expected HasMore=false")
	}

	// convertModelResponse should set provider, organization, and infer types.
	var coder llms.ModelInfo
	for _, m := range result.Models {
		if m.ID == "deepseek-coder" {
			coder = m
		}
		if m.Provider != llms.ProviderDeepSeek {
			t.Errorf("model %s provider = %q, want deepseek", m.ID, m.Provider)
		}
		if m.Organization != "DeepSeek" {
			t.Errorf("model %s organization = %q, want DeepSeek", m.ID, m.Organization)
		}
		// DisplayName falls back to ID when not supplied.
		if m.DisplayName != m.ID {
			t.Errorf("model %s DisplayName = %q, want %q", m.ID, m.DisplayName, m.ID)
		}
	}
	if coder.ID == "" {
		t.Fatal("deepseek-coder not found in results")
	}
	// coder should infer both chat and code types.
	if len(coder.Types) != 2 {
		t.Errorf("deepseek-coder types = %v, want 2 entries", coder.Types)
	}
}

func TestListModels_LimitAndTypeFilter(t *testing.T) {
	server := modelsHandler(t)
	defer server.Close()

	client := newTestClient(t, server.URL)

	t.Run("limit truncates", func(t *testing.T) {
		result, err := client.ListModels(context.Background(), llms.WithModelsLimit(2))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(result.Models) != 2 {
			t.Errorf("expected 2 models with limit, got %d", len(result.Models))
		}
	})

	t.Run("type filter keeps code models", func(t *testing.T) {
		result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeCode))
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		// Only deepseek-coder is tagged with ModelTypeCode.
		if len(result.Models) != 1 || result.Models[0].ID != "deepseek-coder" {
			t.Errorf("expected only deepseek-coder, got %+v", result.Models)
		}
	})
}

func TestListModels_ContextCanceled(t *testing.T) {
	server := modelsHandler(t)
	defer server.Close()

	client := newTestClient(t, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.ListModels(ctx); err == nil {
		t.Fatal("expected error from canceled context")
	}
}

func TestModelInfo(t *testing.T) {
	server := modelsHandler(t)
	defer server.Close()

	client := newTestClient(t, server.URL)

	t.Run("found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "deepseek-reasoner")
		if err != nil {
			t.Fatalf("ModelInfo() error = %v", err)
		}
		if info.ID != "deepseek-reasoner" {
			t.Errorf("ID = %q, want deepseek-reasoner", info.ID)
		}
		if info.Provider != llms.ProviderDeepSeek {
			t.Errorf("Provider = %q, want deepseek", info.Provider)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := client.ModelInfo(context.Background(), "does-not-exist")
		if err == nil {
			t.Fatal("expected error for unknown model")
		}
	})
}

// TestRegisteredFactory exercises the init-registered provider factory in
// register.go via the global registry, covering its config-to-option mapping.
func TestRegisteredFactory(t *testing.T) {
	llm, err := llms.New("deepseek", llms.Config{
		APIKey:          "test-key",
		Model:           "deepseek-coder",
		BaseURL:         "https://example.test/v1",
		Timeout:         15 * time.Second,
		HTTPClient:      &http.Client{},
		AllowPrivateIPs: true,
	})
	if err != nil {
		t.Fatalf("llms.New(deepseek) error = %v", err)
	}

	client, ok := llm.(*Client)
	if !ok {
		t.Fatalf("expected *Client, got %T", llm)
	}
	if client.Provider() != llms.ProviderDeepSeek {
		t.Errorf("Provider() = %q, want deepseek", client.Provider())
	}
	if client.Model() != "deepseek-coder" {
		t.Errorf("Model() = %q, want deepseek-coder", client.Model())
	}
	if client.options.BaseURL != "https://example.test/v1" {
		t.Errorf("BaseURL = %q, want https://example.test/v1", client.options.BaseURL)
	}
	if client.options.Timeout != 15*time.Second {
		t.Errorf("Timeout = %s, want 15s", client.options.Timeout)
	}
	if !client.options.AllowPrivateIPs || !client.options.AllowHTTP {
		t.Error("expected AllowPrivateIPs and AllowHTTP true from config")
	}
}
