// Package synthetic provides a Synthetic.new LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Synthetic.new's OpenAI-compatible
// API, a privacy-focused platform providing access to open-source coding LLMs.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - SYNTHETIC_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := synthetic.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions (Qwen, GLM, Kimi, DeepSeek, and others)
//   - Streaming responses
//   - Function/tool calling
//   - JSON mode
//   - Privacy-focused (no data retention)
//
// # Configuration Options
//
//	client, err := synthetic.New(
//	    synthetic.WithAPIKey("..."),
//	    synthetic.WithModel("Qwen/Qwen3-32B"),
//	    synthetic.WithBaseURL("https://custom-endpoint.com"),
//	    synthetic.WithHTTPClient(customHTTPClient),
//	)
//
// # Available Models
//
// Synthetic.new specializes in coding-focused LLMs:
//   - Qwen (various sizes)
//   - GLM (ChatGLM)
//   - Kimi (Moonshot)
//   - DeepSeek
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package synthetic
