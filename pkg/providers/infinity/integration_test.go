package infinity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

// TestEmbed tests embedding generation.
func TestEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			resp := map[string]any{
				"object": "list",
				"model":  "michaelfeil/bge-small-en-v1.5",
				"data": []map[string]any{
					{
						"object":    "embedding",
						"index":     0,
						"embedding": []float32{0.1, 0.2, 0.3, 0.4, 0.5},
					},
				},
				"usage": map[string]any{
					"prompt_tokens": 5,
					"total_tokens":  5,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	resp, err := client.Embed(ctx, []string{"Hello world"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(resp.Embeddings) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(resp.Embeddings))
	}

	if len(resp.Embeddings[0].Vector) != 5 {
		t.Errorf("expected 5-dimensional vector, got %d", len(resp.Embeddings[0].Vector))
	}
}

// TestEmbedBatch tests batch embedding generation.
func TestEmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			resp := map[string]any{
				"object": "list",
				"model":  "michaelfeil/bge-small-en-v1.5",
				"data": []map[string]any{
					{"object": "embedding", "index": 0, "embedding": []float32{0.1, 0.2, 0.3}},
					{"object": "embedding", "index": 1, "embedding": []float32{0.4, 0.5, 0.6}},
					{"object": "embedding", "index": 2, "embedding": []float32{0.7, 0.8, 0.9}},
				},
				"usage": map[string]any{
					"prompt_tokens": 15,
					"total_tokens":  15,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	resp, err := client.Embed(ctx, []string{"text1", "text2", "text3"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(resp.Embeddings) != 3 {
		t.Errorf("expected 3 embeddings, got %d", len(resp.Embeddings))
	}
}

// TestEmbedQuery tests single query embedding.
func TestEmbedQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			resp := map[string]any{
				"object": "list",
				"model":  "michaelfeil/bge-small-en-v1.5",
				"data": []map[string]any{
					{"object": "embedding", "index": 0, "embedding": []float32{0.1, 0.2, 0.3}},
				},
				"usage": map[string]any{"prompt_tokens": 3, "total_tokens": 3},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	vector, err := llms.EmbedQuery(ctx, client, "Test query")
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	if len(vector) != 3 {
		t.Errorf("expected 3-dimensional vector, got %d", len(vector))
	}
}

// TestEmbedDocuments tests multiple document embedding.
func TestEmbedDocuments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			resp := map[string]any{
				"object": "list",
				"model":  "michaelfeil/bge-small-en-v1.5",
				"data": []map[string]any{
					{"object": "embedding", "index": 0, "embedding": []float32{0.1, 0.2}},
					{"object": "embedding", "index": 1, "embedding": []float32{0.3, 0.4}},
				},
				"usage": map[string]any{"prompt_tokens": 6, "total_tokens": 6},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	vectors, err := llms.EmbedDocuments(ctx, client, []string{"doc1", "doc2"})
	if err != nil {
		t.Fatalf("EmbedDocuments failed: %v", err)
	}

	if len(vectors) != 2 {
		t.Errorf("expected 2 vectors, got %d", len(vectors))
	}
}

// TestRerank tests document reranking.
func TestRerank(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/rerank" && r.Method == http.MethodPost {
			var req rerankRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if req.Query == "" {
				http.Error(w, "query required", http.StatusBadRequest)
				return
			}

			// Return documents in relevance order
			resp := rerankAPIResponse{
				Results: []rerankAPIResult{
					{Index: 0, RelevanceScore: 0.95},
					{Index: 2, RelevanceScore: 0.75},
					{Index: 1, RelevanceScore: 0.30},
				},
				Model: "mixedbread-ai/mxbai-rerank-xsmall-v1",
				Usage: struct {
					TotalTokens int `json:"total_tokens"`
				}{TotalTokens: 50},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	documents := []string{
		"Machine learning is a subset of artificial intelligence.",
		"The weather today is sunny.",
		"Deep learning uses neural networks.",
	}

	result, err := client.Rerank(ctx, "What is machine learning?", documents)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}

	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}

	// Results should be sorted by score descending
	if result.Results[0].Score < result.Results[1].Score {
		t.Error("expected results to be sorted by score descending")
	}

	// First result should be the most relevant
	if result.Results[0].Index != 0 {
		t.Errorf("expected most relevant document index 0, got %d", result.Results[0].Index)
	}
}

// TestRerankWithOptions tests reranking with options.
func TestRerankWithOptions(t *testing.T) {
	var receivedTopN int
	var receivedReturnDocs bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/rerank" {
			var req rerankRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			receivedTopN = req.TopN
			receivedReturnDocs = req.ReturnDocuments

			resp := rerankAPIResponse{
				Results: []rerankAPIResult{
					{Index: 0, RelevanceScore: 0.95, Document: "doc1"},
					{Index: 1, RelevanceScore: 0.85, Document: "doc2"},
				},
				Model: "mixedbread-ai/mxbai-rerank-xsmall-v1",
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	_, err = client.Rerank(ctx, "query",
		[]string{"doc1", "doc2", "doc3"},
		llms.WithTopN(2),
		llms.WithReturnDocuments(true),
	)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}

	if receivedTopN != 2 {
		t.Errorf("expected TopN 2, got %d", receivedTopN)
	}

	if !receivedReturnDocs {
		t.Error("expected ReturnDocuments to be true")
	}
}

// TestRerankEmptyQuery tests reranking with empty query.
func TestRerankEmptyQuery(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	_, err = client.Rerank(ctx, "", []string{"doc1"})
	if err == nil {
		t.Error("expected error for empty query")
	}
}

// TestRerankEmptyDocuments tests reranking with empty documents.
func TestRerankEmptyDocuments(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	_, err = client.Rerank(ctx, "query", []string{})
	if err == nil {
		t.Error("expected error for empty documents")
	}
}

// TestEmbedWithModel tests embedding with custom model.
func TestEmbedWithModel(t *testing.T) {
	var receivedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			receivedModel, _ = req["model"].(string)

			resp := map[string]any{
				"object": "list",
				"model":  receivedModel,
				"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": []float32{0.1}}},
				"usage":  map[string]any{"prompt_tokens": 1, "total_tokens": 1},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	_, err = client.Embed(ctx, []string{"test"}, llms.WithEmbedModel("BAAI/bge-large-en-v1.5"))
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if receivedModel != "BAAI/bge-large-en-v1.5" {
		t.Errorf("expected model 'BAAI/bge-large-en-v1.5', got %q", receivedModel)
	}
}

// TestContextCancellation tests that operations respect context cancellation.
func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = client.Embed(ctx, []string{"test"})
	if err == nil {
		t.Error("expected error due to context cancellation")
	}
}

// TestErrorHandling tests error responses.
func TestErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Invalid model specified",
				"type":    "invalid_request_error",
			},
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	_, err = client.Embed(ctx, []string{"test"})
	if err == nil {
		t.Error("expected error for bad request")
	}
}

// TestValidationErrors tests input validation.
func TestValidationErrors(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()

	// Empty input
	_, err = client.Embed(ctx, []string{})
	if err == nil {
		t.Error("expected error for empty input")
	}

	// Empty string in input
	_, err = client.Embed(ctx, []string{""})
	if err == nil {
		t.Error("expected error for empty string in input")
	}
}

// TestGetProvider tests provider identification.
func TestGetProvider(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if client.Provider() != llms.ProviderInfinity {
		t.Errorf("expected provider infinity, got %s", client.Provider())
	}
}

// TestGetCapabilities_Integration tests capability reporting.
func TestGetCapabilities_Integration(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	caps := client.Capabilities()

	if !caps.Embeddings {
		t.Error("expected embeddings capability")
	}

	if !caps.Batch {
		t.Error("expected batch capability")
	}

	// Infinity is NOT an LLM, so these should be false
	if caps.Streaming {
		t.Error("expected streaming to be false")
	}

	if caps.Tools {
		t.Error("expected tools to be false")
	}
}
