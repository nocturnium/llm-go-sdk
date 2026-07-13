# Configuration

This page is the reference for configuring `llm-go-sdk`: how API keys are
resolved, the full set of environment variables, HTTP client and timeout
settings, the built-in SSRF / network-security policy, and where retries fit
(spoiler: they are off by default).

The root package is imported as:

```go
import llms "github.com/nocturnium/llm-go-sdk/v5"
```

Providers live under `pkg/providers/<name>` and each exposes a typed
`New(...Option)` constructor, for example:

```go
import "github.com/nocturnium/llm-go-sdk/v5/pkg/providers/openai"

client, err := openai.New(
    openai.WithModel("gpt-4o"),
    openai.WithAPIKey("sk-..."), // optional; falls back to env (see below)
)
```

---

## API key resolution

API keys are resolved by `llms.ResolveAPIKey` / `llms.RequireAPIKey`, which
check the following sources **in order of precedence**:

1. The **explicit** value passed in code (e.g. `openai.WithAPIKey(...)`).
2. One or more **provider-specific** environment variables.
3. The generic **`LLM_API_KEY`** fallback.

```go
// Equivalent for a hosted provider: an explicit key wins, otherwise the
// provider env var, otherwise LLM_API_KEY.
key := llms.ResolveAPIKey(explicit, llms.EnvOpenAIAPIKey) // "" if none found
```

`RequireAPIKey` behaves the same but returns an error wrapping
`llms.ErrMissingAPIKey` when nothing is found, so you can branch on it:

```go
client, err := openai.New(openai.WithModel("gpt-4o"))
if errors.Is(err, llms.ErrMissingAPIKey) {
    log.Fatal("set OPENAI_API_KEY or LLM_API_KEY")
}
```

!!! note "The SDK does not load `.env` files"
    `llm-go-sdk` reads variables from the process environment only. It does
    **not** parse a `.env` file automatically. Use your shell, a process
    manager, or a loader such as `github.com/joho/godotenv` in your own
    `main` if you want `.env` support. A ready-to-copy
    [`.env.example`](https://github.com/nocturnium/llm-go-sdk/blob/main/.env.example)
    ships in the repo root.

---

## Environment variables

### API keys

Each hosted provider checks its own variable(s) first, then falls back to
`LLM_API_KEY`. Where two variables are listed, they are checked **in the order
shown**.

| Provider | Environment variable(s) | Notes |
| --- | --- | --- |
| OpenAI | `OPENAI_API_KEY` | |
| Anthropic | `ANTHROPIC_API_KEY` | Native provider |
| Gemini | `GEMINI_API_KEY`, then `GOOGLE_API_KEY` | Native provider; either is accepted |
| Groq | `GROQ_API_KEY` | |
| Cerebras | `CEREBRAS_API_KEY` | |
| DeepSeek | `DEEPSEEK_API_KEY` | |
| Mistral | `MISTRAL_API_KEY` | |
| Fireworks | `FIREWORKS_API_KEY` | |
| Together AI | `TOGETHER_API_KEY` | |
| Featherless | `FEATHERLESS_API_KEY` | |
| Synthetic | `SYNTHETIC_API_KEY` | |
| Perplexity | `PERPLEXITY_API_KEY`, then `PPLX_API_KEY` | Either is accepted |
| Z.AI | `ZAI_API_KEY` | |
| RunPod | `RUNPOD_API_KEY` | Also needs an endpoint ID (see below) |
| Azure OpenAI | `AZURE_OPENAI_API_KEY`, then `AZURE_OPENAI_KEY` | Plus endpoint + deployment (below) |
| Infinity | `INFINITY_API_KEY` | Embeddings / reranking only |
| **Generic fallback** | **`LLM_API_KEY`** | Used by **any** provider when its specific key is unset |

!!! tip "`LLM_API_KEY` as a single switch"
    Setting only `LLM_API_KEY` is enough to authenticate against whichever
    single provider you construct, which is handy in CI or one-off scripts.

### Local / self-hosted endpoints

These providers point at a local server by default and accept a host override.
They allow private IPs and plain HTTP **by default** (see
[Network security](#network-security-ssrf) below).

| Provider | Variable | Default |
| --- | --- | --- |
| Ollama | `OLLAMA_HOST` | `http://localhost:11434` |
| llama.cpp | `LLAMA_CPP_HOST` | `http://localhost:8080` |

Optional keys for these endpoints are also resolved from the environment when
present: Ollama reads `OLLAMA_API_KEY`, llama.cpp reads `LLAMA_CPP_API_KEY`
(both fall back to `LLM_API_KEY`).

### Azure OpenAI

Azure requires an endpoint and a deployment in addition to the key. These are
typically supplied via options (`azure.WithEndpoint`, `azure.WithDeployment`),
and the example env file documents the matching variables:

| Variable | Purpose |
| --- | --- |
| `AZURE_OPENAI_API_KEY` / `AZURE_OPENAI_KEY` | API key |
| `AZURE_OPENAI_ENDPOINT` | Resource endpoint, e.g. `https://my-resource.openai.azure.com` |
| `AZURE_OPENAI_DEPLOYMENT` | Deployment name (Azure addresses models by deployment, not model id) |

!!! note "Azure addresses models by deployment"
    The Azure provider has no `WithModel` option; use
    `azure.WithDeployment(name)` (and `azure.WithEmbeddingDeployment` for
    embeddings). `azure.WithAPIVersion` overrides the API version.

### RunPod and Z.AI extras

When constructing by name with `llms.New`, provider-specific construction
parameters that have no common SDK field are passed through `Config.Extra`:

| Provider | `Extra` key | Effect |
| --- | --- | --- |
| RunPod | `endpoint_id` | Sets the required serverless endpoint ID (equivalent to `runpod.WithEndpointID`) |
| Z.AI | `coding` = `"true"`/`"1"`/`"yes"` | Switches to the Z.AI Coding API endpoint |

```go
import _ "github.com/nocturnium/llm-go-sdk/v5/pkg/providers/all" // register all providers

client, err := llms.New("runpod", llms.Config{
    Model: "meta-llama/Llama-3.1-8B-Instruct",
    Extra: map[string]string{"endpoint_id": "abc123"},
})
```

### Runtime / debugging variables

| Variable | Purpose |
| --- | --- |
| `LLM_PROVIDER` | Default provider name read by `llms.NewFromEnv()` |
| `LLM_MODEL` | Default model read by `llms.NewFromEnv()` |
| `LLM_HTTP_TIMEOUT` | Overrides the default HTTP timeout (Go duration, e.g. `"10m"`, `"300s"`) when an explicit timeout is not set in code |
| `LLM_DEBUG_REQUESTS` | When non-empty, logs raw HTTP requests. Bodies may contain prompts and credentials — **never enable in production** |

---

## HTTP configuration

Every provider exposes the same two HTTP knobs as construction options.

### Timeout

`WithTimeout(time.Duration)` sets the timeout on the underlying HTTP client.

```go
client, err := openai.New(
    openai.WithModel("gpt-4o"),
    openai.WithTimeout(30*time.Second),
)
```

The default timeout is 5 minutes. If you do not set a timeout in code, the SDK
honors the `LLM_HTTP_TIMEOUT` environment variable as an override.

### Custom HTTP client

`WithHTTPClient(*http.Client)` lets you supply a fully configured client —
custom transport, connection pooling, proxy settings, your own timeout, etc.

```go
hc := &http.Client{
    Timeout: 45 * time.Second,
    Transport: &http.Transport{
        MaxIdleConnsPerHost: 32,
        // ... proxy, TLS config, etc.
    },
}

client, err := anthropic.New(
    anthropic.WithModel("claude-sonnet-4-5"),
    anthropic.WithHTTPClient(hc),
)
```

!!! warning "The SDK installs its own redirect policy on your client"
    When you pass a `*http.Client` via `WithHTTPClient`, the SDK **wraps that
    client's `CheckRedirect`** so every redirect hop is re-validated against
    the same SSRF policy as the initial request, and the number of hops is
    bounded (max 10). Your client's `Timeout` and `Transport` are preserved,
    but any custom `CheckRedirect` you set will be replaced. This prevents a
    redirect to a private IP (e.g. `169.254.169.254`) from bypassing SSRF
    protection.

---

## Network security (SSRF)

SSRF (Server-Side Request Forgery) protection is **on by default** for every
provider. Before each request — and on **every redirect hop** — the destination
URL is validated, and requests are **rejected** when they target:

- **Loopback** addresses (`127.0.0.0/8`, `::1`) and the hostname `localhost`
  (including `*.localhost`).
- **Private** ranges (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, and the
  IPv6 equivalents).
- **Link-local** addresses (including the cloud metadata address
  `169.254.169.254`).
- **Unspecified** addresses (`0.0.0.0`, `::`) and **carrier-grade NAT**
  (`100.64.0.0/10`).
- **Obfuscated IPv4 literals** — octal, hexadecimal, decimal, and short-dotted
  spellings of an IP are decoded and checked against the same ranges, so they
  cannot be used to disguise a private/loopback/link-local target.
- Internal hostname suffixes: `.local`, `.internal`, `.localdomain`.
- Non-HTTPS (`http://`) URLs — HTTPS is required.

!!! note "Why hostnames are not DNS-resolved at validation time"
    The validator does not resolve DNS for hostnames before the request, to
    avoid added latency and a TOCTOU (time-of-check/time-of-use) gap where DNS
    changes between validation and connection. The redirect re-validation and
    the literal-IP/internal-suffix checks are the safeguards; the redirect
    policy is what catches a hostname that ultimately redirects to a private IP.

### Opting out for self-hosted / local endpoints

Two options relax the policy and are intended for trusted, self-hosted or
local backends (an internal vLLM, a private proxy, a LAN-hosted model server):

| Option | Effect |
| --- | --- |
| `WithAllowPrivateIPs()` | Permit requests to private / loopback / link-local IPs and internal hostnames |
| `WithAllowHTTP()` | Permit plain-HTTP (non-HTTPS) URLs |

```go
// Talk to a self-hosted OpenAI-compatible server on the LAN over plain HTTP.
client, err := openai.New(
    openai.WithModel("my-model"),
    openai.WithBaseURL("http://10.0.0.5:8000/v1"),
    openai.WithAllowPrivateIPs(),
    openai.WithAllowHTTP(),
)
```

!!! tip "Local providers relax this automatically"
    `ollama`, `llamacpp`, and `infinity` target local servers, so they enable
    `AllowPrivateIPs` **and** `AllowHTTP` by default — no opt-out flags are
    needed to reach `http://localhost`. The options are still available if you
    ever need to override behavior.

When constructing by name, the same relaxations are available through the
independent `Config.AllowPrivateIPs` and `Config.AllowHTTP` flags. A private,
plain-HTTP endpoint needs **both** — enabling private-IP access alone no longer
permits cleartext HTTP:

```go
client, err := llms.New("openai", llms.Config{
    Model:           "my-model",
    BaseURL:         "http://10.0.0.5:8000/v1",
    AllowPrivateIPs: true, // reach the private address
    AllowHTTP:       true, // allow the plain-HTTP scheme
})
```

Custom providers built on `pkg/openaicompat` expose the same toggles on
`openaicompat.ClientConfig` (`AllowPrivateIPs`, `AllowHTTP`).

---

## Retries are off by default

!!! warning "Providers do not retry"
    No provider retries failed requests on its own. A `429` or `5xx` surfaces
    to your caller immediately. This is deliberate: retry behavior is a
    cross-cutting concern with a **single authority** so backoff is not applied
    twice.

To add retries (and optionally circuit breaking), wrap any client with
`resilience.NewResilientClient` (from
`github.com/nocturnium/llm-go-sdk/v5/pkg/middleware/resilience`):

```go
base, _ := openai.New(openai.WithModel("gpt-4o"))

client := resilience.NewResilientClient(base,
    resilience.WithMaxRetries(3),
    // Or full control:
    // resilience.WithRetryConfig(&resilience.RetryConfig{ ... }),
)
```

The default retry conditions are `429` and `5xx` responses. The returned
`*resilience.ResilientClient` satisfies the same `llms.LLM` interface, so it is a
drop-in replacement.

See the [Resilience](guides/resilience.md) guide for the full set of wrappers —
retries, circuit breakers, rate limiting, and fallback chains.

---

## See also

- [Resilience](guides/resilience.md) — retries, circuit breakers, rate limiting, fallbacks.
- [`.env.example`](https://github.com/nocturnium/llm-go-sdk/blob/main/.env.example) — copy-ready environment template.
