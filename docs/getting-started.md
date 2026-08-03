# Getting Started

This guide takes you from an empty project to a working LLM call in a few
minutes. You will install the SDK, configure an API key, run your first program,
and learn the three ways to call a model: `GenerateContent`, the `llms.Call`
one-liner, and `Stream`.

## Prerequisites

- **Go 1.25 or newer.** The module declares `go 1.25.0`. Check your toolchain
  with `go version`.
- **An API key** for at least one provider. The examples below use OpenAI, but
  switching to another provider is a one-line change — see
  [Switching providers](#switching-providers).

## Install

Add the SDK to your module:

```bash
go get github.com/nocturnium/llm-go-sdk/v6@v5.0.0
```

The SDK's root package is named `llms`. Because the import path
(`llm-go-sdk`) does not match the package name (`llms`), use a named import so
your code reads clearly:

```go
import llms "github.com/nocturnium/llm-go-sdk/v6"
```

Each provider lives under `pkg/providers/<name>`. For OpenAI:

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
```

## Choosing how to construct a client

There are three coherent construction mechanisms. Pick one based on who decides
the provider and whether you are authoring a provider or consuming one.

| Mechanism | Use when |
| --- | --- |
| Direct provider constructor: `<pkg>.New(...WithModel...)`, for example `openai.New(openai.WithModel("gpt-4o"))` | The provider is known at compile time and you want strongly typed, provider-specific options. |
| Name-based registry: `llms.New(name, llms.Config{...})` / `llms.NewFromEnv()` with a blank import of `pkg/providers/all` | The provider is chosen at runtime from configuration, environment, user input, or a CLI flag. |
| OpenAI-compatible base provider: `openaicompat.NewClient(...)` + `openaicompat.NewBaseProvider(...)` | You are authoring a new OpenAI-compatible provider; this is not the normal end-user client constructor. |

Two APIs also share the name `WithModel` at different layers:
`openai.WithModel`, `anthropic.WithModel`, and other provider-specific options
set a construction-time default on the client; `llms.WithModel` is a per-call
override passed to `GenerateContent`, `Stream`, or `llms.Call`.

## Configure an API key

Every provider reads its key from an environment variable by default, so you
rarely need to pass secrets in code. For OpenAI:

```bash
export OPENAI_API_KEY="sk-..."
```

Common provider variables include `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`,
`GEMINI_API_KEY` (or `GOOGLE_API_KEY`), and `GROQ_API_KEY`. A generic
`LLM_API_KEY` is used as a fallback when a provider-specific key is unset. The
full list is in `.env.example` and on the [Configuration](configuration.md) and
[Providers](providers.md) pages.

!!! tip "Passing keys explicitly"
    You can override the environment variable with the `WithAPIKey` option,
    e.g. `openai.New(openai.WithAPIKey(os.Getenv("MY_KEY")))`. Prefer
    environment variables in production so keys never land in source control.

## Your first program

Create `main.go`. This program constructs an OpenAI client, sends a chat
message with `GenerateContent`, and prints the reply along with token usage.

```go
package main

import (
	"context"
	"fmt"
	"log"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

func main() {
	ctx := context.Background()

	// New reads OPENAI_API_KEY from the environment.
	// It returns (*openai.Client, error).
	client, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a concise assistant."},
		{Role: llms.RoleUser, Content: "What is the capital of France?"},
	}

	resp, err := client.GenerateContent(ctx, messages)
	if err != nil {
		log.Fatalf("generate failed: %v", err)
	}

	fmt.Println(resp.Content)
	fmt.Printf("Tokens: %d prompt, %d completion, %d total\n",
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
	)
}
```

Run it:

```bash
go run .
```

You should see the model's answer followed by a usage line such as
`Tokens: 23 prompt, 8 completion, 31 total`.

### What's happening

- **`openai.New(...)`** returns `(*openai.Client, error)`. The client satisfies
  the `llms.LLM` interface, so it works anywhere the SDK expects an `LLM`.
- **`[]llms.Message`** is the conversation. Each message has a `Role`
  (`llms.RoleSystem`, `llms.RoleUser`, `llms.RoleAssistant`, or
  `llms.RoleTool`) and `Content`.
- **`client.GenerateContent(ctx, messages, ...)`** returns a `*llms.Response`.
  The reply text is in `resp.Content`; `resp.Usage` carries token counts and
  `resp.FinishReason` is a typed `llms.FinishReason` (for example
  `llms.FinishReasonStop`).

### Tuning the call

`GenerateContent` accepts call options as trailing arguments:

```go
resp, err := client.GenerateContent(ctx, messages,
	llms.WithTemperature(0.3), // 0 is honored, not treated as "unset"
	llms.WithMaxTokens(256),
)
```

See [Configuration](configuration.md) for the full set of call options
(`WithTopP`, `WithStopWords`, `WithTools`, `WithJSONMode`, and more).

## The one-line version: `llms.Call`

For a single prompt where you do not need to build a message slice or inspect
usage, the package-level `llms.Call` helper does it all and returns just the
text:

```go
package main

import (
	"context"
	"fmt"
	"log"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

func main() {
	client, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		log.Fatal(err)
	}

	answer, err := llms.Call(context.Background(), client,
		"What is the capital of France? Reply in one sentence.")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(answer)
}
```

!!! note "`Call` is a package-level function, not a method"
    The signature is
    `llms.Call(ctx context.Context, llm llms.LLM, prompt string, opts ...llms.CallOption) (string, error)`.
    You pass the client in as the second argument. It accepts the same call
    options as `GenerateContent`, e.g.
    `llms.Call(ctx, client, prompt, llms.WithMaxTokens(50))`.

## Switching providers

Because every client implements `llms.LLM`, switching providers is a one-line
change at construction. Import the provider package and call its `New`; the rest
of your code is identical. Each provider reads its own environment variable
(here, `ANTHROPIC_API_KEY`).

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/anthropic"

// Was:
client, err := openai.New(openai.WithModel("gpt-4o"))

// Now:
client, err := anthropic.New(anthropic.WithModel("claude-sonnet-4-5"))
```

The same `client.GenerateContent(...)`, `llms.Call(...)`, and `client.Stream(...)`
calls work unchanged. To select a provider at runtime by name (without importing
each package directly), see the registry approach on the
[Providers](providers.md) page (`llms.New(name, llms.Config{...})` after
importing the `pkg/providers/all` bundle).

!!! tip "Common construction options"
    Most providers share the same options:
    `WithModel`, `WithAPIKey`, `WithBaseURL`, `WithEmbeddingModel`,
    `WithTimeout(time.Duration)`, and `WithHTTPClient(*http.Client)`. Azure
    differs — it uses `WithEndpoint` and `WithDeployment`. See
    [Providers](providers.md) for per-provider details.

## GenerateContent vs Call vs Stream

The SDK offers three entry points for generating text. Choose based on how much
control and feedback you need.

| Entry point | Form | Returns | Use when |
|---|---|---|---|
| `llms.Call` | package function | `(string, error)` | One prompt, you just want the text |
| `client.GenerateContent` | method | `(*llms.Response, error)` | Multi-turn messages, tools, or you need usage / finish reason |
| `client.Stream` | method | `(<-chan llms.StreamChunk, error)` | You want tokens as they arrive (live UIs, long outputs) |

### Streaming example

`Stream` returns a channel of `llms.StreamChunk`. Read until the channel closes;
each chunk carries incremental `Content`, and the final chunk has `Done == true`
plus a populated `Usage`. Always check `chunk.Error`.

```go
stream, err := client.Stream(ctx, messages)
if err != nil {
	log.Fatal(err)
}

for chunk := range stream {
	if chunk.Error != nil {
		log.Fatalf("stream error: %v", chunk.Error)
	}
	if chunk.Done {
		fmt.Printf("\n[done: %s, %d tokens]\n",
			chunk.FinishReason, chunk.Usage.TotalTokens)
		break
	}
	fmt.Print(chunk.Content)
}
```

!!! warning "`Usage` on stream chunks is a pointer"
    On a `llms.StreamChunk`, `Usage` is a `*llms.Usage` (it is only populated on
    the final chunk). The example reads it after confirming `chunk.Done`, when
    the provider has reported totals.

## Next steps

- **[Configuration](configuration.md)** — call options, timeouts, custom HTTP
  clients, environment variables, and network-security settings.
- **[Providers](providers.md)** — all 19 providers, their environment
  variables, provider-specific options, and the construct-by-name registry.

From here you can also explore structured outputs (`llms.GenerateTyped`), tool
calling and agent loops (`llms.RunTools`), embeddings, vision, and resilience
wrappers — each covered in its own page.
