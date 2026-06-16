// Example: Text Embeddings
//
// This example demonstrates how to generate text embeddings for
// semantic search, similarity comparison, and RAG applications.
//
// Run with: go run ./examples/embeddings
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, embedder := createEmbedder()
	if embedder == nil {
		cancel()
		log.Fatal("No embedding-capable provider found. Set OPENAI_API_KEY or GEMINI_API_KEY")
	}

	fmt.Printf("Using: %s (%s)\n\n", client.Provider(), client.Model())

	// Example 1: Embed a single query
	fmt.Println("=== Single Query Embedding ===")
	query := "What is machine learning?"

	vector, err := llms.EmbedQuery(ctx, embedder, query)
	if err != nil {
		log.Fatalf("EmbedQuery failed: %v", err)
	}

	fmt.Printf("Query: %q\n", query)
	fmt.Printf("Vector dimensions: %d\n", len(vector))
	fmt.Printf("First 5 values: [%.4f, %.4f, %.4f, %.4f, %.4f...]\n\n",
		vector[0], vector[1], vector[2], vector[3], vector[4])

	// Example 2: Embed multiple documents
	fmt.Println("=== Document Embeddings ===")
	documents := []string{
		"Machine learning is a subset of artificial intelligence.",
		"Deep learning uses neural networks with many layers.",
		"Natural language processing deals with text and speech.",
		"Computer vision enables machines to interpret images.",
		"The weather today is sunny and warm.",
	}

	vectors, err := llms.EmbedDocuments(ctx, embedder, documents)
	if err != nil {
		log.Fatalf("EmbedDocuments failed: %v", err)
	}

	fmt.Printf("Embedded %d documents\n\n", len(vectors))

	// Example 3: Semantic similarity search
	fmt.Println("=== Semantic Similarity Search ===")
	searchQuery := "How do computers understand pictures?"

	queryVec, err := llms.EmbedQuery(ctx, embedder, searchQuery)
	if err != nil {
		log.Fatalf("Query embedding failed: %v", err)
	}

	fmt.Printf("Search query: %q\n\n", searchQuery)
	fmt.Println("Similarity scores:")

	// Calculate cosine similarity with each document
	for i, docVec := range vectors {
		similarity := cosineSimilarity(queryVec, docVec)
		fmt.Printf("  %.4f - %s\n", similarity, documents[i])
	}

	// Example 4: Using embedding options
	fmt.Println("\n=== With Options ===")
	resp, err := embedder.Embed(ctx, []string{"Test embedding with options"},
		llms.WithDimensions(256), // Reduced dimensions (if supported)
	)
	if err != nil {
		// Some models don't support dimension reduction
		fmt.Printf("Note: Dimension reduction may not be supported: %v\n", err)
	} else {
		fmt.Printf("Embedding dimensions: %d\n", len(resp.Embeddings[0].Vector))
		fmt.Printf("Tokens used: %d\n", resp.Usage.PromptTokens)
	}

	// Example 5: Batch embedding with full response
	fmt.Println("\n=== Batch Embedding Details ===")
	texts := []string{
		"First document to embed",
		"Second document to embed",
		"Third document to embed",
	}

	resp, err = embedder.Embed(ctx, texts)
	if err != nil {
		log.Fatalf("Batch embed failed: %v", err)
	}

	fmt.Printf("Model: %s\n", resp.Model)
	fmt.Printf("Total tokens: %d\n", resp.Usage.TotalTokens)
	fmt.Printf("Embeddings returned: %d\n", len(resp.Embeddings))
	for _, emb := range resp.Embeddings {
		fmt.Printf("  Index %d: %d dimensions\n", emb.Index, len(emb.Vector))
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

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

func createEmbedder() (llms.LLM, llms.Embedder) {
	// Try OpenAI (supports embeddings)
	if os.Getenv("OPENAI_API_KEY") != "" {
		client, err := openai.New(
			openai.WithEmbeddingModel("text-embedding-3-small"),
		)
		if err == nil {
			if embedder, ok := llms.AsEmbedder(client); ok {
				return client, embedder
			}
		}
	}

	// Try Gemini (supports embeddings)
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		client, err := gemini.New()
		if err == nil {
			if embedder, ok := llms.AsEmbedder(client); ok {
				return client, embedder
			}
		}
	}

	return nil, nil
}
