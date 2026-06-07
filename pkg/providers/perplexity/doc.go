// Package perplexity provides a Perplexity AI LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Perplexity's OpenAI-compatible
// API, providing search-augmented generation with real-time web search integration.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - PERPLEXITY_API_KEY (primary)
//   - PPLX_API_KEY (alternative)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := perplexity.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "What are the latest news about AI?")
//
// # Supported Features
//
//   - Chat completions with web search integration
//   - Streaming responses
//   - Citation support in responses
//   - Search domain filtering
//
// # Configuration Options
//
//	client, err := perplexity.New(
//	    perplexity.WithAPIKey("..."),
//	    perplexity.WithModel("sonar"),
//	    perplexity.WithHTTPClient(customHTTPClient),
//	)
//
// # Popular Models
//
//   - sonar (search-enabled, default)
//   - sonar-pro (search-enabled)
//   - sonar-reasoning (search-enabled)
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package perplexity
