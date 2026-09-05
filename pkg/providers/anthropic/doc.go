// Package anthropic provides an Anthropic Claude LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Anthropic's Messages API,
// supporting the Claude 3.x, 4.x and 5.x families including Fable and Mythos.
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
//   - Vision (all Claude 3+ models; base64 and URL image sources)
//   - Extended thinking (budget_tokens on Claude <= 4.5, adaptive + effort on
//     4.6+, always-on with effort on Fable/Mythos)
//   - System prompts with caching
//   - JSON output (json_schema and json_object, via a forced tool)
//   - Provider-specific request fields via llms.WithExtraBody
//
// # Behavior Notes
//
// These are the places where this provider intentionally differs from the
// OpenAI-compatible and Gemini providers, or normalizes inputs to keep requests
// valid:
//
//   - The system prompt is cached by default (a cache_control breakpoint is
//     added to the system block). Use llms.WithoutCache to disable it.
//   - Usage.TotalTokens includes cache-read and cache-creation tokens, and
//     Usage.PromptTokens excludes them, matching the other providers.
//   - temperature and top_p are dropped for Opus 4.7/4.8, Opus 5, Sonnet 5 and
//     Fable/Mythos, which reject them. FrequencyPenalty and PresencePenalty have
//     no Anthropic equivalent and are never sent. Message.Name is not sent.
//   - Budget-based extended thinking (Claude <= 4.5) silently softens a forcing
//     tool_choice to "auto" and raises max_tokens above the budget. Fable 5.1
//     and Mythos reject a forcing tool_choice outright, so it is softened there
//     too. Adaptive thinking (4.6+) keeps a forcing tool_choice as given.
//   - JSON output is served through a forced tool; JSONSchema.Strict is ignored.
//     Requesting JSON disables budget-based thinking on Claude <= 4.5, because
//     those models reject thinking alongside a forced tool.
//   - A system message anywhere but messages[0] is a validation error, as on
//     the OpenAI-compatible providers.
//   - A streamed tool call with no arguments yields Arguments "{}".
//   - A stream that ends (EOF) after message_delta but before message_stop is
//     treated as a normal completion, as on the other providers. An EOF before
//     message_delta is reported as a StreamError (truncated stream).
//   - Encrypted redacted_thinking blocks are carried on
//     ReasoningContent.Metadata and replayed verbatim on the next turn.
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
