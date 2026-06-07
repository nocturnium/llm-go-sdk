// Package infinity provides a client for Infinity embeddings server.
//
// Infinity is a high-throughput, low-latency REST API for serving text-embeddings,
// reranking models, CLIP, and ColPali. It uses an OpenAI-compatible API for embeddings.
//
// # Configuration
//
// By default, the client connects to http://localhost:7997.
// Override with INFINITY_API_KEY environment variable or WithBaseURL option.
// No API key is required for local deployments.
//
// # Quick Start
//
//	client, err := infinity.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Generate embeddings
//	resp, err := client.Embed(ctx, []string{"Hello, world!"})
//
//	// Rerank documents
//	result, err := client.Rerank(ctx, "What is machine learning?",
//	    []string{"ML is a subset of AI", "The weather is nice"})
//
// # Supported Features
//
//   - Text embeddings (OpenAI-compatible)
//   - Document reranking
//   - Batch processing
//   - Multiple model support
//   - GPU acceleration
//
// # Configuration Options
//
//	client, err := infinity.New(
//	    infinity.WithBaseURL("http://localhost:7997/v1"),
//	    infinity.WithEmbeddingModel("michaelfeil/bge-small-en-v1.5"),
//	    infinity.WithRerankModel("mixedbread-ai/mxbai-rerank-xsmall-v1"),
//	    infinity.WithAPIKey("optional-api-key"),
//	)
//
// # Popular Models
//
// Embedding models:
//   - michaelfeil/bge-small-en-v1.5 (default)
//   - BAAI/bge-large-en-v1.5
//   - sentence-transformers/all-MiniLM-L6-v2
//   - intfloat/e5-large-v2
//
// Reranking models:
//   - mixedbread-ai/mxbai-rerank-xsmall-v1 (default)
//   - BAAI/bge-reranker-large
//   - cross-encoder/ms-marco-MiniLM-L-6-v2
//
// # Architecture Note
//
// Unlike other providers in this SDK that implement the full LLM interface,
// Infinity is specialized for embeddings and reranking. It implements:
//   - llms.Embedder for text embeddings
//   - llms.Reranker for document reranking
//
// It does NOT implement llms.LLM as it doesn't support chat completions.
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package infinity
