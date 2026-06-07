// Package groq provides a Groq LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Groq's OpenAI-compatible
// API, providing ultra-fast inference powered by LPU (Language Processing Unit) hardware.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - GROQ_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := groq.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions (Llama, Mixtral, Gemma models)
//   - Streaming responses
//   - Function/tool calling
//   - JSON mode
//   - Ultra-low latency (< 100ms for small prompts)
//
// # Configuration Options
//
//	client, err := groq.New(
//	    groq.WithAPIKey("..."),
//	    groq.WithModel("llama-3.3-70b-versatile"),
//	    groq.WithHTTPClient(customHTTPClient),
//	)
//
// # Popular Models
//
//   - llama-3.3-70b-versatile (default)
//   - llama-3.1-8b-instant
//   - mixtral-8x7b-32768
//   - gemma2-9b-it
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package groq
