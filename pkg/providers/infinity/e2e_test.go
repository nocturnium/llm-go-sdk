//go:build e2e

package infinity

import (
	"context"
	"os"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

// These tests require a running Infinity server.
// Set INFINITY_API_KEY and INFINITY_API_URL environment variables.

func getE2EClient(t *testing.T) *Client {
	apiKey := os.Getenv("INFINITY_API_KEY")
	apiURL := os.Getenv("INFINITY_API_URL")
	apiRunPod := os.Getenv("INFINITY_RUNPOD_ENABLED") == "true"
	if apiKey == "" || apiURL == "" {
		t.Skip("INFINITY_API_KEY and INFINITY_API_URL must be set for E2E tests")
	}

	opts := []Option{
		WithBaseURL(apiURL),
		WithAPIKey(apiKey),
		WithEmbeddingModel("BAAI/bge-small-en-v1.5"), // RunPod model name
		WithRerankModel("BAAI/bge-reranker-base"),    // RunPod reranker model
	}
	if apiRunPod {
		opts = append(opts, WithRunPodMode())
	}

	// RunPod serverless uses the base URL directly with /runsync endpoint
	// The WithRunPodMode option handles the API format differences
	client, err := New(opts...)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	return client
}

func TestE2E_Embed(t *testing.T) {
	client := getE2EClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	texts := []string{"Hello, world!"}

	t.Logf("Sending embedding request for: %v", texts)

	resp, err := client.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	t.Logf("Response model: %s", resp.Model)
	t.Logf("Number of embeddings: %d", len(resp.Embeddings))

	if len(resp.Embeddings) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(resp.Embeddings))
	}

	embedding := resp.Embeddings[0]
	t.Logf("Embedding dimensions: %d", len(embedding.Vector))
	t.Logf("First 5 values: %v", embedding.Vector[:min(5, len(embedding.Vector))])

	if len(embedding.Vector) == 0 {
		t.Error("expected non-empty embedding vector")
	}

	t.Logf("Usage - Prompt tokens: %d, Total tokens: %d",
		resp.Usage.PromptTokens, resp.Usage.TotalTokens)
}

func TestE2E_EmbedBatch(t *testing.T) {
	client := getE2EClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	texts := []string{
		"Machine learning is a subset of artificial intelligence.",
		"The weather today is sunny and warm.",
		"Go is a statically typed programming language.",
	}

	t.Logf("Sending batch embedding request for %d texts", len(texts))

	resp, err := client.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	t.Logf("Response model: %s", resp.Model)
	t.Logf("Number of embeddings: %d", len(resp.Embeddings))

	if len(resp.Embeddings) != len(texts) {
		t.Fatalf("expected %d embeddings, got %d", len(texts), len(resp.Embeddings))
	}

	for i, emb := range resp.Embeddings {
		t.Logf("Embedding %d: %d dimensions", i, len(emb.Vector))
	}
}

func TestE2E_EmbedQuery(t *testing.T) {
	client := getE2EClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	query := "What is machine learning?"

	t.Logf("Embedding query: %s", query)

	vector, err := llms.EmbedQuery(ctx, client, query)
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	t.Logf("Query embedding dimensions: %d", len(vector))

	if len(vector) == 0 {
		t.Error("expected non-empty embedding vector")
	}
}

func TestE2E_EmbedDocuments(t *testing.T) {
	client := getE2EClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	docs := []string{
		"Document 1: Introduction to AI",
		"Document 2: Weather patterns",
	}

	t.Logf("Embedding %d documents", len(docs))

	vectors, err := llms.EmbedDocuments(ctx, client, docs)
	if err != nil {
		t.Fatalf("EmbedDocuments failed: %v", err)
	}

	if len(vectors) != len(docs) {
		t.Fatalf("expected %d vectors, got %d", len(docs), len(vectors))
	}

	for i, vec := range vectors {
		t.Logf("Document %d embedding: %d dimensions", i, len(vec))
	}
}

func TestE2E_SimilaritySearch(t *testing.T) {
	client := getE2EClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Documents to search through
	documents := []string{
		"Machine learning is a method of data analysis that automates analytical model building.",
		"The weather forecast predicts rain tomorrow.",
		"Deep learning is part of a broader family of machine learning methods.",
		"Python is a popular programming language for data science.",
		"Neural networks are computing systems inspired by biological neural networks.",
	}

	// Query to search for
	query := "What is artificial intelligence and machine learning?"

	t.Log("Embedding documents...")
	docVectors, err := llms.EmbedDocuments(ctx, client, documents)
	if err != nil {
		t.Fatalf("EmbedDocuments failed: %v", err)
	}

	t.Log("Embedding query...")
	queryVector, err := llms.EmbedQuery(ctx, client, query)
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	// Calculate cosine similarity
	t.Log("Calculating similarities...")
	similarities := make([]float64, len(documents))
	for i, docVec := range docVectors {
		similarities[i] = cosineSimilarity(queryVector, docVec)
		t.Logf("Doc %d similarity: %.4f - %s", i, similarities[i], documents[i][:min(50, len(documents[i]))]+"...")
	}

	// Find most similar document
	maxIdx := 0
	for i, sim := range similarities {
		if sim > similarities[maxIdx] {
			maxIdx = i
		}
	}

	t.Logf("\nMost similar document (index %d, score %.4f):", maxIdx, similarities[maxIdx])
	t.Logf("  %s", documents[maxIdx])

	// The ML-related documents (0, 2, 4) should have higher similarity
	// than weather (1) and Python (3) for an AI/ML query
	if similarities[1] > similarities[0] {
		t.Log("Warning: Weather document scored higher than ML document")
	}
}

// cosineSimilarity calculates the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dotProduct += av * bv
		normA += av * av
		normB += bv * bv
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt(normA) * sqrt(normB))
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 100; i++ {
		z = (z + x/z) / 2
	}
	return z
}

func TestE2E_EmbedderInterface(t *testing.T) {
	client := getE2EClient(t)

	// Verify the client implements Embedder
	embedder, ok := llms.AsEmbedder(client)
	if !ok {
		t.Fatal("client does not implement Embedder interface")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := embedder.Embed(ctx, []string{"Test via interface"})
	if err != nil {
		t.Fatalf("Embed via interface failed: %v", err)
	}

	if len(resp.Embeddings) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(resp.Embeddings))
	}

	t.Logf("Successfully called Embed via Embedder interface")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
