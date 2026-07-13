# Architecture

This document describes the architecture of the `llm-go-sdk` (the `llms` package):
a unified, dependency-light Go SDK for talking to many LLM providers over native
`net/http`. It explains the core abstractions, how the package tree is laid out,
how providers are built, how the middleware chain composes cross-cutting
behavior, and how observability, batching, embeddings, vision, tools, and
streaming fit together.

It is written for SDK consumers and contributors. For the step-by-step recipe to
add a provider (including coding standards and required tests), see
[`AGENTS.md`](https://github.com/nocturnium/llm-go-sdk/blob/main/AGENTS.md).

- Module path: `github.com/nocturnium/llm-go-sdk/v4`
- License: Apache-2.0 (see `LICENSE` and `NOTICE`)
- No external LLM SDK dependencies; the only notable third-party packages are
  `urfave/cli/v2` (for the CLI), `go.opentelemetry.io/otel` (for tracing and
  metrics), and `golang.org/x/time` (for rate limiting). As of v3 OpenTelemetry is
  pulled in only by `pkg/observability` — the bare `llms` root package no longer
  compiles OTel (rate limiting moved with the resilience middleware into
  `pkg/middleware/resilience`).

---

## High-level design

### The `LLM` interface

Everything in the SDK is organized around one small interface, defined in the
root package (`llms.go`):

```go
type LLM interface {
    GenerateContent(ctx, messages []Message, ...CallOption) (*Response, error)
    Stream(ctx, messages []Message, ...CallOption) (<-chan StreamChunk, error)
    Provider() Provider
    Model() string
}
```

Every provider client and every middleware wrapper satisfies `LLM`. Because
middleware also satisfies `LLM`, wrappers compose freely and a consumer always
programs against the same surface regardless of how many layers are present.

The interface deliberately has no `Call` method. The "single prompt in, text out"
shortcut is the package-level helper `llms.Call(ctx, llm, prompt, opts...)`, which wraps
`GenerateContent`; the agent loop `llms.RunTools(ctx, llm, messages, registry, opts...)`
and the typed-output helper `llms.GenerateTyped[T](ctx, llm, messages, opts...)` are also
free functions over the interface rather than methods, so they work uniformly across any
provider or middleware stack.

Additional behaviors are expressed as **optional interfaces** that a base
provider may also implement. Consumers test for them at runtime rather than
assuming they exist:

| Capability        | Interface        | Runtime check                          |
|-------------------|------------------|----------------------------------------|
| Capability report | `CapableProvider` (`Capabilities()`) | `llms.GetCapabilities(llm)`  |
| Embeddings        | `Embedder`       | `llms.AsEmbedder(v)` / `SupportsEmbeddings(v)` |
| Reranking         | `Reranker`       | `llms.AsReranker(v)` / `SupportsReranking(v)`  |
| Model listing     | `ModelLister`    | type assertion                         |
| Batch processing  | `BatchProcessor` | `llms.SupportsBatch(llm)`              |

`Capabilities` (streaming, tools, vision, embeddings, batch, JSON mode, context
and output token limits) is reported per provider and refined per model via the
capability registry (below). Helpers such as `SupportsStreaming`,
`SupportsTools`, `SupportsVision`, and `SupportsJSONMode` wrap these checks and
unwrap middleware automatically so the question is always answered by the base
provider, not by whatever wrapper happens to be outermost.

### Options pattern

All per-call configuration uses the functional-options pattern. `CallOption`
values mutate a `CallOptions` struct; `ApplyOptions(...)` produces the resolved
options and `Validate()` checks them. Options cover model parameters
(`WithTemperature`, `WithMaxTokens`, `WithTopP`, `WithStopWords`, frequency and
presence penalties), tools (`WithTools` plus the typed tool-choice options
`WithToolChoiceAuto`/`WithToolChoiceNone`/`WithToolChoiceRequired`/`WithToolChoiceTool`),
output shape (`WithJSONMode` for a JSON object, `WithJSONSchema` for schema-constrained
JSON), streaming tuning (`WithStreamBufferSize`, `WithStreamSendTimeout`), message
handling (`WithDisableMessageMerging`), token accounting (`WithEstimateTokens`),
per-call trace context (`WithTrace(TraceOptions{...})`), and provider-specific escape
hatches (`WithExtraBody`, `WithAdapterID`, `WithWebSearch`).

`WithMaxTokens(int)` is unchanged for callers, but internally `CallOptions.MaxTokens`
is now a `*int`: unset means "use the provider/model default" (Anthropic still sends an
explicit limit), while a set value — including an explicit `0` — is forwarded verbatim.
`Temperature` and `TopP` follow the same pointer convention.

Provider *construction* uses the same pattern with a separate per-provider
`Option` type (e.g. `openai.WithAPIKey`, `openai.WithModel`,
`openai.WithBaseURL`). For HTTP control every provider exposes
`WithTimeout(time.Duration)` (per-request timeout) and `WithHTTPClient(*http.Client)`
(supply your own `net/http` client), plus two separate no-argument SSRF opt-outs —
`WithAllowPrivateIPs()` (allow private/loopback IPs) and `WithAllowHTTP()` (allow
plain-HTTP) — for self-hosted/private endpoints. Construction options configure
the client; `CallOption`s configure individual requests. Each provider's construction
surface is `New(...)` plus its `WithX(...)` options — the option struct, its constructor,
and the apply helper are unexported implementation details.

Providers can also be constructed by name through the package-level registry:
`llms.New(name, llms.Config{...})` and `llms.NewFromEnv()` (reading `LLM_PROVIDER` /
`LLM_MODEL`). `llms.Config` carries the common settings (`APIKey`, `Model`, `BaseURL`,
`Timeout`, `AllowPrivateIPs`, `AllowHTTP`, `HTTPClient`) plus an `Extra map[string]string` for
provider-specific construction params (e.g. RunPod `endpoint_id`, Z.AI `coding`).
Each provider package registers its factory in `init()`; blank-importing
`pkg/providers/all` wires up all 17 chat providers at once.

### Provider model

A provider is a concrete `Client` that implements `LLM`. There are two flavors:

1. **OpenAI-compatible providers** embed `openaicompat.BaseProvider`, which
   supplies the entire `LLM` surface (plus embeddings and capability reporting)
   on top of a shared OpenAI-shaped HTTP client. The provider package mostly
   declares its base URL, default model, env vars, and capability defaults.
2. **Native providers** (Anthropic, Gemini, Ollama, llama.cpp) implement `LLM`
   directly because their wire protocols differ from OpenAI's. Each delegates
   protocol details to a dedicated `internal/<name>api` client and keeps the
   SDK-facing conversion logic in the provider package.

Provider clients are safe for concurrent use; a single client can be shared
across goroutines.

### Middleware / decorator chain

Cross-cutting concerns are implemented as **decorators**: each wraps an `LLM`
and returns an `LLM`, so they nest arbitrarily. Every wrapper implements the
`Wrapper` interface (`LLM` plus `Unwrap() LLM`), which makes the chain
introspectable:

- `UnwrapAll(llm)` peels every layer to reach the base provider.
- `GetMiddleware(llm)` returns the wrappers from outermost to innermost.

This is why capability checks work through arbitrarily deep stacks: they call
`UnwrapAll` first.

A typical composed client, outermost first:

```
LoggingMiddleware
  └─ OTelMiddleware / LangfuseOTelMiddleware   (tracing + metrics)
       └─ MetricsMiddleware                    (combined metrics + cost)
            └─ CostMiddleware                  (token cost accounting)
                 └─ RateLimitedClient          (token-bucket throttling)
                      └─ ResilientClient        (retry + circuit breaker)
                           └─ FallbackChain      (failover across providers)
                                └─ provider Client (e.g. openai.Client)
```

The available decorators. As of v3 cost tracking and response caching stay in the
root package, while the resilience decorators live in `pkg/middleware/resilience`
and the observability decorators in `pkg/observability`:

| Concern         | Type / constructor                                   | File                              |
|-----------------|------------------------------------------------------|-----------------------------------|
| Retry + breaker | `ResilientClient` / `NewResilientClient`             | `pkg/middleware/resilience/resilience.go`, `resilience_retry.go`, `resilience_circuit_breaker.go` |
| Circuit breaker | `CircuitBreaker` (closed/open/half-open)             | `pkg/middleware/resilience/resilience_circuit_breaker.go` |
| Rate limiting   | `RateLimitedClient` / `NewRateLimitedClient`, `SharedRateLimiter` | `pkg/middleware/resilience/ratelimit.go` |
| Fallback        | `FallbackChain`, `WeightedFallbackChain`             | `pkg/middleware/resilience/fallback.go` |
| Cost tracking   | `CostMiddleware`, `CostTracker`                      | `cost.go`                         |
| Token estimation| `TokenEstimator` (also used via `WithEstimateTokens`)| `tokens.go`                       |
| Logging         | `LoggingMiddleware`, `Logger`                        | `pkg/observability/logging.go`, `logging_slog.go`, `logging_json.go` |
| Metrics         | `MetricsMiddleware`                                  | `pkg/observability/metrics.go`, `metrics_otel.go`, `metrics_sliding_window.go` |
| Tracing (OTel)  | `OTelMiddleware`                                     | `pkg/observability/otel.go`, `otel_genai.go` |
| Tracing (Langfuse)| `LangfuseOTelMiddleware`                           | `pkg/observability/otel_langfuse.go`, `langfuse.go`, `langfuse_format.go` |

Ordering matters and is the consumer's choice. A common convention is to put
fallback and resilience closest to the provider (so retries and failover happen
before metrics/cost are recorded once for the whole call), and to put logging and
tracing outermost (so they see the full, final outcome).

---

## Package layout

### Where the code lives

The SDK has a small, flat public surface. The guiding rule:

> **The root package holds the core types and logic, period. There are no alias
> packages — the only other public packages are the providers and the
> custom-provider base.**

The public surface is exactly:

- **Root (`llms "github.com/nocturnium/llm-go-sdk/v4"`)** — every shared type and
  function: the `LLM` interface, `Message`/`Response`/`Tool` and friends, the
  `CallOption` builders, errors and sentinels, streaming, the capability registry,
  and cost tracking. A single import reaches the core (`llms.Message`,
  `llms.WithTemperature`, `llms.NewCostTracker`, …).
- **`pkg/middleware/resilience`** — retry, circuit-breaker, rate-limiting, and
  fallback wrappers (`resilience.NewResilientClient`, `resilience.NewFallbackChain`, …),
  each a drop-in `llms.LLM`.
- **`pkg/observability`** — OpenTelemetry, Langfuse, and structured-logging
  middleware (`observability.NewOTelMiddleware`, `observability.NewLoggingMiddleware`, …).
- **`pkg/providers/<name>`** — the 19 provider implementations (17 chat-registered;
  HuggingFace and Infinity are embeddings-only). Import the one you
  need, e.g. `github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai`.
- **`pkg/openaicompat`** — the shared OpenAI-compatible base, public so external code
  can build custom providers on it (see [Extension points](#extension-points)).

Everything else lives under `internal/` and is not importable by external code.

> **History:** earlier pre-1.0 builds also shipped `pkg/types`, `pkg/options`,
> `pkg/errors`, `pkg/streaming`, `pkg/search`, `pkg/middleware/*`, and a top-level
> `providers/*` backwards-compatibility shim. **These have all been removed.** The
> symbols they re-exported now live only in the root package, and providers are
> imported only from `pkg/providers/<name>`. See
> [`migration-guide.md`](./migration-guide.md) for the layout reference.

> **Decision — why not a "facade at root"?** A recurring suggestion is to shrink
> the root by moving its code into sub-packages and re-exporting from a thin root
> facade. This does not work in Go here, and we have deliberately rejected it:
> the root package is the dependency **leaf** — every provider imports it for both
> *types* (`llms.Message`) and *functions* (`llms.ApplyOptions`, `llms.PrepareMessages`,
> `llms.WrapProviderError`). A root that re-exported from sub-packages would need to
> import them, creating `root → subpkg → root` **import cycles** that do not compile.
> Type aliases (`type X = sub.X`) can forward types but cannot forward functions
> (the bulk of the surface), so a facade also means hundreds of hand-maintained
> wrappers that fracture godoc. The correct dependency direction is *inward to the
> core*: features depend on the core, never the reverse. If the root must shed a
> heavy dependency (e.g. OpenTelemetry), the answer is to move that **middleware
> out** to a leaf sub-package that imports the core — a breaking change reserved for
> a deliberate major version, documented in
> [`v3-package-taxonomy.md`](./v3-package-taxonomy.md).

### `pkg/openaicompat`

The OpenAI-compatible base lives in `pkg/openaicompat`; it is public precisely so
external code can build custom providers on it without forking the SDK (see
[Extension points](#extension-points)).

### Directory map

```
llm-go-sdk/
├── *.go                      # ROOT package `llms`: the real core types & logic
│   ├── llms.go               #   LLM interface, Provider consts, Capabilities, Wrapper/UnwrapAll
│   ├── message.go            #   Message, Role, message prep/merge/validate
│   ├── response.go           #   Response, Usage, ReasoningContent, StreamChunk
│   ├── options.go            #   CallOption / CallOptions / With* builders
│   ├── tools.go              #   Tool, ToolCall, ToolChoice, ToolRegistry, handlers
│   ├── vision.go             #   ContentPart, ImageContent, image helpers
│   ├── embeddings.go         #   Embedder/Reranker interfaces, As*/Supports* helpers
│   ├── errors.go / error_mapper.go   # error types + per-provider error mapping
│   ├── streaming.go          #   StreamSender, StreamProcessor
│   ├── apikey.go             #   ResolveAPIKey / RequireAPIKey + Env* constants
│   ├── capabilities_registry.go      # per-model capability registry
│   ├── models.go / models_util.go    # ModelInfo, ModelLister, listing helpers
│   ├── batch.go              #   BatchProcessor, ConcurrentBatcher
│   ├── cost.go / tokens.go   #   cost tracking + token estimation
│   ├── caching*.go           #   response-caching middleware
│   └── websearch.go          #   web search config + result types
│
├── pkg/
│   ├── openaicompat/   # public base for OpenAI-compatible providers
│   ├── mcp/            # Model Context Protocol client
│   ├── tokenizer/      # standalone token estimation
│   ├── middleware/resilience/   # resilience middleware (moved out of root in v3)
│   │   ├── resilience*.go        #   retry + circuit breaker
│   │   └── ratelimit.go / fallback.go    # rate limiting + failover
│   ├── observability/  # observability middleware (moved out of root in v3)
│   │   ├── logging*.go / metrics*.go     # logging + metrics middleware
│   │   └── otel*.go / langfuse*.go       # OTel + Langfuse tracing
│   └── providers/<name>/   # provider implementations (19 providers)
│
├── internal/           # not importable by external code
│   ├── httpclient/     # shared HTTP client: retry, backoff, SSE, security
│   ├── anthropicapi/   # Anthropic Messages API client + types
│   ├── geminiapi/      # Google Gemini API client + types
│   ├── ollamaapi/      # Ollama native API client + types
│   ├── llamacppapi/    # llama.cpp native API client + types
│   ├── websearch/      # Brave / Tavily search clients
│   └── testutil/       # internal test helpers
│
├── cmd/                # llms-cli — a real shipping CLI built on the SDK
├── docs/               # documentation (this file)
└── examples/           # runnable usage examples
```

---

## How providers work

### OpenAI-compatible providers and `BaseProvider`

Most providers expose an OpenAI-shaped chat-completions API. For them,
`pkg/openaicompat` does the heavy lifting:

- `openaicompat.Client` is the HTTP layer. It builds requests against
  `/chat/completions`, `/embeddings`, and `/models`, sets auth headers
  (`Authorization: Bearer …`, or `api-key` for Azure), reads SSE streams via a
  `StreamReader`, and tolerates both `{"object":"list","data":[…]}` and bare
  array model-list responses.
- `openaicompat.BaseProvider` embeds that client and implements the full `LLM`
  interface plus `Embedder` and `CapableProvider`. Its `GenerateContent` applies
  and validates options, prepares messages (merging consecutive same-role
  messages unless disabled), resolves the effective model, builds the request,
  converts the response, and optionally estimates token usage when the provider
  omits it. `Stream` does the same and spawns a goroutine that pumps converted
  chunks through a `StreamSender`.
- `openaicompat.ProviderConfig` carries the `Provider` enum value, a name for
  error messages, the default chat model (`DefaultModel`), the default embedding
  model (`DefaultEmbeddingModel`), and default `Capabilities`.

A concrete provider therefore reduces to declaring its specifics and embedding
the base. The OpenAI provider (`pkg/providers/openai/openai.go`) is the
reference shape:

```go
type Client struct {
    openaicompat.BaseProvider
    options *options // unexported option struct
}

func New(opts ...Option) (*Client, error) {
    options := apply(opts...) // unexported apply helper
    apiKey, err := llms.RequireAPIKey("openai", options.APIKey, llms.EnvOpenAIAPIKey)
    // ... resolve baseURL, headers ...
    client := openaicompat.NewClient(openaicompat.ClientConfig{
        BaseURL:         baseURL,
        APIKey:          apiKey,
        Headers:         headers,
        Timeout:         options.Timeout,         // from WithTimeout
        HTTPClient:      options.HTTPClient,       // from WithHTTPClient
        AllowPrivateIPs: options.AllowPrivateIPs,  // from WithAllowPrivateIPs
        AllowHTTP:       options.AllowHTTP,         // from WithAllowHTTP
    })
    // NewBaseProvider takes (client, config); the default chat/embedding models
    // come from config.DefaultModel / config.DefaultEmbeddingModel.
    cfg := *options.ProviderConfig
    cfg.DefaultModel = options.Model
    cfg.DefaultEmbeddingModel = options.EmbeddingModel
    return &Client{
        BaseProvider: openaicompat.NewBaseProvider(client, cfg),
        options:      options,
    }, nil
}

var (
    _ llms.LLM             = (*Client)(nil)
    _ llms.Embedder        = (*Client)(nil)
    _ llms.CapableProvider = (*Client)(nil)
)
```

A provider's construction surface is just `New(...)` and its `WithX(...)` options; the
`options` struct, its `apply` helper, and the defaults are unexported.

OpenAI-compatible providers built on this base include OpenAI, Azure, Cerebras,
DeepSeek, Featherless, Fireworks, Groq, Mistral, Perplexity, RunPod, Synthetic,
TogetherAI, and Z.AI. Azure reuses the same base with header and URL tweaks
(`api-key` header, an `api-version` query parameter).

### Native (non-OpenAI-compatible) providers

When a provider's protocol diverges meaningfully from OpenAI's, it gets a native
implementation rather than being forced through `BaseProvider`. The provider
package implements `LLM` directly and delegates protocol I/O to an
`internal/<name>api` client, keeping all SDK-type conversion in the provider
package's `converters.go`.

- **Anthropic** uses the Messages API: a different endpoint (`/messages`), the
  system prompt as a separate field rather than a message, an SSE event model
  built from `content_block_start`/`delta` events, and unique features such as
  extended thinking and prompt caching (`cache_control`, with cache token usage
  surfaced in `Usage`). `internal/anthropicapi` handles the wire format;
  `pkg/providers/anthropic` adapts it to `llms` types.
- **Gemini** uses Google's `generateContent` shape via `internal/geminiapi`.
- **Ollama** and **llama.cpp** target local servers with their own native APIs
  via `internal/ollamaapi` and `internal/llamacppapi`.
- **Infinity** is embeddings-and-reranking only. It does not implement the chat
  `LLM` interface; it implements `Embedder` and `Reranker`.

### Internal HTTP clients

The `internal/` tree is not importable by external code; it is the engine room.

- `internal/httpclient` is the shared transport for every provider. It wraps a
  `*http.Client` with a configurable `RetryPolicy`, exposes `DoJSON`, `DoRaw`,
  and `DoStream`, and includes an `SSEReader` for streaming. **Retries are off by
  default** (`NoRetryPolicy`): a bare provider makes a single attempt per call.
  This is deliberate — the higher-level `ResilientClient` middleware
  (`resilience.NewResilientClient`) is the single authority for retrying whole `LLM`
  operations (with backoff) and adds the circuit breaker, so retry behavior lives
  in one place rather than being silently duplicated at the HTTP layer. Streaming
  requests are never retried (a partially consumed stream cannot be safely
  replayed). A `security` layer guards request construction (SSRF protection,
  HTTPS enforcement — see below).
- `internal/anthropicapi`, `internal/geminiapi`, `internal/ollamaapi`,
  `internal/llamacppapi` each provide a typed client for one native provider's
  protocol, built on `internal/httpclient`.
- `internal/websearch` provides Brave and Tavily clients used by the web-search
  feature.

### Capability registry

`capabilities_registry.go` holds a thread-safe `CapabilityRegistry` keyed by
`"provider:model"`. It records per-model `ModelCapabilities` (context window,
max output, vision/tools/streaming/JSON support) plus per-provider defaults used
when a specific model is not registered. `GetModelCapabilities(provider, model)`
queries the global registry; `RegisterModelCapabilities(...)` extends it.

This exists so capability reporting is *per model*, not a single hardcoded
provider-level answer. `BaseProvider.Capabilities()` consults the registry
for the active model and falls back to the provider's configured defaults, then
overlays embeddings/batch flags. The result is what `llms.GetCapabilities(llm)`
and the `Supports*` helpers return.

### API key resolution

`apikey.go` centralizes credential lookup. `ResolveAPIKey(explicit, envVars...)`
returns the first non-empty value among the explicit argument, the
provider-specific env vars in order, and finally `LLM_API_KEY` as a global
fallback. `RequireAPIKey(providerName, ...)` does the same but returns an error
wrapping `ErrMissingAPIKey` (matchable with `errors.Is`) when nothing is found.
Provider env var names are constants (`EnvOpenAIAPIKey`, `EnvAnthropicAPIKey`, …)
to avoid typos.

---

## Observability architecture

The SDK provides three composable tracing/metrics layers, all as `LLM`
middleware, plus logging and cost tracking described earlier.

### OpenTelemetry (GenAI semantic conventions)

- `OTelMiddleware` (`pkg/observability/otel.go`) emits spans and metrics for each operation. It
  records request type, provider, model, finish reason, error type, streaming
  flag, tool-call count, request duration, prompt/completion token counts, and
  stream-chunk counts. It uses the global OTel `TracerProvider`/`MeterProvider`,
  so it integrates with whatever exporters the host application configures.
  **Prompt/response content is not recorded by default**; opt in with
  `WithContentRecording(true)`.
- `pkg/observability/otel_genai.go` defines the OpenTelemetry **GenAI** semantic-convention
  attribute keys (`gen_ai.system`, `gen_ai.request.model`,
  `gen_ai.response.model`, `gen_ai.usage.prompt_tokens`,
  `gen_ai.usage.completion_tokens`, `gen_ai.response.finish_reason`, request
  parameters, etc.), so traces are portable to any GenAI-aware backend.
- `MetricsMiddleware` (`pkg/observability/metrics.go`) combines OTel metrics with cost tracking and
  a sliding-window aggregator (`pkg/observability/metrics_sliding_window.go`) for in-process
  rate/latency stats.

### Langfuse compatibility

`LangfuseOTelMiddleware` (`pkg/observability/otel_langfuse.go`) extends the OTel middleware with
Langfuse's attribute conventions, layered on top of the GenAI conventions so a
single instrumented call is legible to both generic OTel backends and Langfuse:

- Identity/context attributes: `langfuse.user.id`, `langfuse.session.id`,
  `langfuse.tags`, `langfuse.version`, `langfuse.environment`, and propagated
  `langfuse.metadata.*` keys.
- Optional input/output capture with configurable truncation
  (`WithLangfuseInputCapture(enabled, maxLen)`,
  `WithLangfuseOutputCapture(enabled, maxLen)`), recorded under both the GenAI
  (`gen_ai.prompt` / `gen_ai.completion`) and OpenInference (`input.value` /
  `output.value`) keys. **Content capture is off by default** for privacy — prompts
  and responses are not recorded unless you opt in via these options.

Trace context is carried through `context.Context`. `pkg/observability/langfuse.go` defines
`TraceContext` (user, session, tags, version, release, environment, metadata)
with `WithTraceContext`/context accessors and `PropagateAttributes`, which clones
the parent context's attributes into a child so nested calls inherit identity and
tagging. Per-call trace overrides are supplied through the standard call option
`WithTrace(llms.TraceOptions{TraceID, SpanID, ParentID, UserID, SessionID, Tags,
Metadata, Version})`, which the Langfuse middleware reads when annotating a span.
`pkg/observability/langfuse_format.go` handles serializing messages and responses for the captured
input/output fields.

### Logging

`Logger` is a small interface; `LoggingMiddleware` records a structured
`LogEntry` per call (operation, provider, model, duration, usage, and extensible
metadata, including Langfuse-compatible fields). Adapters ship for `log/slog`
(`pkg/observability/logging_slog.go`) and JSON (`pkg/observability/logging_json.go`); `NopLogger` disables logging. Both
the slog and JSON adapters **redact prompt/response content by default** for privacy;
pass `WithRedaction(false)` to opt into logging message content.

---

## Feature subsystems

### Batching

`batch.go` defines a `BatchProcessor` interface and a `ConcurrentBatcher` that
fans a slice of `BatchRequest`s across goroutines against any `LLM`, with
configurable concurrency and per-request options, returning `BatchResult`s that
preserve input order and capture per-item errors. This is a client-side
concurrency helper and is independent of any provider's native batch API.

### Embeddings and reranking

`embeddings.go` defines the `Embedder` interface — a single method, `Embed(ctx,
texts, ...EmbedOption) (*EmbeddingResponse, error)` — and the `Reranker` interface,
with `EmbeddingResponse`, `Embedding`, and `EmbeddingUsage` types. Embedding vectors
are `[]float32` throughout (`Embedding.Vector`). The query/document conveniences are
package-level functions over any `Embedder`, not methods:
`llms.EmbedQuery(ctx, e, text) ([]float32, error)` and
`llms.EmbedDocuments(ctx, e, texts) ([][]float32, error)`. OpenAI-compatible providers
inherit embedding support from `BaseProvider` when a default embedding model is
configured. Consumers discover support at runtime with `AsEmbedder` /
`SupportsEmbeddings` and `AsReranker` / `SupportsReranking`.

### Vision / multimodal

`vision.go` models multimodal input as `ContentPart`s on a `Message`. A message
either carries plain `Content` or a list of `Parts`; when `Parts` is non-empty it
takes precedence. Helpers build image parts from URLs, base64, files, byte
slices, or readers (`NewImageURLPart`, `NewImageBase64Part`, `NewImageFromFile`,
`NewImageFromBytes`, `NewImageFromReader`). Providers that report `Vision`
capability convert these into their native image-content format.

### Tools / function calling

`tools.go` defines `Tool`, `FunctionDefinition`, `ToolCall`, `FunctionCall`, and the
typed `ToolChoice`. `NewFunctionTool(name, description, parameters)` constructs a tool;
tools are passed per call via `WithTools`, and tool selection is controlled with the
typed options `WithToolChoiceAuto`/`WithToolChoiceNone`/`WithToolChoiceRequired`/
`WithToolChoiceTool(name)`. Responses expose `ToolCall(id)` and `ToolCallByName(name)`
accessors. A `ToolRegistry` maps tool names to `ToolHandler` functions and can execute
the tool calls in a `Response`, producing the follow-up tool-result messages to feed
back to the model.

`runtools.go` builds the full agent loop on top of this:
`RunTools(ctx, llm, messages, registry, opts...)` repeatedly calls the model with the
registry's tools, executes requested tool calls (concurrently, bounded by
`WithToolConcurrency`), and feeds results back until the model stops requesting tools or
the `WithMaxIterations` guard is hit (returning an error wrapping `ErrMaxIterations`). It
returns the final `*Response` and the full `transcript []Message`; `WithOnStep` provides
a per-iteration observability hook. `structured.go` adds typed structured output:
`SchemaFrom[T]()` reflects a JSON Schema from a Go type, and
`GenerateTyped[T](ctx, llm, messages, opts...)` sends that schema (via `WithJSONSchema`)
and unmarshals the reply into `T`.

### Streaming

Streaming is uniform across providers: `Stream` returns `<-chan StreamChunk`. The
channel is always closed with a terminal chunk — either one with `Done == true` on
success or one carrying `Error` on failure — so a consumer can simply range over the
channel and be guaranteed it ends in one of those two states. The `StreamSender`
(`streaming.go`) sends chunks with a send timeout (`StreamSendTimeout`, a
`time.Duration`, tunable per call via `WithStreamSendTimeout`) so a consumer that stops
reading cannot leak the producing goroutine; it tracks timeout state and respects
context cancellation. `StreamProcessor` allows middleware to transform chunks as they
flow. Under the hood, OpenAI-compatible streaming is parsed by the
`internal/httpclient` `SSEReader`; native providers parse their own SSE event formats
in their `internal/<name>api` clients.

### Web search

`websearch.go` defines `WebSearchConfig`, `WebSearchProvider`, and `SearchResult`.
Search can run against a provider's native web-search feature or against external
engines (Brave, Tavily) via the `internal/websearch` clients. It is selected per call
with `WithWebSearch`.

### Models listing

`models.go` defines `ModelInfo`, `ModelType`, pricing, and the `ModelLister`
interface with `ListModels` plus filtering options. Providers that can enumerate
models implement it; `models_util.go` supplies shared helpers.

### Errors

`errors.go` defines structured error types (`APIError`, `ProviderError`,
`ValidationError`/`ValidationErrors`, `StreamError`) and sentinel errors usable
with `errors.Is`. `error_mapper.go` provides a per-provider `ErrorMapper` and
registry that normalize provider-specific HTTP/error payloads into the SDK's
standard errors (rate-limited, retryable, etc.), so resilience logic and callers
can reason about failures uniformly. `ProviderFromError(err)` extracts the originating
`Provider` from an `APIError` or `ProviderError`.

---

## Extension points

### Adding a provider

The supported, documented workflow lives in [`AGENTS.md`](https://github.com/nocturnium/llm-go-sdk/blob/main/AGENTS.md) under
"Adding a New Provider". In short:

- **If the provider is OpenAI-compatible**, create
  `pkg/providers/<name>/<name>.go` with a `Client` that embeds
  `openaicompat.BaseProvider`, declare a `defaultProviderConfig`
  (`openaicompat.ProviderConfig`) with the provider enum, name, default chat model
  (`DefaultModel`), default embedding model (`DefaultEmbeddingModel`), and
  capabilities, resolve the key via `llms.RequireAPIKey`,
  and assert interface compliance with `var _ llms.LLM = (*Client)(nil)` (and
  `Embedder`/`CapableProvider`/`ModelLister` as applicable). Add the provider
  constant to `llms.go`, register the factory for `llms.New` (the `all` package
  blank-imports each provider for this), and register any per-model entries in the
  capability registry.
- **If it is not OpenAI-compatible**, write a native client (justified in the
  package doc, as Anthropic/Gemini/Ollama/llama.cpp do), add a typed
  `internal/<name>api` client over `internal/httpclient`, and keep conversions
  in the provider package.

New providers live under `pkg/providers/<name>`, the only provider location.

### Building custom providers on `pkg/openaicompat`

`pkg/openaicompat` is public precisely so code outside this repository can build
its own OpenAI-compatible providers without forking the SDK. The fast path is to
embed `BaseProvider`: construct an `openaicompat.Client` with `NewClient(ClientConfig{...})`
pointed at any OpenAI-shaped endpoint, then call
`NewBaseProvider(client, ProviderConfig{...})`. `NewBaseProvider` takes exactly two
arguments — the client and the config — and reads the default chat/embedding models from
`ProviderConfig.DefaultModel` and `ProviderConfig.DefaultEmbeddingModel`; the embedded
`BaseProvider` then supplies the full `LLM`, `Embedder`, and `CapableProvider` surface
(streaming, embeddings, capability reporting, token estimation).

The package exposes a curated extension API for both the "embed and go" case and the
"drive the client directly" case:

- **Construction / embedding:** `NewClient`, `ClientConfig`, `NewBaseProvider`,
  `BaseProvider`, `ProviderConfig`.
- **Type conversions** (used internally by `BaseProvider`, exported for custom request
  shapes): `BuildChatRequest`, `ConvertMessages`, `ConvertResponse`,
  `ConvertEmbeddingResponse`, `WrapError`, and `ProcessStream`.

When working with the wire types directly, `ChatMessage` exposes `Content()` (the
message text) and `ReasoningText()` (provider reasoning/thinking text) accessors. See the
package documentation in `pkg/openaicompat/doc.go` for the recommended pattern, and
`pkg/providers/openai` as the reference implementation.

### Middleware

Because middleware is just "an `LLM` wrapping an `LLM`," consumers can write
their own decorators: implement the `LLM` methods, store the wrapped client, and
implement `Unwrap() LLM` so the wrapper participates in `UnwrapAll` /
`GetMiddleware` introspection and capability passthrough. The built-in
decorators — cost and response caching in the root package, and the wrappers in
pkg/middleware/resilience and pkg/observability — are examples to follow.

---

## CLI

`cmd/` builds `llms-cli`, a shipping command-line tool over the SDK (using
`urfave/cli/v2`). It is a first-class consumer of the public API and a practical
end-to-end example of wiring providers and options together.
