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

**Why:** `pkg/mcp` is a client-only implementation. The three capabilities an MCP server can ask
*of the client* are unimplemented: **sampling** (`sampling/createMessage`), **roots**, and
**elicitation**. Sampling is the highest-value of the three and a genuine differentiator for this
SDK specifically — it lets an MCP server request an LLM completion from the host, and this SDK
already owns a provider-agnostic `llms.Model` to serve it with. No other Go LLM SDK is positioned
to close that loop as cleanly.

**Scope (sequence as separate PRs):**
1. **Sampling types + capability advertisement** — declare `sampling` in the client's
   `ClientCapabilities` during `initialize`; add the `sampling/createMessage` request/result types.
2. **Handler wiring** — route incoming `sampling/createMessage` requests to a caller-supplied
   handler, plus a built-in adapter that satisfies them from an `llms.Model`. Must include a
   human-in-the-loop approval hook: the MCP spec treats sampling as requiring user consent, and a
   server that can silently spend the host's tokens is a real abuse vector.
3. **Roots** — expose filesystem roots to the server, with the same path-confinement discipline
   the transport layer already applies elsewhere.
4. **Elicitation** — structured user-input requests from server to client.

**Risks:** this inverts the request direction (server → client), so the jsonrpc layer needs an
inbound request path alongside the existing notification pump; sampling additionally needs a
consent model, so it should not ship without the approval hook in step 2.
