// Package llms provides a unified interface for interacting with various LLM providers.
//
// This package offers a consistent API across multiple LLM backends including OpenAI,
// Anthropic, Google Gemini, TogetherAI, and Featherless. It uses native Go HTTP calls
// with no external LLM dependencies.
//
// # API Reference Map
//
// The package surface is large; this map groups the exported API by role so you
// can find the right entry point quickly. (The middleware groups will move to
// dedicated subpackages in a future v3 — see docs/v3-package-taxonomy.md.)
//
//   - Core types: [Message], [Role], [Response], [StreamChunk], [Usage],
//     [ToolCall], [FunctionCall], [ContentPart], [FinishReason].
//   - Entry points: [Call] (one-shot prompt), the [LLM] interface
//     (GenerateContent / Stream), and [StreamText] / [CollectStream] for draining
//     a stream.
//   - Call options: [CallOption] builders such as [WithTemperature], [WithMaxTokens],
//     [WithTools], [WithReasoning], [WithJSONMode], [WithJSONSchema]; applied via [ApplyOptions].
//   - Tools & agent loop: [Tool], [NewFunctionTool], [ToolRegistry], [RunTools],
//     [ToolHandler], [ParseToolArguments].
//   - Structured output: [GenerateTyped] and the response-format helpers.
//   - Capabilities & models: [Capabilities], [ModelInfo], the [ModelType] constants,
//     and the runtime introspection helpers [SupportsEmbeddings]/[AsEmbedder],
//     [SupportsReranking]/[AsReranker], [SupportsModelListing]/[AsModelLister],
//     [AsCapableProvider].
//   - Construction & registry: [Config], [New], [NewFromEnv], [RegisterProvider],
//     [RegisteredProviders]. Prefer a provider's own constructor (e.g. openai.New)
//     when the provider is known at compile time.
//   - Cost & tokens: [CostTracker], [ModelPricing], [EstimateTokens], [TokenEstimator].
//   - Resilience middleware: [NewResilientClient], [NewFallbackChain], rate limiting.
//   - Observability middleware: [NewMetricsMiddleware], [NewOTelMiddleware], the
//     Langfuse exporters, and the slog/JSON logging middleware.
//   - Errors: the [Err]-prefixed sentinels (match with errors.Is), [APIError],
//     [IsTemporary], [GetErrorDetails].
//   - Provider-authoring toolkit (for implementing a new provider): [WrapProviderError],
//     [RequireAPIKey], [PrepareMessages], [NewStreamSender], [ValidateMessages].
//
// # Quick Start
//
// Create a provider client and make requests:
//
//	client, err := openai.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Simple call
//	response, err := llms.Call(ctx, client, "Hello, world!")
//
//	// Or with messages for more control
//	messages := []llms.Message{
//	    {Role: llms.RoleSystem, Content: "You are helpful."},
//	    {Role: llms.RoleUser, Content: "What is Go?"},
//	}
//	resp, err := client.GenerateContent(ctx, messages,
//	    llms.WithTemperature(0.7),
//	    llms.WithMaxTokens(1024),
//	)
//
// # Streaming
//
// Stream responses in real-time:
//
//	chunks, err := client.Stream(ctx, messages)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	text, err := llms.StreamText(chunks)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Print(text)
//
// Or consume chunks incrementally:
//
//	chunks, err := client.Stream(ctx, messages)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        log.Fatal(chunk.Error)
//	    }
//	    if chunk.Done {
//	        break
//	    }
//	    fmt.Print(chunk.Content)
//	}
//
// # Tool Calling
//
// Define and use tools:
//
//	tool := llms.NewFunctionTool("get_weather", "Get weather", params)
//	resp, err := client.GenerateContent(ctx, messages,
//	    llms.WithTools([]llms.Tool{tool}),
//	)
//	if len(resp.ToolCalls) > 0 {
//	    // Handle tool call
//	}
//
// # Providers
//
// Import the provider you need from its canonical pkg/providers path:
//
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/anthropic"
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/gemini"
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/togetherai"
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/featherless"
//
// Each provider reads its API key from environment variables by default.
// See provider documentation for configuration options.
//
// # Provider Capability Matrix
//
// The following matrix shows which features are supported by each provider:
//
//	| Provider    | Streaming | Tools | Vision | Embeddings | Reranking | JSON Mode | Model Listing |
//	|-------------|-----------|-------|--------|------------|-----------|-----------|---------------|
//	| OpenAI      | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Anthropic   | ✓         | ✓     | ✓      | ✗          | ✗         | ✗         | ✓             |
//	| Gemini      | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Groq        | ✓         | ✓     | ✓      | ✗          | ✗         | ✓         | ✓             |
//	| Mistral     | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| DeepSeek    | ✓         | ✓     | ✗      | ✗          | ✗         | ✓         | ✓             |
//	| Cerebras    | ✓         | ✓     | ✗      | ✗          | ✗         | ✓         | ✓             |
//	| Fireworks   | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| TogetherAI  | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Perplexity  | ✓         | ✗     | ✗      | ✗          | ✗         | ✓         | ✓             |
//	| Featherless | ✓         | ✓     | ✗      | ✗          | ✗         | ✓         | ✓             |
//	| Synthetic   | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Ollama      | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Azure       | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Infinity    | ✗         | ✗     | ✗      | ✓          | ✓         | ✗         | ✗             |
//
// Notes:
//   - Infinity is an embeddings-only service and does not implement the LLM interface
//   - Vision support may depend on the specific model being used
//   - Tool support may vary by model within a provider
//   - Use SupportsEmbeddings() and AsEmbedder() to check embedding capability at runtime
//   - Use SupportsReranking() and AsReranker() to check reranking capability at runtime
//   - Use SupportsModelListing() and AsModelLister() to check model listing capability at runtime
//   - Use AsCapableProvider() for capability introspection through middleware
package llms
