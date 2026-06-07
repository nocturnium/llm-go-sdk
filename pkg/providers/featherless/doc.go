// Package featherless provides a Featherless.ai LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Featherless.ai's OpenAI-compatible
// API, providing access to thousands of open-source models with serverless inference.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - FEATHERLESS_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := featherless.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions (thousands of Hugging Face models)
//   - Streaming responses
//   - Function/tool calling
//   - JSON mode
//   - Serverless model inference (models load on-demand)
//
// # Configuration Options
//
//	client, err := featherless.New(
//	    featherless.WithAPIKey("..."),
//	    featherless.WithModel("Qwen/Qwen3-32B"),
//	    featherless.WithBaseURL("https://custom-endpoint.com"),
//	    featherless.WithHTTPClient(customHTTPClient),
//	)
//
// # Default Model
//
// The default model is Qwen/Qwen3-32B. Override with WithModel.
// Browse available models at https://featherless.ai/models
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package featherless
