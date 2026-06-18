# Roadmap — cutting-edge tracks

This file tracks the "cutting edge of LLM SDKs" work identified in the v2 architecture
teardown (CTO / 10x / Go-idioms review). Items are grouped by status.

## Shipped (v2.x, additive / non-breaking)

- **API contract gate** — `apidiff` baseline (`api/v2.txt`) + `make apidiff` in CI guards the
  ~950-symbol public surface (single-version policy, no v1 fallback).
- **Discoverability** — curated API map in `doc.go`, runnable `Example*` tests, no-facade
  decision recorded in `ARCHITECTURE.md`.
- **Accurate tokenization** — `llms.Tokenizer` interface + opt-in `pkg/tokenizer` (tiktoken,
  offline loader). Replaces the chars/4 heuristic that produced wrong cost estimates.
- **Structured-output self-repair** — `GenerateTyped` recovers fenced/prose-wrapped JSON;
  `GenerateTypedWithRepair` retries with model feedback on parse failure.
- **Response caching** — `CachedClient` middleware + pluggable `ResponseCache`
  (in-memory TTL default), complementing the existing provider-neutral prompt-cache control.

## Planned — follow-up tracks (each a multi-PR effort; deliberately not built inline)

These are large, provider-coupled, and benefit from integration testing against live APIs.
Scoped here so they can be picked up as focused PRs.

### Track A — OpenAI Responses API

**Why:** `pkg/openaicompat` is `/chat/completions`-only (`client.go`). The Responses API
(`POST /v1/responses`) is where the category is moving: stateful conversations, built-in
tools, and first-class reasoning items.

**Scope (sequence as separate PRs):**
1. **Types + non-streaming client.** Add Responses request/response types to `pkg/openaicompat`
   and a `Responses(ctx, ...)` client method hitting `/responses`. Map `[]llms.Message` →
   the `input` item array; parse `output` items (`message`/`output_text`, `function_call`,
   `reasoning`) back into `*llms.Response` (content + `ToolCalls` + reasoning). Unit-test with a
   mock transport, mirroring `engine_test.go`.
2. **Provider opt-in.** A client option on `pkg/providers/openai` (e.g. `WithResponsesAPI()`)
   that routes `GenerateContent` through `/responses`; default stays chat-completions for
   compatibility.
3. **Statefulness.** Support `store` + `previous_response_id` (surface via `CallOptions`
   extras or a dedicated option) so callers can chain server-side conversation state.
4. **Streaming.** Parse the Responses SSE event types (`response.output_text.delta`,
   `response.reasoning.*`, `response.completed`, …) into the existing `StreamChunk` channel.
5. **Reasoning-item round-trip.** For o-series models, pass prior `reasoning` items back on
   the next turn. Needs live verification.

**Risks:** distinct request/response shape and a new SSE event grammar; correctness needs
live-API testing, hence the staged PRs rather than an inline build.

### Track B — Best-in-class MCP client

**Why:** `pkg/mcp` already implements a client (jsonrpc + http/stdio transports) — a genuine
wedge, since few Go LLM SDKs have first-class MCP. Worth investing to make it best-in-class.

**Scope (sequence as separate PRs):**
1. **Audit + harden the existing client** — transport lifecycle/cancellation, error mapping,
   timeouts, reconnect; raise test coverage on the current surface before adding features.
2. **Capabilities coverage** — tools, resources, prompts, and sampling per the MCP spec; map
   MCP tools onto `llms.Tool`/`ToolRegistry` so they drop into the `RunTools` agent loop.
3. **Streaming/notifications** — handle server-initiated notifications and progress.
4. **Ergonomics** — a one-call helper to mount an MCP server's tools into a `ToolRegistry`.

**Risks:** spec breadth and transport edge cases; stage behind the audit so hardening lands
before new surface.

## Notable structural item (separate doc)

- **v3 middleware extraction** — move observability + resilience middleware out of the root so
  bare `llms` stops compiling ~20 OpenTelemetry packages. Breaking; reserved for a deliberate
  major. See [`v3-package-taxonomy.md`](./v3-package-taxonomy.md).
