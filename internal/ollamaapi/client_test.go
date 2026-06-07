package ollamaapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient(ClientConfig{AllowPrivateIPs: true, AllowHTTP: true})

	if client.baseURL != defaultBaseURL {
		t.Errorf("expected default base URL %s, got %s", defaultBaseURL, client.baseURL)
	}
}

func TestNewClientWithBaseURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://localhost:11434/v1", "http://localhost:11434"},
		{"http://localhost:11434/v1/", "http://localhost:11434"},
		{"http://localhost:11434", "http://localhost:11434"},
		{"http://localhost:11434/", "http://localhost:11434"},
	}

	for _, tt := range tests {
		client := NewClient(ClientConfig{BaseURL: tt.input, AllowPrivateIPs: true, AllowHTTP: true})
		if client.baseURL != tt.expected {
			t.Errorf("input %s: expected %s, got %s", tt.input, tt.expected, client.baseURL)
		}
	}
}

func TestListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}

		resp := ListModelsResponse{
			Models: []Model{
				{
					Name:       "llama3.2:latest",
					ModifiedAt: time.Now(),
					Size:       4000000000,
					Digest:     "sha256:abc123",
					Details: &ModelDetails{
						Format:        "gguf",
						Family:        "llama",
						ParameterSize: "8B",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	resp, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Models))
	}

	if resp.Models[0].Name != "llama3.2:latest" {
		t.Errorf("expected model name 'llama3.2:latest', got %q", resp.Models[0].Name)
	}
}

func TestShowModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		var req ShowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Name != "llama3.2" {
			http.Error(w, "model not found", http.StatusNotFound)
			return
		}

		resp := ShowResponse{
			License:   "Meta Llama License",
			Modelfile: "FROM llama3.2",
			Template:  "{{ .System }}\n{{ .Prompt }}",
			Details: &ModelDetails{
				Format:            "gguf",
				Family:            "llama",
				ParameterSize:     "8B",
				QuantizationLevel: "Q4_0",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	resp, err := client.ShowModel(context.Background(), "llama3.2", true)
	if err != nil {
		t.Fatalf("ShowModel failed: %v", err)
	}

	if resp.Details.Family != "llama" {
		t.Errorf("expected family 'llama', got %q", resp.Details.Family)
	}
}

func TestListRunning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			http.NotFound(w, r)
			return
		}

		resp := ListRunningResponse{
			Models: []RunningModel{
				{
					Name:      "llama3.2:latest",
					Size:      4000000000,
					SizeVRAM:  3000000000,
					ExpiresAt: time.Now().Add(5 * time.Minute),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	resp, err := client.ListRunning(context.Background())
	if err != nil {
		t.Fatalf("ListRunning failed: %v", err)
	}

	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 running model, got %d", len(resp.Models))
	}
}

func TestGetVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			http.NotFound(w, r)
			return
		}

		resp := VersionResponse{Version: "0.5.4"}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	version, err := client.GetVersion(context.Background())
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}

	if version != "0.5.4" {
		t.Errorf("expected version '0.5.4', got %q", version)
	}
}

func TestDeleteModel(t *testing.T) {
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/delete" || r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}

		var req DeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Name == "test-model" {
			deleted = true
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	err := client.DeleteModel(context.Background(), "test-model")
	if err != nil {
		t.Fatalf("DeleteModel failed: %v", err)
	}

	if !deleted {
		t.Error("expected model to be deleted")
	}
}

func TestCopyModel(t *testing.T) {
	copied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/copy" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		var req CopyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Source == "llama3.2" && req.Destination == "my-llama" {
			copied = true
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "invalid request", http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	err := client.CopyModel(context.Background(), "llama3.2", "my-llama")
	if err != nil {
		t.Fatalf("CopyModel failed: %v", err)
	}

	if !copied {
		t.Error("expected model to be copied")
	}
}

func TestGenerate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		resp := GenerateResponse{
			Model:     "llama3.2",
			CreatedAt: time.Now(),
			Response:  "Hello! How can I help you?",
			Done:      true,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	resp, err := client.Generate(context.Background(), &GenerateRequest{
		Model:  "llama3.2",
		Prompt: "Hello",
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if resp.Response == "" {
		t.Error("expected non-empty response")
	}
}

func TestChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		resp := ChatResponse{
			Model:     "llama3.2",
			CreatedAt: time.Now(),
			Message: ChatMessage{
				Role:    "assistant",
				Content: "Hello! I'm here to help.",
			},
			Done: true,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	resp, err := client.Chat(context.Background(), &ChatRequest{
		Model: "llama3.2",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Message.Content == "" {
		t.Error("expected non-empty message content")
	}
}

func TestEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		resp := EmbedResponse{
			Model:      "nomic-embed-text",
			Embeddings: [][]float32{{0.1, 0.2, 0.3, 0.4, 0.5}},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	resp, err := client.Embed(context.Background(), &EmbedRequest{
		Model: "nomic-embed-text",
		Input: "Hello world",
	})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(resp.Embeddings) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(resp.Embeddings))
	}

	if len(resp.Embeddings[0]) != 5 {
		t.Errorf("expected 5-dimensional vector, got %d", len(resp.Embeddings[0]))
	}
}

func TestWrapError(t *testing.T) {
	// Test nil error
	if WrapError("test", nil) != nil {
		t.Error("expected nil for nil error")
	}

	// Test regular error
	err := WrapError("test", context.Canceled)
	if err == nil {
		t.Error("expected non-nil error")
	}
}
