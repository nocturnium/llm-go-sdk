# Prompt Caching

Caching a large, stable prompt prefix — a long system prompt, tool definitions,
or a document — lets the provider reuse it across requests instead of
reprocessing it every time. Cache reads are billed at a steep discount, so
caching cuts both latency and cost for repeated or multi-turn calls.

Providers fall into two camps, and the SDK presents one surface for both:

- **Explicit breakpoints** (Anthropic): you choose what to cache.
- **Automatic** (OpenAI, DeepSeek, Gemini): the provider caches transparently.

Either way, cache token usage is reported uniformly so you can see and cost it.

## Enabling caching

```go
// Cache the stable prefix (system prompt + tool definitions) where the provider
// supports explicit breakpoints. A no-op on automatic-caching providers.
resp, err := client.GenerateContent(ctx, messages, llms.WithCache())

// Request a specific cache lifetime. A TTL >= 1h selects Anthropic's 1-hour tier.
resp, err := client.GenerateContent(ctx, messages, llms.WithCacheTTL(time.Hour))

// Disable the SDK's automatic caching for a call.
resp, err := client.GenerateContent(ctx, messages, llms.WithoutCache())
```

On Anthropic the system prompt is cached by default; `WithCache` additionally
caches tool definitions, and `WithoutCache` turns caching off.

## Explicit per-message breakpoints

For fine control, mark a message with `CacheControl`. The provider caches the
prefix up to and including that message (Anthropic only; ignored elsewhere):

```go
messages := []llms.Message{
    {Role: llms.RoleSystem, Content: longSystemPrompt},
    {
        Role:         llms.RoleUser,
        Content:      bigDocument,
        CacheControl: &llms.CacheControl{TTL: time.Hour},
    },
    {Role: llms.RoleUser, Content: "Summarize the document."},
}
```

Anthropic allows at most four cache breakpoints per request; the SDK keeps the
highest-value ones (system, tools, then earliest messages) and drops any extras
so the request never fails.

## Reading cache usage

Token usage is normalized so cost is computed consistently across providers:

- `Usage.PromptTokens` — input tokens billed at the standard rate (**excludes**
  cached tokens).
- `Usage.CacheReadTokens` — tokens served from cache (discounted).
- `Usage.CacheCreationTokens` — tokens written to cache (Anthropic).

```go
fmt.Printf("prompt=%d cache_read=%d cache_creation=%d\n",
    resp.Usage.PromptTokens, resp.Usage.CacheReadTokens, resp.Usage.CacheCreationTokens)
```

## Cost tracking

`CostTracker` and `EstimateCost` account for the discounted cache-read and
cache-write rates, so cached workloads are estimated accurately:

```go
tracker := llms.NewCostTracker()
tracker.Record(client.Provider(), client.Model(), resp.Usage)
fmt.Println(llms.FormatCost(tracker.GetTotalCost()))
```

## Capability detection

```go
if client.Capabilities().PromptCaching {
    // provider reports cache usage / supports caching
}
```

!!! note "Gemini explicit cached content"
    Gemini's implicit caching (2.5 models) is reported via `CacheReadTokens`. The
    explicit `CachedContent` resource API is not yet wrapped by the SDK.
