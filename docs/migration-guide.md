# Migrating from v4 to v5

v5 is a major release that removes the last of the long-deprecated shims and
cleans up several overloaded or inconsistent APIs. The core surface (`Call`,
`GenerateContent`, `Stream`, tools, structured output, embeddings, providers,
middleware) is unchanged in shape — most migrations are a mechanical import-path
bump plus a handful of renames the compiler will point you straight at.

## 1. Update the import path

```bash
go get github.com/nocturnium/llm-go-sdk/v5@v5.0.0
```

Rewrite every import of the module from the `/v4` path to `/v5` (the core package
name stays `llms`):

```bash
# from the root of your module
grep -rl 'nocturnium/llm-go-sdk/v4' --include='*.go' . \
  | xargs sed -i 's#nocturnium/llm-go-sdk/v4#nocturnium/llm-go-sdk/v5#g'
go mod tidy
```

If you only use the core API and none of the symbols below, that plus the
compiler is the entire migration.

## 2. Security: `AllowHTTP` is now independent of `AllowPrivateIPs`

In v4, setting `Config.AllowPrivateIPs` also implicitly permitted plain-HTTP
(non-TLS) URLs. In v5 the two are **independent** — `AllowPrivateIPs` governs
private/loopback destinations only, and a new `Config.AllowHTTP bool` governs the
`http://` scheme. Both default to `false` (secure).

```go
// v4: one flag relaxed both checks
cfg := llms.Config{AllowPrivateIPs: true} // also allowed http://

// v5: opt into each relaxation explicitly
cfg := llms.Config{AllowPrivateIPs: true, AllowHTTP: true}
```

If you talk to a local `http://` endpoint (Ollama, llama.cpp, a private gateway),
add `AllowHTTP: true`. The provider-level options gained a matching
`WithAllowHTTP()` alongside `WithAllowPrivateIPs()`.

## 3. The deprecated `Thinking` API is removed

`Response.Thinking()`, `StreamChunk.Thinking()`, the `ThinkingContent` type alias,
and the `WithThinkingMode` option (all deprecated throughout v4) are **gone**. Use
the canonical `Reasoning` surface:

```go
rc := resp.Reasoning                 // was resp.Thinking()
// build requests with WithReasoning(...) instead of WithThinkingMode(...)
```

## 4. Pricing types unified into one `llms.Pricing`

v4 had two pricing structs — the cost-layer `Pricing` (`PromptPerMillion` /
`CompletionPerMillion`) and the model-layer `ModelPricing` (`Input` / `Output` /
`Hourly`). v5 merges them into a single canonical type:

```go
type Pricing struct {
    Input      float64 // per 1M input tokens, USD
    Output     float64 // per 1M output tokens, USD
    CacheRead  float64 // per 1M cache-read tokens, USD
    CacheWrite float64 // per 1M cache-write tokens, USD
    Hourly     float64 // dedicated-instance rate, USD (metadata)
    Finetune   float64 // fine-tune cost per 1M tokens, USD (metadata)
    Base       float64 // provider-specific base cost, USD (metadata)
}
```

`ModelPricing` is removed; `ModelInfo.Pricing` is now `*Pricing`. If you read
prices off the old `Pricing.PromptPerMillion` / `.CompletionPerMillion`, switch to
`.Input` / `.Output`.

## 5. `EstimateCost` returns `(cost, known)`

`EstimateCost` now reports whether pricing was known via a second return value,
and the separate `EstimateCostKnown` function is removed:

```go
// v4
cost := llms.EstimateCost(provider, model, usage)
known := llms.EstimateCostKnown(provider, model, usage)

// v5 — comma-ok
cost, known := llms.EstimateCost(provider, model, usage)
```

## 6. `ToolChoice` is no longer overloaded

The overloaded `ToolChoice{Type ToolChoiceType; Function *FunctionReference}` is
replaced by `ToolChoice{Mode ToolChoiceMode; Tool string}`. `ToolChoiceType` is
renamed `ToolChoiceMode`, a `ToolChoiceTool` mode is added, and the
`FunctionReference` type is removed.

```go
// v4: force a specific tool
tc := llms.ToolChoice{Type: llms.ToolChoiceFunction,
    Function: &llms.FunctionReference{Name: "get_weather"}}

// v5
tc := llms.ToolChoice{Mode: llms.ToolChoiceTool, Tool: "get_weather"}
// or the option helper:
llms.WithToolChoiceTool("get_weather")
```

`auto` / `none` / `required` are now `ToolChoiceAuto` / `ToolChoiceNone` /
`ToolChoiceRequired` values of `Mode` (the wire encoding is unchanged).

## 7. `AnthropicTTL` moved into the anthropic provider

The root-package `AnthropicTTL` helper (an Anthropic-specific prompt-cache detail)
is removed from `llms`. Prompt-cache TTL handling now lives inside the anthropic
provider and needs no caller action — drop any direct reference to
`llms.AnthropicTTL`.

## 8. Renames and constructor changes

| v4 | v5 |
|----|----|
| `WithModelsLimit(n)` | `WithModelLimit(n)` |
| `WithModelsCursor(c)` | `WithModelCursor(c)` |
| `openaicompat.ProviderConfig.ProviderName` | removed — `Provider` is the single identity |
| `NewBoundedMemoryResponseCache(ttl, max)` | `NewMemoryResponseCache(ttl, max)` (now bounded by default) |

`NewMemoryResponseCache` now takes a `maxEntries` argument and is bounded by
default; the separate `NewBoundedMemoryResponseCache` is removed. And
`resilience.ErrRateLimitExceeded` now wraps `llms.ErrRateLimited`, so
`errors.Is(err, llms.ErrRateLimited)` matches a resilience rate-limit error.

## 9. Model-ID cleanup and new Gemini default

- The default Gemini model is now **`gemini-2.5-flash`** (was `gemini-2.0-flash`).
- Removed retired model IDs: `gemini-2.0-flash`, `gemini-2.0-flash-lite`
  (`gemini-2.0-flash-exp` is retained), and the RunPod `ModelLlama31_405B` /
  `ModelMixtral8x7B` and Z.AI `ModelGLM47FlashX` / `ModelGLM47Flash` constants.
- If you pinned any of those IDs, pick a current model from `ListModels` or the
  provider's `models.go`.

---

# Migrating from v3 to v4

v4 is a major release whose changes are dominated by a security / correctness / resilience
hardening sweep — the breaking surface is small and migration is mostly a mechanical
import-path bump. The core API (`Call`, `GenerateContent`, `Stream`, tools, structured
output, embeddings, providers, middleware) is unchanged.

## 1. Update the import path

```bash
go get github.com/nocturnium/llm-go-sdk/v4@v4.0.0
```

Rewrite every import of the module from the `/v3` path to `/v4` (the core package name
stays `llms`):

```bash
# from the root of your module
grep -rl 'nocturnium/llm-go-sdk/v3' --include='*.go' . \
  | xargs sed -i 's#nocturnium/llm-go-sdk/v3#nocturnium/llm-go-sdk/v4#g'
go mod tidy
```

If you only use the core API, that is the entire migration — you are done.

## 2. Replace `Thinking` with `Reasoning`

The `Response.Thinking()` / `StreamChunk.Thinking()` methods, the `ThinkingContent` type
alias, and the `WithThinkingMode` option have been **removed in v5** (they were deprecated
throughout v4). Read chain-of-thought from the canonical `Reasoning` field:

```go
rc := resp.Reasoning         // canonical field
```

If you built a `Response` / `StreamChunk` struct literal with `Thinking:`, set `Reasoning:`
instead, and replace `WithThinkingMode(true/false)` with `WithReasoning`.

## 3. Drop any use of the `ErrorMapper` registry (it was dead code)

`ErrorMapper`, `ErrorMapperRegistry`, `MapProviderError`, `RegisterErrorMapper`, and
`DefaultErrorMapperRegistry` were never used on any production path and have been removed.
Error classification is automatic — match the exported sentinels with `errors.Is`:

```go
if errors.Is(err, llms.ErrModelNotFound) { ... }      // now also fires for a bare HTTP 404
if errors.Is(err, llms.ErrServiceUnavailable) { ... } // now also fires for 502 / 504 / 529
```

## 4. Note: `gemini-2.0-flash` cost estimate corrected

`EstimateCost` / `CostTracker` priced `gemini-2.0-flash` at the Flash-Lite rate by mistake;
it is now $0.10 / $0.40 per 1M input/output tokens (its actual price). No API change — only
the computed cost differs.

## 5. `llms-cli` flag parsing (only if you script the demo CLI)

The `llms-cli` binary was rewritten on the standard-library `flag` package, removing the
`urfave/cli` dependency from the module. All commands and flags are preserved; only the
help-text formatting differs.

---

# Migrating from v2 to v3

v3 is a major release with **one** structural change: the observability and resilience
middleware moved out of the root `llms` package into leaf subpackages, so importing
`llms` for the core types no longer pulls in the OpenTelemetry SDK. Every moved symbol
keeps its exact name and signature — migration is a mechanical import/qualifier update.

## 1. Update the import path

```bash
go get github.com/nocturnium/llm-go-sdk/v3@v3.0.0
```

Rewrite every import of the module from the `/v2` path to `/v3` (the core package name
stays `llms`):

```bash
# from the root of your module
grep -rl 'nocturnium/llm-go-sdk/v2' --include='*.go' . \
  | xargs sed -i 's#nocturnium/llm-go-sdk/v2#nocturnium/llm-go-sdk/v3#g'
go mod tidy
```

If you only use the core API (providers, `Call`, `GenerateContent`, tools, structured
output, embeddings, cost tracking), that is the entire migration — you are done.

## 2. Repoint moved middleware (only if you used it)

The middleware constructors/types kept their names but now live in two new packages.
Add the relevant import and change the qualifier:

| v2 (root `llms`) | v3 package | import |
|---|---|---|
| `llms.NewResilientClient`, `llms.NewFallbackChain`, `llms.NewRateLimitedClient`, `llms.NewCircuitBreaker`, `RetryConfig`, `ResilienceOption`, fallback/rate-limit/circuit-breaker types & options | `resilience.*` | `github.com/nocturnium/llm-go-sdk/v3/pkg/middleware/resilience` |
| `llms.NewOTelMiddleware`, `llms.NewMetricsMiddleware`, `llms.NewLangfuseOTelMiddleware`, `llms.NewJSONLogger`, `llms.NewSlogLogger`, the `Attr*` semantic-convention constants, `LogEntry`, `Logger`, trace-context helpers | `observability.*` | `github.com/nocturnium/llm-go-sdk/v3/pkg/observability` |

Example:

```go
// v2
import llms "github.com/nocturnium/llm-go-sdk/v2"

client := llms.NewResilientClient(base, llms.WithMaxRetries(3))
client = llms.NewOTelMiddleware(client)

// v3
import (
	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/middleware/resilience"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/observability"
)

client := resilience.NewResilientClient(base, resilience.WithMaxRetries(3))
client = observability.NewOTelMiddleware(client)
```

There are **no other breaking changes** in v3 — no signature changes, no removed
behavior. If your code does not reference the moved middleware, step 1 is sufficient.

Why the move: it lets a consumer import `llms` for `Message`/`Response`/`Call` without
transitively compiling the OpenTelemetry SDK (the bare root package's OTel dependency
count drops from ~20 packages to 0). The middleware is opt-in; you only pay for OTel
when you import `pkg/observability`.

---

# Migrating from v1 to v2

v2 is a major release. Per Go's semantic import versioning, the module path gains a
`/v2` suffix, so upgrading is a two-part change: update the import path, then adjust
for a small number of breaking API changes.

## 1. Update the import path

```bash
go get github.com/nocturnium/llm-go-sdk/v2@v2.0.0
```

Then rewrite every import of the module to the `/v2` path (the package name stays
`llms`):

```go
// v1
import (
	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/providers/openai"
)

// v2
import (
	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
)
```

A mechanical sweep works for the whole tree:

```bash
find . -name '*.go' -print0 | xargs -0 sed -i \
  's#github.com/nocturnium/llm-go-sdk#github.com/nocturnium/llm-go-sdk/v2#g; s#/v2/v2#/v2#g'
```

v1 and v2 are distinct module paths and can coexist, so you can migrate one consumer
at a time.

## 2. Breaking API changes

| Change | v1 | v2 | Migration |
|--------|----|----|-----------|
| **Tool handlers take a context** | `func(args json.RawMessage) (any, error)` | `func(ctx context.Context, args json.RawMessage) (any, error)` | Add `ctx context.Context` as the first parameter of every `ToolHandler` / `RegisterFunc` handler. `ToolRegistry.Handle` and `HandleAll` also gain a leading `ctx`. Handlers now receive a context that is canceled when the agent loop is canceled or a turn errors — honor it for long-running tools. |
| **Sampling penalties are pointers** | `FrequencyPenalty float64`, `PresencePenalty float64` | `*float64` | If you set these via the `WithFrequencyPenalty` / `WithPresencePenalty` options, no change is needed. If you construct `CallOptions` as a struct literal, wrap values in a `*float64` (an explicit `0` is now distinguishable from unset). |
| **`MustParseToolArguments` removed** | `MustParseToolArguments[T](tc)` (panicked on malformed model output) | — | Use the error-returning `ParseToolArguments[T](tc)` or `ParseToolArgumentsMap(tc)`. The `Must` variant was a denial-of-service risk because it panicked on model-controlled JSON. |
| **RunPod registry error value** | `llms.New("runpod", cfg)` without `endpoint_id` returned `runpod.ErrMissingEndpointID` | returns an error wrapping `llms.ErrInvalidParameters` (names both provider and key) | Construction still fails the same way; only the error *value* changed. If you matched `errors.Is(err, runpod.ErrMissingEndpointID)` on the **registry** path, match `llms.ErrInvalidParameters` instead. The direct `runpod.New(...)` constructor still returns `ErrMissingEndpointID`. |

Everything else is additive. New conveniences in v2 worth knowing: `llms.CollectStream` /
`llms.StreamText` (drain a stream without dropping the terminal error), the completed
capability-helper set (`AsModelLister`, `SupportsModelListing`, `AsCapableProvider`,
`SupportsReasoning`, `SupportsPromptCaching`), and `Config.RequireExtra` for explicit,
validated provider config keys (`ExtraRunPodEndpointID`, `ExtraZAICoding`).

# Package Layout

The `llm-go-sdk` has a small, flat public surface. There is **one** import style, and
all shared types live in the root package.

> **Note:** Earlier pre-1.0 builds shipped a set of `pkg/*` alias packages
> (`pkg/types`, `pkg/options`, `pkg/errors`, `pkg/streaming`, `pkg/search`,
> `pkg/middleware/*`) and a top-level `providers/*` backwards-compatibility shim.
> **Those have all been removed.** If you used any of them, switch to the layout below:
> the same symbols now live in the root `llms` package, and providers are imported from
> `pkg/providers/<name>`.

## Public packages

| Package | Import path | What's in it |
|---------|-------------|--------------|
| Root (`llms`) | `github.com/nocturnium/llm-go-sdk/v5` | The entire core: the `LLM` interface, `Message`/`Response`/`Tool`/`Usage` types, `CallOption` builders (`WithTemperature`, `WithMaxTokens`, …), errors and sentinels, streaming, the capability registry, and every middleware (cost, resilience, rate limiting, fallback, OTel, Langfuse, logging, metrics). |
| Providers | `github.com/nocturnium/llm-go-sdk/v5/pkg/providers/<name>` | The 18 provider implementations (`openai`, `anthropic`, `gemini`, `groq`, …). Each exposes `New(...)` plus its own `WithX(...)` construction options. |
| All-providers registry | `github.com/nocturnium/llm-go-sdk/v5/pkg/providers/all` | Blank-import only. Registers all 17 chat providers' factories so `llms.New(name, llms.Config{...})` and `llms.NewFromEnv()` can construct them by name. |
| OpenAI-compatible base | `github.com/nocturnium/llm-go-sdk/v5/pkg/openaicompat` | The shared base client for building your own OpenAI-compatible provider without forking the SDK. |

Everything else lives under `internal/` and is not importable by external code.

## The one import style

Import the root package for the shared types and options, and import the provider you
need from `pkg/providers/<name>`:

```go
package main

import (
	"context"
	"fmt"
	"log"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/openai"
)

func main() {
	client, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		log.Fatal(err)
	}

	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are helpful"},
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := client.GenerateContent(context.Background(), messages,
		llms.WithTemperature(0.7),
		llms.WithMaxTokens(100),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Content)
}
```

## Construct by name (registry)

In addition to importing a provider package directly, you can construct a provider from a
string name. Blank-import `pkg/providers/all` once to register every chat provider's
factory, then call `llms.New` or `llms.NewFromEnv`:

```go
import (
	llms "github.com/nocturnium/llm-go-sdk/v5"
	_ "github.com/nocturnium/llm-go-sdk/v5/pkg/providers/all" // registers all 17 chat providers
)

client, err := llms.New("openai", llms.Config{Model: "gpt-4o-mini"})

// Or entirely from the environment (LLM_PROVIDER required, LLM_MODEL optional):
client, err = llms.NewFromEnv()
```

`llms.Config` holds the common construction settings (`APIKey`, `Model`, `BaseURL`,
`Timeout`, `AllowPrivateIPs`, `AllowHTTP`, `HTTPClient`) plus `Extra map[string]string` for
provider-specific construction params (e.g. RunPod `endpoint_id`, Z.AI `coding`).

## Adding middleware

Every middleware wraps an `llms.LLM` and returns a value that also satisfies `llms.LLM`,
so hold the client in an `llms.LLM` variable and reassign it as you add layers:

```go
base, _ := openai.New()
var client llms.LLM = base

tracker := llms.NewCostTracker()
client = llms.NewCostMiddleware(client, tracker)
client = llms.NewResilientClient(client) // opt-in retries + circuit breaker
```

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full design, including the
middleware/decorator chain and how providers are built.
