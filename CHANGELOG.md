# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added

- Together AI, Groq, Featherless, Mistral and Z.AI media routes, including native Together/Z.AI video jobs, vendor speech and transcription mappings, Together raw SSE, Featherless terminal character-usage callbacks, quota/moderation errors, sourced pricing, and gated integration tests. Together video preserves proxy prefixes and keeps unverified outputs.cost as metadata. Wire contracts are documentation-verified; live availability remains unverified.

- Gemini native image generation/editing, Veo polling video jobs with authenticated downloads, PCM speech, and Interactions transcription with speaker labels and word timing; media model options, pricing, and quota-aware live tests.

- ElevenLabs native media-only provider: speech and streaming, character timestamps, dialogue, Scribe transcription, sound effects/music, and Pro-plan Flows image editing/generation and polling video jobs, with media pricing and plan/moderation errors.

- OpenRouter chat and media provider with native image/video discovery, polling video jobs, speech and transcription, provider-reported media costs, optional speech usage lookup, and HTTP-Referer/X-Title attribution options.

- OpenAI image generation and editing, speech synthesis and SSE streaming, multipart transcription, and polling video jobs, with shared configurable OpenAI-compatible media routes and media pricing.

### Changed

- **`pkg/providers/anthropic`: behavior parity with the other providers.** A review
  of the Anthropic provider against the OpenAI-compatible and Gemini paths found a
  set of silent divergences; all are now aligned or documented (see the package
  doc's "Behavior Notes"):
  - `Stream` with `WithJSONSchema`/`WithJSONMode` now delivers the JSON on the
    terminal chunk's `Content`, matching `GenerateContent`.
  - `WithJSONMode` is honored (forced tool with an open object schema) instead of
    being ignored.
  - A streamed tool call with no arguments yields `Arguments == "{}"` rather than
    an empty string that fails to parse.
  - A stream closed by the server after `message_delta` but before `message_stop`
    completes normally instead of surfacing a `StreamError`; an EOF before
    `message_delta` is still reported as a truncated-stream error.
  - A system message anywhere but `messages[0]` is rejected by validation instead
    of being dropped.
  - `llms.WithExtraBody` / `WithExtraBodyParam` are merged into the request body
    (standard fields win on collision).
  - URL image sources are sent as `{"type":"url"}` instead of returning an error.
  - `redacted_thinking` blocks are captured on `ReasoningContent.Metadata` and
    replayed on the next turn.
  - `Usage.TotalTokens` now includes cache-read and cache-creation tokens, as the
    OpenAI and Gemini providers already did. `PromptTokens` is unchanged.
  - Thinking is sent in the shape each model generation accepts: `budget_tokens`
    on Claude 4.5 and earlier, `{type:"adaptive"}` plus `output_config.effort` on
    4.6 through the 5 family (where `budget_tokens` is rejected), and effort only
    on Fable/Mythos (thinking always on). `Enabled: false` sends
    `{type:"disabled"}` where accepted.
  - `temperature`/`top_p` are dropped only for the models that reject them
    (Opus 4.7/4.8, Opus 5, Sonnet 5, Fable/Mythos). Opus 4.6, Sonnet 4.6 and
    earlier, previously stripped by an over-broad match, receive them again.
  - A forcing `tool_choice` is softened to `auto` on Fable 5.1 / Mythos, which
    reject it. The existing softening for extended thinking now applies only to
    budget-based thinking (Claude <= 4.5); adaptive thinking accepts a forcing
    choice, verified live on Opus 4.6 and Sonnet 5.

### Added

- **Provider-agnostic media core.** Adds image generation/editing, video jobs, speech and transcription interfaces, shared media assets and moderation errors, media cost tracking, and multipart, binary, and polling transport helpers. Adds `ModelInfo.DeprecatedAt` and the four `Capabilities` fields `ImageGeneration`, `VideoGeneration`, `Speech`, and `Transcription`. HTTP 402 now maps to `ErrPlanRequired`; previously, unrecognized codes with `invalid_request_error` fell through to `ErrInvalidParameters`.

- **`pkg/mcp`: roots.** `WithRoots` publishes a fixed set of filesystem roots to the
  server; `WithRootsHandler` publishes a dynamic one, consulted per request. Adds
  `Root`, `RootsHandler` and `Client.RootsChanged`.

  URIs are validated: absolute `file://`, empty authority, no `.` or `..` segments.
  Static roots are validated at **construction** (a bad URI is a wiring bug worth
  surfacing immediately); dynamic ones per response, and a malformed root is reported
  to the server rather than published. Validation **rejects rather than normalizes** —
  silently rewriting a caller's path would publish a location they did not write.
  Notably `file://relative/path` is rejected: two slashes make `relative` the
  *authority*, so that URI does not mean what it looks like.

  This is advertisement hygiene, not sandboxing. The SDK serves no file reads, so a
  root grants a server no access it did not have; what a malformed one does is
  misinform it. Only a dynamic handler advertises `listChanged`, since only a dynamic
  set can change, and `RootsChanged` is a no-op otherwise.

- **`pkg/mcp`: elicitation.** `WithElicitationHandler` serves `elicitation/create`,
  letting a server ask the user for structured input. Adds `ElicitationRequest`,
  `ElicitationResult`, `ElicitationHandler` and the `ElicitationAccept`/`Decline`/
  `Cancel` actions.

  No separate approver is needed here, unlike sampling: the handler *is* the
  human-in-the-loop, and declining is expressible in the result. Content is **dropped**
  on decline or cancel, so a handler cannot leak partially-filled form data, and an
  unrecognized action is reported as an error rather than passed through — a server
  seeing an unknown action might otherwise assume success. With no handler registered
  the capability is unadvertised and requests get `MethodNotFound`, so a host with no
  way to ask a human is never presented as able to.

  `RequestedSchema` is server-supplied and therefore untrusted: render it as a form,
  and never auto-accept a schema you did not show a user — a server can ask for
  anything, including credentials.

  This completes MCP Track C.

### Added

- **`pkg/mcp`: sampling — serve `sampling/createMessage` from any `llms.LLM`.** An MCP
  server can now ask the host to run a completion, and the host answers with a model it
  already owns, under its own credentials and budget. `WithSamplingLLM` adapts any
  `llms.LLM`; `WithSamplingHandler` takes a custom source. Adds `SamplingRequest`,
  `SamplingResult`, `SamplingMessage`, `ModelPreferences`, `ModelHint`,
  `SamplingHandler`, `SamplingApprover`, `SamplingApproval`, `ApproveAllSampling`, and
  `ContentBlock.Data`/`.MimeType` for image content. All additions are
  apidiff-compatible, and `ContentBlock` stays **comparable** (both new fields are
  strings) — pinned by a test.

  **Consent is mandatory and enforced at construction.** Configuring sampling without
  `WithSamplingApprover` makes `NewStdioClient`/`NewHTTPClient` **return an error**.
  Denying at request time instead would hide the misconfiguration until a server first
  asks — which is exactly when nobody is watching. `ApproveAllSampling()` is the single,
  greppable opt-out; do not use it with a server you do not control.

  The approver runs **before** any model call, on the inbound worker rather than the
  read path, so it may block on real human input. A denial invokes no LLM at all
  (pinned by a stub that fails the test if called) and returns `CodeInvalidRequest` —
  a deliberate refusal the server should not retry. `SamplingApproval`'s zero value is
  a **denial**, so a forgotten field refuses rather than approves. An approval may only
  **lower** the requested `MaxTokens`, never raise it.

  Host-supplied `llms.CallOption` values are applied **last**, so the host always wins —
  pass `llms.WithModel` to force a cheaper model regardless of what the server asked for.
  `modelPreferences` is accepted and **ignored**: model choice belongs to whoever pays
  for the tokens. `includeContext` is likewise accepted and ignored, since honoring
  `"allServers"` would splice other servers' conversation into a request made by this one.

  **Transport boundary:** server-initiated requests work over **stdio only**. The
  Streamable HTTP transport has no standalone SSE listener to receive them and no path
  to send a response frame back, so a handler registered on an HTTP client is never
  invoked — and the capability is **not advertised** there, since telling a server this
  client samples when the request can never arrive leaves it waiting on a promise the
  transport cannot keep.

- **`pkg/mcp`: server-initiated request dispatch.** The client can now serve
  requests a server sends *to it*, the direction the package previously had no path
  for at all (v6.0.1 made such frames answerable; this makes them serviceable).
  Adds `ClientCapabilities` — with `SamplingCapability`, `RootsCapability`,
  `ElicitationCapability` — plus `Client.ClientCapabilities()` and
  `Client.RefusedRequests()`. All additions are apidiff-compatible.

  **Capabilities are derived from registered handlers, never declared separately.**
  A server told this client samples, which then answers `MethodNotFound`, has no way
  to recover — so handlers are installed before `initialize` and the advertised set
  is computed from them. Advertisement and capability cannot drift.

  **Concurrency deliberately differs from the notification pump.** That pump is
  serial and *drops* on overflow, which is fine for progress events. Neither
  property is safe for requests: dropping one leaves the server waiting forever, and
  serial execution would head-of-line-block every request behind a slow one
  (sampling can take tens of seconds). Handlers therefore run concurrently, bounded
  at 8, and overflow is **refused with an error response and counted** via
  `RefusedRequests()` rather than dropped. Parsing, handler lookup and refusal stay
  on the transport read path — they are cheap and a refusal must not consume a
  slot — while handler execution moves to a worker, so a slow handler cannot stall
  responses to the client's own in-flight calls.

  A panicking handler is converted into an `InternalError` response and the client
  keeps serving; the panic value is **not** put on the wire, since it can carry host
  internals and the peer is not necessarily trusted. Teardown waits for in-flight
  handlers but is bounded, so a wedged handler cannot make `Close` hang.

  No capability is served yet — this is the machinery. Sampling, roots and
  elicitation follow.

## [6.1.0] - 2026-08-03

### Added

- **Request-mode pricing (batch / flex / fast).** `PricingMode` plus
  `WithPricingMode`, `CostTracker.RecordMode`/`SetModePricing`/`GetModeCosts`, and
  `EstimateCostMode` price a request under a provider's asynchronous or
  premium-latency rate card instead of the interactive one. The zero value is
  standard, so existing behavior is unchanged; every addition is apidiff-compatible.

  **Rates are absolute per-model cards, not multipliers** — because that is how
  providers publish them and the ratios are not uniform. OpenAI's Fast mode is 2×
  standard on `gpt-5.6-sol` but 2.5× on `gpt-5.5`; its Batch tier drops cached-input
  pricing entirely for some older models; and not every model appears in every
  tier's table (`gpt-5.4-nano` has no Fast row at all). A per-provider multiplier
  would have invented precision the published data does not have. Anthropic's Batch
  tier is the one exception: it publishes a flat 50% rule, so those cards are derived
  from stated policy rather than transcribed, with prompt-cache multipliers applied
  to the batch input rate as documented.

  Seeded from first-party tables verified 2026-08-02: OpenAI Batch/Flex/Fast for the
  gpt-5.6/5.5/5.4 families, and Anthropic Batch (all models) plus Fast (Opus 5 and
  Opus 4.8 only — Opus 4.7 rejects fast mode and Opus 4.6 silently runs at standard
  rates). A provider/model with no published card for a mode is priced at **standard**
  rates and reports `known=false`, so an unknown lane is never silently discounted.

  The mode is **accounting only** — it does not route the request. Send the request to
  the lane with the provider's own mechanism (OpenAI's `service_tier` via
  `WithExtraBodyParam`, Anthropic's Batches API), then set the matching mode.

  Per-mode cost lives on the tracker (`GetModeCosts`) rather than on `ModelUsage`,
  which stays **comparable** — adding a map field to it would have been the same
  breaking change that forced v6.

  *Known gap:* OpenAI publishes separate long-context columns for Batch and Flex that
  are not yet transcribed, so a batch request above the 272K threshold prices at
  short-context batch rates and reads low. A test pins this as deliberate rather than
  accidental; encoding a guessed long-context batch rate would be worse.

## [6.0.1] - 2026-08-03

### Fixed

- **`pkg/mcp`: server-initiated requests were misrouted, silently corrupting
  concurrent calls.** Frame dispatch classified purely on the presence of a JSON-RPC
  `id`, but a server-initiated request (`sampling/createMessage`, `roots/list`,
  `elicitation/create`) carries **both** a method and an id — so it was treated as a
  *response*. If its id collided with an in-flight client call, the request frame was
  delivered to that caller, which then found neither a result nor an error and
  returned a **nil-result success**: `CallTool` handed back an empty result with no
  error. With no collision the frame was dropped and the server waited forever.
  Server ids are server-chosen while client ids start at 1, so collision was the
  likely case, not an exotic one. A second flaw: ids were parsed with `ParseInt`, so
  a non-numeric string id (`"abc"`) failed to parse and was misrouted to the
  *notification* sink.

  Dispatch now classifies frames three ways (response / notification / request) and
  ids are echoed back verbatim in their original JSON form, as JSON-RPC requires. A
  client with no handler registered answers `MethodNotFound` rather than dropping the
  frame, so a nonconforming server is unblocked instead of hanging. The HTTP framing
  path drops inbound requests rather than mistaking one for a POST's response.

  No exported API changed. Anyone using `pkg/mcp` against a server that initiates
  requests should treat prior empty-but-successful tool results as suspect.

## [6.0.0] - 2026-08-03

v6 is a deliberately small major release. The module path is now
`github.com/nocturnium/llm-go-sdk/v6` (Go semantic import versioning); v5 and v6 are
distinct module paths and can coexist, so consumers may migrate incrementally. See
`docs/migration-guide.md` for the v5 → v6 guide.

There is exactly **one** breaking change, and it is a compile-time break rather than a
behavioral one: `llms.Pricing` gained a `Tiers` field so long-context pricing can be
modeled correctly, and because that field is a slice, `Pricing` is no longer comparable.
Nothing about existing pricing behavior changes. Most migrations are an import-path bump
plus `go mod tidy`.

### Changed (BREAKING)

- **Module path → `/v6`.** `go get github.com/nocturnium/llm-go-sdk/v6@v6.0.0` (the core
  package name stays `llms`).
- **`llms.Pricing` is no longer comparable.** It gained `Tiers []PricingTier`, so `==`,
  `!=`, use as a map key, and comparison of any enclosing struct no longer compile. Use
  `reflect.DeepEqual`, or compare the scalar rate fields directly. Every existing field,
  its JSON key, and the cost arithmetic for untiered models are unchanged; `Tiers` is
  omitted from JSON when empty, so serialized metadata for untiered models is
  byte-identical to v5. This is the sole reason for the major version.

### Added

- **Long-context pricing tiers.** `Pricing.Tiers` models the case where a provider
  reprices an *entire* request once its input crosses a threshold — OpenAI's gpt-5 family
  above 272K input tokens (2× input, 1.5× output) and Gemini Pro tiers above 200K. v5
  priced these at the short-context rate, so **cost estimates for long-context requests on
  those models were roughly half the true figure**; they are now correct. The threshold is
  evaluated on total input (`PromptTokens + CacheReadTokens + CacheCreationTokens`), since
  `Usage.PromptTokens` excludes cache tokens by contract while providers threshold on the
  full input. Tiers need not be sorted — the highest matching threshold wins — and an unset
  cache rate on a tier falls back to that tier's own `Input` rate. Models with no published
  long-context row (OpenAI's mini/nano variants) are deliberately left flat rather than
  given an invented tier. **If you have dashboards or budget alerts calibrated against v5's
  understated long-context numbers, re-baseline them.**

- **Current-generation model coverage** (verified against first-party pricing/model pages
  on 2026-08-02, not inferred): Anthropic `claude-opus-5` ($5/$25) and `claude-sonnet-5`
  ($3/$15) — the current Claude 5 flagships, previously absent entirely — plus
  `claude-mythos-5`; OpenAI's `gpt-5.6` line (`-sol`, `-terra`, `-luna`) and the
  `gpt-5.1`/`5.2`/`5.3-codex` tiers; Google `gemini-3.6-flash` and `gemini-3.5-flash-lite`.
  Each is registered in all three places that carry model data — provider `knownModels`,
  `DefaultPricing`, and the capability registry — so `EstimateCost` now returns a price
  (rather than `ok=false`) and capability lookups no longer fall through to stale provider
  defaults for these IDs.

### Fixed

- **Gemini cache-read pricing was overstated ~2.5×.** `DefaultPricing` derived Gemini
  `CacheRead` from an assumed 0.25× multiple of the prompt rate, but Google publishes
  cached input at ≈0.1×. Every Gemini entry now uses the published per-model figure
  (e.g. `gemini-2.5-flash` 0.075 → 0.03, `gemini-3.5-flash` 0.375 → 0.15), and models
  that previously had no cache rate at all — and so silently billed cache reads at the
  full prompt rate — now carry one. Cost estimates for cache-heavy Gemini workloads were
  too high; they are now correct.

### Notes

- Claude Sonnet 5 carries introductory pricing of $2/$10 per MTok through 2026-08-31.
  `DefaultPricing` records the standard $3/$15: a static table cannot express a
  time-boxed promotion, and an estimate that silently expires is worse than one that is
  consistently conservative. Register the promotional rate via the cost tracker's
  custom-pricing API if you need it.
- OpenAI `gpt-5.x` prices are the short-context (&lt;272K) tier. Requests above 272K input
  tokens bill at 2× input / 1.5× output for the entire request — not expressible in a
  per-token table, so cost estimates for those requests will read low.
- `gpt-5.6` publishes a total context window (1,050,000) larger than its maximum input
  (922,000), the remainder being reserved for output. `ModelInfo.ContextLength` carries the
  total; `ModelCapabilities.MaxContextTokens` — documented as the maximum *input* window —
  carries 922,000, which is the figure to size a prompt against. This is the first model
  family where the two genuinely differ, which is why every other entry mirrors a single
  number. Max output is 128,000 for all three variants.
- Claude Opus 4.1 (`claude-opus-4-1`) is deprecated and retires **2026-08-05**. Its pricing
  entry is retained for cost attribution of historical usage.
- Documentation: `docs/roadmap.md` marked MCP Track B items 2–4 (capabilities coverage,
  notifications, `Register` ergonomics) as shipped — all three have been implemented since
  the doc was last touched — corrected the apidiff baseline reference from `api/v3.txt` to
  `api/v5.txt`, and added Track C scoping the unimplemented MCP server-side surface
  (sampling, roots, elicitation).

## [5.1.0] - 2026-07-12

### Changed

- **`pkg/mcp`: `WithAllowHTTP` is now independent of `WithAllowPrivateIPs`**,
  matching the root `llms.Config` decoupling shipped in v5.0.0. Plain (non-TLS)
  HTTP is off by default and no longer rides along with private-IP access:
  reaching an `http://` MCP server now requires **both** `WithAllowPrivateIPs(true)`
  **and** `WithAllowHTTP(true)`. This closes the same silent-cleartext /
  credential-leak vector for the MCP subsystem that v5.0.0 closed for provider
  clients. No exported API changes (`apidiff` stays compatible) — if you connect
  to a local `http://` MCP server, add `WithAllowHTTP(true)`.

## [5.0.0] - 2026-07-12

v5 is a major release. The module path is now `github.com/nocturnium/llm-go-sdk/v5`
(Go semantic import versioning); v4 and v5 are distinct module paths and can coexist, so
consumers may migrate incrementally. See `docs/migration-guide.md` for the full v4 → v5
guide. v5 removes the last long-deprecated shims and cleans up several overloaded or
inconsistent APIs; the core surface (`Call`, `GenerateContent`, `Stream`, tools, structured
output, embeddings, providers, middleware) is unchanged in shape. Every change was built as
an independently expert-gated packet, and the release passed a unanimous tri-reviewer
(CTO + Go-expert + 10x) ship gate.

### Changed (BREAKING)

- **Module path → `/v5`.** `go get github.com/nocturnium/llm-go-sdk/v5@v5.0.0` (the core
  package name stays `llms`).
- **`Config.AllowHTTP` is now independent of `AllowPrivateIPs`** (secure default `false`).
  Enabling private-IP access previously also permitted plain HTTP; reaching a private
  `http://` endpoint via `Config` now requires `AllowHTTP: true`. Local providers
  (`ollama`/`llamacpp`/`infinity`) still default both on. Closes a cleartext / API-key-leak
  foot-gun.
- **Removed the deprecated `Thinking` API.** `Response.Thinking()`, `StreamChunk.Thinking()`,
  the `ThinkingContent` alias, and `WithThinkingMode` are gone — use `Reasoning`.
- **Unified the two pricing types into one `llms.Pricing`**
  (`{Input, Output, CacheRead, CacheWrite, Hourly, Finetune, Base}`). `ModelPricing` is
  removed and `ModelInfo.Pricing` is now `*Pricing`; the `openaicompat.ModelPricing` alias
  becomes `openaicompat.Pricing`. All pricing values are preserved.
- **`EstimateCost` now returns `(float64, bool)`** (comma-ok); `EstimateCostKnown` is removed.
- **`ToolChoice` de-overloaded** to `{Mode ToolChoiceMode; Tool string}` (was
  `{Type ToolChoiceType; Function *FunctionReference}`); `ToolChoiceTool` added and
  `FunctionReference` removed. The provider wire encoding is unchanged.
- **`AnthropicTTL` removed from the root package** — prompt-cache TTL handling now lives
  inside the anthropic provider.
- **Renames & constructor changes:** `WithModelsLimit`/`WithModelsCursor` →
  `WithModelLimit`/`WithModelCursor`; `openaicompat.ProviderConfig.ProviderName` removed
  (`Provider` is the single identity); `NewMemoryResponseCache(ttl, maxEntries)` is now
  bounded by default (`NewBoundedMemoryResponseCache` removed); `resilience.ErrRateLimitExceeded`
  now wraps `llms.ErrRateLimited` (so `errors.Is(err, llms.ErrRateLimited)` matches).
- **Default Gemini model is now `gemini-2.5-flash`** (was the retired `gemini-2.0-flash`);
  removed the retired `gemini-2.0-flash`/`-lite`, RunPod `ModelLlama31_405B`/`ModelMixtral8x7B`,
  and Z.AI `ModelGLM47FlashX`/`ModelGLM47Flash` model IDs.

### Notes

- Relocating the stream-authoring toolkit to a dedicated package was evaluated and
  **deferred** (a genuine root-package import cycle via `CostMiddleware.Stream`); it has no
  effect on the v5 public API and is earmarked for a future architectural pass.

## [4.2.0] - 2026-07-12

A correctness, resilience, and transport-security hardening sweep. Additive — no breaking
changes. See the
[v4.2.0 release notes](https://github.com/nocturnium/llm-go-sdk/releases/tag/v4.2.0) for the
full list.

## [4.1.1] - 2026-06-21

Security and correctness hardening — the HIGH-severity items from a post-v4.1.0 review.
Additive — no breaking changes.

### Security

- **Strip provider API-key headers on cross-host redirects** (`x-goog-api-key`, `api-key`,
  `x-api-key`, `Authorization`, `Proxy-Authorization`) so a redirect to a foreign host cannot
  leak the raw key (CWE-200).

### Fixed

- **`FallbackChain` data race** between dispatch reads and `AddClient`/`RemoveClient` (an
  immutable snapshot is now taken under the read lock).
- **Streaming token usage** silently lost on OpenAI-compatible providers — streaming requests
  now send `stream_options.include_usage` so the terminal usage chunk is emitted.
- **Stale model capabilities** for the current GPT-5 / Claude / Gemini flagships.
- **Non-compiling embeddings godoc** examples for the OpenAI and Gemini packages.

## [4.1.0] - 2026-06-21

Additive, non-breaking fast-follow on top of v4.0.0 — observability for two
previously-silent failure paths, plus internal cleanups. Every change is API-compatible
(the `apidiff` baseline grows, with no removals).

### Added

- **`mcp.Client.DroppedNotifications() uint64`** — exposes the cumulative count of
  notifications dropped when a slow or wedged handler overflows the bounded notification
  queue, so consumers can observe otherwise-silent notification loss.
- **`resilience.WithOnCallbackPanic(func(recovered any, from, to CircuitState))`** — an
  opt-in circuit-breaker option that delivers a panicking `onStateChange` callback's
  recovered value (and the state transition) to a hook instead of silently discarding it.
  Default behavior is unchanged (the panic is still recovered with no output) unless the
  option is set; the hook runs inside the existing single recovered goroutine.

### Changed

- **Consolidated the CR/LF log-injection sanitizer** into a single canonical
  `internal/logsanitize` used by both the HTTP client and the observability loggers. The
  unified implementation is a strict superset of the previous per-package copies — it
  neutralizes CR, LF, tab, NUL, ESC, DEL, and all Unicode control runes.
- **HTTP-client error messages** are now extracted from provider error JSON envelopes
  (`{"error":{"message":…}}`, `{"error":"…"}`, `{"message":…}`) when present, falling back
  to the bounded, sanitized response body. `APIError.Code` / `Type` (and therefore error
  classification via `errors.Is`) are unchanged.
- **`llamacpp` caches the `/props` response** after the first successful fetch, so
  `Model()` and `Capabilities()` no longer issue a network request on every call (a failed
  fetch stays uncached and is retried on the next call).

## [4.0.0] - 2026-06-21

v4 is a major release. The module path is now `github.com/nocturnium/llm-go-sdk/v4`
(Go semantic import versioning); v3 and v4 are distinct module paths and can coexist, so
consumers may migrate incrementally. See `docs/migration-guide.md` for the v3 → v4 guide.
The release is dominated by a security / correctness / resilience hardening sweep (each
fix gated by an independent expert review); the breaking surface is deliberately small.

### Changed (BREAKING)

- **Module path → `/v4`.** `go get github.com/nocturnium/llm-go-sdk/v4@v4.0.0` (the core
  package name stays `llms`).
- **Removed the deprecated `Thinking` fields.** `Response.Thinking` and
  `StreamChunk.Thinking` were exported, mutable alias *fields* that had to be hand-synced
  with `Reasoning` — a desync foot-gun (`Response{Reasoning: x}` left `Thinking` nil). They
  are now deprecated *methods* `Thinking() *ReasoningContent` computed from `Reasoning`. Use
  `Reasoning` (or call `.Thinking()`).
- **Removed the unused `ErrorMapper` registry.** `ErrorMapper`, `ErrorMapperRegistry`,
  `MapProviderError`, `RegisterErrorMapper`, `DefaultErrorMapperRegistry`, and related
  symbols were never wired into any production path (dead code). Error classification is
  automatic — match the exported sentinels with `errors.Is`.

### Security

- **SSRF: DNS-rebinding is closed on the custom-`DialContext` path.** When a caller
  supplied an `*http.Client` whose transport already had a `DialContext`, the resolved-IP
  guard was silently skipped; the resolved remote IP is now re-validated on every dial path.
- **`WithHTTPClient` no longer mutates the caller's `*http.Client`** — it shallow-copies
  before installing the SSRF dialer / redirect policy, leaving the caller's `Transport` and
  `CheckRedirect` untouched.
- **The Ollama NDJSON stream reader is bounded** (4 MB cap) against unbounded-allocation /
  OOM from a hostile endpoint, matching the SSE and JSON readers.
- Reject NAT64-embedded private IPv6 addresses; strip userinfo / query / fragment from
  `APIError` messages so query-string secrets are not leaked in error text.

### Fixed

- **Streaming requests are no longer bound by the unary `http.Client.Timeout`** (default
  5 m), which previously tore down long streams mid-read; streams are bounded by `ctx`.
- **MCP notification handlers are never invoked concurrently** (the documented contract):
  the overflow path no longer spawns a goroutine per notification — overflow is counted
  atomically and drained by the single serial pump.
- **Error classification now maps 404 → `ErrModelNotFound` and 502/504/529 →
  `ErrServiceUnavailable`**, with the classify and retry tables driven from one source so
  they cannot disagree.
- **The circuit-breaker `onStateChange` path no longer leaks a watchdog goroutine + timer**
  per transition; the callback runs in a single recovered goroutine.
- **`MetricsMiddleware.Stream` no longer permanently skews `ActiveRequests()`** if the
  wrapped stream panics synchronously.
- **The Gemini and Anthropic streaming goroutines recover panics** and deliver a terminal
  stream error instead of crashing the host process (matching the shared streaming path).
- **Model token pricing has a single source of truth** (`DefaultPricing`); displayed and
  billed prices can no longer drift, enforced by reconciliation tests. Corrected
  `gemini-2.0-flash` to $0.10 / $0.40 per 1M tokens (it was mistakenly priced at
  Flash-Lite's rate).
- **Reasoning options compose order-independently** — `WithReasoning` now merges into a
  previously-set `WithReasoningEffort` / `WithReasoningBudget` / `WithThinkingMode` instead
  of clobbering it.
- Retryable response bodies are drained before close, restoring keep-alive connection reuse.

### Changed

- **The `llms-cli` demo no longer depends on `urfave/cli`.** It was rewritten on the
  standard-library `flag` package, removing `urfave/cli/v2`, `russross/blackfriday/v2`,
  `cpuguy83/go-md2man/v2`, and `xrash/smetrics` from the module's dependency graph entirely
  — they no longer appear in a library consumer's `go.sum`. CLI commands and flags are
  unchanged; only help-text formatting differs.

## [3.1.0] - 2026-06-21

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
