// Package mistral provides a Mistral AI LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Mistral AI's OpenAI-compatible
// API, providing access to European-hosted open-weight models.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - MISTRAL_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := mistral.New()
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
//   - Function/tool calling
//   - JSON mode
//   - Code-specialized models (Codestral)
//   - Embeddings
//   - European data residency
//
// # Configuration Options
//
//	client, err := mistral.New(
//	    mistral.WithAPIKey("..."),
//	    mistral.WithModel("mistral-large-latest"),
//	    mistral.WithHTTPClient(customHTTPClient),
//	)
//
// # Popular Models
//
//   - mistral-large-latest (default)
//   - mistral-medium-latest
//   - mistral-small-latest
//   - codestral-latest
//   - open-mistral-nemo
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package mistral
