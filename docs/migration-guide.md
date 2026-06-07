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
| Root (`llms`) | `github.com/nocturnium/llm-go-sdk` | The entire core: the `LLM` interface, `Message`/`Response`/`Tool`/`Usage` types, `CallOption` builders (`WithTemperature`, `WithMaxTokens`, …), errors and sentinels, streaming, the capability registry, and every middleware (cost, resilience, rate limiting, fallback, OTel, Langfuse, logging, metrics). |
| Providers | `github.com/nocturnium/llm-go-sdk/pkg/providers/<name>` | The 18 provider implementations (`openai`, `anthropic`, `gemini`, `groq`, …). Each exposes `New(...)` plus its own `WithX(...)` construction options. |
| All-providers registry | `github.com/nocturnium/llm-go-sdk/pkg/providers/all` | Blank-import only. Registers all 17 chat providers' factories so `llms.New(name, llms.Config{...})` and `llms.NewFromEnv()` can construct them by name. |
| OpenAI-compatible base | `github.com/nocturnium/llm-go-sdk/pkg/openaicompat` | The shared base client for building your own OpenAI-compatible provider without forking the SDK. |

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

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/providers/openai"
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
	llms "github.com/nocturnium/llm-go-sdk"
	_ "github.com/nocturnium/llm-go-sdk/pkg/providers/all" // registers all 17 chat providers
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
