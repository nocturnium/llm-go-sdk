---
hide:
  - navigation
---

<div class="noct-hero" markdown>
<span class="noct-hero__eyebrow">github.com/nocturnium/llm-go-sdk/v5</span>

# One Go interface for <span class="accent">every LLM provider</span> { .noct-hero__title }

<p class="noct-hero__tagline" markdown>
Streaming, tool calling, vision, embeddings, and built-in resilience — across **19 providers**, on the standard <code>net/http</code>. Switch from OpenAI to Anthropic to a local Ollama by changing one import and one constructor.
</p>

<div class="noct-hero__cta" markdown>
[Get started](getting-started.md){ .md-button .md-button--primary }
[60-second quickstart ↓](#60-second-quickstart){ .md-button }
[GitHub](https://github.com/nocturnium/llm-go-sdk){ .md-button }
</div>
</div>

!!! info "At a glance"
    **Module** `github.com/nocturnium/llm-go-sdk/v5` · **Import** `llms "github.com/nocturnium/llm-go-sdk/v5"` · **Version** v5.0.0 · **License** Apache-2.0 · **Go** 1.25+

## Why this SDK

<div class="grid cards" markdown>

-   :material-vector-link:{ .lg .middle } __Unified interface__

    ---

    One `LLM` interface — `GenerateContent`, `Stream`, `Provider`, `Model` — works identically across every provider.

-   :material-language-go:{ .lg .middle } __Native HTTP, zero LLM deps__

    ---

    Built directly on `net/http`. No vendor SDKs are pulled into your build.

-   :material-lightning-bolt:{ .lg .middle } __Streaming__

    ---

    Real-time token streaming over Go channels, with the terminal error surfaced explicitly.

-   :material-tools:{ .lg .middle } __Tools & agents__

    ---

    A consistent tool-calling API plus an automatic `RunTools` agent loop with typed tool choice.

-   :material-code-json:{ .lg .middle } __Structured outputs__

    ---

    Typed JSON via JSON Schema with the generic `GenerateTyped[T]` and `WithJSONSchema`.

-   :material-image-multiple:{ .lg .middle } __Vision__

    ---

    Multi-modal image input (PNG, JPEG, GIF, WebP) through simple message helpers.

-   :material-vector-triangle:{ .lg .middle } __Embeddings & reranking__

    ---

    First-class embeddings (`[]float32`) for semantic search and RAG, plus reranking.

-   :material-shield-refresh:{ .lg .middle } __Resilience (opt-in)__

    ---

    Circuit breaker, retries with backoff, rate limiting, and fallback chains as composable wrappers.

-   :material-chart-line:{ .lg .middle } __Observability & cost__

    ---

    OpenTelemetry and Langfuse middleware, per-call tracing, and a built-in cost tracker.

-   :material-power-plug:{ .lg .middle } __Model Context Protocol__

    ---

    A first-class MCP client — tools, resources, prompts, and progress — that drops into `RunTools`.

-   :material-shield-lock:{ .lg .middle } __Secure by default__

    ---

    SSRF protection blocks private/loopback/link-local/cloud-metadata targets and re-validates redirects.

-   :material-layers-triple:{ .lg .middle } __Composable middleware__

    ---

    Wrap a client with `llms.Chain(base, ...)` — resilience innermost, observability outermost.

</div>

## Supported providers

**19 providers** — 17 chat providers auto-registered, plus HuggingFace (chat or embeddings, direct-construct) and Infinity (embeddings and reranking).

| Native | OpenAI-compatible | Local / self-hosted | Direct-construct |
| --- | --- | --- | --- |
| anthropic, gemini | openai, azure, groq, cerebras, deepseek, mistral, fireworks, togetherai, featherless, synthetic, perplexity, zai, runpod | ollama, llamacpp | huggingface, infinity |

!!! tip "Construct by name"
    Blank-import `github.com/nocturnium/llm-go-sdk/v5/pkg/providers/all` to register the 17
    auto-registered chat providers, then build one with `llms.New(name, llms.Config{...})`. Call
    `llms.RegisteredProviders()` for the live list. HuggingFace and Infinity are built directly
    (`huggingface.New(...)` / `infinity.New(...)`).

## 60-second quickstart

Install the module:

```bash
go get github.com/nocturnium/llm-go-sdk/v5
```

Make your first call. The package-level helper `llms.Call` sends a single prompt and
returns the text:

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

## Where to next

<div class="grid cards" markdown>

-   :material-rocket-launch:{ .lg .middle } __[Getting Started](getting-started.md)__

    ---

    Installation, environment variables, and your first project.

-   :material-server-network:{ .lg .middle } __[Providers](providers.md)__

    ---

    Per-provider setup, models, and configuration for all 19.

-   :material-tools:{ .lg .middle } __[Tool calling & agents](guides/tools.md)__

    ---

    Function calling and the `RunTools` loop.

-   :material-shield-refresh:{ .lg .middle } __[Resilience](guides/resilience.md)__

    ---

    Retries, circuit breakers, rate limiting, and fallback.

</div>
