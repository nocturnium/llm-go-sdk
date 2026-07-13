# Provider Registry (Construct by Name)

For the canonical decision tree that distinguishes direct constructors, the
registry, and the `pkg/openaicompat` provider-author extension point, see
[Choosing how to construct a client](../getting-started.md#choosing-how-to-construct-a-client).

Most of the SDK constructs a client through a provider package, e.g.
`openai.New(openai.WithModel("gpt-4o"))`. That is the right approach when the
provider is known at compile time. When the provider is chosen at **runtime** —
from a config file, an environment variable, a CLI flag, or a database — use the
**registry** instead.

The registry maps a provider **name** (a string) to a factory that builds a
client from a common [`llms.Config`](#the-config-struct). It is the foundation
for config-driven applications that need to switch providers without recompiling.

```go
import (
	llms "github.com/nocturnium/llm-go-sdk/v4"
	_ "github.com/nocturnium/llm-go-sdk/v4/pkg/providers/all" // register all chat providers
)

client, err := llms.New("openai", llms.Config{Model: "gpt-4o"})
```

!!! important "You must register providers before `llms.New` can find them"
    `llms.New` only knows about providers that have registered themselves. The
    simplest way is the blank import `_ ".../pkg/providers/all"`, which registers
    all 17 chat providers. See [Registering providers](#registering-providers).

## When to use the registry

Use `llms.New` / `llms.NewFromEnv` when:

- The provider name comes from configuration, environment, or user input.
- You want to swap providers without changing code (12-factor / config-driven apps).
- You are writing a tool, gateway, or CLI that supports many providers behind one flag.

Use the direct provider constructor (`openai.New(...)`, `anthropic.New(...)`)
when the provider is fixed at compile time and you want the strongly-typed,
provider-specific options. The registry deliberately exposes only the **common**
construction settings shared across providers (plus an [`Extra`](#provider-specific-extra-keys)
escape hatch); the direct constructors expose the full option set.

## Registering providers

A provider must register a factory under its name before `llms.New("<name>", …)`
can construct it. Each provider package does this in its `init()` function, so a
**blank import** is enough to wire it up.

### Register everything

```go
import _ "github.com/nocturnium/llm-go-sdk/v4/pkg/providers/all"
```

This registers all **17 chat providers**:

`anthropic`, `azure`, `cerebras`, `deepseek`, `featherless`, `fireworks`,
`gemini`, `groq`, `llamacpp`, `mistral`, `ollama`, `openai`, `perplexity`,
`runpod`, `synthetic`, `togetherai`, `zai`.

!!! note "Infinity is not in the chat registry"
    `infinity` is an embeddings/reranking-only provider and is **not** a chat
    provider, so it is not registered by `pkg/providers/all` and cannot be
    constructed with `llms.New`. Construct it directly with
    `infinity.New(...)`.

### Register only the providers you need

To keep the binary smaller (and avoid pulling in providers you don't use), blank
import only the specific provider packages:

```go
import (
	_ "github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"
	_ "github.com/nocturnium/llm-go-sdk/v4/pkg/providers/anthropic"
)
```

### List what is registered

```go
names := llms.RegisteredProviders() // sorted []string
fmt.Println(names)
// e.g. [anthropic azure cerebras deepseek featherless fireworks gemini
//       groq llamacpp mistral ollama openai perplexity runpod synthetic
//       togetherai zai]
```

`RegisteredProviders` returns the names that are currently registered (sorted
alphabetically). It reflects exactly which provider packages have been imported.

## `llms.New`

```go
func New(name string, cfg Config) (LLM, error)
```

Constructs a registered provider by name and returns an `llms.LLM`.

- The name lookup is **case-insensitive** (`"OpenAI"`, `"openai"`, and
  `"  OPENAI "` all resolve to the same provider; surrounding whitespace is
  trimmed).
- If the provider name is not registered, `New` returns an error that includes
  the list of currently registered providers — a useful hint when a blank import
  is missing.

```go
client, err := llms.New("anthropic", llms.Config{
	Model:   "claude-sonnet-4-5",
	Timeout: 30 * time.Second,
})
if err != nil {
	log.Fatal(err) // e.g. unknown provider, or missing API key
}
```

!!! tip "API keys are resolved from the environment by default"
    If you leave `Config.APIKey` empty, the provider falls back to its standard
    environment variables (e.g. `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`), and then
    to the generic `LLM_API_KEY`. Set `Config.APIKey` only when you want to pass
    the key explicitly.

## `llms.NewFromEnv`

```go
func NewFromEnv() (LLM, error)
```

A convenience wrapper that reads the provider and model from environment
variables:

| Variable       | Required | Meaning                          |
| -------------- | -------- | -------------------------------- |
| `LLM_PROVIDER` | yes      | Provider name (e.g. `openai`)    |
| `LLM_MODEL`    | no       | Model ID passed as `Config.Model` |

The provider-specific API key (e.g. `OPENAI_API_KEY`) is resolved by the
provider constructor, as described above. `NewFromEnv` returns an error if
`LLM_PROVIDER` is unset.

```go
// LLM_PROVIDER=groq LLM_MODEL=llama-3.3-70b-versatile GROQ_API_KEY=...
client, err := llms.NewFromEnv()
if err != nil {
	log.Fatal(err)
}
fmt.Println(client.Provider(), client.Model())
```

!!! note
    `NewFromEnv` only sets `Config.Model` from `LLM_MODEL`. For other settings
    (custom `BaseURL`, `Timeout`, `Extra` keys, etc.) call `llms.New` directly
    and build the `Config` yourself.

## The `Config` struct

`Config` carries the construction settings shared across providers.

```go
type Config struct {
	APIKey          string            // explicit API key (else env fallback)
	Model           string            // model ID, e.g. "gpt-4o"
	BaseURL         string            // override the API base URL
	Timeout         time.Duration     // per-request timeout
	AllowPrivateIPs bool              // permit private/loopback hosts (self-hosted)
	AllowHTTP       bool              // permit plain-HTTP (non-TLS) URLs
	HTTPClient      *http.Client      // custom HTTP client
	Extra           map[string]string // provider-specific keys (see below)
}
```

!!! warning "`AllowPrivateIPs` and `AllowHTTP` are independent"
    SSRF protection is on by default: the SDK blocks private/loopback/link-local
    and cloud-metadata addresses and requires HTTPS. `AllowPrivateIPs: true`
    relaxes only the host check (reach a private/loopback address); `AllowHTTP:
    true` relaxes only the scheme check (allow plain HTTP). They are independent,
    so an API key cannot leak over cleartext merely because you enabled
    private-IP access. A self-hosted endpoint reached over **plain HTTP** (Ollama,
    llama.cpp, a local gateway) needs **both** flags set. Use them only for
    endpoints you trust.

### Provider-specific `Extra` keys

A few providers need a construction parameter that has no common `Config` field.
These are passed through `Config.Extra`:

| Provider | Extra key     | Value                                   | Effect                                   |
| -------- | ------------- | --------------------------------------- | ---------------------------------------- |
| `runpod` | `endpoint_id` | your serverless endpoint ID             | Sets the required RunPod endpoint        |
| `zai`    | `coding`      | `"true"`, `"1"`, or `"yes"` (case-insensitive) | Enables the Z.AI Coding API         |

```go
// RunPod serverless: endpoint_id is required.
rp, err := llms.New("runpod", llms.Config{
	Model: "your-model",
	Extra: map[string]string{"endpoint_id": "abc123"},
})

// Z.AI Coding API.
zai, err := llms.New("zai", llms.Config{
	Model: "glm-4.6",
	Extra: map[string]string{"coding": "true"},
})
```

Keys that a provider does not recognize are ignored.

## Looking up model metadata: `ModelInfo`

`llms.New` returns the `llms.LLM` interface, which exposes
`GenerateContent`, `Stream`, `Provider()`, and `Model()`. Model metadata lookup
lives on a separate interface, `llms.ModelLister`, so type-assert to it first:

```go
type ModelLister interface {
	ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error)
	ModelInfo(ctx context.Context, modelID string) (*llms.ModelInfo, error)
}
```

`ModelInfo` returns metadata for a single model and returns
`llms.ErrModelNotFound` when the model is unknown to the provider. Check it with
`errors.Is`.

```go
client, err := llms.New("openai", llms.Config{Model: "gpt-4o"})
if err != nil {
	log.Fatal(err)
}

lister, ok := client.(llms.ModelLister)
if !ok {
	log.Fatal("provider does not support model lookup")
}

info, err := lister.ModelInfo(context.Background(), "gpt-4o")
switch {
case errors.Is(err, llms.ErrModelNotFound):
	fmt.Println("unknown model")
case err != nil:
	log.Fatal(err)
default:
	fmt.Printf("%s: context=%d max_output=%d\n",
		info.ID, info.ContextLength, info.MaxOutput)
}
```

!!! note
    Not every provider implements `ModelLister`. Always check the
    `client.(llms.ModelLister)` assertion before calling `ModelInfo` or
    `ListModels`.

## Registering a custom provider

You can add your own provider to the registry so it is constructible by name
alongside the built-ins. Register a `ProviderFactory` — a function that builds an
`llms.LLM` from a `Config` — using `llms.RegisterProvider`:

```go
func RegisterProvider(name string, factory ProviderFactory)
// ProviderFactory is: func(Config) (LLM, error)
```

- Names are **case-insensitive**.
- Registering the same name again **overwrites** the previous factory.
- Do registration in an `init()` so a blank import is enough to wire it up (this
  is exactly how the built-in providers register themselves).

A factory typically translates the common `Config` into your provider's own
functional options. For a provider built on
[`pkg/openaicompat`](custom-providers.md), that often looks like:

```go
package myprovider

import llms "github.com/nocturnium/llm-go-sdk/v4"

func init() {
	llms.RegisterProvider("myprovider", func(cfg llms.Config) (llms.LLM, error) {
		opts := []Option{} // your provider's option type
		if cfg.APIKey != "" {
			opts = append(opts, WithAPIKey(cfg.APIKey))
		}
		if cfg.Model != "" {
			opts = append(opts, WithModel(cfg.Model))
		}
		if cfg.BaseURL != "" {
			opts = append(opts, WithBaseURL(cfg.BaseURL))
		}
		if cfg.Timeout != 0 {
			opts = append(opts, WithTimeout(cfg.Timeout))
		}
		if cfg.HTTPClient != nil {
			opts = append(opts, WithHTTPClient(cfg.HTTPClient))
		}
		if cfg.AllowPrivateIPs {
			opts = append(opts, WithAllowPrivateIPs())
		}
		if cfg.AllowHTTP {
			opts = append(opts, WithAllowHTTP())
		}
		return New(opts...) // your provider's constructor
	})
}
```

Consumers then blank-import your package and construct it by name:

```go
import _ "example.com/myproject/myprovider"

client, err := llms.New("myprovider", llms.Config{Model: "my-model"})
```

## Complete runnable example

A small config-driven CLI: it picks a provider/model from environment variables,
prints the registered providers, and runs one prompt.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	_ "github.com/nocturnium/llm-go-sdk/v4/pkg/providers/all" // register all chat providers
)

func main() {
	fmt.Println("registered providers:", llms.RegisteredProviders())

	// Construct from env: set LLM_PROVIDER (e.g. "openai"), optionally LLM_MODEL,
	// and the provider's API key (e.g. OPENAI_API_KEY).
	client, err := llms.NewFromEnv()
	if err != nil {
		// Fall back to an explicit configuration.
		client, err = llms.New("openai", llms.Config{
			Model:   "gpt-4o",
			Timeout: 30 * time.Second,
		})
		if err != nil {
			log.Fatalf("construct client: %v", err)
		}
	}
	fmt.Printf("using %s / %s\n", client.Provider(), client.Model())

	// Optional: look up model metadata when the provider supports it.
	if lister, ok := client.(llms.ModelLister); ok {
		if info, err := lister.ModelInfo(context.Background(), client.Model()); err == nil {
			fmt.Printf("context window: %d tokens\n", info.ContextLength)
		} else if !errors.Is(err, llms.ErrModelNotFound) {
			log.Printf("model lookup failed: %v", err)
		}
	}

	// Run one prompt via the package-level helper.
	out, err := llms.Call(context.Background(), client, "Say hello in one word.")
	if err != nil {
		log.Fatalf("call: %v", err)
	}
	fmt.Println(out)
}
```

## See also

- [Custom providers](custom-providers.md) — build a new provider on
  `pkg/openaicompat` and register it.
- [Configuration & security](../index.md) — `AllowPrivateIPs`, SSRF protection,
  and environment variables.
