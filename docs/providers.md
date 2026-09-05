# Providers

llm-go-sdk ships first-class clients for **20 providers** behind a single
[`llms.LLM`](index.md) interface. Each provider lives in its own subpackage
under `pkg/providers/<name>` and is constructed with functional options, for
example:

```go
import (
	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

client, err := openai.New(
	openai.WithModel("gpt-4o"),
	openai.WithAPIKey("sk-..."), // or omit to read OPENAI_API_KEY
)
```

Every provider's `New(...) (*Client, error)` returns a `*Client` that satisfies
the `llms.LLM` interface, so the same `GenerateContent` / `Stream` / typed-output
code works regardless of which provider you picked.

!!! note "API styles"
    Two providers — **anthropic** and **gemini** — talk to their vendors'
    native HTTP APIs (Anthropic Messages, Gemini `generateContent`). The other
    chat providers are **OpenAI-compatible**: they build on
    [`pkg/openaicompat`](guides/custom-providers.md) and speak the OpenAI
    chat-completions wire format. **infinity** is not a chat provider at all —
    it serves embeddings and reranking only.

---

## Provider matrix

| Provider | Import path | Auth / host env var(s) | API style | Default chat model | Notes |
|----------|-------------|------------------------|-----------|--------------------|-------|
| openai | `pkg/providers/openai` | `OPENAI_API_KEY` | OpenAI native | `gpt-4o` | Chat, vision, tools, embeddings (`text-embedding-3-small`) |
| openrouter | `pkg/providers/openrouter` | `OPENROUTER_API_KEY` | OpenAI-compatible chat + native media | `google/gemini-3.5-flash-lite` | Images, async video, speech, transcription; embeddings require a model option |
| anthropic | `pkg/providers/anthropic` | `ANTHROPIC_API_KEY` | Native (Messages) | `claude-sonnet-4-20250514` | Vision, tools, thinking, prompt caching |
| gemini | `pkg/providers/gemini` | `GEMINI_API_KEY` or `GOOGLE_API_KEY` | Native (`generateContent`) | `gemini-2.5-flash` | Vision, tools, embeddings (`text-embedding-004`) |
| azure | `pkg/providers/azure` | `AZURE_OPENAI_API_KEY` (or `AZURE_OPENAI_KEY`) + `AZURE_OPENAI_ENDPOINT` + `AZURE_OPENAI_DEPLOYMENT` | OpenAI-compatible | deployment-dependent | Uses deployment + endpoint, not a model name |
| groq | `pkg/providers/groq` | `GROQ_API_KEY` | OpenAI-compatible | `llama-3.3-70b-versatile` | Fast inference; no embeddings |
| cerebras | `pkg/providers/cerebras` | `CEREBRAS_API_KEY` | OpenAI-compatible | `llama3.1-70b` | Fast inference; no embeddings |
| deepseek | `pkg/providers/deepseek` | `DEEPSEEK_API_KEY` | OpenAI-compatible | `deepseek-chat` | Reasoning models; no public embeddings |
| mistral | `pkg/providers/mistral` | `MISTRAL_API_KEY` | OpenAI-compatible | `mistral-large-latest` | Embeddings (`mistral-embed`) |
| fireworks | `pkg/providers/fireworks` | `FIREWORKS_API_KEY` | OpenAI-compatible | `accounts/fireworks/models/llama-v3p1-70b-instruct` | Embeddings (`nomic-ai/nomic-embed-text-v1.5`) |
| togetherai | `pkg/providers/togetherai` | `TOGETHER_API_KEY` | OpenAI-compatible | `meta-llama/Llama-3.3-70B-Instruct-Turbo` | Large OSS model catalog |
| featherless | `pkg/providers/featherless` | `FEATHERLESS_API_KEY` | OpenAI-compatible | `Qwen/Qwen3-32B` | Serverless OSS models |
| synthetic | `pkg/providers/synthetic` | `SYNTHETIC_API_KEY` | OpenAI-compatible | `hf:Qwen/Qwen3-Coder-480B-A35B-Instruct` | Coding-oriented OSS models |
| perplexity | `pkg/providers/perplexity` | `PERPLEXITY_API_KEY` or `PPLX_API_KEY` | OpenAI-compatible | `sonar` | Search-augmented generation |
| zai | `pkg/providers/zai` | `ZAI_API_KEY` | OpenAI-compatible | `glm-4.7` | GLM models; optional Coding API + native web search |
| runpod | `pkg/providers/runpod` | `RUNPOD_API_KEY` + endpoint ID | OpenAI-compatible | endpoint-dependent | Serverless vLLM endpoints |
| ollama | `pkg/providers/ollama` | `OLLAMA_HOST` (default `http://localhost:11434`) | OpenAI-compatible | `llama3.2` | Local models; embeddings (`nomic-embed-text`) |
| llamacpp | `pkg/providers/llamacpp` | `LLAMA_CPP_HOST` (default `http://localhost:8080`) | OpenAI-compatible | discovered from server | Local `llama.cpp` server |
| huggingface | `pkg/providers/huggingface` | `HF_TOKEN` or `HUGGINGFACE_API_KEY` | OpenAI-compatible (TGI/TEI) | endpoint-dependent | Chat (TGI) or embeddings (TEI) per deployed model; needs `WithEndpoint(...)`; direct-construct |
| infinity | `pkg/providers/infinity` | `INFINITY_API_KEY` (default host `http://localhost:7997/v1`) | OpenAI-compatible | n/a (no chat) | Embeddings + reranking only |

!!! tip "API key resolution"
    Keys are resolved by `llms.ResolveAPIKey` in this order: an explicit value
    passed to `WithAPIKey(...)`, then the provider-specific environment
    variable(s) above, then the generic `LLM_API_KEY` fallback. The
    `.env.example` file at the repo root lists every variable.

---

## Common construction options

These options are accepted by (nearly) every provider's `New`:

| Option | Purpose |
|--------|---------|
| `WithModel(string)` | Default chat/completion model |
| `WithAPIKey(string)` | Explicit API key (overrides env vars) |
| `WithBaseURL(string)` | Override the API base URL (gateways, proxies, self-hosted) |
| `WithEmbeddingModel(string)` | Default embedding model (providers that support embeddings) |
| `WithTimeout(time.Duration)` | HTTP client timeout |
| `WithHTTPClient(*http.Client)` | Supply your own `*http.Client` |
| `WithAllowPrivateIPs()` | Permit private/loopback IPs (SSRF opt-out) |
| `WithAllowHTTP()` | Permit plain-HTTP (non-HTTPS) requests |

Azure differs: it uses `WithEndpoint(...)` and `WithDeployment(...)` instead of
`WithBaseURL`/`WithModel` (see below).

---

## Construct by name

You can also build any chat provider from a string name. Blank-import the `all`
package to register the 18 auto-registered chat providers, then call `llms.New`:

```go
import (
	llms "github.com/nocturnium/llm-go-sdk/v6"
	_ "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/all"
)

llm, err := llms.New("openai", llms.Config{
	Model:  "gpt-4o",
	APIKey: "sk-...", // optional; falls back to env vars
})
```

`llms.Config` carries the common construction settings:

```go
type Config struct {
	APIKey          string
	Model           string
	BaseURL         string
	Timeout         time.Duration
	AllowPrivateIPs bool
	AllowHTTP       bool
	HTTPClient      *http.Client
	Extra           map[string]string
}
```

`Config.Extra` carries provider-specific construction parameters with no common
field. Recognized keys:

- **runpod** — `"endpoint_id"` sets the required serverless endpoint ID.
- **zai** — `"coding"` set to `"true"`, `"1"`, or `"yes"` enables the Coding API.

Other helpers:

- `llms.NewFromEnv()` reads `LLM_PROVIDER` / `LLM_MODEL`.
- `llms.RegisteredProviders() []string` lists the registered chat providers.

!!! warning "Two providers are not auto-registered: infinity and huggingface"
    The `all` package registers 18 chat providers but **not** infinity or
    huggingface. Infinity does not implement chat generation (embeddings and
    reranking only). HuggingFace *does* serve chat (and embeddings), but it needs
    an explicit Inference-Endpoint URL and a chat-vs-embeddings mode, so it cannot
    be built from a name alone. Construct either one directly —
    `infinity.New(...)` / `huggingface.New(...)`. The auto-registered set is:
    anthropic, azure, cerebras, deepseek, featherless, fireworks, gemini, groq,
    llamacpp, mistral, ollama, openai, openrouter, perplexity, runpod, synthetic, togetherai,
    zai.

---

## Per-provider notes

### OpenRouter — chat and native media

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openrouter"

client, err := openrouter.New(
    openrouter.WithSiteURL("https://example.com"), // optional HTTP-Referer
    openrouter.WithAppName("My app"),             // optional X-Title
)
```

Chat defaults to `google/gemini-3.5-flash-lite`, verified in public discovery on
2026-09-05. Image and video catalogs are available through `ListImageModels` and
`ListVideoModels`. `ListModels` fetches `/models`, supports local type/limit
filtering, and rejects cursors. Model metadata is uncached; token prices are not
inferred from the catalog. Supply an embedding model with `WithEmbeddingModel`.

Media responses preserve reported costs. `WithUsageLookup()` optionally fetches
speech cost using the response's generation ID. Lookup errors return the audio
alongside the error; avoid repeating the paid synthesis call. The speech default
`fish-audio/s2.1-pro` was live-verified on 2026-09-05 via
[GET /models?output_modalities=speech](https://openrouter.ai/api/v1/models?output_modalities=speech).
`ListSpeechModels` returns typed entries with reported character pricing.
Unset voices are omitted; specify `WithSpeechVoice` for providers requiring one.
See the [media guide](guides/media.md) for defaults and option mappings.

### Azure OpenAI — deployments & endpoint

Azure is keyed on a **resource endpoint** and a **deployment name** rather than
a model id. The API key is read from `AZURE_OPENAI_API_KEY` or
`AZURE_OPENAI_KEY`; the endpoint and deployment default to
`AZURE_OPENAI_ENDPOINT` and `AZURE_OPENAI_DEPLOYMENT` when not set in code. The
default API version is `2024-02-15-preview`.

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/azure"

client, err := azure.New(
	azure.WithEndpoint("https://my-resource.openai.azure.com"),
	azure.WithDeployment("gpt-4o-deployment"),
	azure.WithAPIVersion("2024-02-15-preview"), // optional
	// API key read from AZURE_OPENAI_API_KEY / AZURE_OPENAI_KEY
)
```

When constructing by name, `Config.Model` maps to the deployment and
`Config.BaseURL` maps to the endpoint.

### Ollama & llama.cpp — local hosts

Both target a local server over plain HTTP by default, so SSRF restrictions are
relaxed automatically (no need for `WithAllowPrivateIPs`/`WithAllowHTTP`).

- **ollama** defaults to `http://localhost:11434/v1` and reads `OLLAMA_HOST` to
  override the base URL. Default chat model `llama3.2`, default embedding model
  `nomic-embed-text`. Optional `OLLAMA_API_KEY` is supported for authenticated
  instances.
- **llamacpp** defaults to `http://localhost:8080` and reads `LLAMA_CPP_HOST`.
  The model is discovered from the running server (`/props`) when not set.
  Optional `LLAMA_CPP_API_KEY` is supported.

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/ollama"

client, err := ollama.New(
	ollama.WithModel("llama3.2"),
	// reads OLLAMA_HOST, or use WithBaseURL("http://my-host:11434/v1")
)
```

### Infinity — embeddings & reranking only

infinity is an embeddings/reranking server, not a chat provider. It implements
`llms.Embedder` and `llms.Reranker`, and defaults to a local host
(`http://localhost:7997/v1`, SSRF relaxed). Default embedding model
`michaelfeil/bge-small-en-v1.5`, default rerank model
`mixedbread-ai/mxbai-rerank-xsmall-v1`.

```go
import (
	"context"
	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/infinity"
)

client, err := infinity.New(
	infinity.WithEmbeddingModel("michaelfeil/bge-small-en-v1.5"),
	infinity.WithRerankModel("mixedbread-ai/mxbai-rerank-xsmall-v1"),
)
if err != nil {
	// handle err
}

// Embeddings
vec, err := llms.EmbedQuery(context.Background(), client, "hello world")

// Reranking
res, err := client.Rerank(context.Background(), "best pizza", []string{
	"Pizza in Naples", "Tax law in Delaware",
})
```

### RunPod — serverless endpoint ID

RunPod targets serverless **vLLM** endpoints. The endpoint ID is **required**;
construction returns `runpod.ErrMissingEndpointID` without it. The client builds
the URL as `https://api.runpod.ai/v2/<ENDPOINT_ID>/openai/v1`.

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/runpod"

client, err := runpod.New(
	runpod.WithEndpointID("ep-123"),
	runpod.WithModel("my-vllm-model"), // matches MODEL_NAME on the endpoint
	// API key read from RUNPOD_API_KEY
)
```

When constructing by name, pass the endpoint via `Config.Extra`:

```go
llm, err := llms.New("runpod", llms.Config{
	Extra: map[string]string{"endpoint_id": "ep-123"},
})
```

### Z.AI — Coding API

Z.AI defaults to the general endpoint `https://api.z.ai/api/paas/v4` with the
flagship `glm-4.7` model. Opt into the coding-specific endpoint
(`https://api.z.ai/api/coding/paas/v4`) with `WithUseCodingAPI()`. Z.AI also
exposes native web search.

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/zai"

client, err := zai.New(
	zai.WithModel("glm-4.7"),
	zai.WithUseCodingAPI(), // use the Coding API endpoint
	// API key read from ZAI_API_KEY
)
```

By name, enable the Coding API via `Config.Extra`:

```go
llm, err := llms.New("zai", llms.Config{
	Extra: map[string]string{"coding": "true"},
})
```

### Perplexity — search-augmented generation

Perplexity uses an OpenAI-compatible API with built-in search augmentation. The
default model is `sonar`. The key is read from `PERPLEXITY_API_KEY` or
`PPLX_API_KEY`. When web search returns sources, they surface on the response as
`Response.SearchResults`.

```go
import "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/perplexity"

client, err := perplexity.New(
	perplexity.WithModel("sonar"),
	// API key read from PERPLEXITY_API_KEY / PPLX_API_KEY
)
```

---

## Network security defaults

SSRF protection is **on by default** for hosted providers: requests to
private/loopback/link-local addresses and cloud-metadata endpoints are blocked,
HTTPS is required, and redirects are re-validated. For self-hosted or local
endpoints, opt out with `WithAllowPrivateIPs()` and `WithAllowHTTP()`. The
local-first providers — **ollama**, **llamacpp**, and **infinity** — relax these
restrictions automatically.
