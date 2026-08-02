# AGENTS.md - LLMs Package Code Quality Standards

This document defines strict code quality standards for AI coding assistants working on the `llms` package. These guidelines exist to prevent architectural drift and maintain consistency across providers.

**Scope**: This applies to all code in the module root `github.com/nocturnium/llm-go-sdk/v6` (this repository), including providers, internal packages, and core interfaces.

---

## Table of Contents

1. [Critical Rules](#critical-rules)
2. [Code Organization](#code-organization)
3. [Testing Requirements](#testing-requirements)
4. [Configuration Standards](#configuration-standards)
5. [Security Requirements](#security-requirements)
6. [Performance Guidelines](#performance-guidelines)
7. [Observability Standards](#observability-standards)
8. [Resilience Patterns](#resilience-patterns)
9. [Message and Content Handling](#message-and-content-handling)
10. [Documentation Standards](#documentation-standards)
11. [Forbidden Patterns](#forbidden-patterns)
12. [Adding a New Provider](#adding-a-new-provider)
13. [Pull Request Checklist](#pull-request-checklist)
14. [Known Technical Debt](#known-technical-debt)

---

## Critical Rules

### 1. Provider Implementation Pattern

**REQUIRED**: All new providers MUST use the `openaicompat.BaseProvider` pattern when the provider has an OpenAI-compatible API.

```go
// CORRECT - Use BaseProvider for OpenAI-compatible APIs
type Client struct {
    openaicompat.BaseProvider
    options *options // unexported
}

func New(opts ...Option) (*Client, error) {
    options := apply(opts...) // unexported apply helper
    apiKey, err := llms.RequireAPIKey("<name>", options.APIKey, llms.Env<Name>APIKey)
    if err != nil {
        return nil, err
    }
    client := openaicompat.NewClient(openaicompat.ClientConfig{
        BaseURL:         baseURL,
        APIKey:          apiKey,
        Timeout:         options.Timeout,
        HTTPClient:      options.HTTPClient,
        AllowPrivateIPs: options.AllowPrivateIPs,
        AllowHTTP:       options.AllowHTTP,
    })
    // NewBaseProvider takes (client, config); the default chat/embedding models are
    // carried on config.DefaultModel / config.DefaultEmbeddingModel.
    cfg := defaultProviderConfig
    cfg.DefaultModel = options.Model
    cfg.DefaultEmbeddingModel = options.EmbeddingModel
    return &Client{
        BaseProvider: openaicompat.NewBaseProvider(client, cfg),
        options:      options,
    }, nil
}
```

The constructor is named `New` and its option struct/apply helper are unexported. Each
provider exposes only `New(...)` plus `WithX(...)` options.

```go
// INCORRECT - Do not duplicate GenerateContent/Stream implementations.
// (The LLM interface has no Call method; the one-shot helper is the package
// function llms.Call(ctx, llm, prompt, ...) over GenerateContent.)
func (c *Client) GenerateContent(ctx context.Context, messages []llms.Message, opts ...llms.CallOption) (*llms.Response, error) {
    // Manual implementation - AVOID THIS
}
```

**Exception**: Only Anthropic and Gemini may have native implementations due to incompatible APIs. Any new native implementation requires explicit justification.

### 2. No Hardcoded Capabilities

**FORBIDDEN**: Do not hardcode model capabilities that vary by model variant.

```go
// INCORRECT
Capabilities: llms.Capabilities{
    MaxContextTokens: 128000,  // Assumes specific model
}

// CORRECT - Use model-specific lookup or conservative defaults
Capabilities: llms.Capabilities{
    MaxContextTokens: getModelContextLength(options.Model),
}
```

If model-specific capabilities cannot be determined, document the assumption clearly.

### 3. Error Handling Standards

All provider errors MUST:

1. Wrap with provider name prefix
2. Map to standard error types when possible
3. Preserve original error for unwrapping

```go
// CORRECT
if resp.StatusCode == 429 {
    return nil, fmt.Errorf("%s: %w", ProviderName, llms.ErrRateLimited)
}
return nil, fmt.Errorf("%s: request failed: %w", ProviderName, err)

// INCORRECT - Missing provider context
return nil, err

// INCORRECT - Losing original error
return nil, fmt.Errorf("request failed")
```

### 4. Context Cancellation

All long-running operations MUST check context cancellation:

```go
// REQUIRED in ListModels, ModelInfo, and any API calls
select {
case <-ctx.Done():
    return nil, ctx.Err()
default:
}
```

### 5. Thread Safety

**REQUIRED**: All shared data structures must be thread-safe.

```go
// CORRECT - Store by value to prevent aliasing
var modelIndex map[string]llms.ModelInfo  // Values, not pointers

// CORRECT - Return copies, not references
func lookupModel(id string) *llms.ModelInfo {
    if info, ok := modelIndex[id]; ok {
        return copyModelInfo(&info)  // Deep copy
    }
    return nil
}

// INCORRECT - Pointer aliasing creates data races
var modelIndex map[string]*llms.ModelInfo
```

---

## Code Organization

### Provider Package Structure

Each provider MUST have:

```
pkg/providers/<name>/
├── <name>.go           # Main client implementation
├── <name>_options.go   # Options and configuration
├── <name>_test.go      # Unit tests
├── models.go           # Model listing (if cached)
├── models_test.go      # Model listing tests
└── integration_test.go # Integration tests (build tag: integration)
```

**Canonical location**: All provider code lives in `pkg/providers/<name>/`. This is the only provider location — import providers as `github.com/nocturnium/llm-go-sdk/v6/pkg/providers/<name>`. (Earlier builds had a top-level `providers/*` backwards-compat shim and `pkg/*` alias packages; both have been removed. The shared types now live only in the root `llms` package.)

### Required Interfaces

All providers MUST implement:

| Interface | Required | Notes |
|-----------|----------|-------|
| `llms.LLM` | Yes | Core chat/completion (`GenerateContent`, `Stream`, `Provider`, `Model` — no `Call`) |
| `llms.CapableProvider` | Yes | Capability reporting |
| `llms.ModelLister` | Yes | Model enumeration (`ListModels` + `ModelInfo`) |
| `llms.Embedder` | If supported | Embedding generation (single `Embed` method) |

Verify interface compliance with compile-time checks:

```go
var (
    _ llms.LLM             = (*Client)(nil)
    _ llms.CapableProvider = (*Client)(nil)
    _ llms.ModelLister     = (*Client)(nil)
)
```

---

## Testing Requirements

### Minimum Test Coverage

Every provider MUST have tests for:

1. **Options Tests**
   - `TestDefaultOptions` - Verify defaults are sensible
   - `TestApplyOptions` - Verify option application
   - `TestNewClientMissingAPIKey` - Error on missing key
   - `TestNewClientWithEnvAPIKey` - Environment variable fallback
   - `TestNewClientWithLLMAPIKeyFallback` - Generic fallback

2. **Interface Tests**
   - `TestClientImplementsInterface` - Compile-time interface check
   - `TestClientImplementsModelLister` - If applicable
   - `TestClientImplementsEmbedder` - If applicable

3. **Model Listing Tests** (if cached models)
   - `TestCachedModelsMetadata` - All models have required fields
   - `TestListModels` - Pagination, filtering
   - `TestModelInfo` - Exact match, case-insensitive, not found (`llms.ErrModelNotFound`)
   - `TestConcurrentListModels` - Thread safety
   - `TestConcurrentModelInfo` - Thread safety

4. **Integration Tests** (build tag: `integration`)
   - Basic completion test
   - Streaming test
   - Error handling test

### Race Detection

All tests MUST pass with race detection:

```bash
go test -race ./...
```

### Test Naming Convention

```go
func TestFeature(t *testing.T) { }           // Top-level feature
func TestFeature_Subfeature(t *testing.T) { } // Subfeature
func TestFeature_EdgeCase(t *testing.T) { }   // Edge case

// Use t.Run for related subtests
func TestListModels(t *testing.T) {
    t.Run("returns all models", func(t *testing.T) { })
    t.Run("with limit", func(t *testing.T) { })
    t.Run("with cursor", func(t *testing.T) { })
}
```

---

## Configuration Standards

### Environment Variables

Follow this precedence (highest to lowest):

1. Explicit option: `WithAPIKey("key")`
2. Provider-specific env: `PROVIDER_API_KEY`
3. Generic fallback: `LLM_API_KEY`

```go
func resolveAPIKey(explicit string, providerEnvVars ...string) string {
    if explicit != "" {
        return explicit
    }
    for _, env := range providerEnvVars {
        if key := os.Getenv(env); key != "" {
            return key
        }
    }
    return os.Getenv("LLM_API_KEY")
}
```

### Default Models

- MUST be a current, widely-available model
- MUST be documented in the provider's doc comment
- SHOULD be updated when models are deprecated

---

## Security Requirements

### API Key Handling

**CRITICAL**: API keys are secrets. Follow these rules strictly:

```go
// FORBIDDEN - Never log API keys
log.Printf("Using API key: %s", apiKey)  // NEVER DO THIS

// FORBIDDEN - Never include in error messages
return fmt.Errorf("auth failed for key %s", apiKey)  // NEVER DO THIS

// CORRECT - Redact or omit entirely
log.Printf("Using API key: %s...%s", apiKey[:4], apiKey[len(apiKey)-4:])
return fmt.Errorf("authentication failed")  // No key in message
```

### Input Validation

All user-provided inputs MUST be validated:

```go
// REQUIRED - Validate before use
func (c *Client) GenerateContent(ctx context.Context, messages []llms.Message, opts ...llms.CallOption) (*llms.Response, error) {
    if len(messages) == 0 {
        return nil, fmt.Errorf("%s: messages cannot be empty", ProviderName)
    }
    // ... continue
}
```

### URL Construction

**REQUIRED**: Use `net/url` package for all URL construction:

```go
// CORRECT - Use url.Values for query parameters
params := url.Values{}
params.Set("model", modelID)
params.Set("limit", strconv.Itoa(limit))
fullURL := baseURL + "?" + params.Encode()

// INCORRECT - Manual string concatenation (injection risk)
fullURL := baseURL + "?model=" + modelID + "&limit=" + limit
```

### SSRF Protection

The shared HTTP layer (`internal/httpclient`) blocks requests to private, loopback,
link-local, and cloud-metadata addresses and **requires HTTPS** by default. Providers
must thread the `AllowPrivateIPs` / `AllowHTTP` flags from their options into
`openaicompat.ClientConfig` (or the native client config) so users can opt out for
self-hosted/private endpoints via the no-argument `WithAllowPrivateIPs()` and
`WithAllowHTTP()` options.

- Off-by-default is the rule: never enable private/HTTP access implicitly.
- Local-only providers (Ollama, llama.cpp, Infinity) may default these flags to `true`,
  since they target `localhost` servers — document that default.

### Content Filtering

- Respect provider content filtering responses
- Map content filter errors to `llms.ErrContentFiltered`
- Do not attempt to bypass content filters

---

## Performance Guidelines

### Connection Management

**REQUIRED**: Reuse HTTP clients and connections:

```go
// CORRECT - Share HTTP client across requests
type Client struct {
    httpClient *httpclient.Client  // Reused for all requests
}

// INCORRECT - Creating new client per request
func (c *Client) GenerateContent(...) {
    client := &http.Client{}  // AVOID - creates new connections each time
}
```

### Timeout Configuration

All network operations MUST have timeouts:

```go
// REQUIRED - Always set timeouts
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

// Provider-specific defaults (document these):
// - Chat completion: 120s (long responses)
// - Streaming first byte: 30s
// - Model listing: 30s
// - Embeddings: 60s
```

### Streaming Memory Management

**REQUIRED**: Properly manage streaming resources:

```go
// CORRECT - Use buffered channels with reasonable size
chunks := make(chan llms.StreamChunk, options.StreamBufferSize)

// CORRECT - Always close channels
defer close(chunks)

// CORRECT - Stop processing on context cancellation
select {
case <-ctx.Done():
    return
case chunks <- chunk:
}
```

### Batch Operations

When processing multiple items:

```go
// CORRECT - Use bounded concurrency
sem := make(chan struct{}, maxConcurrent)
for _, item := range items {
    sem <- struct{}{}
    go func(item Item) {
        defer func() { <-sem }()
        // process item
    }(item)
}

// INCORRECT - Unbounded goroutines
for _, item := range items {
    go process(item)  // Can overwhelm provider rate limits
}
```

---

## Observability Standards

### OpenTelemetry Integration

The package uses OpenTelemetry. Follow these standards:

```go
// REQUIRED - Create spans for provider operations
ctx, span := otel.Tracer("llms").Start(ctx, "provider.operation")
defer span.End()

// REQUIRED - Add relevant attributes
span.SetAttributes(
    attribute.String("llm.provider", string(c.Provider())),
    attribute.String("llm.model", c.Model()),
    attribute.String("llm.operation", "chat"),
)

// REQUIRED - Record errors
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
}
```

### Metrics to Capture

Providers SHOULD expose these metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `llm.request.duration` | Histogram | Request duration in ms |
| `llm.request.tokens.input` | Counter | Input tokens consumed |
| `llm.request.tokens.output` | Counter | Output tokens generated |
| `llm.request.errors` | Counter | Error count by type |
| `llm.stream.chunks` | Counter | Stream chunks delivered |

### Logging Standards

```go
// Use structured logging fields
// CORRECT
slog.Info("request completed",
    "provider", c.Provider(),
    "model", c.Model(),
    "duration_ms", duration.Milliseconds(),
    "tokens_in", usage.InputTokens,
    "tokens_out", usage.OutputTokens,
)

// INCORRECT - Unstructured logging
log.Printf("Request to %s completed in %v", provider, duration)
```

---

## Resilience Patterns

### Retry Policy

Use exponential backoff for transient errors:

```go
// CORRECT - Exponential backoff with jitter
retryableErrors := []error{llms.ErrRateLimited, llms.ErrServiceUnavailable}

backoff := time.Second
for attempt := 0; attempt < maxRetries; attempt++ {
    result, err := doRequest(ctx)
    if err == nil {
        return result, nil
    }
    if !isRetryable(err, retryableErrors) {
        return nil, err
    }

    jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    case <-time.After(backoff + jitter):
    }
    backoff *= 2
    if backoff > maxBackoff {
        backoff = maxBackoff
    }
}
```

### Retryable vs Non-Retryable Errors

| Error | Retryable | Notes |
|-------|-----------|-------|
| `ErrRateLimited` | Yes | Back off and retry |
| `ErrServiceUnavailable` | Yes | Provider temporary issue |
| `ErrTimeout` | Yes | Network issues |
| `ErrAuthentication` | No | Fix credentials |
| `ErrQuotaExceeded` | No | Billing issue |
| `ErrContentFiltered` | No | Content policy |
| `ErrModelNotFound` | No | Wrong model ID |
| `ErrContextLengthExceeded` | No | Reduce input size |

### Fallback Chains

When using fallback providers:

```go
// CORRECT - Preserve original error context
var lastErr error
for _, provider := range fallbackChain {
    result, err := llms.Call(ctx, provider, prompt)
    if err == nil {
        return result, nil
    }
    lastErr = fmt.Errorf("%s failed: %w (previous: %v)",
        provider.Provider(), err, lastErr)

    // Don't fallback on non-retryable errors
    if errors.Is(err, llms.ErrContentFiltered) {
        return nil, err
    }
}
return nil, fmt.Errorf("all providers failed: %w", lastErr)
```

---

## Message and Content Handling

### System Messages

```go
// CORRECT - System message is always first
messages := []llms.Message{
    {Role: llms.RoleSystem, Content: systemPrompt},
    {Role: llms.RoleUser, Content: userMessage},
}

// CORRECT - Single system message only
// Multiple system messages should be concatenated
```

### Tool/Function Calls

```go
// REQUIRED - Validate tool definitions
func validateTools(tools []llms.Tool) error {
    for _, tool := range tools {
        if tool.Name == "" {
            return fmt.Errorf("tool name is required")
        }
        if tool.Description == "" {
            return fmt.Errorf("tool %q: description is required", tool.Name)
        }
        // Validate JSON schema if present
    }
    return nil
}

// REQUIRED - Handle tool results properly
// Tool results must reference the original tool call ID
```

### Vision/Multi-Modal Content

```go
// REQUIRED - Validate image content before sending
if err := llms.ValidateImageContent(imageData); err != nil {
    return nil, fmt.Errorf("invalid image: %w", err)
}

// REQUIRED - Check provider vision support
caps := llms.GetCapabilities(client)
if !caps.Vision {
    return nil, fmt.Errorf("%s: vision not supported", client.Provider())
}

// Image size limits (check provider docs):
// - OpenAI: 20MB
// - Anthropic: 5MB base64
// - Gemini: varies by model
```

### Message Validation

```go
// REQUIRED - Validate message sequence
func validateMessages(messages []llms.Message) error {
    if len(messages) == 0 {
        return fmt.Errorf("at least one message is required")
    }

    // Check for valid role sequence
    for i, msg := range messages {
        if msg.Role == "" {
            return fmt.Errorf("message %d: role is required", i)
        }
        if msg.Content == "" && len(msg.Parts) == 0 {
            return fmt.Errorf("message %d: content or parts required", i)
        }
    }
    return nil
}
```

---

## Documentation Standards

### Package Documentation

Every provider package MUST have a doc comment:

```go
// Package <name> provides an LLM client for <Provider Name>.
//
// # Authentication
//
// Set the <PROVIDER>_API_KEY environment variable or use WithAPIKey().
//
// # Default Model
//
// The default model is "<model-id>". Override with WithModel().
//
// # Supported Features
//
// - Chat completions
// - Streaming
// - Tool/function calling
// - Vision (if applicable)
// - Embeddings (if applicable)
//
// # Example
//
//     client, err := <name>.New()
//     if err != nil {
//         log.Fatal(err)
//     }
//     response, err := llms.Call(ctx, client, "Hello!")
package <name>
```

### Exported Functions

All exported functions MUST have doc comments explaining:
- What it does
- Parameters
- Return values
- Errors that may be returned

---

## Forbidden Patterns

### Do Not

1. **Duplicate streaming logic** - Use BaseProvider or extract to shared helper
2. **Return pointers to cached data** - Always return copies
3. **Ignore context cancellation** - Check `ctx.Done()` in loops
4. **Use `strings.Title`** - Deprecated; use custom titleCase or cases package
5. **Hardcode model lists without FromCache flag** - Mark cached data appropriately
6. **Skip error wrapping** - Always include provider context
7. **Use manual URL string concatenation** - Use `url.Values{}` for query params
8. **Create providers without interface compliance checks** - Add `var _ Interface = (*Type)(nil)`

### Avoid

1. Provider-specific message formats in core package
2. Blocking operations without timeout
3. Returning nil slices when empty slice is expected
4. Complex nested conditionals (extract to functions)

---

## Adding a New Provider

Follow this step-by-step guide when adding a new LLM provider:

### Step 1: Determine Provider Type

```
Is the provider OpenAI-compatible?
├── YES → Use openaicompat.BaseProvider (proceed to Step 2a)
└── NO  → Requires native implementation (needs justification, proceed to Step 2b)
```

### Step 2a: OpenAI-Compatible Provider

The construction surface is `New(...)` plus `WithX(...)` options. The `options` struct,
its `apply`/`defaultOptions` helpers, and the `defaultProviderConfig` are **unexported**.
Resolve the API key with `llms.RequireAPIKey` (explicit → provider env var → `LLM_API_KEY`),
and pass the HTTP/SSRF knobs through to `openaicompat.ClientConfig`.

```go
// pkg/providers/<name>/<name>.go
package <name>

import (
    llms "github.com/nocturnium/llm-go-sdk/v6"
    "github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

const defaultBaseURL = "https://api.<provider>.com/v1"

// defaultProviderConfig declares the provider's identity and capability defaults.
var defaultProviderConfig = openaicompat.ProviderConfig{
    Provider:     llms.Provider<Name>,
    Capabilities: llms.Capabilities{ /* Streaming, Tools, JSONMode, ... */ },
}

type Client struct {
    openaicompat.BaseProvider
    options *options // unexported
}

func New(opts ...Option) (*Client, error) {
    options := apply(opts...)

    apiKey, err := llms.RequireAPIKey("<name>", options.APIKey, llms.Env<Name>APIKey)
    if err != nil {
        return nil, err
    }

    baseURL := options.BaseURL
    if baseURL == "" {
        baseURL = defaultBaseURL
    }

    client := openaicompat.NewClient(openaicompat.ClientConfig{
        BaseURL:         baseURL,
        APIKey:          apiKey,
        Timeout:         options.Timeout,         // WithTimeout
        HTTPClient:      options.HTTPClient,       // WithHTTPClient
        AllowPrivateIPs: options.AllowPrivateIPs,  // WithAllowPrivateIPs
        AllowHTTP:       options.AllowHTTP,        // WithAllowHTTP
    })

    if options.ProviderConfig == nil {
        options.ProviderConfig = &defaultProviderConfig
    }

    // NewBaseProvider takes (client, config). Put the resolved default chat and
    // embedding models on the config before constructing the base provider.
    cfg := *options.ProviderConfig
    cfg.DefaultModel = options.Model
    cfg.DefaultEmbeddingModel = options.EmbeddingModel

    return &Client{
        BaseProvider: openaicompat.NewBaseProvider(client, cfg),
        options:      options,
    }, nil
}

// Interface compliance checks
var (
    _ llms.LLM             = (*Client)(nil)
    _ llms.CapableProvider = (*Client)(nil)
    _ llms.ModelLister     = (*Client)(nil)
)
```

### Step 2b: Native Provider (Requires Justification)

Document why BaseProvider cannot be used:

```go
// pkg/providers/<name>/<name>.go

// Package <name> provides a native LLM client for <Provider>.
//
// This provider uses a native implementation because:
// - <Reason 1: e.g., "Uses non-OpenAI message format">
// - <Reason 2: e.g., "Requires custom authentication flow">
// - <Reason 3: e.g., "Has provider-specific streaming format">
package <name>
```

### Step 3: Create Options File

The `Option` type and every `WithX` constructor are exported; the `options` struct and
the `defaultOptions`/`apply` helpers are **unexported** (consumers configure the client
only through `New` + `WithX`). Every provider must expose `WithTimeout`,
`WithHTTPClient`, and the two no-argument SSRF opt-outs `WithAllowPrivateIPs` and
`WithAllowHTTP` for HTTP/SSRF control.

```go
// pkg/providers/<name>/<name>-options.go
package <name>

import (
    "net/http"
    "time"
)

// Option configures a <name> client.
type Option func(*options)

// options is the unexported configuration struct.
type options struct {
    APIKey          string
    Model           string
    BaseURL         string
    HTTPClient      *http.Client
    Timeout         time.Duration
    AllowPrivateIPs bool
    AllowHTTP       bool
    // Provider-specific options
}

func defaultOptions() *options {
    return &options{Model: "<default-model-id>"}
}

func apply(opts ...Option) *options {
    o := defaultOptions()
    for _, opt := range opts {
        opt(o)
    }
    return o
}

func WithAPIKey(key string) Option { return func(o *options) { o.APIKey = key } }
func WithModel(model string) Option { return func(o *options) { o.Model = model } }
func WithBaseURL(url string) Option { return func(o *options) { o.BaseURL = url } }
func WithHTTPClient(c *http.Client) Option { return func(o *options) { o.HTTPClient = c } }
func WithTimeout(d time.Duration) Option { return func(o *options) { o.Timeout = d } }

// WithAllowPrivateIPs allows requests to private/loopback IPs. Off by default
// (SSRF protection); needed for self-hosted or local endpoints.
func WithAllowPrivateIPs() Option { return func(o *options) { o.AllowPrivateIPs = true } }

// WithAllowHTTP allows plain-HTTP (non-HTTPS) requests. Off by default; pair with
// WithAllowPrivateIPs for a local/private plain-HTTP endpoint.
func WithAllowHTTP() Option { return func(o *options) { o.AllowHTTP = true } }
```

### Step 4: Register Provider Constant

In `llms.go`, add the provider constant:

```go
const (
    // ... existing providers
    Provider<Name> Provider = "<name>"
)
```

### Step 5: Implement Model Listing

```go
// pkg/providers/<name>/models.go
package <name>

// If using cached models:
var cachedModels = []llms.ModelInfo{
    {
        ID:            "<model-id>",
        DisplayName:   "<Model Name>",
        Provider:      llms.Provider<Name>,
        Organization:  "<Org>",
        ContextLength: 128000,
        MaxOutput:     8192,
        Types:         []llms.ModelType{llms.ModelTypeChat},
        FromCache:     true,
    },
    // ... more models
}

func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    // Implementation
}

// ModelInfo satisfies llms.ModelLister; return llms.ErrModelNotFound for an unknown ID.
func (c *Client) ModelInfo(ctx context.Context, modelID string) (*llms.ModelInfo, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    // Implementation - return copy, not reference
}
```

### Step 6: Create Required Tests

Create these test files with minimum coverage:

```
pkg/providers/<name>/
├── <name>_test.go        # Options, client creation, interface tests
├── models_test.go        # Model listing tests
└── integration_test.go   # Integration tests (build tag)
```

### Step 7: Update Documentation

1. Add provider to main package README
2. Document environment variables
3. Add usage examples

---

## Dependency Management

### Allowed Dependencies

| Category | Allowed | Notes |
|----------|---------|-------|
| Standard library | Yes | Prefer stdlib when possible |
| `go.opentelemetry.io/otel` | Yes | Observability |
| Provider SDKs | Case-by-case | Prefer HTTP client over SDK |
| Testing | `testify` discouraged | Use stdlib `testing` |

### Internal Package Rules

- `internal/` packages are implementation details
- Do not expose internal types in public APIs
- `internal/httpclient` - shared HTTP client
- `internal/anthropicapi` - Anthropic-specific types
- `internal/geminiapi` - Gemini-specific types
- `internal/ollamaapi` - Ollama-specific types
- `internal/llamacppapi` - llama.cpp-specific types
- Note: the OpenAI-compatible base provider is now **public** at `pkg/openaicompat` (no longer under `internal/`)

---

## Versioning and Deprecation

### Model Deprecation

When a model is deprecated:

```go
// 1. Add deprecation notice
{
    ID:          "old-model-id",
    DisplayName: "Old Model (Deprecated)",
    Deprecated:  true,  // If field exists
    // ...
}

// 2. Update default model if affected
const DefaultModel = "new-model-id"  // Update default

// 3. Document in CHANGELOG
```

### Breaking Changes

Breaking changes require:

1. Major version bump (if using semver)
2. Migration guide in CHANGELOG
3. Deprecation period for removed features (minimum 1 release)

---

## Pull Request Checklist

Before submitting changes to this package:

### Required Checks

- [ ] All tests pass: `go test ./...`
- [ ] Race detection passes: `go test -race ./...`
- [ ] No new linter warnings: `go vet ./...`
- [ ] Code is formatted: `gofmt -s -w .`

### Code Quality

- [ ] New provider follows BaseProvider pattern (or has documented justification)
- [ ] Interface compliance verified with `var _ = (*Type)(nil)` checks
- [ ] Errors wrapped with provider context
- [ ] Context cancellation checked in long operations
- [ ] Cached data returns copies, not references
- [ ] No API keys or secrets in code or logs

### Testing

- [ ] Unit tests added for new functionality
- [ ] Concurrent access tests for shared data
- [ ] Integration tests added for new providers
- [ ] Edge cases covered (empty input, cancellation, timeouts)

### Documentation

- [ ] Package doc comment present and complete
- [ ] Exported functions have doc comments
- [ ] Environment variables documented
- [ ] CHANGELOG updated for user-facing changes

### Performance

- [ ] HTTP client reused (not created per request)
- [ ] Streaming properly cleans up resources
- [ ] No unbounded goroutines or memory growth

---

## Naming Conventions

### Package Names

```
pkg/providers/<lowercase>/ # Provider packages: openai, anthropic, gemini
internal/<lowercase>/      # Internal packages: httpclient, anthropicapi, geminiapi
```

### Type Names

```go
// Client types
type Client struct { }           // Main client type (not Provider)

// Option types
type Option func(*options)       // Functional option (exported)
type options struct { }          // Configuration struct (UNEXPORTED)

// Constants (unexported defaults are fine)
const defaultBaseURL = "https://..." // Full URL with scheme
```

### Function Names

```go
// Constructor — named New (not NewClient)
func New(opts ...Option) (*Client, error)

// Functional options - With prefix (exported)
func WithAPIKey(key string) Option
func WithModel(model string) Option
func WithBaseURL(url string) Option
func WithTimeout(d time.Duration) Option
func WithHTTPClient(c *http.Client) Option
func WithAllowPrivateIPs() Option // no-arg SSRF opt-out (private/loopback IPs)
func WithAllowHTTP() Option       // no-arg SSRF opt-out (plain HTTP)

// Defaults / apply helpers — UNEXPORTED
func defaultOptions() *options
func apply(opts ...Option) *options
```

### Error Variables

```go
// Package-level errors - Err prefix
var (
    ErrRateLimited           = errors.New("rate limited")
    ErrAuthentication        = errors.New("authentication failed")
    ErrModelNotFound         = errors.New("model not found")
    ErrContextLengthExceeded = errors.New("context length exceeded")
)
```

### Test Names

```go
func TestClient_GenerateContent(t *testing.T)              // Method tests
func TestClient_GenerateContent_EmptyMessages(t *testing.T) // Edge case
func TestNewClient_MissingAPIKey(t *testing.T)             // Constructor error
func TestWithAPIKey(t *testing.T)                          // Option function
```

---

## Known Technical Debt

The following issues are known and should be addressed in future refactoring. **Do not add to this debt.** New code should follow the patterns in this document.

### Critical (Address Soon)

| Issue | Description | Files Affected |
|-------|-------------|----------------|
| Streaming duplication | Anthropic and Gemini have ~150 lines of nearly identical streaming code | `anthropic.go`, `gemini.go` |
| Hardcoded capabilities | Provider capabilities don't vary by model | All providers |

### High Priority

| Issue | Description | Files Affected |
|-------|-------------|----------------|
| Options duplication | Each provider has identical options boilerplate (~50 lines) | `*_options.go` |
| API key resolution | Identical env var lookup code in every provider | All providers |
| Error code mapping | Provider-specific error codes not consistently mapped | Converters |

### Medium Priority

| Issue | Description | Files Affected |
|-------|-------------|----------------|
| Missing provider registry | No dynamic provider discovery or factory | Core package |
| Inconsistent pagination | Cursor formats vary by provider without clear docs | Model listing |
| Test duplication | Same test patterns repeated across providers | `*_test.go` |
| Missing MockLLM | No test double for client testing | Tests |

### Low Priority (Nice to Have)

| Issue | Description | Files Affected |
|-------|-------------|----------------|
| Vision format coupling | Each provider has different image handling | Converters |
| Missing batch interface | Batch operations not standardized | Core interfaces |
| Embedder not in Wrapper | Wrapper pattern doesn't cover Embedder interface | `wrapper.go` |

### Debt Reduction Guidelines

When fixing technical debt:

1. Create an issue/ticket before starting
2. Ensure backward compatibility or document breaking changes
3. Add tests that would have caught the original issue
4. Update this section when debt is resolved

---

## Quick Reference

### Common Commands

```bash
# Run all tests
go test ./...

# Run with race detection
go test -race ./...

# Run specific provider tests
go test ./pkg/providers/openai/...

# Run integration tests (requires API keys)
go test -tags=integration ./...

# Format code
gofmt -s -w .

# Lint
go vet ./...

# Check for common issues
staticcheck ./...
```

### Environment Variables

| Variable | Provider | Description |
|----------|----------|-------------|
| `OPENAI_API_KEY` | OpenAI | OpenAI API key |
| `ANTHROPIC_API_KEY` | Anthropic | Anthropic API key |
| `GEMINI_API_KEY` | Gemini | Google AI API key |
| `GOOGLE_API_KEY` | Gemini | Alternative for Gemini |
| `GROQ_API_KEY` | Groq | Groq API key |
| `TOGETHER_API_KEY` | TogetherAI | Together AI API key |
| `FIREWORKS_API_KEY` | Fireworks | Fireworks API key |
| `MISTRAL_API_KEY` | Mistral | Mistral API key |
| `DEEPSEEK_API_KEY` | DeepSeek | DeepSeek API key |
| `CEREBRAS_API_KEY` | Cerebras | Cerebras API key |
| `PERPLEXITY_API_KEY` | Perplexity | Perplexity API key |
| `FEATHERLESS_API_KEY` | Featherless | Featherless API key |
| `SYNTHETIC_API_KEY` | Synthetic | Synthetic API key |
| `LLM_API_KEY` | All | Fallback for any provider |

---

## Version History

| Date | Change |
|------|--------|
| 2025-12-27 | Initial version |
| 2025-12-27 | Added security, performance, observability, resilience sections |
| 2025-12-27 | Added new provider guide, naming conventions, quick reference |
