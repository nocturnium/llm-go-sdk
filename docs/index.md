# llm-go-sdk

> One Go interface for every LLM provider — streaming, tool calling, vision, embeddings, and built-in resilience over the standard `net/http`.

`llm-go-sdk` is a unified, dependency-light Go SDK for **18 LLM providers**. A single
`LLM` interface works everywhere: switch from OpenAI to Anthropic to a local Ollama
server by changing one import and one constructor — streaming, tools, retries, fallback,
cost tracking, and tracing all stay the same.

!!! info "At a glance"
    - **Module:** `github.com/nocturnium/llm-go-sdk`
    - **Import alias:** `llms "github.com/nocturnium/llm-go-sdk"`
    - **Version:** v1.2.1 · **License:** Apache-2.0 · **Go:** 1.25+

---

## Why this SDK

- **Unified interface** — one `LLM` interface (`GenerateContent`, `Stream`, `Provider`, `Model`) across all providers.
- **Native HTTP, zero LLM dependencies** — built directly on `net/http`; no vendor SDKs pulled in.
- **Streaming** — real-time token streaming over Go channels.
- **Tool / function calling** — a consistent tool-calling API, plus an automatic `RunTools` agent loop with typed tool choice.
- **Structured outputs** — typed JSON via JSON Schema with the generic `GenerateTyped[T]` and `WithJSONSchema`.
- **Vision** — multi-modal image input (PNG, JPEG, GIF, WebP) via simple message helpers.
- **Embeddings & reranking** — first-class embeddings (`[]float32`) for semantic search and RAG, plus reranking via the Infinity provider.
- **Resilience (opt-in)** — circuit breaker, retries with backoff, rate limiting, and fallback chains as composable wrappers.
- **Observability & cost** — OpenTelemetry and Langfuse middleware, per-call tracing, and a built-in cost tracker.
- **Security by default** — SSRF protection blocks private/loopback/link-local/cloud-metadata targets and re-validates redirects.

---

## Supported providers

**18 providers** out of the box — 17 chat-capable, plus Infinity for embeddings and reranking.

| Native | OpenAI-compatible | Local / self-hosted | Embeddings only |
| --- | --- | --- | --- |
| anthropic, gemini | openai, azure, groq, cerebras, deepseek, mistral, fireworks, togetherai, featherless, synthetic, perplexity, zai, runpod | ollama, llamacpp | infinity |

!!! tip "Construct by name"
    Blank-import `github.com/nocturnium/llm-go-sdk/pkg/providers/all` to register every chat
    provider, then build one with `llms.New(name, llms.Config{...})`. Call
    `llms.RegisteredProviders()` for the live list.

---

## 60-second quickstart

Install the module:

```bash
go get github.com/nocturnium/llm-go-sdk
```

Make your first call. The package-level helper `llms.Call` sends a single prompt and
returns the text:

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
	// API key is read from OPENAI_API_KEY when WithAPIKey is omitted.
	client, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		log.Fatal(err)
	}

	answer, err := llms.Call(context.Background(), client, "Say hello in one sentence.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(answer)
}
```

Need full control over the conversation? Use `GenerateContent` with a message slice and
read the typed `Response`:

```go
messages := []llms.Message{
	{Role: llms.RoleSystem, Content: "You are a concise assistant."},
	{Role: llms.RoleUser, Content: "What is the capital of France?"},
}

resp, err := client.GenerateContent(context.Background(), messages,
	llms.WithTemperature(0),
	llms.WithMaxTokens(64),
)
if err != nil {
	log.Fatal(err)
}

fmt.Println(resp.Content)                 // the model's reply
fmt.Println(resp.FinishReason)            // typed llms.FinishReason
fmt.Println(resp.Usage)                   // token accounting
fmt.Println(client.Provider(), client.Model())
```

!!! note "Same code, any provider"
    Swap the import and constructor — `anthropic.New(anthropic.WithModel("claude-..."))`
    or `ollama.New(ollama.WithModel("llama3.1"))` — and everything below the constructor
    is unchanged.

---

## Where to next

- **[Getting Started](getting-started.md)** — installation, environment variables, and your first project.
- **[Providers](providers.md)** — per-provider setup, models, and configuration for all 18.
- **Guides** — deeper how-tos:
    - [Streaming](guides/streaming.md) — token streaming over channels.
    - [Tool calling & agents](guides/tools.md) — function calling and the `RunTools` loop.
    - [Structured outputs](guides/structured-outputs.md) — `GenerateTyped[T]` and JSON Schema.
    - [Vision](guides/vision.md) — multi-modal image input.
    - [Embeddings & reranking](guides/embeddings.md) — semantic search and RAG building blocks.
    - [Resilience](guides/resilience.md) — retries, circuit breakers, rate limiting, fallback.
    - [Observability & cost](guides/observability.md) — OpenTelemetry, Langfuse, and cost tracking.
