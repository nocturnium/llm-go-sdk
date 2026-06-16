package llamacpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

func TestNew(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if client == nil {
		t.Fatal("New() returned nil client")
	}

	if client.Provider() != llms.ProviderLlamaCpp {
		t.Errorf("Provider() = %v, want %v", client.Provider(), llms.ProviderLlamaCpp)
	}
}

func TestNewWithOptions(t *testing.T) {
	client, err := New(
		WithBaseURL("http://custom:9999"),
		WithModel("test-model"),
		WithAPIKey("test-key"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if client.Model() != "test-model" {
		t.Errorf("Model() = %v, want test-model", client.Model())
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %v, want http://localhost:8080", opts.BaseURL)
	}

	if opts.Model != "" {
		t.Errorf("Model = %v, want empty string", opts.Model)
	}
}

func TestClientImplementsInterfaces(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// LLM interface
	var _ llms.LLM = client

	// Embedder interface
	var _ llms.Embedder = client

	// CapableProvider interface
	var _ llms.CapableProvider = client

	// ModelLister interface
	var _ llms.ModelLister = client

	// Manager interface
	var _ Manager = client
}

func TestGetCapabilities(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	caps := client.Capabilities()

	if !caps.Streaming {
		t.Error("Expected Streaming capability")
	}

	if !caps.JSONMode {
		t.Error("Expected JSONMode capability")
	}
}

func setupMockServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New(WithBaseURL(server.URL))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func TestHealth(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("Expected path /health, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"slots_idle":       2,
			"slots_processing": 1,
		})
	})

	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}

	if health.Status != "ok" {
		t.Errorf("Status = %v, want ok", health.Status)
	}

	if health.SlotsIdle != 2 {
		t.Errorf("SlotsIdle = %v, want 2", health.SlotsIdle)
	}

	if health.SlotsActive != 1 {
		t.Errorf("SlotsActive = %v, want 1", health.SlotsActive)
	}
}

func TestModelProps(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			t.Errorf("Expected path /props, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_slots":   4,
			"chat_template": "llama3",
			"default_generation_settings": map[string]any{
				"model":       "llama-3.2-1b",
				"n_ctx":       4096,
				"n_predict":   512,
				"temperature": 0.8,
				"top_p":       0.95,
				"top_k":       40,
			},
		})
	})

	props, err := client.ModelProps(context.Background())
	if err != nil {
		t.Fatalf("ModelProps() error = %v", err)
	}

	if props.ModelName != "llama-3.2-1b" {
		t.Errorf("ModelName = %v, want llama-3.2-1b", props.ModelName)
	}

	if props.NCtx != 4096 {
		t.Errorf("NCtx = %v, want 4096", props.NCtx)
	}

	if props.TotalSlots != 4 {
		t.Errorf("TotalSlots = %v, want 4", props.TotalSlots)
	}
}

func TestListSlots(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slots" {
			t.Errorf("Expected path /slots, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 0, "state": 0, "n_ctx": 4096, "n_past": 0},
			{"id": 1, "state": 1, "n_ctx": 4096, "n_past": 128, "prompt": "Hello"},
		})
	})

	slots, err := client.ListSlots(context.Background())
	if err != nil {
		t.Fatalf("ListSlots() error = %v", err)
	}

	if len(slots) != 2 {
		t.Fatalf("Expected 2 slots, got %d", len(slots))
	}

	if slots[0].State != SlotIdle {
		t.Errorf("Slot 0 state = %v, want SlotIdle", slots[0].State)
	}

	if slots[1].State != SlotProcessing {
		t.Errorf("Slot 1 state = %v, want SlotProcessing", slots[1].State)
	}

	if slots[1].TokensUsed != 128 {
		t.Errorf("Slot 1 TokensUsed = %v, want 128", slots[1].TokensUsed)
	}
}

func TestIsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected bool
	}{
		{"ok status", "ok", true},
		{"loading status", "loading model", false},
		{"error status", "error", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status":           tt.status,
					"slots_idle":       1,
					"slots_processing": 0,
				})
			})

			result := client.IsHealthy(context.Background())
			if result != tt.expected {
				t.Errorf("IsHealthy() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSlotStateString(t *testing.T) {
	tests := []struct {
		state    SlotState
		expected string
	}{
		{SlotIdle, "idle"},
		{SlotProcessing, "processing"},
		{SlotState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("SlotState.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestInferOrganization(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"llama-3.2-1b", "Meta"},
		{"mistral-7b", "Mistral AI"},
		{"mixtral-8x7b", "Mistral AI"},
		{"gemma-2b", "Google"},
		{"phi-3", "Microsoft"},
		{"qwen-7b", "Alibaba"},
		{"deepseek-coder", "DeepSeek"},
		{"unknown-model", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			if got := inferOrganization(tt.modelID); got != tt.expected {
				t.Errorf("inferOrganization(%q) = %v, want %v", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestInferModelTypes(t *testing.T) {
	tests := []struct {
		modelID  string
		expected []llms.ModelType
	}{
		{"nomic-embed-text", []llms.ModelType{llms.ModelTypeEmbedding}},
		{"llava-1.5", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}},
		{"codellama-7b", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode}},
		{"llama-3.2-1b", []llms.ModelType{llms.ModelTypeChat}},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := inferModelTypes(tt.modelID)
			if len(got) != len(tt.expected) {
				t.Errorf("inferModelTypes(%q) = %v, want %v", tt.modelID, got, tt.expected)
				return
			}
			for i, typ := range got {
				if typ != tt.expected[i] {
					t.Errorf("inferModelTypes(%q)[%d] = %v, want %v", tt.modelID, i, typ, tt.expected[i])
				}
			}
		})
	}
}

func TestListModels(t *testing.T) {
	requestCount := 0
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data": []map[string]any{
					{"id": "llama-3.2-1b", "object": "model", "created": 1700000000},
				},
			})
		case "/props":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_slots": 4,
				"default_generation_settings": map[string]any{
					"model": "llama-3.2-1b",
					"n_ctx": 4096,
				},
			})
		default:
			t.Errorf("Unexpected path: %s", r.URL.Path)
		}
	})

	result, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}

	if len(result.Models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(result.Models))
	}

	model := result.Models[0]
	if model.ID != "llama-3.2-1b" {
		t.Errorf("Model ID = %v, want llama-3.2-1b", model.ID)
	}

	if model.Provider != llms.ProviderLlamaCpp {
		t.Errorf("Model Provider = %v, want %v", model.Provider, llms.ProviderLlamaCpp)
	}

	if model.ContextLength != 4096 {
		t.Errorf("Model ContextLength = %v, want 4096", model.ContextLength)
	}

	if model.Organization != "Meta" {
		t.Errorf("Model Organization = %v, want Meta", model.Organization)
	}
}
