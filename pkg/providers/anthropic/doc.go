// Package anthropic provides an Anthropic Claude LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Anthropic's Messages API,
// supporting Claude 3.5, Claude 3, and legacy Claude models.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - ANTHROPIC_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := anthropic.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions (all Claude models)
//   - Streaming responses
//   - Function/tool calling
//   - Vision (all Claude 3+ models)
//   - Extended thinking (Claude 3.5+)
//   - System prompts with caching
//
// # Configuration Options
//
//	client, err := anthropic.New(
//	    anthropic.WithAPIKey("sk-ant-..."),
//	    anthropic.WithModel("claude-sonnet-4-20250514"),
//	    anthropic.WithBaseURL("https://custom-endpoint.com"),
//	    anthropic.WithHTTPClient(customHTTPClient),
//	)
//
// # Default Model
//
// The default model is claude-sonnet-4-20250514. Override with WithModel.
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
//
// # Architecture Note
//
// Unlike providers such as OpenAI, Perplexity, and Fireworks that use the
// shared openaicompat.BaseProvider (since they all use OpenAI-compatible APIs),
// the Anthropic provider has its own implementation. This is because
// Anthropic's Messages API has a fundamentally different structure:
//
//   - Different endpoint paths (/messages instead of /chat/completions)
//   - Different request format (system prompt as separate field, not a message)
//   - Different SSE streaming format (content_block_start/delta events)
//   - Unique features like extended thinking and prompt caching
//   - Different content block structure for function calling
//
// The internal/anthropicapi package handles the API specifics, while this
// package provides the llms.LLM interface implementation.
package anthropic
