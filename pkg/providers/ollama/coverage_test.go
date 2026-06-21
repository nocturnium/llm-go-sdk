package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/ollamaapi"
)

// TestOptionSetters exercises every WithX option setter through apply() and
// asserts each one mutates the expected field on the options struct. These run
// fully offline and pin the default-relaxed SSRF behavior for the local
// provider.
func TestOptionSetters(t *testing.T) {
	httpClient := &http.Client{Timeout: 3 * time.Second}

	tests := []struct {
		name   string
		option Option
		check  func(t *testing.T, o *options)
	}{
		{
			name:   "WithAPIKey",
			option: WithAPIKey("secret"),
			check: func(t *testing.T, o *options) {
				if o.APIKey != "secret" {
					t.Errorf("APIKey = %q, want %q", o.APIKey, "secret")
				}
			},
		},
		{
			name:   "WithModel",
			option: WithModel("mistral"),
			check: func(t *testing.T, o *options) {
				if o.Model != "mistral" {
					t.Errorf("Model = %q, want %q", o.Model, "mistral")
				}
			},
		},
		{
			name:   "WithBaseURL",
			option: WithBaseURL("http://remote:11434/v1"),
			check: func(t *testing.T, o *options) {
				if o.BaseURL != "http://remote:11434/v1" {
					t.Errorf("BaseURL = %q, want %q", o.BaseURL, "http://remote:11434/v1")
				}
			},
		},
		{
			name:   "WithEmbeddingModel",
			option: WithEmbeddingModel("mxbai-embed-large"),
			check: func(t *testing.T, o *options) {
				if o.EmbeddingModel != "mxbai-embed-large" {
					t.Errorf("EmbeddingModel = %q, want %q", o.EmbeddingModel, "mxbai-embed-large")
				}
			},
		},
		{
			name:   "WithHTTPClient",
			option: WithHTTPClient(httpClient),
			check: func(t *testing.T, o *options) {
				if o.HTTPClient != httpClient {
					t.Error("HTTPClient was not set to the provided client")
				}
			},
		},
		{
			name:   "WithTimeout",
			option: WithTimeout(42 * time.Second),
			check: func(t *testing.T, o *options) {
				if o.Timeout != 42*time.Second {
					t.Errorf("Timeout = %v, want %v", o.Timeout, 42*time.Second)
				}
			},
		},
		{
			name:   "WithAllowPrivateIPs",
			option: WithAllowPrivateIPs(),
			check: func(t *testing.T, o *options) {
				if !o.AllowPrivateIPs {
					t.Error("AllowPrivateIPs = false, want true")
				}
			},
		},
		{
			name:   "WithAllowHTTP",
			option: WithAllowHTTP(),
			check: func(t *testing.T, o *options) {
				if !o.AllowHTTP {
					t.Error("AllowHTTP = false, want true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := apply(tt.option)
			tt.check(t, o)
		})
	}
}

// TestNewAppliesOptions verifies that options threaded through New() reach the
// constructed client (provider metadata + retained options struct).
func TestNewAppliesOptions(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "")
	t.Setenv("OLLAMA_API_KEY", "")

	client, err := New(
		WithModel("qwen2"),
		WithEmbeddingModel("mxbai-embed-large"),
		WithBaseURL("http://localhost:11434/v1"),
		WithAPIKey("k"),
		WithTimeout(7*time.Second),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if client.Provider() != llms.ProviderOllama {
		t.Errorf("Provider() = %s, want %s", client.Provider(), llms.ProviderOllama)
	}
	if client.options.Model != "qwen2" {
		t.Errorf("options.Model = %q, want %q", client.options.Model, "qwen2")
	}
	if client.options.EmbeddingModel != "mxbai-embed-large" {
		t.Errorf("options.EmbeddingModel = %q, want %q", client.options.EmbeddingModel, "mxbai-embed-large")
	}
	if client.options.APIKey != "k" {
		t.Errorf("options.APIKey = %q, want %q", client.options.APIKey, "k")
	}
	if client.options.Timeout != 7*time.Second {
		t.Errorf("options.Timeout = %v, want %v", client.options.Timeout, 7*time.Second)
	}
}

// TestRegisterFactory drives the init()-registered factory through the central
// registry so the provider can be constructed by name from a Config.
func TestRegisterFactory(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "")
	t.Setenv("OLLAMA_API_KEY", "")

	llm, err := llms.New("ollama", llms.Config{
		APIKey:          "cfg-key",
		Model:           "llama3.2",
		BaseURL:         "http://localhost:11434/v1",
		Timeout:         5 * time.Second,
		AllowPrivateIPs: true,
		HTTPClient:      &http.Client{},
	})
	if err != nil {
		t.Fatalf("llms.New(ollama): %v", err)
	}

	if llm.Provider() != llms.ProviderOllama {
		t.Errorf("Provider() = %s, want %s", llm.Provider(), llms.ProviderOllama)
	}

	client, ok := llm.(*Client)
	if !ok {
		t.Fatalf("factory returned %T, want *Client", llm)
	}
	if client.options.Model != "llama3.2" {
		t.Errorf("options.Model = %q, want %q", client.options.Model, "llama3.2")
	}
	if client.options.APIKey != "cfg-key" {
		t.Errorf("options.APIKey = %q, want %q", client.options.APIKey, "cfg-key")
	}
	if !client.options.AllowPrivateIPs {
		t.Error("AllowPrivateIPs should be true when cfg.AllowPrivateIPs is set")
	}
	if !client.options.AllowHTTP {
		t.Error("AllowHTTP should be true when cfg.AllowPrivateIPs is set")
	}
}

// TestRegisterFactoryMinimalConfig drives the factory with an empty Config so the
// zero-valued branches in init() (no APIKey/Model/BaseURL/etc.) are exercised.
func TestRegisterFactoryMinimalConfig(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "")
	t.Setenv("OLLAMA_API_KEY", "")

	llm, err := llms.New("ollama", llms.Config{})
	if err != nil {
		t.Fatalf("llms.New(ollama) minimal: %v", err)
	}
	if llm.Provider() != llms.ProviderOllama {
		t.Errorf("Provider() = %s, want %s", llm.Provider(), llms.ProviderOllama)
	}
}

// newModelsServer returns a test server that answers the OpenAI-compatible
// /v1/models endpoint with a fixed model list.
func newModelsServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			resp := map[string]any{
				"object": "list",
				"data": []map[string]any{
					{"id": "llama3.2", "object": "model", "created": int64(1234567890), "owned_by": "meta"},
					{"id": "nomic-embed-text", "object": "model", "created": int64(1234567891), "owned_by": "nomic"},
					{"id": "llava", "object": "model", "created": int64(1234567892), "owned_by": "ollama"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
}

// TestListModels exercises ListModels (+ convertModelResponse) and the
// limit/type filtering options.
func TestListModels(t *testing.T) {
	server := newModelsServer(t)
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	t.Run("all", func(t *testing.T) {
		result, err := client.ListModels(ctx)
		if err != nil {
			t.Fatalf("ListModels: %v", err)
		}
		if len(result.Models) != 3 {
			t.Fatalf("got %d models, want 3", len(result.Models))
		}
		// convertModelResponse should infer org + creation time + display name.
		var llama llms.ModelInfo
		for _, m := range result.Models {
			if m.ID == "llama3.2" {
				llama = m
			}
		}
		if llama.Organization != "Meta" {
			t.Errorf("Organization = %q, want Meta", llama.Organization)
		}
		if llama.DisplayName != "llama3.2" {
			t.Errorf("DisplayName = %q, want llama3.2 (fallback to ID)", llama.DisplayName)
		}
		if llama.Provider != llms.ProviderOllama {
			t.Errorf("Provider = %s, want %s", llama.Provider, llms.ProviderOllama)
		}
		if llama.CreatedAt.IsZero() {
			t.Error("CreatedAt should be populated from created timestamp")
		}
	})

	t.Run("limit", func(t *testing.T) {
		result, err := client.ListModels(ctx, llms.WithModelsLimit(1))
		if err != nil {
			t.Fatalf("ListModels limit: %v", err)
		}
		if len(result.Models) != 1 {
			t.Errorf("got %d models, want 1 (limited)", len(result.Models))
		}
	})

	t.Run("filter-by-type", func(t *testing.T) {
		result, err := client.ListModels(ctx, llms.WithModelTypes(llms.ModelTypeEmbedding))
		if err != nil {
			t.Fatalf("ListModels type filter: %v", err)
		}
		for _, m := range result.Models {
			if m.ID != "nomic-embed-text" {
				t.Errorf("type filter returned non-embedding model %q", m.ID)
			}
		}
	})
}

// TestListModelsContextCanceled checks the early-return guard for a canceled ctx.
func TestListModelsContextCanceled(t *testing.T) {
	server := newModelsServer(t)
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.ListModels(ctx); err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// TestModelInfo covers both the found and not-found paths of ModelInfo.
func TestModelInfo(t *testing.T) {
	server := newModelsServer(t)
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		info, err := client.ModelInfo(ctx, "llava")
		if err != nil {
			t.Fatalf("ModelInfo: %v", err)
		}
		if info.ID != "llava" {
			t.Errorf("ID = %q, want llava", info.ID)
		}
	})

	t.Run("not-found", func(t *testing.T) {
		_, err := client.ModelInfo(ctx, "does-not-exist")
		if err == nil {
			t.Fatal("expected error for unknown model")
		}
	})
}

// TestManagementEndpoints exercises the native model-management wrappers
// (Version, ListLocalModels, ShowModel, ListRunningModels, DeleteModel,
// CopyModel) against a single mux mirroring the integration-test fixtures.
func TestManagementEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/version":
			_ = json.NewEncoder(w).Encode(ollamaapi.VersionResponse{Version: "0.6.0"})
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(ollamaapi.ListModelsResponse{
				Models: []ollamaapi.Model{{
					Name:       "llama3.2:latest",
					Model:      "llama3.2:latest",
					ModifiedAt: time.Now(),
					Size:       4_000_000_000,
					Digest:     "sha256:abc123",
					Details: &ollamaapi.ModelDetails{
						Format:            "gguf",
						Family:            "llama",
						Families:          []string{"llama"},
						ParameterSize:     "8B",
						QuantizationLevel: "Q4_0",
					},
				}},
			})
		case "/api/show":
			_ = json.NewEncoder(w).Encode(ollamaapi.ShowResponse{
				License:    "Meta Llama License",
				Modelfile:  "FROM llama3.2",
				Parameters: "temperature 0.8",
				Template:   "{{ .Prompt }}",
				Details: &ollamaapi.ModelDetails{
					Format:            "gguf",
					Family:            "llama",
					Families:          []string{"llama"},
					ParameterSize:     "8B",
					QuantizationLevel: "Q4_0",
				},
			})
		case "/api/ps":
			_ = json.NewEncoder(w).Encode(ollamaapi.ListRunningResponse{
				Models: []ollamaapi.RunningModel{{
					Name:      "llama3.2:latest",
					Model:     "llama3.2:latest",
					Size:      4_000_000_000,
					Digest:    "sha256:abc123",
					ExpiresAt: time.Now().Add(5 * time.Minute),
					SizeVRAM:  3_000_000_000,
				}},
			})
		case "/api/delete", "/api/copy":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	t.Run("Version", func(t *testing.T) {
		v, err := client.Version(ctx)
		if err != nil {
			t.Fatalf("Version: %v", err)
		}
		if v != "0.6.0" {
			t.Errorf("Version = %q, want 0.6.0", v)
		}
	})

	t.Run("ListLocalModels", func(t *testing.T) {
		models, err := client.ListLocalModels(ctx)
		if err != nil {
			t.Fatalf("ListLocalModels: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("got %d local models, want 1", len(models))
		}
		if models[0].Name != "llama3.2:latest" {
			t.Errorf("Name = %q, want llama3.2:latest", models[0].Name)
		}
		if models[0].Details == nil || models[0].Details.ParameterSize != "8B" {
			t.Errorf("Details.ParameterSize mismatch: %+v", models[0].Details)
		}
	})

	t.Run("ShowModel", func(t *testing.T) {
		details, err := client.ShowModel(ctx, "llama3.2")
		if err != nil {
			t.Fatalf("ShowModel: %v", err)
		}
		if details.Family != "llama" {
			t.Errorf("Family = %q, want llama", details.Family)
		}
		if details.Name != "llama3.2" {
			t.Errorf("Name = %q, want llama3.2", details.Name)
		}
		if details.QuantizationLevel != "Q4_0" {
			t.Errorf("QuantizationLevel = %q, want Q4_0", details.QuantizationLevel)
		}
	})

	t.Run("ListRunningModels", func(t *testing.T) {
		models, err := client.ListRunningModels(ctx)
		if err != nil {
			t.Fatalf("ListRunningModels: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("got %d running models, want 1", len(models))
		}
		if models[0].SizeVRAM != 3_000_000_000 {
			t.Errorf("SizeVRAM = %d, want 3000000000", models[0].SizeVRAM)
		}
	})

	t.Run("DeleteModel", func(t *testing.T) {
		if err := client.DeleteModel(ctx, "llama3.2"); err != nil {
			t.Fatalf("DeleteModel: %v", err)
		}
	})

	t.Run("CopyModel", func(t *testing.T) {
		if err := client.CopyModel(ctx, "llama3.2", "my-llama"); err != nil {
			t.Fatalf("CopyModel: %v", err)
		}
	})
}

// TestPullModel covers the streaming pull wrapper, including the percent
// computation in the progress callback and the nil-callback path.
func TestPullModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		responses := []ollamaapi.PullResponse{
			{Status: "pulling manifest"},
			{Status: "downloading", Digest: "sha256:abc", Total: 1000, Completed: 500},
			{Status: "downloading", Digest: "sha256:abc", Total: 1000, Completed: 1000},
			{Status: "success"},
		}
		for _, resp := range responses {
			data, _ := json.Marshal(resp)
			_, _ = w.Write(append(data, '\n'))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	t.Run("with-callback", func(t *testing.T) {
		var updates []PullProgress
		if err := client.PullModel(ctx, "llama3.2", func(p PullProgress) {
			updates = append(updates, p)
		}); err != nil {
			t.Fatalf("PullModel: %v", err)
		}
		if len(updates) == 0 {
			t.Fatal("expected progress updates")
		}

		var sawPercent, sawSuccess bool
		for _, p := range updates {
			if p.Total > 0 && p.Completed == p.Total && p.Percent == 100 {
				sawPercent = true
			}
			if p.Status == "success" {
				sawSuccess = true
			}
		}
		if !sawPercent {
			t.Error("expected a 100%% progress update")
		}
		if !sawSuccess {
			t.Error("expected a success status update")
		}
	})

	t.Run("nil-callback", func(t *testing.T) {
		if err := client.PullModel(ctx, "llama3.2", nil); err != nil {
			t.Fatalf("PullModel nil callback: %v", err)
		}
	})
}
