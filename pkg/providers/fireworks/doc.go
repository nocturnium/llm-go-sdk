// Package fireworks provides a Fireworks AI LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Fireworks AI's OpenAI-compatible
// API, providing fast inference with function calling and serverless deployment.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - FIREWORKS_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := fireworks.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions (Llama, Mixtral, Qwen models)
//   - Streaming responses
//   - Function/tool calling
//   - JSON mode
//   - Fine-tuned model support
//   - Speculative decoding
//
// # Configuration Options
//
//	client, err := fireworks.New(
//	    fireworks.WithAPIKey("..."),
//	    fireworks.WithModel("accounts/fireworks/models/llama-v3p1-70b-instruct"),
//	    fireworks.WithHTTPClient(customHTTPClient),
//	)
//
// # Popular Models
//
//   - accounts/fireworks/models/llama-v3p1-70b-instruct (default)
//   - accounts/fireworks/models/llama-v3p1-405b-instruct
//   - accounts/fireworks/models/mixtral-8x22b-instruct
//   - accounts/fireworks/models/qwen2p5-72b-instruct
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package fireworks
