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
| Root (`llms`) | `github.com/nocturnium/llm-go-sdk/v2` | The entire core: the `LLM` interface, `Message`/`Response`/`Tool`/`Usage` types, `CallOption` builders (`WithTemperature`, `WithMaxTokens`, …), errors and sentinels, streaming, the capability registry, and every middleware (cost, resilience, rate limiting, fallback, OTel, Langfuse, logging, metrics). |
| Providers | `github.com/nocturnium/llm-go-sdk/v2/pkg/providers/<name>` | The 18 provider implementations (`openai`, `anthropic`, `gemini`, `groq`, …). Each exposes `New(...)` plus its own `WithX(...)` construction options. |
| All-providers registry | `github.com/nocturnium/llm-go-sdk/v2/pkg/providers/all` | Blank-import only. Registers all 17 chat providers' factories so `llms.New(name, llms.Config{...})` and `llms.NewFromEnv()` can construct them by name. |
| OpenAI-compatible base | `github.com/nocturnium/llm-go-sdk/v2/pkg/openaicompat` | The shared base client for building your own OpenAI-compatible provider without forking the SDK. |

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

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
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
	llms "github.com/nocturnium/llm-go-sdk/v2"
	_ "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/all" // registers all 17 chat providers
)

client, err := llms.New("openai", llms.Config{Model: "gpt-4o-mini"})

// Or entirely from the environment (LLM_PROVIDER required, LLM_MODEL optional):
client, err = llms.NewFromEnv()
```

`llms.Config` holds the common construction settings (`APIKey`, `Model`, `BaseURL`,
`Timeout`, `AllowPrivateIPs`, `HTTPClient`) plus `Extra map[string]string` for
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
