# Roadmap — cutting-edge tracks

This file tracks the "cutting edge of LLM SDKs" work identified in the v2 architecture
teardown (CTO / 10x / Go-idioms review). Items are grouped by status.

## Shipped (v2.x, additive / non-breaking)

- **API contract gate** — `apidiff` baseline (`api/v5.txt`) + `make apidiff` in CI guards the
  ~950-symbol public surface (single-version policy, no v1 fallback).
- **Discoverability** — curated API map in `doc.go`, runnable `Example*` tests, no-facade
  decision recorded in `ARCHITECTURE.md`.
- **Accurate tokenization** — `llms.Tokenizer` interface + opt-in `pkg/tokenizer` (tiktoken,
  offline loader). Replaces the chars/4 heuristic that produced wrong cost estimates.
- **Structured-output self-repair** — `GenerateTyped` recovers fenced/prose-wrapped JSON;
  `GenerateTypedWithRepair` retries with model feedback on parse failure.
- **Response caching** — `CachedClient` middleware + pluggable `ResponseCache`
  (in-memory TTL default), complementing the existing provider-neutral prompt-cache control.

## Shipped (v3.0.0)

- ✅ **v3 middleware extraction** — moved the observability + resilience middleware out of the
  root so bare `llms` stops compiling ~20 OpenTelemetry packages. Observability middleware now
  lives in `pkg/observability` and resilience middleware in `pkg/middleware/resilience`; the root
  retains cost tracking and response caching. Shipped in v3.0.0 (the deliberate major it was
  reserved for). See [`v3-package-taxonomy.md`](./v3-package-taxonomy.md).

## Planned — follow-up tracks (each a multi-PR effort; deliberately not built inline)

These are large, provider-coupled, and benefit from integration testing against live APIs.
Scoped here so they can be picked up as focused PRs.

### Track A — OpenAI Responses API

**Why:** `pkg/openaicompat` is `/chat/completions`-only (`client.go`). The Responses API
(`POST /v1/responses`) is where the category is moving: stateful conversations, built-in
tools, and first-class reasoning items.

**Scope (sequence as separate PRs):**
1. ✅ **Types + non-streaming client + provider opt-in.** *(Shipped.)* Responses wire types,
   `Client.CreateResponse`, and `BuildResponsesRequest`/`ConvertResponsesResponse` in
   `pkg/openaicompat`; `ProviderConfig.UseResponsesAPI` routes non-streaming `GenerateContent`
   through `/responses`; `openai.WithResponsesAPI()` opt-in. Basic `store`/`previous_response_id`
   pass-through via `CallOptions.ExtraBody`. Unit + round-trip + end-to-end tests.
2. ✅ **Statefulness ergonomics.** *(Shipped.)* `llms.Response.ID` is surfaced from both the
   chat-completions and Responses converters; `openai.WithPreviousResponseID(id)` and
   `openai.WithStore(bool)` chain server-side conversation state without touching `ExtraBody`.
3. ✅ **Streaming.** *(Shipped — live-smoke-tested.)* `Stream` routes through
   `/responses` when `WithResponsesAPI()` is set. The typed SSE events
   (`response.output_text.delta`, `response.reasoning_summary_text.delta`, `response.completed`,
   `error`, …) are parsed into the existing `StreamChunk` channel; tool calls, usage, and finish
   reason come from the authoritative `response.completed` payload.
4. ✅ **Reasoning-item round-trip.** *(Shipped — live-verified.)* For stateless multi-turn with
   o-series / gpt-5 reasoning models, `openai.WithReasoningRoundTrip()` requests encrypted
   reasoning items (`include: ["reasoning.encrypted_content"]`); they are stashed on
   `Response.Reasoning.Metadata` and replayed as `"reasoning"` input items when that reasoning is
   carried on the next assistant `Message.Reasoning`. Validated live (`TestLiveResponses_ReasoningRoundTrip`).

> Note: streaming is exercised against both mock SSE fixtures and a live OpenAI smoke test
> (`pkg/providers/openai/responses_live_test.go`, `//go:build integration`, gated on
> `OPENAI_API_KEY`).

**Risks:** distinct request/response shape and a new SSE event grammar; correctness needs
live-API testing, hence the staged PRs rather than an inline build.

### Track B — Best-in-class MCP client

**Why:** `pkg/mcp` already implements a client (jsonrpc + http/stdio transports) — a genuine
wedge, since few Go LLM SDKs have first-class MCP. Worth investing to make it best-in-class.

**Scope (sequence as separate PRs):**
1. ✅ **Audit + error mapping.** *(Shipped.)* Audit found the transport layer already well-hardened
   (bounded reads, env isolation, concurrent id-correlated dispatch, context cancellation,
   kill-on-timeout close, SSRF defaults, session-id capture, pagination). Gap was error
   inspectability: exported `RPCError` (+ standard JSON-RPC code constants) so protocol errors are
   `errors.As`-able, and added `ToolError` so a tool that ran-but-failed is distinguishable from a
   protocol failure.
2. ✅ **Capabilities coverage** — *(Shipped, client side.)* Tools, resources (`ListResources`/
   `ReadResource`), and prompts (`ListPrompts`/`GetPrompt`) per the MCP spec, with
   `ServerCapabilities` exposed on the client. MCP tools map onto `llms.Tool`/`ToolRegistry`
   via `Client.Register`, so they drop into the `RunTools` agent loop. **Sampling is not
   implemented** — see Track C below.
3. ✅ **Streaming/notifications** — *(Shipped.)* Server-initiated notifications are dispatched
   by a pump goroutine (`notifications.go`): `OnProgress`/`OnLog` handlers, per-call progress
   tokens via `CallToolWithProgress`, bounded queues with a `DroppedNotifications` counter, and
   teardown on context cancellation as well as `Close`.
4. ✅ **Ergonomics** — *(Shipped.)* `Client.Register(*llms.ToolRegistry)` mounts every tool an
   MCP server exposes in one call (`mount.go`).

**Risks:** spec breadth and transport edge cases; stage behind the audit so hardening lands
before new surface.

### Track C — MCP server-side surface (sampling, roots, elicitation)

**Status: complete.** All four steps have shipped; what follows is the record of what each
delivered.

**Why:** `pkg/mcp` was a client-only implementation. The three capabilities an MCP server can ask
*of the client* are unimplemented: **sampling** (`sampling/createMessage`), **roots**, and
**elicitation**. Sampling is the highest-value of the three and a genuine differentiator for this
SDK specifically — it lets an MCP server request an LLM completion from the host, and this SDK
already owns a provider-agnostic `llms.Model` to serve it with. No other Go LLM SDK is positioned
to close that loop as cleanly.

**Scope (sequence as separate PRs):**
0. ✅ **Inbound-frame routing fix.** *(Shipped, v6.0.1.)* A prerequisite that turned out to be a
   live bug: frames were classified on the presence of a JSON-RPC id, so a server-initiated
   request — which carries both a method and an id — was treated as a response and could resolve
   an unrelated in-flight call with an empty result.
1. ✅ **Inbound request dispatch + capability advertisement.** *(Shipped.)* Bounded-concurrency
   dispatch (`inbound.go`) that refuses rather than drops on overflow (`RefusedRequests`), runs
   handlers off the transport read path, contains panics, and tears down bounded. Typed
   `ClientCapabilities` derived from the registered handlers so advertisement cannot drift.
2. ✅ **Sampling + consent.** *(Shipped.)* `WithSamplingLLM` answers `sampling/createMessage` from
   any `llms.LLM`; `WithSamplingHandler` takes a custom source. **`WithSamplingApprover` is
   mandatory** — configuring sampling without it fails client construction, so a server can never
   silently spend the host's tokens; `ApproveAllSampling()` is the single explicit opt-out. An
   approval may only lower the requested `MaxTokens`, never raise it. `modelPreferences` and
   `includeContext` are accepted and ignored (model choice stays with the host; honoring
   `allServers` would leak other servers' context).
3. ✅ **Roots.** *(Shipped.)* `WithRoots` for a fixed set (URIs validated at construction) and
   `WithRootsHandler` for a dynamic one (validated per response, and the only form that advertises
   `listChanged`; `Client.RootsChanged` emits the notification). Validation requires an absolute
   `file://` URI with an empty authority and no `.`/`..` segments, and **rejects rather than
   normalizes** — silently rewriting a caller's path would publish a location they did not write.
   This is advertisement hygiene, not sandboxing: the SDK serves no file reads, so a root grants no
   access, but a malformed one misinforms the server.
4. ✅ **Elicitation.** *(Shipped.)* `WithElicitationHandler` serves `elicitation/create`; the
   handler is itself the human-in-the-loop, so no separate approver is needed (declining is
   expressible in the result). Content is dropped on decline/cancel so partially-filled form data
   cannot leak, and an unrecognized action is reported as an error rather than passed through — a
   server seeing an unknown action might assume success.

**Transport boundary:** server-initiated requests work over **stdio only**. The Streamable HTTP
transport has no standalone GET SSE listener to receive them and no path to POST a response frame
back, so handlers registered on an HTTP client are never invoked and their capabilities are not
advertised. Adding a GET SSE listener would unblock this wholesale and is worth its own track.

**Risks:** this inverts the request direction (server → client), which the jsonrpc layer now
handles alongside the notification pump. The concurrency model deliberately differs from that pump:
dropping a request hangs the server, and serial execution would head-of-line-block everything
behind a slow sampling call.

### Track D — Media generation (image, video, speech, transcription)

**Status: packets D0 to D5 and D7 shipped (2026-09-05); D6 dedicated providers remain opt-in.** Research and
full design in
[`docs/track-d-media-generation.md`](track-d-media-generation.md).

**Why:** the SDK can list image and audio models but cannot call a single generation endpoint,
and the provider landscape has consolidated onto a small set of shapes: OpenAI-style sync routes
for images and audio, async create → poll → fetch jobs for video, and three output deliveries
(inline bytes, expiring URL, cloud URI). One capability layer plus the existing `openaicompat`
client covers OpenAI, Azure, OpenRouter audio, Together, Groq, Featherless, Mistral STT and Z.AI;
native adapters cover ElevenLabs and Gemini.

**Scope (sequence as separate PRs):**
0. ✅ Core interfaces (`ImageGenerator`, `VideoGenerator`/`VideoJob`, `SpeechSynthesizer`,
   `Transcriber`), `MediaAsset`, moderation error, `MediaPricing`, multipart/binary/poll transport.
1. ✅ `openaicompat` media routes + OpenAI (gpt-image-2, sora-2, gpt-4o-mini-tts, gpt-transcribe).
2. ✅ OpenRouter provider (chat + `/images`, `/videos`, `/audio/*`).
3. ✅ ElevenLabs provider (TTS, STT, SFX, music, flows image/video).
4. ✅ Gemini media (native image, Veo 3.1, TTS, transcribe).
5. ✅ Thin-provider enablement (Together, Groq, Featherless, Mistral, Z.AI).
6. ⏸ Dedicated providers on explicit go: fal, Replicate, Black Forest Labs, Deepgram. *(Not started; opt-in.)*
7. ✅ Docs, examples, release.

**Risks:** video live tests are expensive (gate behind a second env var); several endpoint details
are unverified until probed; asset URLs expire (10 min on BFL); ElevenLabs image/video is a
Pro-plan beta reseller surface.
