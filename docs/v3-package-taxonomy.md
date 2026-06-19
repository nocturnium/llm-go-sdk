# v3 Package Taxonomy (design — not yet implemented)

> Status: **realized in v3.0.0.** This document originally captured the plan; it has now
> been fully executed on the `reorg/middleware-extraction` branch as the v3 release:
> resilience → `pkg/middleware/resilience`, observability → `pkg/observability`, with the
> module path bumped to `/v3`. Verified payoff: `go list -deps` OTel count on the bare root
> dropped **20 → 0**, root shrank 40 → 26 non-test files, 172 middleware symbols moved to
> the leaf packages, with build/vet/test/lint/apidiff green. See `CHANGELOG.md` (3.0.0) and
> the v2 → v3 section of `docs/migration-guide.md`. It is the considered output of a
> CTO / 10x-architect / Go-idioms teardown of the v2 layout.

## Why a v3 at all

The v2 root package `llms` is a clean dependency **leaf**: every provider imports it for
both types (`llms.Message`) and functions (`llms.ApplyOptions`, `llms.PrepareMessages`,
`llms.WrapProviderError`), and the root imports **none** of its own sub-packages. That flat
layout is idiomatic for a Go SDK and should mostly stay.

The one real defect: **importing bare `llms` transitively compiles ~20 OpenTelemetry
packages.** Verified:

```
GOWORK=off go list -deps github.com/nocturnium/llm-go-sdk/v2 | grep -c go.opentelemetry.io
# => 20
```

A library that markets itself as "native Go HTTP calls with no external LLM dependencies"
should not force the OTel SDK onto consumers who only want `Message`/`Response`/`Call`. The
fix is to move the observability and resilience **middleware out** of root into leaf
sub-packages. Because the dependency arrow already points the right way
(`middleware → core`, never the reverse), this is cohesive — but it changes public import
paths, so it is **breaking** and belongs in a major version.

### Why not a facade instead (rejected)

A "thin root that re-exports from sub-packages" does not work in Go here and is explicitly
rejected (see `ARCHITECTURE.md`): root re-exporting from sub-packages it depends on creates
`root → subpkg → root` **import cycles**; and functions (the bulk of the ~950-symbol surface)
cannot be aliased, only wrapped, which fractures godoc and creates hundreds of hand-maintained
shims. v3 does **real package moves with a curated root**, not a re-export shim.

## Target layout

```
root  llms/                       # CORE CONTRACT — stays (the leaf everything imports)
pkg/providers/<name>/             # unchanged (18 providers, self-register via init())
pkg/openaicompat/                 # unchanged (public custom-provider base)
pkg/observability/                # NEW: OTel + Langfuse + metrics + logging middleware
pkg/middleware/resilience/        # NEW: retry, circuit breaker, fallback, rate limit
internal/*                        # unchanged
```

### Stays at root (the curated core)

- Core types: `Message`, `Response`, `StreamChunk`, `Usage`, `Tool`/`ToolCall`/`FunctionCall`,
  `ContentPart`, `CallOption`/`CallOptions`, `Capabilities`, the `LLM`/`Wrapper`/`CapableProvider`/
  `Embedder`/`Reranker` interfaces.
- Registry + construction: `Config`, `New`, `NewFromEnv`, `RegisterProvider`,
  `RegisteredProviders`, `ProviderFactory`. **Must stay** — provider `init()` registration and the
  `ProviderFactory` signature are baked into every `register.go`.
- The provider-authoring toolkit consumed by providers/openaicompat in their signatures:
  `WrapProviderError`, `RequireAPIKey`, `ApplyOptions`, `PrepareMessages`, `NewStreamSender`,
  `WrapStreamWithFinalizer`, `ValidateMessages`, the `Env*`/`ModelType*` constants, `ModelInfo`,
  `ModelPricing`, errors + sentinels.
- Features that are part of the core vocabulary: structured output, vision, embeddings, tokens,
  cost types, capability registry. (Cost *middleware* may move; the `Pricing`/`Usage` types stay.)

### Moves out (leaf middleware — imports core, nothing imports it)

| New package | Files moved | Carries dep |
|---|---|---|
| `pkg/observability` | `metrics.go`, `metrics_otel.go`, `metrics_sliding_window.go`, `otel.go`, `otel_genai.go`, `otel_langfuse.go`, `langfuse.go`, `langfuse_format.go`, `logging.go`, `logging_json.go`, `logging_slog.go` | OpenTelemetry SDK |
| `pkg/middleware/resilience` | `resilience.go`, `resilience_circuit_breaker.go`, `resilience_retry.go`, `fallback.go`, `ratelimit.go` | none |

Payoff metric after the move:

```
GOWORK=off go list -deps github.com/nocturnium/llm-go-sdk/v2 | grep -c go.opentelemetry.io
# expected => 0
```

## The seam is already clean (verified)

The private helpers each cluster uses are **cohesive within that cluster**, so they move with
their files — no pre-extraction or duplication needed:

- `truncateUTF8`, `slidingWindow`/`newSlidingWindow` → used only by telemetry files → move into
  `pkg/observability`.
- `isProviderUnhealthy` → used only by the resilience cluster → moves into `pkg/middleware/resilience`.
- `WrapStreamWithFinalizer`, `NewStreamSender` → cross-cluster but already **exported at root**
  (provider toolkit) → stay; moving packages import them from root.
- The only other unexported core helpers (`deliverTerminalForContext`, `isValidRole`,
  `joinStrings`, `mapGenericStatus`) are **not** referenced by any moving cluster.

Invariant for whoever executes v3: if a moving file references an *unexported* root symbol, that
symbol must be exported (deliberate new API) or relocated — never duplicated. Per the audit above,
no such case exists today.

## Migration shape (breaking — no forwarders)

There is **no non-breaking bridge**: a root forwarder like `var NewResilientClient = resilience.New`
forces `root → resilience` and re-creates the cycle. So v3 is a clean rename:

| v2 (root) | v3 |
|---|---|
| `llms.NewOTelMiddleware(...)` | `observability.NewOTel(...)` |
| `llms.NewMetricsMiddleware(...)` | `observability.NewMetrics(...)` |
| `llms.NewLangfuse*` / logging middleware | `observability.New*` |
| `llms.NewResilientClient(...)` | `resilience.New(...)` |
| `llms.NewFallbackChain(...)` | `resilience.NewFallbackChain(...)` |

`NewResilientClient` is referenced **only at root** today (verified), so extracting resilience does
not pull resilience/OTel into `pkg/providers/all`. Ship a `docs/migration-guide.md` v2→v3 section
with this table and codemod-friendly sed recipes.

## Execution sequencing (keep build green at every step)

1. Add the apidiff baseline for the *new* v3 module surface up front (the v2 gate already exists).
2. Move one cluster at a time, **file + its white-box test together** (36/37 test files are
   `package llms`; each becomes `package observability` / `package resilience`). Build + test after
   each file.
3. Update `examples/resilience`, `examples/fallback`, `examples/cost-tracking` to the new import
   paths in the same change (they gate the module build and are the migration canary).
4. After observability moves, assert the payoff metric (`grep -c go.opentelemetry.io` on root deps
   is 0).
5. Bump the module path to `/v3`, regenerate the API baseline, write the migration guide.

## Explicitly NOT in v3

- No facade / re-export shim (cycles; see above).
- No `pkg/types`/`pkg/options` re-split (already deleted once for v2; the core is a correct leaf).
- No telemetry rewrite — the decision was to keep the existing observability stack and only change
  *where* it lives.
