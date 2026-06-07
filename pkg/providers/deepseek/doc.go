// Package deepseek provides a DeepSeek LLM client using native HTTP.
//
// This package implements the llms.LLM interface for DeepSeek's OpenAI-compatible
// API, providing access to code-specialized and reasoning models at competitive pricing.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - DEEPSEEK_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := deepseek.New()
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
//   - Code generation specialization
//   - Chain-of-thought reasoning
//   - Very competitive pricing
//
// # Configuration Options
//
//	client, err := deepseek.New(
//	    deepseek.WithAPIKey("..."),
//	    deepseek.WithModel("deepseek-chat"),
//	    deepseek.WithHTTPClient(customHTTPClient),
//	)
//
// # Popular Models
//
//   - deepseek-chat (default)
//   - deepseek-coder
//   - deepseek-reasoner
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package deepseek
