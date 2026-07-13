// Package gemini provides a Google Gemini LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Google's Gemini API,
// supporting Gemini 2.0, Gemini 1.5, and Gemini 1.0 model families.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - GEMINI_API_KEY (primary)
//   - GOOGLE_API_KEY (alternative)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := gemini.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions (all Gemini models)
//   - Streaming responses
//   - Function/tool calling
//   - Vision (all Gemini models)
//   - Embeddings (text-embedding-004)
//   - JSON mode
//   - Safety settings configuration
//
// # Configuration Options
//
//	client, err := gemini.New(
//	    gemini.WithAPIKey("..."),
//	    gemini.WithModel("gemini-2.5-flash"),
//	    gemini.WithEmbeddingModel("text-embedding-004"),
//	    gemini.WithHTTPClient(customHTTPClient),
//	)
//
// # Embeddings
//
//	client, err := gemini.New()
//	embedder, ok := llms.AsEmbedder(client)
//	if ok {
//	    vectors, err := llms.EmbedDocuments(ctx, embedder, texts,
//	        llms.WithTaskType(llms.TaskTypeRetrievalDocument),
//	    )
//	}
//
// # Default Model
//
// The default model is gemini-2.5-flash. Override with WithModel.
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package gemini
