# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

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
