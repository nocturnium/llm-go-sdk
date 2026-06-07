// Package togetherai provides a TogetherAI LLM client using native HTTP.
//
// This package implements the llms.LLM interface for TogetherAI's OpenAI-compatible
// API, providing access to open-source models like Llama, Mixtral, and others.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - TOGETHER_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := togetherai.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions (Llama, Mixtral, Qwen, and many more)
//   - Streaming responses
//   - Function/tool calling
//   - JSON mode
//
// # Configuration Options
//
//	client, err := togetherai.New(
//	    togetherai.WithAPIKey("..."),
//	    togetherai.WithModel("meta-llama/Llama-3.3-70B-Instruct-Turbo"),
//	    togetherai.WithBaseURL("https://custom-endpoint.com"),
//	    togetherai.WithHTTPClient(customHTTPClient),
//	)
//
// # Popular Models
//
//   - meta-llama/Llama-3.3-70B-Instruct-Turbo (default)
//   - meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo
//   - meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo
//   - mistralai/Mixtral-8x7B-Instruct-v0.1
//   - Qwen/Qwen2.5-72B-Instruct-Turbo
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package togetherai
