// Package cerebras provides a Cerebras LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Cerebras' OpenAI-compatible
// API, providing wafer-scale inference with extremely fast generation speeds.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - CEREBRAS_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := cerebras.New()
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
//   - Wafer-scale inference for fast generation
//
// # Configuration Options
//
//	client, err := cerebras.New(
//	    cerebras.WithAPIKey("..."),
//	    cerebras.WithModel("qwen-3.8-27b"),
//	    cerebras.WithHTTPClient(customHTTPClient),
//	)
//
// # Popular Models
//
//   - gpt-oss-120b (default)
//   - qwen-3.8-27b
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package cerebras
