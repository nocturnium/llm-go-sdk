# Building a Custom Provider

Most LLM services speak the OpenAI chat-completions wire format. The SDK ships a
reusable building block, `pkg/openaicompat`, that factors out the request/response
machinery so a new OpenAI-compatible provider can be implemented in a few dozen
lines instead of re-deriving HTTP, SSE streaming, message preparation, and error
wrapping.

This guide shows the fast path: construct a low-level client, declare a provider
config, embed `BaseProvider` to inherit the full `llms.LLM` (and `llms.Embedder`)
implementation, and — optionally — register the provider so callers can build it
by name.

!!! tip "When to use this"
    Use `pkg/openaicompat` when your endpoint accepts `POST /v1/chat/completions`
    (and optionally `/v1/embeddings`, `/v1/models`) in the standard OpenAI shape.
    If your API is wire-incompatible (like the native Anthropic Messages API or
    the native Gemini API), you implement the `llms.LLM` interface directly
    instead. See [ARCHITECTURE.md](../ARCHITECTURE.md) for how native providers
    are structured.

## The three layers

`pkg/openaicompat` is organized into three layers you compose:

| Layer | Type | Responsibility |
| --- | --- | --- |
| Wire types | `ChatCompletionRequest`, `ChatCompletionResponse`, `StreamChunk`, `EmbeddingRequest`, ... | Mirror the OpenAI JSON shapes. |
| Low-level client | `openaicompat.Client` (via `NewClient`) | Auth headers, JSON + SSE transport, SSRF/HTTPS enforcement, tolerant `/models` parsing. |
| High-level provider | `openaicompat.BaseProvider` (via `NewBaseProvider`) | Adapts `Client` onto `llms.LLM`/`llms.Embedder`: message prep, streaming, token estimation, error wrapping, capability merging. |

The common case only touches the bottom and top layers: build a `Client`, wrap it
in a `BaseProvider`, and embed that in your own struct.

## Construction surface

### `openaicompat.NewClient(ClientConfig)`

```go
type ClientConfig struct {
	BaseURL      string
	APIKey       string
	Headers      map[string]string
	HTTPClient   *http.Client  // supply a custom transport; nil uses the default
	Timeout      time.Duration // 0 uses the default
	AzureAPIKey  bool          // use the "api-key" header instead of Authorization
	AzureVersion string        // appended as the api-version query parameter

	AllowPrivateIPs bool // SSRF opt-out (private/loopback IPs)
	AllowHTTP       bool // allow plain-HTTP (non-HTTPS) endpoints
}
```

`NewClient` returns a `*openaicompat.Client`. It sets `Authorization: Bearer
<APIKey>` by default (or the Azure `api-key` header when `AzureAPIKey` is true),
merges any custom `Headers`, and routes everything through the SDK's hardened HTTP
client.

!!! warning "SSRF protection is on by default"
    The transport blocks private, loopback, link-local, and cloud-metadata
    addresses and requires HTTPS, re-validating on redirects. For a self-hosted or
    local endpoint, set `AllowPrivateIPs: true` and/or `AllowHTTP: true`. See the
    network-security section of [ARCHITECTURE.md](../ARCHITECTURE.md) for details.

### `openaicompat.NewBaseProvider(client, ProviderConfig)`

```go
type ProviderConfig struct {
	Provider              llms.Provider // a Provider value used in errors/metadata
	ProviderName          string        // shown in error messages, e.g. "acmeai"
	DefaultModel          string        // default chat model
	DefaultEmbeddingModel string        // default embedding model (optional)
	Capabilities          llms.Capabilities
}
```

`NewBaseProvider` returns a `BaseProvider` value (not a pointer). Embed it in your
own type. It implements every method of `llms.LLM`
(`GenerateContent`, `Stream`, `Provider`, `Model`) plus `Capabilities()`,
`Embed`, `EmbedQuery`, `EmbedDocuments`, and a convenience `Call`.

!!! note "Capabilities are merged, not overwritten"
    `BaseProvider.Capabilities()` merges your static `ProviderConfig.Capabilities`
    with the per-model capability registry. Your explicitly declared, non-zero
    fields always win; the registry only fills fields you left at their zero
    value. A provider that declares `Vision: true` keeps reporting it even if the
    registry has a conservative default.

## A complete custom provider

The following is a complete, compiling provider for a fictional `Acme AI`
endpoint. It uses the idiomatic functional-options constructor that the SDK's
built-in OpenAI-compatible providers (e.g. `pkg/providers/featherless`) use.

```go
// Package acmeai is a custom provider for the (fictional) Acme AI inference API,
// an OpenAI-compatible endpoint. It is built on pkg/openaicompat.
package acmeai

import (
	"net/http"
	"time"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

const (
	defaultBaseURL = "https://api.acme.ai/v1"
	defaultModel   = "acme-large"
	envAPIKey      = "ACME_API_KEY"
)

// providerConfig declares the static identity and capabilities of the provider.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.Provider("acmeai"),
	ProviderName:          "acmeai",
	DefaultModel:          defaultModel,
	DefaultEmbeddingModel: "acme-embed",
	Capabilities: llms.Capabilities{
		Streaming:  true,
		Tools:      true,
		JSONMode:   true,
		Embeddings: true,
	},
}

// options holds resolved construction settings.
type options struct {
	apiKey          string
	model           string
	embeddingModel  string
	baseURL         string
	timeout         time.Duration
	httpClient      *http.Client
	allowPrivateIPs bool
	allowHTTP       bool
}

// Option configures a Client.
type Option func(*options)

// WithAPIKey sets the API key explicitly (otherwise ACME_API_KEY is read).
func WithAPIKey(key string) Option { return func(o *options) { o.apiKey = key } }

// WithModel overrides the default chat model.
func WithModel(model string) Option { return func(o *options) { o.model = model } }

// WithEmbeddingModel overrides the default embedding model.
func WithEmbeddingModel(model string) Option { return func(o *options) { o.embeddingModel = model } }

// WithBaseURL overrides the API base URL (useful for proxies/self-hosting).
func WithBaseURL(url string) Option { return func(o *options) { o.baseURL = url } }

// WithTimeout sets the per-request HTTP timeout.
func WithTimeout(d time.Duration) Option { return func(o *options) { o.timeout = d } }

// WithHTTPClient supplies a custom *http.Client.
func WithHTTPClient(c *http.Client) Option { return func(o *options) { o.httpClient = c } }

// WithAllowPrivateIPs opts out of SSRF protection (for private/local endpoints).
func WithAllowPrivateIPs() Option { return func(o *options) { o.allowPrivateIPs = true } }

// WithAllowHTTP allows plain-HTTP (non-HTTPS) requests.
func WithAllowHTTP() Option { return func(o *options) { o.allowHTTP = true } }

// Client is the Acme AI provider. It embeds openaicompat.BaseProvider, which
// supplies the full llms.LLM and llms.Embedder implementation.
type Client struct {
	openaicompat.BaseProvider
}

// New constructs an Acme AI client.
func New(opts ...Option) (*Client, error) {
	o := &options{
		model:   defaultModel,
		baseURL: defaultBaseURL,
	}
	for _, fn := range opts {
		fn(o)
	}

	// Resolve the key from the explicit option, then ACME_API_KEY, then
	// LLM_API_KEY. Returns an error wrapping llms.ErrMissingAPIKey if absent.
	apiKey, err := llms.RequireAPIKey(providerConfig.ProviderName, o.apiKey, envAPIKey)
	if err != nil {
		return nil, err
	}

	oc := openaicompat.NewClient(openaicompat.ClientConfig{
		BaseURL:         o.baseURL,
		APIKey:          apiKey,
		Headers:         map[string]string{"User-Agent": "acmeai-go/1.0"},
		Timeout:         o.timeout,
		HTTPClient:      o.httpClient,
		AllowPrivateIPs: o.allowPrivateIPs,
		AllowHTTP:       o.allowHTTP,
	})

	// Copy the static config and apply per-instance overrides.
	cfg := providerConfig
	cfg.DefaultModel = o.model
	if o.embeddingModel != "" {
		cfg.DefaultEmbeddingModel = o.embeddingModel
	}

	return &Client{
		BaseProvider: openaicompat.NewBaseProvider(oc, cfg),
	}, nil
}

// Compile-time interface checks document the contract you satisfy.
var (
	_ llms.LLM             = (*Client)(nil)
	_ llms.CapableProvider = (*Client)(nil)
	_ llms.Embedder        = (*Client)(nil)
)
```

That is the whole provider. By embedding `BaseProvider`, `*Client` already
satisfies `llms.LLM`, `llms.CapableProvider`, and `llms.Embedder` — no further
method implementations are required.

## Using your provider

The custom client behaves exactly like a built-in one. It plugs into every
package-level helper, middleware, and agent loop in the SDK.

```go
ctx := context.Background()

c, err := acmeai.New(acmeai.WithModel("acme-large"))
if err != nil {
	log.Fatal(err)
}

// Package-level one-shot helper (Call is NOT a method on the interface).
text, _ := llms.Call(ctx, c, "Hello!")

// Full message API with call options.
resp, _ := c.GenerateContent(ctx, []llms.Message{
	{Role: llms.RoleUser, Content: "Summarize the SDK in one line."},
}, llms.WithTemperature(0.7), llms.WithMaxTokens(256))
fmt.Println(resp.Content)

// Streaming.
chunks, _ := c.Stream(ctx, []llms.Message{
	{Role: llms.RoleUser, Content: "Stream a haiku."},
})
for chunk := range chunks {
	if chunk.Error != nil {
		log.Fatal(chunk.Error)
	}
	if chunk.Done {
		break
	}
	fmt.Print(chunk.Content)
}

// Embeddings (BaseProvider also implements llms.Embedder).
if emb, ok := llms.AsEmbedder(c); ok {
	vec, _ := llms.EmbedQuery(ctx, emb, "vectorize me")
	fmt.Println(len(vec)) // []float32
}

// Capability introspection.
fmt.Println(c.Provider(), c.Model(), c.Capabilities().Streaming)
```

Because `*Client` is an ordinary `llms.LLM`, it composes with the SDK's
decorators — for example resilience and observability:

```go
resilient := llms.NewResilientClient(c, llms.WithMaxRetries(3))
observed := llms.NewOTelMiddleware(resilient)
_ = observed // still an llms.LLM
```

## Registering for construct-by-name

To let callers build your provider through `llms.New("acmeai", cfg)` (and the
`pkg/providers/all` registration pattern), register a factory in an `init`
function. `llms.RegisterProvider` lives in the root package; names are
case-insensitive.

```go
func init() {
	llms.RegisterProvider("acmeai", func(cfg llms.Config) (llms.LLM, error) {
		opts := make([]Option, 0, 6)
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
		return New(opts...)
	})
}
```

With that `init` linked into the build, callers can construct your provider
without importing your package directly:

```go
import _ "example.com/acmeai" // run the init() that registers the factory

llm, err := llms.New("acmeai", llms.Config{Model: "acme-large"})
// llms.RegisteredProviders() now includes "acmeai"
```

`llms.Config` carries the common construction settings the factory maps to your
options:

```go
type Config struct {
	APIKey          string
	Model           string
	BaseURL         string
	Timeout         time.Duration
	AllowPrivateIPs bool
	HTTPClient      *http.Client
	Extra           map[string]string // provider-specific keys
}
```

Use `Config.Extra` for parameters that have no common field (the built-in RunPod
provider, for instance, reads `Extra["endpoint_id"]`).

## Provider-specific request fields

Some endpoints accept extra top-level JSON fields (a LoRA adapter id, a custom
sampler, etc.). Rather than forking the request type, set them via the call
option `llms.WithExtraBodyParam` / `llms.WithExtraBody` / `llms.WithAdapterID` —
`BaseProvider` flattens them into the request body:

```go
resp, _ := c.GenerateContent(ctx, msgs,
	llms.WithAdapterID("predibase/sql-lora"),
	llms.WithExtraBodyParam("acme_sampler", "top_a"),
)
```

If you need full control over the wire request, hold a `*openaicompat.Client`
directly and call its methods, using `ExtraBody` on the request:

```go
oc := openaicompat.NewClient(openaicompat.ClientConfig{
	BaseURL: "https://api.acme.ai/v1",
	APIKey:  apiKey,
})

req := &openaicompat.ChatCompletionRequest{
	Model: "acme-large",
	Messages: []openaicompat.ChatMessage{
		{Role: "user", ContentValue: "Hello"},
	},
	// Flattened to the top level of the JSON body, not nested under "extra_body".
	ExtraBody: map[string]any{"adapter_id": "predibase/sql-lora"},
}

res, err := oc.CreateChatCompletion(ctx, req)
```

The `Client` exposes `CreateChatCompletion`, `CreateChatCompletionStream`,
`CreateEmbedding`, `ListModels`, and `ListModelsWithQuery`. If you have embedded
`BaseProvider`, you can also reach the underlying client through its `Client()`
accessor.

## Conversion helpers

When driving the `Client` directly you can reuse the same conversions
`BaseProvider` uses internally, so your custom path produces identical
`llms` types:

- `openaicompat.BuildChatRequest(model, messages, opts, stream)` — build a
  `ChatCompletionRequest` from `llms` messages and applied `CallOptions`.
- `openaicompat.ConvertMessages(messages)` — `[]llms.Message` to `[]ChatMessage`.
- `openaicompat.ConvertResponse(resp)` — `*ChatCompletionResponse` to `*llms.Response`.
- `openaicompat.ConvertEmbeddingResponse(resp)` — to `*llms.EmbeddingResponse`.
- `openaicompat.WrapError(provider, op, err)` — attach provider context to errors.

## Checklist

1. Build a `Client` with `openaicompat.NewClient(ClientConfig{...})`.
2. Declare a `ProviderConfig` (identity + static `Capabilities`).
3. Define a `Client` struct that embeds `openaicompat.BaseProvider`.
4. In `New`, resolve the key with `llms.RequireAPIKey`, then call
   `openaicompat.NewBaseProvider(client, cfg)`.
5. Add compile-time `var _ llms.LLM = (*Client)(nil)` assertions.
6. (Optional) Register a factory with `llms.RegisterProvider` in `init`.

## See also

- [ARCHITECTURE.md](../ARCHITECTURE.md) — "Building custom providers on
  `pkg/openaicompat`" and the provider-model overview.
- The `pkg/providers/featherless` and `pkg/providers/synthetic` packages are
  minimal real-world references built exactly this way.
- The `ExampleBaseProvider` example in `pkg/openaicompat` shows the smallest
  possible embedding-only provider.
