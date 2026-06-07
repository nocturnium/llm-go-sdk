// Package ollama provides a local Ollama LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Ollama's OpenAI-compatible
// API, providing local inference without requiring an internet connection.
// It also includes native Ollama API support for model management operations.
//
// # Configuration
//
// By default, the client connects to http://localhost:11434.
// Override with OLLAMA_HOST environment variable or WithBaseURL option.
// No API key is required for local Ollama.
//
// # Quick Start
//
//	client, err := ollama.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions
//   - Streaming responses
//   - Function/tool calling (model dependent)
//   - JSON mode
//   - Embeddings (nomic-embed-text, etc.)
//   - Vision (llava, etc.)
//   - Local inference (no internet required)
//   - GPU acceleration
//
// # Model Management
//
// The client provides native Ollama API methods for managing models:
//
//	// Pull a model from the registry
//	err := client.PullModel(ctx, "llama3.2", func(p ollama.PullProgress) {
//	    fmt.Printf("Progress: %.1f%%\n", p.Percent)
//	})
//
//	// List local models with details
//	models, err := client.ListLocalModels(ctx)
//
//	// Get detailed model information
//	details, err := client.ShowModel(ctx, "llama3.2")
//
//	// List currently loaded models
//	running, err := client.ListRunningModels(ctx)
//
//	// Delete a model
//	err := client.DeleteModel(ctx, "old-model")
//
//	// Copy a model
//	err := client.CopyModel(ctx, "llama3.2", "my-llama")
//
//	// Get Ollama version
//	version, err := client.Version(ctx)
//
// # Configuration Options
//
//	client, err := ollama.New(
//	    ollama.WithModel("llama3.2"),
//	    ollama.WithBaseURL("http://localhost:11434/v1"),
//	    ollama.WithEmbeddingModel("nomic-embed-text"),
//	)
//
// # Popular Models
//
//   - llama3.2 (default) - Meta's latest Llama model
//   - llama3.2:70b - Larger variant
//   - codellama - Code generation
//   - mistral - Mistral AI's model
//   - mixtral - Mixture of experts
//   - llava - Vision model
//   - nomic-embed-text - Embeddings
//
// # Architecture Note
//
// This provider uses two APIs:
//
//   - OpenAI-compatible API (/v1/) for chat, streaming, and embeddings
//   - Native Ollama API (/api/) for model management operations
//
// The OpenAI-compatible endpoint is used for inference as it provides a
// standardized interface, while the native API is used for Ollama-specific
// features like model pulling and detailed model information.
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package ollama
