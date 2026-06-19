# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

Additive, non-breaking changes on top of v3.0.0. The module path is unchanged and
every change is API-compatible (the `apidiff` baseline grows, with no removals).

### Added

- **Middleware composition — `llms.Chain`.** `Chain(base, ...Middleware)` (where
  `Middleware = func(LLM) LLM`) composes middleware around a base client: the first
  listed sits innermost (resilience/fallback), the last outermost (observability/
  logging). A flat alternative to manual nesting now that the middleware live in
  `pkg/middleware/resilience` and `pkg/observability`.
- **OpenAI Responses API — stateless reasoning round-trip.**
  `openai.WithReasoningRoundTrip()` requests encrypted reasoning items
  (`include: ["reasoning.encrypted_content"]`) so a reasoning model's thinking can be
  replayed across turns without server-side state. The new `Message.Reasoning` field
  carries it (also usable for Anthropic extended-thinking signatures); `pkg/openaicompat`
  gains `ResponsesReasoningItem` and `MetadataKeyResponsesReasoning`.
- **MCP client — resources, prompts, capabilities, mounting, and notifications**
  (`pkg/mcp`, extending the tools-only client):
  - Resources: `Client.ListResources` (cursor-paginated) and `Client.ReadResource`
    (`Resource`, `ResourceContents`, `ReadResourceResult`).
  - Prompts: `Client.ListPrompts` and `Client.GetPrompt` (`Prompt`, `PromptArgument`,
    `PromptMessage`, `GetPromptResult`), with `GetPromptResult.LLMMessages()` bridging a
    server prompt into `[]llms.Message` for `RunTools`.
  - Capabilities: typed `ServerCapabilities` parsed from the initialize handshake,
    exposed via `Client.ServerCapabilities()`.
  - One-call mounting: `MountStdio` / `MountHTTP` connect a server and register its tools.
  - Notifications: `Client.OnProgress` / `Client.OnLog`, plus per-call `WithProgress` and
    `Client.CallToolWithProgress` (`ProgressNotification`, `LogMessage`). stdio surfaces all
    server notification frames; the Streamable HTTP transport surfaces those that arrive
    interleaved on a POST response stream.

### Changed

- **Model metadata refreshed (data only).** Re-verified the OpenAI / Anthropic / Gemini
  pricing-and-context overlays against current published pricing (correcting stale cost
  estimates), and refreshed the Featherless and RunPod curated model lists. Z.AI and
  Synthetic were already current and are unchanged.

## [3.0.0] - 2026-06-19

v3 is a major release. The module path is now `github.com/nocturnium/llm-go-sdk/v3`
(Go semantic import versioning). v2 and v3 are distinct module paths and can coexist,
so consumers may migrate incrementally. See `docs/migration-guide.md` for the full
v2 → v3 guide.

The single structural change: **the observability and resilience middleware moved out
of the root `llms` package into leaf subpackages.** Importing `llms` for the core types
(`Message`, `Response`, `Call`, …) no longer compiles the OpenTelemetry SDK — the OTel
dependency count of the bare root package drops from ~20 packages to **0**. Exported
symbol names are unchanged; only the package that holds them moved.

### Changed (BREAKING)

- **Module path → `/v3`.** Update imports to `github.com/nocturnium/llm-go-sdk/v3`
  (the core package name stays `llms`): `go get github.com/nocturnium/llm-go-sdk/v3@v3.0.0`.
- **Observability middleware moved to `pkg/observability`.** `llms.NewOTelMiddleware`,
  `llms.NewMetricsMiddleware`, the Langfuse exporters, the JSON/slog loggers, the GenAI
  semantic-convention `Attr*` constants, and the trace-context helpers now live in
  `github.com/nocturnium/llm-go-sdk/v3/pkg/observability` (e.g. `observability.NewOTelMiddleware`).
- **Resilience middleware moved to `pkg/middleware/resilience`.** `llms.NewResilientClient`,
  `llms.NewFallbackChain`, the rate limiter, the circuit breaker, `RetryConfig`, and their
  option helpers now live in `github.com/nocturnium/llm-go-sdk/v3/pkg/middleware/resilience`
  (e.g. `resilience.NewResilientClient`).

There are **no other breaking changes**: every moved symbol keeps its exact name and
signature, so migration is a mechanical import/qualifier update (see the migration guide).

## [2.0.0] - 2026-06-15

v2 is a major release. The module path is now `github.com/nocturnium/llm-go-sdk/v2`
(Go semantic import versioning). v1 and v2 are distinct module paths and can coexist,
so consumers may migrate incrementally. See `docs/migration-guide.md` for the full
v1 → v2 guide.

### Changed (BREAKING)

- **Module path → `/v2`.** Update imports to `github.com/nocturnium/llm-go-sdk/v2`
  (the package name stays `llms`): `go get github.com/nocturnium/llm-go-sdk/v2@v2.0.0`.
- **Tool handlers take a context.** `ToolHandler`, `RegisterFunc`'s typed handler, and
  `ToolRegistry.Handle`/`HandleAll` now take a leading `context.Context`. `RunTools`
  cancels in-flight tool handlers when the loop is canceled or a turn errors. Add
  `ctx context.Context` as the first handler parameter.
- **Sampling penalties are pointers.** `CallOptions.FrequencyPenalty` and
  `PresencePenalty` are now `*float64`, so an explicit `0` is distinguishable from
  unset. `WithFrequencyPenalty`/`WithPresencePenalty` callers are unaffected;
  struct-literal callers must use a `*float64`.
- **Removed `MustParseToolArguments`.** It panicked on model-controlled tool-call JSON
  (a denial-of-service vector). Use the error-returning `ParseToolArguments` /
  `ParseToolArgumentsMap`.
- **Registry construction of `runpod`** without an `endpoint_id` now returns an error
  wrapping `llms.ErrInvalidParameters` (previously `runpod.ErrMissingEndpointID`). The
  direct `runpod.New` constructor is unchanged.

### Added

- `llms.CollectStream` / `llms.StreamText` (and `StreamResult`) — drain a stream to
  completion and surface the terminal error explicitly instead of dropping the in-band
  `StreamChunk.Error`.
- Capability helpers completing the `As*`/`Supports*` set: `AsModelLister`,
  `SupportsModelListing`, `AsCapableProvider`, `SupportsReasoning`,
  `SupportsPromptCaching`, and `ModelCapabilities.ModelTypes()`.
- `Config.RequireExtra(provider, key)` plus exported `ExtraRunPodEndpointID` /
  `ExtraZAICoding` constants for explicit, validated provider configuration.
- Exported message validators `ValidateToolCallIDs` and `ValidateInlineSystem` for
  provider-author opt-in; core `ValidateMessages` is now provider-neutral.
- Built-in pricing for groq, fireworks, perplexity (sonar), and Z.AI (glm-4.7,
  glm-4.7-Flash), each with a sourced, dated comment. Local and BYO/subscription
  providers, and any model whose public price could not be verified, intentionally
  return `ok=false` rather than a fabricated $0.

### Fixed

- **Gemini streaming** now surfaces a SAFETY/RECITATION finish that produced no content
  as a terminal `StreamChunk.Error`, matching the non-streaming path (previously the
  stream completed cleanly with empty content and no error).
- **`Response`/`StreamChunk` JSON unmarshal** repopulates the deprecated `Thinking`
  alias from the canonical `Reasoning` field, which was lost on a JSON round-trip.
- **Metrics streaming span** is now ended before the active-request counter is
  decremented, fixing a telemetry-ordering race in which an observer could see
  `ActiveRequests() == 0` before the span had ended.

## [1.2.1] - 2026-06-15

### Fixed

- **Release tooling.** Removed an invalid `GOWORK=off` prefix from the GoReleaser
  `before` hooks. GoReleaser execs hooks directly (not through a shell), so the
  prefix was treated as an executable name and aborted artifact generation for the
  `v1.2.0` tag. The `v1.2.0` source is unaffected and installable via `go get`;
  this patch only repairs the release pipeline so binaries/SBOMs are published.

## [1.2.0] - 2026-06-15

Post-1.1.0 hardening from a full correctness/security/resilience review. No
breaking changes to exported APIs; additions are backward-compatible.

### Added

- `WithCallOptions(...CallOption)` for `RunTools`: forward model, temperature,
  max tokens, reasoning, and response-format options to every model turn in the
  agent loop. Registry tools still take precedence for the tools field.
- Rate-limit pacing options `WithRequestBurst(n)` and `WithTokenBurst(n)`.
- `WithMaxBatchSize(n)` for batch processing; exceeding the limit returns
  `ErrBatchTooLarge`.

### Fixed

- **Structured outputs.** `GenerateTyped[T]` now generates OpenAI strict-compatible
  schemas (`additionalProperties: false` on every object, all non-skipped fields
  required) and maps `time.Time`→`string` (date-time), `[]byte`→`string` (base64),
  and `json.RawMessage`/`encoding.TextMarshaler`/`json.Marshaler` to their real JSON
  shapes — previously these produced HTTP 400s or silently wrong output.
- **Reasoning leak.** Chain-of-thought no longer leaks into the visible `Content`
  stream and is no longer double-counted for OpenAI-compatible reasoning models
  (DeepSeek / Z.AI / Qwen), on both the streaming and non-streaming paths.
- **Anthropic.** Mid-stream `error` events (e.g. `overloaded_error`) now surface as
  a real error instead of a bare `EOF`; `tool_choice: "none"` is preserved instead
  of being coerced to `"auto"`.
- **Gemini.** System messages are sent via `systemInstruction` instead of being
  demoted to a user turn; `SAFETY`/`RECITATION` finishes with no content return a
  clear error instead of a silent empty success; model names are sanitized before
  URL interpolation.
- **Resilience.** The circuit breaker and the default fallback selector now treat
  transport-level failures (connection refused, EOF, timeouts) as provider-unhealthy,
  so they open / fail over when a provider is fully down (previously only HTTP
  429/5xx counted). Retries honor the `Retry-After` header; `MaxAttempts < 1` is
  clamped to 1 so a misconfigured retry never silently returns `(nil, nil)`; the
  underlying error is preserved when the breaker trips; a single logical call no
  longer multiplies breaker failure counts by the retry count; client-fault (4xx)
  errors no longer poison a provider's health.
- **Batch.** `ConcurrentBatcher` recovers per-item panics (one bad item no longer
  crashes the process) and rejects duplicate request IDs instead of silently
  dropping results.
- **Rate limiting.** Request burst defaults to 1 so requests are actually paced;
  token over-estimates are refunded; `WithMaxRetries`/`WithRetryDelay` no longer
  mutate a caller-shared `RetryConfig`; jitter is clamped to `[0,1]`.
- **Observability.** Emitted span/metric cost now includes cache tokens and honors
  custom pricing (matching `CostTracker`); models without pricing data are reported
  as unknown rather than a silent `$0.00`; `error_type` metric labels are bounded to
  prevent cardinality blow-up; request IDs are unique; span-text truncation is
  UTF-8-safe; the metrics streaming wrapper no longer leaks a goroutine / the
  active-request counter on consumer abandonment; cancelled streams record a non-OK
  status.
- **Other.** `ModelCapabilities.ToCapabilities()` no longer drops `Embeddings`/`Batch`;
  `WebSearchConfig`/`SearchResult` use snake_case JSON tags; `ValidateMessages`
  rejects a leading tool message; the Ollama native NDJSON client surfaces in-stream
  errors and removes the 64 KB line cap.

### Security

- Installing the SSRF dialer clones the HTTP transport instead of mutating a
  shared/default `http.Transport`, so constructing a client can no longer break
  unrelated HTTP in the host application.
- The SSRF guard rejects obfuscated IPv4 literals (octal / hex / decimal).
- SSE and response readers are size-bounded to prevent OOM from untrusted endpoints.
- The MCP stdio transport launches subprocesses with a minimal environment allowlist
  instead of inheriting the full parent environment, so provider API keys are not
  leaked to MCP servers by default — use `WithEnv` to pass variables a server needs.

### MCP

- The Streamable HTTP transport captures and echoes `Mcp-Session-Id` for stateful
  servers; `initialize` validates the server's protocol version; JSON-RPC id
  handling covers null / string / number ids and batch responses; tool calls use
  the per-call context.

### Docs

- `SECURITY.md` corrected (supported versions, latest release, no-arg
  `WithAllowPrivateIPs()` signature). The `llms-cli` binary now supports `--version`
  and a `version` subcommand.

## [1.1.0] - 2026-06-08

### Added

- **Unified reasoning ("thinking") API.** `WithReasoning`, `WithReasoningEffort`,
  and `WithReasoningBudget` request model reasoning across providers, mapping onto
  OpenAI `reasoning_effort`, Anthropic extended thinking (with automatic
  `max_tokens` adjustment), Gemini `thinkingConfig`, and Z.AI/Qwen toggles.
  Reasoning output is surfaced on `Response.Reasoning` / `StreamChunk.Reasoning`
  (with an Anthropic thinking `Signature`), reasoning tokens are reported in
  `Usage.ReasoningTokens`, and `Capabilities.Reasoning` advertises support.
- **Cross-provider prompt caching.** `WithCache`, `WithCacheTTL`, and
  `WithoutCache` control automatic caching; `Message.CacheControl` marks explicit
  breakpoints (Anthropic). Cache token usage is normalized across OpenAI,
  Anthropic, DeepSeek, and Gemini (`Usage.PromptTokens` now excludes cached
  tokens), `CostTracker`/`EstimateCost` price discounted cache reads/writes, and
  `Capabilities.PromptCaching` advertises support.
- **MCP client (`pkg/mcp`).** A minimal, dependency-free Model Context Protocol
  client (stdio + Streamable HTTP) for the tools subset. `Client.Register` exposes
  a server's tools to `llms.RunTools` via the existing `ToolRegistry`.

### Changed

- `Usage.PromptTokens` now excludes cache-read tokens for all providers so cost is
  computed uniformly; cache reads/writes are billed at their discounted rates.

### Hardened (pre-release review)

- Streaming tool-call accumulation rejects malformed indices (negative → no panic,
  absurd → no unbounded allocation); stream processing and the `RunTools` loop
  recover from panics instead of crashing the host.
- OpenAI reasoning models (o-series, gpt-5) send `max_completion_tokens` and omit
  unsupported sampling params instead of failing with HTTP 400.
- Added pricing for current default/flagship models (Claude Sonnet/Opus 4,
  Gemini 2.5, GPT-4.1, o3/o4-mini, DeepSeek) so cost tracking is no longer $0 by
  default; Gemini token accounting reconciles reasoning tokens correctly.
- HTTP client validates the resolved IP at dial time (blocks DNS-rebinding and
  redirect-to-private SSRF) and caps non-streaming response bodies.
- Resilience and fallback now observe streaming outcomes: a failed stream trips the
  circuit breaker and fails the chain over to the next client.
- Fixed a sliding-window metrics counter corruption; corrected DeepSeek reasoning
  capability and context window; Gemini reports `tool_calls` finish reason.

### Deprecated

- `WithThinkingMode` (use `WithReasoning`), `Response.Thinking` /
  `StreamChunk.Thinking` (use `Reasoning`), and `ThinkingContent` (use
  `ReasoningContent`). The old names remain as aliases and will be removed in a
  future major release.

## [1.0.0] - 2026-06-07

### Added

- Initial public release of the unified LLM SDK, including the core `llms.LLM`
  interface, provider implementations, streaming, tool calling, embeddings,
  resilience middleware, observability integrations, examples, and `llms-cli`.
