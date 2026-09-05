# llm-go-sdk

[![Go Reference](https://pkg.go.dev/badge/github.com/nocturnium/llm-go-sdk/v6.svg)](https://pkg.go.dev/github.com/nocturnium/llm-go-sdk/v6)
[![CI](https://github.com/nocturnium/llm-go-sdk/actions/workflows/ci.yml/badge.svg)](https://github.com/nocturnium/llm-go-sdk/actions/workflows/ci.yml)
[![CodeQL](https://github.com/nocturnium/llm-go-sdk/actions/workflows/codeql.yml/badge.svg)](https://github.com/nocturnium/llm-go-sdk/actions/workflows/codeql.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/nocturnium/llm-go-sdk/v6)](https://goreportcard.com/report/github.com/nocturnium/llm-go-sdk/v6)
[![Go Version](https://img.shields.io/github/go-mod/go-version/nocturnium/llm-go-sdk)](https://github.com/nocturnium/llm-go-sdk/blob/main/go.mod)

> A unified, dependency-light Go SDK for **21 AI providers** — streaming, tool calling,
> vision, embeddings, and built-in resilience over the standard `net/http`.

One `LLM` interface across chat providers. Switch from OpenAI to Anthropic to a local
Ollama server by changing a single import and constructor — everything else
(streaming, tools, retries, fallback, cost tracking, tracing) stays the same.

## Features

- **Unified interface** — a single `LLM` interface works across chat providers
- **Native HTTP** — zero external LLM SDK dependencies, built on `net/http`
- **Streaming** — real-time token streaming over channels
- **Tool / function calling** — consistent tool-calling API across providers, plus an
  automatic `RunTools` agent loop
- **MCP client** — connect to Model Context Protocol servers (`pkg/mcp`) and use their tools
- **Reasoning** — cross-provider thinking controls (`WithReasoningEffort`, `WithReasoningBudget`)
- **Structured outputs** — typed JSON via JSON Schema (`GenerateTyped[T]`, `WithJSONSchema`)
- **Vision** — multi-modal image input (PNG, JPEG, GIF, WebP)
- **Embeddings & reranking** — for semantic search and RAG
- **Prompt caching** — cross-provider caching (`WithCache`) with discounted cache-token cost accounting
- **Web search** — native provider search plus external Brave / Tavily backends
- **Cost tracking** — token usage and cost estimation with built-in pricing
- **Resilience** — circuit breaker, retries with backoff, rate limiting, fallback chains
- **Observability** — OpenTelemetry (GenAI semantic conventions) and Langfuse
- **Functional options** — clean, composable configuration

## 60-Second Quickstart

```bash
go get github.com/nocturnium/llm-go-sdk/v6
export OPENAI_API_KEY="sk-..."   # or set the env var for any other provider
```

```go
package main

import (
	"context"
	"fmt"
	"log"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

func main() {
	// Reads OPENAI_API_KEY (falls back to LLM_API_KEY) from the environment.
	client, err := openai.New(
		openai.WithModel("gpt-4o"), // optional; gpt-4o is the default
	)
	if err != nil {
		log.Fatal(err)
	}

	// Simplest one-liner: prompt in, text out.
	answer, err := llms.Call(context.Background(), client, "Name three Go web frameworks.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(answer)

	// Full control: messages in, detailed Response out.
	resp, err := client.GenerateContent(context.Background(),
		[]llms.Message{
			{Role: llms.RoleSystem, Content: "You are a concise assistant."},
			{Role: llms.RoleUser, Content: "Name three Go web frameworks."},
		},
		llms.WithTemperature(0.7),
		llms.WithMaxTokens(256),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Content)
	fmt.Printf("tokens: %d prompt + %d completion\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
}
```

Switching providers is a one-line import + constructor change (e.g. `anthropic.New()`,
`gemini.New()`, `groq.New()`) — every chat provider implements the same `llms.LLM` interface.

### Construct by name

You can also build a provider from a string name with `llms.New(name, llms.Config{...})`,
or entirely from the environment with `llms.NewFromEnv()` (which reads `LLM_PROVIDER` and
optional `LLM_MODEL`). Blank-import the `all` package once to register every chat provider's
factory:

```go
import (
	llms "github.com/nocturnium/llm-go-sdk/v6"
	_ "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/all" // registers the 18 auto-registered chat providers
)

// By name. llms.Config carries the common construction settings.
client, err := llms.New("anthropic", llms.Config{
	Model: "claude-sonnet-4-20250514",
})

// Entirely from env: LLM_PROVIDER (required) + LLM_MODEL (optional),
// with the provider's own API-key env var resolved automatically.
client, err = llms.NewFromEnv()

// Provider-specific construction params that have no common field go in Extra:
client, err = llms.New("runpod", llms.Config{Extra: map[string]string{"endpoint_id": "abc123"}})
client, err = llms.New("zai", llms.Config{Extra: map[string]string{"coding": "true"}})
```

`llms.Config` fields: `APIKey`, `Model`, `BaseURL`, `Timeout`, `AllowPrivateIPs`,
`AllowHTTP` (independent of `AllowPrivateIPs` — a private plain-HTTP endpoint
needs both), `HTTPClient`, and `Extra map[string]string` for provider-specific keys.

## Installation

```bash
go get github.com/nocturnium/llm-go-sdk/v6
```

Requires **Go 1.25+**.

## Supported Providers

The SDK ships **21 providers** (18 chat-registered; HuggingFace, Infinity and ElevenLabs are
direct-construct — HuggingFace serves chat or embeddings per its deployed model,
Infinity serves embeddings/reranking and ElevenLabs serves media only).
Import each from its canonical path
`github.com/nocturnium/llm-go-sdk/v6/pkg/providers/<name>`. Every chat provider also
falls back to `LLM_API_KEY` if its own key var is unset. "OpenAI-compatible"
providers share the `pkg/openaicompat` base (they speak OpenAI's `/chat/completions`
schema); "Native" providers implement a provider-specific wire format.

| Provider | Canonical import (`pkg/providers/<name>`) | Auth / host env var(s) | API style | Notable models / capabilities |
|----------|-------------------------------------------|------------------------|-----------|-------------------------------|
| OpenAI | `pkg/providers/openai` | `OPENAI_API_KEY` | Native (OpenAI) | `gpt-4o` (default); chat, streaming, tools, vision, JSON mode, embeddings (`text-embedding-3-*`), custom base URL |
| ElevenLabs | `pkg/providers/elevenlabs` | `ELEVENLABS_API_KEY` | Native media | Speech, Scribe transcription, SFX/music, Pro-plan Flows image/video; direct-construct, no chat |
| OpenRouter | `pkg/providers/openrouter` | `OPENROUTER_API_KEY` | OpenAI-compatible chat + native media | `google/gemini-3.5-flash-lite` (default); images, speech, transcription, async video, model discovery |
| Anthropic | `pkg/providers/anthropic` | `ANTHROPIC_API_KEY` | **Native** (Messages API) | `claude-sonnet-4-20250514` (default); chat, streaming, tools, vision, extended thinking, prompt caching (`cache_control`) |
| Gemini | `pkg/providers/gemini` | `GEMINI_API_KEY`, `GOOGLE_API_KEY` | **Native** (Gemini API) | `gemini-2.5-flash` (default); chat, streaming, tools, vision, embeddings (`text-embedding-004`), JSON mode, safety settings |
| Azure OpenAI | `pkg/providers/azure` | `AZURE_OPENAI_API_KEY` or `AZURE_OPENAI_KEY`; `AZURE_OPENAI_ENDPOINT`; `AZURE_OPENAI_DEPLOYMENT` | OpenAI-compatible | Deployment-based; chat, streaming, tools, JSON mode, embeddings; API version default `2024-02-15-preview`; enterprise/PTU/content filtering |
| Groq | `pkg/providers/groq` | `GROQ_API_KEY` | OpenAI-compatible | `llama-3.3-70b-versatile` (default); LPU ultra-low-latency; Llama/Mixtral/Gemma; tools, JSON mode |
| Cerebras | `pkg/providers/cerebras` | `CEREBRAS_API_KEY` | OpenAI-compatible | `llama3.1-70b` (default), `llama3.1-8b`; wafer-scale fast inference; tools, JSON mode |
| DeepSeek | `pkg/providers/deepseek` | `DEEPSEEK_API_KEY` | OpenAI-compatible | `deepseek-chat` (default), `deepseek-coder`, `deepseek-reasoner`; tools, JSON mode, chain-of-thought |
| Mistral | `pkg/providers/mistral` | `MISTRAL_API_KEY` | OpenAI-compatible | `mistral-large-latest` (default), `codestral-latest`, `open-mistral-nemo`; tools, JSON mode, embeddings; EU data residency |
| Fireworks AI | `pkg/providers/fireworks` | `FIREWORKS_API_KEY` | OpenAI-compatible | `accounts/fireworks/models/llama-v3p1-70b-instruct` (default); Llama/Mixtral/Qwen; tools, JSON mode, speculative decoding, fine-tunes |
| TogetherAI | `pkg/providers/togetherai` | `TOGETHER_API_KEY` | OpenAI-compatible | `meta-llama/Llama-3.3-70B-Instruct-Turbo` (default); Llama/Mixtral/Qwen; tools, JSON mode |
| Featherless.ai | `pkg/providers/featherless` | `FEATHERLESS_API_KEY` | OpenAI-compatible | `Qwen/Qwen3-32B` (default); thousands of Hugging Face models, serverless on-demand load; tools, JSON mode |
| Synthetic.new | `pkg/providers/synthetic` | `SYNTHETIC_API_KEY` | OpenAI-compatible | `hf:Qwen/Qwen3-Coder-480B-A35B-Instruct` (default); Qwen/GLM/Kimi/DeepSeek coding models; tools, JSON mode |
| Perplexity | `pkg/providers/perplexity` | `PERPLEXITY_API_KEY`, `PPLX_API_KEY` | OpenAI-compatible | `sonar` (default); `sonar-pro`, `sonar-reasoning`; search-augmented generation, citations, domain filtering |
| Z.AI | `pkg/providers/zai` | `ZAI_API_KEY` | OpenAI-compatible | `glm-4.7` (default), `glm-4.7-FlashX`, `glm-4.7-Flash`; 200K context / 128K output; tools, vision, JSON mode, native web search, coding endpoint via `WithUseCodingAPI` |
| RunPod | `pkg/providers/runpod` | `RUNPOD_API_KEY` (+ `WithEndpointID`) | OpenAI-compatible (vLLM) | Serverless vLLM endpoints (`https://api.runpod.ai/v2/<id>/openai/v1`); model set by deployment; chat, streaming, tools (model-dependent) |
| Ollama | `pkg/providers/ollama` | `OLLAMA_HOST` (default `http://localhost:11434`), `OLLAMA_API_KEY` (optional) | OpenAI-compatible + native mgmt API | `llama3.2` (default); local inference; chat, streaming, tools (model-dependent), vision (`llava`), embeddings (`nomic-embed-text`); pull/list/show/delete management |
| llama.cpp | `pkg/providers/llamacpp` | `LLAMA_CPP_HOST` (default `http://localhost:8080`), `LLAMA_CPP_API_KEY` (optional) | OpenAI-compatible + native server API | Model discovered from `/props`; local inference; chat, streaming, tools (model-dependent), grammar JSON mode, vision (LLaVA), embeddings; `/health`, `/slots`, `/props` |
| Infinity | `pkg/providers/infinity` | `INFINITY_API_KEY` (optional), `WithBaseURL` (default `http://localhost:7997/v1`) | OpenAI-compatible (embeddings) | **Embeddings + reranking only — does NOT implement chat/`llms.LLM`**; implements `llms.Embedder` + `llms.Reranker`; default embed `michaelfeil/bge-small-en-v1.5`, default rerank `mixedbread-ai/mxbai-rerank-xsmall-v1` |

> **Note:** Infinity is an embeddings/reranking provider only — it does not implement
> the chat `llms.LLM` interface.

## Environment Variables

For chat providers, `ResolveAPIKey` / `RequireAPIKey` check the provider-specific
var(s) in order, then fall back to `LLM_API_KEY`. Any key can also be set explicitly
via the provider's `WithAPIKey(...)` option. Copy [`.env.example`](./.env.example) to
`.env` and fill in only what you use.

| Variable | Purpose | Used by |
|----------|---------|---------|
| `OPENAI_API_KEY` | OpenAI API key | OpenAI |
| `OPENROUTER_API_KEY` | OpenRouter API key | OpenRouter |
| `ELEVENLABS_API_KEY` | ElevenLabs API key | ElevenLabs |
| `ANTHROPIC_API_KEY` | Anthropic API key | Anthropic |
| `GEMINI_API_KEY` / `GOOGLE_API_KEY` | Google Gemini API key (either accepted) | Gemini |
| `GROQ_API_KEY` | Groq API key | Groq |
| `CEREBRAS_API_KEY` | Cerebras API key | Cerebras |
| `DEEPSEEK_API_KEY` | DeepSeek API key | DeepSeek |
| `MISTRAL_API_KEY` | Mistral AI API key | Mistral |
| `FIREWORKS_API_KEY` | Fireworks AI API key | Fireworks |
| `TOGETHER_API_KEY` | TogetherAI API key | TogetherAI |
| `FEATHERLESS_API_KEY` | Featherless.ai API key | Featherless |
| `SYNTHETIC_API_KEY` | Synthetic.new API key | Synthetic |
| `PERPLEXITY_API_KEY` / `PPLX_API_KEY` | Perplexity API key (either accepted) | Perplexity |
| `ZAI_API_KEY` | Z.AI API key | Z.AI |
| `RUNPOD_API_KEY` | RunPod API key | RunPod |
| `AZURE_OPENAI_API_KEY` / `AZURE_OPENAI_KEY` | Azure OpenAI API key (either accepted) | Azure |
| `AZURE_OPENAI_ENDPOINT` | Azure resource endpoint URL | Azure |
| `AZURE_OPENAI_DEPLOYMENT` | Azure deployment name | Azure |
| `INFINITY_API_KEY` | Infinity server API key (optional) | Infinity |
| `OLLAMA_HOST` | Ollama server URL (default `http://localhost:11434`) | Ollama |
| `OLLAMA_API_KEY` | Ollama API key (optional) | Ollama |
| `LLAMA_CPP_HOST` | llama.cpp server URL (default `http://localhost:8080`) | llama.cpp |
| `LLAMA_CPP_API_KEY` | llama.cpp API key (optional) | llama.cpp |
| `BRAVE_API_KEY` / `TAVILY_API_KEY` | External web-search backends | Web search |
| `LLM_API_KEY` | Generic fallback key for any chat provider | All (fallback) |
| `LLM_DEBUG_REQUESTS` | Set to log raw HTTP requests (never use in production) | Debug |

## Package Layout

The SDK is a single Go module (`github.com/nocturnium/llm-go-sdk/v6`, Go 1.25+) with a
small, deliberately flat public surface. The **core lives in the root package**
(`package llms`): the `LLM` interface, all shared types and options, errors,
streaming, and the cost + response-caching middleware. The other public packages
are the provider packages under `pkg/providers/*`, the custom-provider base in
`pkg/openaicompat`, the MCP client in `pkg/mcp`, the resilience middleware in
`pkg/middleware/resilience`, the observability middleware in `pkg/observability`,
and the standalone tokenizer in `pkg/tokenizer`.
Everything else lives under `internal/` and is not importable by external code.

| Location | Role |
|----------|------|
| Root (`llms "github.com/nocturnium/llm-go-sdk/v6"`) | The core: `LLM` interface, `Message`/`Response`/`Tool` types, options, errors, streaming, cost-tracking and response-caching middleware, capability registry |
| `pkg/providers/<name>` | **Provider implementations** (21 providers; 18 chat-registered) |
| `pkg/openaicompat` | Shared OpenAI-compatible base client; the base for building custom providers |
| `pkg/mcp` | Model Context Protocol client (`tools/list`, `tools/call`) |
| `pkg/middleware/resilience` | Resilience middleware: retry, circuit breaker, fallback chain, rate limiting |
| `pkg/observability` | Observability middleware: OTel tracing, metrics, Langfuse exporters, slog/JSON logging |
| `pkg/tokenizer` | Standalone token estimation |
| `internal/*` | Non-public building blocks: `httpclient`, `anthropicapi`, `geminiapi`, `ollamaapi`, `llamacppapi`, `websearch`, `testutil` |
| `cmd/` | `llms-cli`, the shipping command-line tool |

```
llm-go-sdk/
├── *.go                      # Root package `llms` (core types, interface, middleware)
│   ├── llms.go               #   LLM interface + Provider constants
│   ├── message.go vision.go  #   messages / multi-modal
│   ├── embeddings.go         #   Embedder / Reranker interfaces
│   ├── tools.go options.go   #   tool calling / call options
│   ├── cost.go caching.go    #   cost-tracking + response-caching middleware
│   └── capabilities_registry.go errors.go apikey.go
├── pkg/
│   ├── openaicompat/                                # shared OpenAI-compatible base
│   ├── mcp/                                         # Model Context Protocol client
│   ├── tokenizer/                                   # standalone token estimation
│   ├── middleware/resilience/                       # retry / circuit breaker / fallback / rate limit
│   │   └── resilience*.go ratelimit*.go fallback*.go
│   ├── observability/                               # OTel / metrics / Langfuse / logging
│   │   └── otel*.go metrics*.go langfuse*.go logging*.go
│   └── providers/<name>/                            # provider impls (21)
├── internal/                 # httpclient + per-provider API adapters (non-public)
├── cmd/                      # llms-cli CLI
└── examples/                 # runnable examples
```

For a deep dive into the design — the interface, the middleware/decorator chain, how
providers and the capability registry work, and the observability stack — see
[docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md). A short package-layout reference also
lives in [docs/migration-guide.md](./docs/migration-guide.md).

### Import Style

There is one import style: import the root package for the shared types and options,
and import the provider you need from `pkg/providers/<name>`.

```go
import llms "github.com/nocturnium/llm-go-sdk/v6"
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"

// Use types directly
messages := []llms.Message{ /* ... */ }
```

## Examples

See [examples/](./examples/) for runnable programs:

- **basic** — simple chat completion
- **streaming** — real-time streaming responses
- **tools** — function / tool calling
- **reasoning** — reasoning / "thinking" models
- **caching** — cross-provider prompt caching
- **mcp** — Model Context Protocol tools via `RunTools`
- **vision** — image analysis
- **embeddings** — text embeddings
- **resilience** — circuit breakers and retry
- **fallback** — multi-provider failover
- **cost-tracking** — token usage and cost estimation
- **new-style** — the root + `pkg/providers/<name>` import style

## API Reference

### LLM Interface

All chat providers implement the `llms.LLM` interface:

```go
type LLM interface {
    // GenerateContent provides full control over messages and returns a detailed response
    GenerateContent(ctx context.Context, messages []Message, options ...CallOption) (*Response, error)

    // Stream returns chunks via channel for real-time streaming
    Stream(ctx context.Context, messages []Message, options ...CallOption) (<-chan StreamChunk, error)

    // Provider returns the provider type (e.g., "openai", "anthropic")
    Provider() Provider

    // Model returns the model name
    Model() string
}
```

For the common "single prompt in, text out" case, use the package helper
`llms.Call(ctx, client, prompt, opts...)` rather than a method on the client:

```go
text, err := llms.Call(ctx, client, "Hello, world!", llms.WithMaxTokens(64))
```

### Messages

```go
messages := []llms.Message{
    {Role: llms.RoleSystem, Content: "You are a helpful assistant."},
    {Role: llms.RoleUser, Content: "What is Go?"},
}

resp, err := client.GenerateContent(ctx, messages)
```

Available roles: `RoleSystem`, `RoleUser`, `RoleAssistant`, `RoleTool`

Message accessors drop the `Get` prefix: `msg.Text()` returns the message's text
(from either `Content` or text `Parts`), and `msg.Images()` returns its image parts.

### Call Options

```go
resp, err := client.GenerateContent(ctx, messages,
    llms.WithTemperature(0.7),
    llms.WithMaxTokens(2048),
    llms.WithTopP(0.9),
    llms.WithModel("gpt-4o-mini"),  // Override default model
    llms.WithJSONMode(),            // Enable JSON-object output
    llms.WithStopWords([]string{"\n\n"}),
)
```

| Option | Description |
|--------|-------------|
| `WithTemperature(float64)` | Sampling temperature (0.0-2.0). `0` is honored as a request for deterministic output; when the option is omitted the provider's own default temperature is used (the SDK does not force a value). |
| `WithMaxTokens(int)` | Maximum output tokens. When omitted, the model's provider default applies (Anthropic still sends an explicit limit). |
| `WithTopP(float64)` | Nucleus sampling threshold |
| `WithFrequencyPenalty(float64)` | Frequency penalty (-2.0 to 2.0) |
| `WithPresencePenalty(float64)` | Presence penalty (-2.0 to 2.0) |
| `WithStopWords([]string)` | Stop sequences |
| `WithModel(string)` | Override the default model |
| `WithJSONMode()` | Enable JSON-object output mode |
| `WithJSONSchema(name, schema, strict)` | Constrain output to a JSON Schema (see [Structured Outputs](#structured-outputs)) |
| `WithTools([]Tool)` | Enable tool/function calling |
| `WithToolChoiceAuto()` | Let the model decide whether to call a tool (default) |
| `WithToolChoiceNone()` | Prevent the model from calling tools |
| `WithToolChoiceRequired()` | Force the model to call some tool |
| `WithToolChoiceTool(name)` | Force the model to call the named tool |
| `WithReasoningEffort(ReasoningEffort)` | Request reasoning at a qualitative effort level (see [Reasoning](docs/guides/reasoning.md)) |
| `WithReasoningBudget(int)` | Cap reasoning ("thinking") tokens (Anthropic/Gemini) |
| `WithReasoning(ReasoningConfig)` | Full reasoning configuration |
| `WithCache()` / `WithCacheTTL(d)` / `WithoutCache()` | Control prompt caching (see [Prompt Caching](docs/guides/caching.md)) |
| `WithTrace(TraceOptions)` | Attach per-call trace context (trace/user/session IDs, tags, metadata) |

### Response

```go
type Response struct {
    Content       string            // Generated text
    Reasoning     *ReasoningContent // Reasoning/chain-of-thought (nil if unsupported)
    FinishReason  FinishReason      // "stop", "length", "tool_calls", "content_filter"
    Usage         Usage             // Token usage statistics
    ToolCalls     []ToolCall        // Tool calls requested by the model
    SearchResults []SearchResult    // Web search results (when requested)
}

type Usage struct {
    PromptTokens        int // Input tokens billed at the standard rate (excludes cache tokens)
    CompletionTokens    int // Output tokens (includes ReasoningTokens)
    TotalTokens         int
    CacheReadTokens     int // Prompt tokens read from cache (discounted)
    CacheCreationTokens int // Prompt tokens written to cache
    ReasoningTokens     int // Reasoning tokens (subset of CompletionTokens), when reported
}
```

## Streaming

The stream channel always closes with a terminal chunk: either one with `Done == true`
(success) or one with `Error != nil` (failure). You can simply range over the channel —
it is guaranteed to end — and check for those two cases:

```go
chunks, err := client.Stream(ctx, messages, llms.WithMaxTokens(1000))
if err != nil {
    log.Fatal(err)
}

for chunk := range chunks {
    if chunk.Error != nil {
        log.Fatal(chunk.Error)
    }
    if chunk.Done {
        fmt.Printf("\n[Finish: %s, Tokens: %d]\n",
            chunk.FinishReason, chunk.Usage.TotalTokens)
        break
    }
    fmt.Print(chunk.Content)
}
```

```go
type StreamChunk struct {
    Content      string            // Text content in this chunk
    Reasoning    *ReasoningContent // Reasoning content in this chunk (if any)
    ToolCalls    []ToolCall        // Tool calls (accumulated, sent on final chunk)
    FinishReason FinishReason      // Set on final chunk
    Usage        *Usage            // Token usage (final chunk only, if available)
    Error        error             // Error if streaming failed
    Done         bool              // True for the final chunk
}
```

## Tool Calling

```go
weatherTool := llms.NewFunctionTool(
    "get_weather",
    "Get the current weather for a location",
    map[string]any{
        "type": "object",
        "properties": map[string]any{
            "location": map[string]any{
                "type":        "string",
                "description": "City and state, e.g. San Francisco, CA",
            },
        },
        "required": []string{"location"},
    },
)

resp, err := client.GenerateContent(ctx, messages,
    llms.WithTools([]llms.Tool{weatherTool}),
)

if len(resp.ToolCalls) > 0 {
    tc := resp.ToolCalls[0]
    fmt.Printf("Tool: %s, Args: %s\n", tc.Function.Name, tc.Function.Arguments)

    // Execute the tool, then continue the conversation
    messages = append(messages,
        llms.Message{Role: llms.RoleAssistant, ToolCalls: resp.ToolCalls},
        llms.Message{
            Role:       llms.RoleTool,
            Content:    `{"temperature": 72, "condition": "sunny"}`,
            ToolCallID: tc.ID,
            Name:       tc.Function.Name,
        },
    )

    resp, err = client.GenerateContent(ctx, messages,
        llms.WithTools([]llms.Tool{weatherTool}),
    )
}
```

Control tool selection with the typed tool-choice options:

```go
llms.WithToolChoiceAuto()            // Model decides (default)
llms.WithToolChoiceNone()            // Never call tools
llms.WithToolChoiceRequired()        // Must call some tool
llms.WithToolChoiceTool("get_weather") // Must call this specific tool
```

Find tool calls on the response with the accessors (no `Get` prefix):
`resp.ToolCall(id)` returns the call with a given ID, and `resp.ToolCallByName(name)`
returns the first call to a named function (both return `nil` if not present).

### Automatic tool loop (RunTools)

`llms.RunTools` runs the full model → tool → model agent loop for you. Register your
tools and their handlers in a `ToolRegistry`, then let `RunTools` drive the conversation
until the model stops asking for tools:

```go
registry := llms.NewToolRegistry()

weatherTool := llms.NewFunctionTool("get_weather", "Get the weather", map[string]any{
    "type":       "object",
    "properties": map[string]any{"location": map[string]any{"type": "string"}},
    "required":   []string{"location"},
})

type weatherArgs struct {
    Location string `json:"location"`
}
llms.RegisterFunc(registry, weatherTool, func(args weatherArgs) (any, error) {
    return map[string]any{"location": args.Location, "temperature": 72}, nil
})

final, transcript, err := llms.RunTools(ctx, client,
    []llms.Message{{Role: llms.RoleUser, Content: "Weather in SF and NYC?"}},
    registry,
    llms.WithMaxIterations(5),
    llms.WithOnStep(func(iteration int, resp *llms.Response) {
        log.Printf("step %d: %d tool calls", iteration, len(resp.ToolCalls))
    }),
)
if err != nil {
    // If the loop hits the iteration guard, err wraps llms.ErrMaxIterations.
    log.Fatal(err)
}

fmt.Println(final.Content)               // final assistant message
fmt.Printf("%d messages\n", len(transcript)) // full conversation, including tool results
```

`RunTools(ctx, llm, messages, registry, opts...)` returns the final `*Response`, the
complete `transcript []Message` (the input messages plus every assistant/tool message it
appended), and an error. Tool calls within a turn run concurrently; tool-result messages
are still appended in request order. Options: `WithMaxIterations(n)` (default 10),
`WithOnStep(fn)`, and `WithToolConcurrency(n)` (default 8).

## Structured Outputs

Ask for typed JSON output constrained by a JSON Schema. The generic helper
`llms.GenerateTyped[T]` derives a schema from `T`, sends it as the response format, and
unmarshals the reply into a value of type `T`:

```go
type Recipe struct {
    Name        string   `json:"name"`
    Ingredients []string `json:"ingredients"`
    Minutes     int      `json:"minutes"`
}

recipe, resp, err := llms.GenerateTyped[Recipe](ctx, client,
    []llms.Message{{Role: llms.RoleUser, Content: "Give me a pancake recipe."}},
)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("%s (%d min)\n", recipe.Name, recipe.Minutes)
_ = resp // the raw *Response is also returned (usage, finish reason, etc.)
```

To drive the schema yourself, build it with `llms.SchemaFrom[T]()` and pass it via
`llms.WithJSONSchema(name, schema, strict)`:

```go
schema, _ := llms.SchemaFrom[Recipe]()
resp, err := client.GenerateContent(ctx, messages,
    llms.WithJSONSchema("Recipe", schema, true), // name, schema, strict
)
```

For looser JSON without a schema, `llms.WithJSONMode()` requests a JSON object.

## Vision / Multi-Modal

```go
// From URL
msg := llms.NewImageMessage("What's in this image?", "https://example.com/photo.jpg")

// From file
msg, err := llms.NewImageFileMessage("Describe this diagram", "./diagram.png")

// Multiple parts
msg := llms.NewMultiPartMessage(llms.RoleUser,
    llms.NewTextPart("Compare these two images:"),
    llms.NewImageURLPart("https://example.com/image1.jpg"),
    llms.NewImageURLPart("https://example.com/image2.jpg"),
)

resp, err := client.GenerateContent(ctx, []llms.Message{msg})
```

**Supported formats:** PNG, JPEG, GIF, WebP (max 20MB).

## Embeddings

Generate vector embeddings (supported by OpenAI, Gemini, Mistral, Ollama, llama.cpp,
and the Infinity provider). `Embedder` is a single-method interface (`Embed`); the
query/document convenience functions are **package-level** and return `[]float32` /
`[][]float32`:

```go
if embedder, ok := llms.AsEmbedder(client); ok {
    // Package-level helpers (NOT methods on the client).
    vector, err := llms.EmbedQuery(ctx, embedder, "search query")    // []float32

    vectors, err := llms.EmbedDocuments(ctx, embedder, []string{     // [][]float32
        "First document",
        "Second document",
    })

    // Full control via the Embed method; resp.Embeddings[i].Vector is []float32.
    resp, err := embedder.Embed(ctx, texts,
        llms.WithEmbedModel("text-embedding-3-small"),
        llms.WithDimensions(512),
    )
}
```

To look up a model's metadata, use `client.ModelInfo(ctx, id)` on a provider that
implements `llms.ModelLister`; an unknown ID returns `llms.ErrModelNotFound`.

## Cost Tracking

Every middleware wraps an `llms.LLM` and returns a value that also satisfies `llms.LLM`,
so hold the client as `llms.LLM` and reassign it as you add layers:

```go
base, _ := openai.New()
var client llms.LLM = base

tracker := llms.NewCostTracker()
client = llms.NewCostMiddleware(client, tracker)

resp, _ := client.GenerateContent(ctx, messages)
_ = resp

fmt.Printf("Total cost: %s\n", llms.FormatCost(tracker.GetTotalCost()))

for _, usage := range tracker.Report() {
    fmt.Printf("%s/%s: %d requests, %s\n",
        usage.Provider, usage.Model, usage.Requests,
        llms.FormatCost(usage.EstimatedCost))
}
```

Built-in pricing for 25+ models including GPT-4o, Claude, Gemini, and Llama models.

## Resilience

> **A bare provider does not retry.** A client returned by `openai.New()` (or any
> other provider) makes exactly one attempt per call and surfaces the error. Retries,
> circuit breaking, and failover are **opt-in** and come from wrapping the provider with
> the middleware below — most prominently `resilience.NewResilientClient(...)`. This keeps the
> base client's behavior predictable and puts you in control of when calls are retried.

The resilience wrappers live in
`github.com/nocturnium/llm-go-sdk/v6/pkg/middleware/resilience`.
`NewResilientClient` returns an `llms.LLM`; hold the client as `llms.LLM` and reassign
it as you add layers.

### Circuit Breaker

```go
base, _ := openai.New()
var client llms.LLM = base

cb := resilience.NewCircuitBreaker(
    resilience.WithMaxFailures(5),
    resilience.WithResetTimeout(30*time.Second),
    resilience.WithOnStateChange(func(from, to resilience.CircuitState) {
        log.Printf("Circuit: %s -> %s", from, to)
    }),
)

client = resilience.NewResilientClient(client, resilience.WithCircuitBreaker(cb))
```

States: Closed (normal) → Open (blocking) → Half-Open (testing).

### Retry with Backoff

```go
base, _ := openai.New()
var client llms.LLM = base

client = resilience.NewResilientClient(client,
    resilience.WithMaxRetries(3),
    resilience.WithRetryDelay(1*time.Second),
    resilience.WithRetryConfig(&resilience.RetryConfig{
        MaxAttempts:   4,
        InitialDelay:  500 * time.Millisecond,
        MaxDelay:      30 * time.Second,
        BackoffFactor: 2.0,
        Jitter:        0.1,
        ShouldRetry:   resilience.DefaultShouldRetry,
    }),
)
```

Wrapping with `NewResilientClient` enables retries (default policy: 3 attempts with
exponential backoff) and a circuit breaker; tune them with the options above. Default
retry conditions: 429 (rate limit), 500, 502, 503, 504.

## Rate Limiting

```go
base, _ := openai.New()
var client llms.LLM = base

client = resilience.NewRateLimitedClient(client,
    resilience.WithRequestsPerMinute(60),
    resilience.WithRequestBurst(5),
    resilience.WithTokensPerMinute(100000),
    resilience.WithTokenBurst(20000),
    resilience.WithBlocking(true),
    resilience.WithWaitTimeout(30*time.Second),
)

// Or use provider defaults
client = resilience.NewProviderRateLimitedClient(base)
```

The request burst defaults to 1, which strictly paces requests instead of
releasing a full minute's quota immediately. The token burst defaults to a full
minute's token budget.

## Fallback Chains

```go
primary, _ := openai.New()
secondary, _ := anthropic.New()
tertiary, _ := gemini.New()

chain := resilience.NewFallbackChain(
    []llms.LLM{primary, secondary, tertiary},
    resilience.WithRecoveryAfter(60*time.Second), // cooldown before a failed client is retried
    resilience.WithOnFallback(func(fromIdx, toIdx int, from, to llms.LLM, err error) {
        log.Printf("Falling back from %s to %s: %v",
            from.Provider(), to.Provider(), err)
    }),
)

resp, err := chain.GenerateContent(ctx, messages)
```

A failed client is skipped for `WithRecoveryAfter(d)` (default 30s) before it becomes
eligible again, so the chain stops hammering a provider that just errored. Selectors:
`DefaultFallbackSelector` (429/5xx, transport-level errors such as
connection-refused/EOF/timeouts, and circuit-open), `AlwaysFallbackSelector`,
`NeverFallbackSelector`. Weighted variant:

```go
chain, _ := resilience.NewWeightedFallbackChain(
    []llms.LLM{primary, secondary, tertiary},
    []int{10, 5, 1}, // Higher weight = tried first
)
```

## Observability

The observability middleware lives in
`github.com/nocturnium/llm-go-sdk/v6/pkg/observability`.

### OpenTelemetry

Traces and metrics following the OpenTelemetry GenAI semantic conventions. Prompt and
response content is **not recorded by default** — opt in with `WithContentRecording(true)`:

```go
base, _ := openai.New()

otelClient, err := observability.NewOTelMiddleware(base,
    observability.WithContentRecording(true), // opt in to recording prompts/responses
)
if err != nil {
    log.Fatal(err)
}
var client llms.LLM = otelClient
```

| Metric | Description |
|--------|-------------|
| `llm.requests` | Request count by provider/model |
| `llm.tokens.prompt` | Prompt tokens used |
| `llm.tokens.completion` | Completion tokens generated |
| `llm.request.duration` | Request duration histogram |
| `llm.errors` | Error count by type |
| `llm.stream.chunks` | Stream chunks received |

### Langfuse

Built-in [Langfuse](https://langfuse.com) tracing maps GenAI spans to Langfuse
generations/observations, with per-call trace context (trace/user/session IDs, tags,
metadata) supplied through the standard `WithTrace` option.

Prompt/response capture is **off by default** for privacy — enable it explicitly with
`WithLangfuseInputCapture` / `WithLangfuseOutputCapture`:

```go
base, _ := openai.New()

lf, err := observability.NewLangfuseOTelMiddleware(base,
    observability.WithLangfuseInputCapture(true, 4096),  // opt in; max captured length
    observability.WithLangfuseOutputCapture(true, 4096),
)
if err != nil {
    log.Fatal(err)
}
var client llms.LLM = lf

// Per-call trace context goes through WithTrace(llms.TraceOptions{...}).
resp, err := client.GenerateContent(ctx, messages,
    llms.WithTrace(llms.TraceOptions{
        UserID:    "user-123",
        SessionID: "session-abc",
        Tags:      []string{"summarize-doc"},
        Metadata:  map[string]any{"tenant": "acme"},
    }),
)
```

See [docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md#observability) for the full
observability design.

## Provider Configuration

All providers share the functional-options pattern (`provider.New(provider.WithX(...))`).
A few representative examples (see the [provider matrix](#supported-providers) for the
full set):

### HTTP configuration

Every provider accepts two HTTP knobs:

- `WithTimeout(d time.Duration)` — sets the per-request timeout on the HTTP client.
- `WithHTTPClient(c *http.Client)` — supplies your own `*net/http.Client` for full
  control over transport, connection pooling, proxies, and timeouts.

> Note: the SDK installs its own `CheckRedirect` on the client you pass (to re-validate
> redirect hops for SSRF protection) and may set its `Timeout` if you also use
> `WithTimeout`. Pass a client dedicated to the SDK rather than a shared one.

```go
import (
    "net/http"
    "time"

    "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

// Simple: just a request timeout.
client, _ := openai.New(openai.WithTimeout(60 * time.Second))

// Advanced: bring your own *http.Client.
hc := &http.Client{Timeout: 90 * time.Second}
client, _ = openai.New(openai.WithHTTPClient(hc))
```

### Network security (SSRF protection)

By default the SDK refuses to send requests to private, loopback, link-local, or
cloud-metadata addresses, and **requires HTTPS** — this guards against SSRF when a
base URL is derived from untrusted input. These are two separate, no-argument opt-outs:

- `WithAllowPrivateIPs()` — allow requests to private/loopback IP addresses.
- `WithAllowHTTP()` — allow plain-HTTP (non-HTTPS) requests.

A self-hosted endpoint on a private network over plain HTTP typically needs both:

```go
client, _ := openai.New(
    openai.WithBaseURL("http://10.0.0.5:8000/v1"),
    openai.WithAllowPrivateIPs(), // private/loopback IPs
    openai.WithAllowHTTP(),       // plain HTTP (non-HTTPS)
)
```

The local providers — **Ollama**, **llama.cpp**, and **Infinity** — default to allowing
local/private addresses and plain HTTP, since they target `localhost` servers, so no
flag is needed for the usual local setup.

### OpenAI

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"

client, err := openai.New(
    openai.WithAPIKey("sk-..."),       // Or use OPENAI_API_KEY env
    openai.WithModel("gpt-4o-mini"),
    openai.WithBaseURL("https://..."), // Custom endpoint
)
```

### Anthropic

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/anthropic"

client, err := anthropic.New(
    anthropic.WithAPIKey("sk-ant-..."), // Or use ANTHROPIC_API_KEY env
    anthropic.WithModel("claude-sonnet-4-20250514"),
)
```

### Gemini

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/gemini"

client, err := gemini.New(
    gemini.WithAPIKey("..."), // Or use GEMINI_API_KEY / GOOGLE_API_KEY env
    gemini.WithModel("gemini-2.5-flash"),
)
```

### Azure OpenAI

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/azure"

client, err := azure.New(
    azure.WithAPIKey("..."),                                  // AZURE_OPENAI_API_KEY
    azure.WithBaseURL("https://my-resource.openai.azure.com"), // AZURE_OPENAI_ENDPOINT
    azure.WithModel("my-deployment"),                          // AZURE_OPENAI_DEPLOYMENT
)
```

### Ollama (local)

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/ollama"

client, err := ollama.New(
    ollama.WithBaseURL("http://localhost:11434"), // Or OLLAMA_HOST env
    ollama.WithModel("llama3.2"),
)
```

### Z.AI

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/zai"

client, err := zai.New(
    zai.WithAPIKey("..."),         // Or use ZAI_API_KEY env
    zai.WithModel(zai.ModelGLM47), // GLM-4.7 (default)
)

// For coding-specific tasks, use the coding endpoint
client, err = zai.New(zai.WithAPIKey("..."), zai.WithUseCodingAPI())
```

## CLI

A CLI tool (`llms-cli`) is included for testing providers. It supports the **18
auto-registered chat providers** (every provider except HuggingFace, Infinity and ElevenLabs, which
are constructed directly); run `./llms-cli providers` for the full list of providers,
their default models, and the env vars they read.

```bash
# Build the CLI
go build -o llms-cli ./cmd

# List providers (name, default model, env vars)
./llms-cli providers

# Chat with a provider
./llms-cli chat -p openai "What is the capital of France?"

# With options
./llms-cli chat -p anthropic -m claude-sonnet-4-20250514 -t 0.5 "Explain Go interfaces"

# Simple completion
./llms-cli complete -p gemini "The sky is"

# Tool calling demo
./llms-cli tool-demo -p openai
```

| Flag | Alias | Description |
|------|-------|-------------|
| `--provider` | `-p` | Provider name (required) |
| `--model` | `-m` | Model override |
| `--temperature` | `-t` | Temperature (default: 0.7) |
| `--max-tokens` | `-n` | Max tokens (default: 1024) |
| `--system` | `-s` | System prompt |

## Error Handling

```go
import (
    "errors"

    llms "github.com/nocturnium/llm-go-sdk/v6"
    "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

client, err := openai.New()
if errors.Is(err, llms.ErrMissingAPIKey) {
    log.Fatal("Please set OPENAI_API_KEY")
}
```

Common sentinel errors:

- `llms.ErrMissingAPIKey` — no API key provided or found in the environment
- `llms.ErrProviderNotSupported` — unknown provider requested

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](./CONTRIBUTING.md) for the
development workflow, coding standards (see also [AGENTS.md](./AGENTS.md)), and the PR
checklist. By participating you agree to the [Code of Conduct](./CODE_OF_CONDUCT.md).

## Security

Please report security vulnerabilities privately as described in
[SECURITY.md](./SECURITY.md) — do not open public issues for security reports.

## License

Licensed under the **Apache License 2.0** — see [LICENSE](./LICENSE) and
[NOTICE](./NOTICE). Copyright © 2026 Nocturnium, Inc.
