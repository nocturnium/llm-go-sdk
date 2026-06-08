# Observability & Cost Tracking

Every observability feature in this SDK is implemented as **middleware** — a thin
wrapper that satisfies the `llms.LLM` interface and forwards to the client it
wraps. You compose them by nesting: each layer adds tracing, metrics, logging, or
cost accounting without touching your call sites. Because the wrappers implement
`llms.LLM`, the package-level helpers (`llms.Call`, `llms.GenerateTyped`,
`llms.RunTools`, …) work transparently on a wrapped client.

This guide covers four pillars:

| Pillar | Constructor | Verified source |
| --- | --- | --- |
| OpenTelemetry traces + metrics | `llms.NewOTelMiddleware` | `otel.go` |
| Langfuse (GenAI semconv) | `llms.NewLangfuseOTelMiddleware` | `otel_langfuse.go`, `otel_genai.go` |
| Structured logging | `llms.NewLoggingMiddleware` + `NewSlogLogger`/`NewJSONLogger` | `logging.go`, `logging_slog.go`, `logging_json.go` |
| Cost tracking | `llms.NewCostTracker` + `llms.NewCostMiddleware` | `cost.go` |

!!! info "Privacy-safe by default"
    Prompt and response **content is never captured by default**. The OTel
    middleware (`recordContent: false`), the Langfuse middleware
    (`captureInput`/`captureOutput: false`), and both structured loggers
    (`redact: true`) all start in a privacy-safe mode. You must explicitly opt in
    to record message bodies.

---

## OpenTelemetry

`llms.NewOTelMiddleware` wraps a client with OpenTelemetry spans and metrics. It
uses the globally registered tracer and meter providers
(`otel.Tracer(...)` / `otel.Meter(...)`) unless you override them, so it works
with whatever OTel SDK exporter you have configured (OTLP, stdout, Prometheus,
etc.).

```go
package main

import (
	"context"
	"log"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/providers/openai"
)

func main() {
	base, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		log.Fatal(err)
	}

	// Wrap with OpenTelemetry instrumentation.
	// Content recording is OFF by default; the false here is explicit for clarity.
	client, err := llms.NewOTelMiddleware(base, llms.WithContentRecording(false))
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "Summarize the theory of relativity in one line."},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(resp.Content)
}
```

`NewOTelMiddleware` returns `(*OTelMiddleware, error)` — the error surfaces if a
metric instrument fails to initialize. The returned value implements `llms.LLM`,
so it exposes `GenerateContent`, `Stream`, `Provider()`, `Model()`, and
`Unwrap()` (to retrieve the wrapped client).

### Options

| Option | Effect |
| --- | --- |
| `llms.WithContentRecording(bool)` | When `true`, records truncated prompt/response text on spans (`llm.prompt`, `llm.last_message`, `llm.response`). Off by default. |
| `llms.WithOTelTracer(trace.Tracer)` | Use a specific `trace.Tracer` instead of the global one. |
| `llms.WithOTelMeter(metric.Meter)` | Use a specific `metric.Meter` instead of the global one. |

The instrumentation scope name is exported as `llms.InstrumentationName`
(`"github.com/nocturnium/llm-go-sdk"`).

!!! warning "Content recording exposes data in traces"
    `WithContentRecording(true)` writes (truncated) prompt and completion text to
    span attributes. Only enable it in trusted environments — span data is often
    shipped to third-party backends.

### Spans

| Method | Span name | Span kind |
| --- | --- | --- |
| `GenerateContent` | `llm.generate_content` | client |
| `Stream` | `llm.stream` | client |
| `Call` (package helper, via wrapper) | `llm.call` | client |

Each span carries these attributes (recorded regardless of content settings):

- `llm.provider`, `llm.model`, `llm.request.type`, `llm.streaming`
- `llm.message_count`
- `llm.finish_reason`, `llm.tool_calls`
- `llm.tokens.prompt`, `llm.tokens.completion`, `llm.tokens.total`
- on error: `llm.error.type`, `llm.error.status_code` (the latter when the error
  is an `*llms.APIError`)

Errors are recorded with `span.RecordError` and the span status is set to
`codes.Error`; successful spans get `codes.Ok`.

### Metrics

The middleware emits six instruments on first use:

| Metric name | Type | Unit | Meaning |
| --- | --- | --- | --- |
| `llm.requests` | Int64 counter | `{request}` | Number of requests started |
| `llm.tokens.prompt` | Int64 counter | `{token}` | Prompt tokens consumed |
| `llm.tokens.completion` | Int64 counter | `{token}` | Completion tokens generated |
| `llm.request.duration` | Float64 histogram | `s` | Request latency in seconds |
| `llm.errors` | Int64 counter | `{error}` | Errors, tagged with `llm.error.type` |
| `llm.stream.chunks` | Int64 counter | `{chunk}` | Stream chunks received |

All metrics are tagged with `llm.provider`, `llm.model`, and
`llm.request.type` so you can break dashboards down per provider, model, and call
type.

### Streaming

`Stream` keeps its span open for the lifetime of the stream and finalizes
token/chunk metrics and the span status when the channel closes — even on panic
or early consumer exit. Captured streaming content is bounded (100 KB) to prevent
unbounded memory growth.

```go
chunks, err := client.Stream(ctx, messages)
if err != nil {
	log.Fatal(err)
}
for chunk := range chunks {
	if chunk.Error != nil {
		log.Printf("stream error: %v", chunk.Error)
		break
	}
	fmt.Print(chunk.Content)
}
// Span ends + final metrics recorded once the loop drains the channel.
```

---

## Langfuse

`llms.NewLangfuseOTelMiddleware` emits OpenTelemetry spans using the
[GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/)
that [Langfuse](https://langfuse.com/integrations/native/opentelemetry)
recognizes natively. Point your OTLP exporter at the Langfuse OTel endpoint and
the spans appear as generations — no Langfuse-specific transport code needed.

```go
base, err := openai.New(openai.WithModel("gpt-4o"))
if err != nil {
	log.Fatal(err)
}

tracker := llms.NewCostTracker()

client, err := llms.NewLangfuseOTelMiddleware(base,
	// Capture is OFF by default; opt in with an explicit byte budget.
	llms.WithLangfuseInputCapture(true, 4096),
	llms.WithLangfuseOutputCapture(true, 4096),
	// Optional: attach cost estimation to spans + the gen_ai.client.cost metric.
	llms.WithLangfuseCostTracker(tracker),
)
if err != nil {
	log.Fatal(err)
}

resp, err := client.GenerateContent(ctx, messages)
```

`NewLangfuseOTelMiddleware` returns `(*LangfuseOTelMiddleware, error)`. All spans
are named `llm.generation` with client span kind.

### Options

| Option | Effect |
| --- | --- |
| `llms.WithLangfuseInputCapture(enabled bool, maxLen int)` | Record the input messages on the span. Off by default. `maxLen` caps captured bytes (defaults to 100 KB when `0`). |
| `llms.WithLangfuseOutputCapture(enabled bool, maxLen int)` | Record the output text on the span. Off by default. |
| `llms.WithLangfuseInputFormat(llms.InputFormat)` | How input is serialized (`llms.InputFormatMessages` is the default). |
| `llms.WithLangfuseOutputFormat(llms.OutputFormat)` | How output is serialized (`llms.OutputFormatStructured` is the default). |
| `llms.WithLangfuseCostTracker(*llms.CostTracker)` | Enables cost estimation: records into the tracker, adds `gen_ai.usage.cost` to spans, and emits the `gen_ai.client.cost` metric. |
| `llms.WithLangfuseTracer(trace.Tracer)` / `llms.WithLangfuseMeter(metric.Meter)` | Override the tracer/meter. |

### GenAI attributes & metrics

Span attributes follow the GenAI semconv (exported as `llms.AttrGenAI*` /
`llms.AttrLangfuse*` constants in `otel_genai.go`):

- `gen_ai.system` (mapped from the provider via `llms.ProviderToGenAISystem`),
  `gen_ai.operation.name`, `gen_ai.request.model`, `gen_ai.response.model`
- request params: `gen_ai.request.temperature`, `gen_ai.request.max_tokens`,
  `gen_ai.request.top_p`, `gen_ai.request.frequency_penalty`,
  `gen_ai.request.presence_penalty`
- usage: `gen_ai.usage.prompt_tokens`, `gen_ai.usage.completion_tokens`,
  `gen_ai.usage.total_tokens`, `gen_ai.usage.cost`
- response: `gen_ai.response.finish_reason`, `gen_ai.tokens_per_second`
- streaming: `gen_ai.streaming`, `gen_ai.time_to_first_token`
- Langfuse-specific: `langfuse.user.id`, `langfuse.session.id`, `langfuse.tags`,
  `langfuse.version`, `langfuse.release`, `langfuse.environment`,
  `langfuse.trace.id`, `langfuse.observation.id`,
  `langfuse.parent_observation.id`, plus `langfuse.metadata.<key>` entries

Metrics (exported as `llms.MetricGenAI*` constants):

| Metric name | Type | Meaning |
| --- | --- | --- |
| `gen_ai.client.requests` | counter | Requests started |
| `gen_ai.client.tokens.prompt` | counter | Prompt tokens |
| `gen_ai.client.tokens.completion` | counter | Completion tokens |
| `gen_ai.client.duration` | histogram | Request latency (s) |
| `gen_ai.client.cost` | counter | Estimated cost (USD), when a cost tracker is attached |
| `gen_ai.client.time_to_first_token` | histogram | TTFT for streams (s) |
| `gen_ai.client.errors` | counter | Errors |

### Per-call trace context with `WithTrace`

Attach Langfuse trace identity, user/session, tags, version, and metadata to an
individual request with the `llms.WithTrace` call option. These values land on
the span as Langfuse attributes.

```go
resp, err := client.GenerateContent(ctx, messages,
	llms.WithTrace(llms.TraceOptions{
		TraceID:   "checkout-flow-42",
		UserID:    "user_123",
		SessionID: "sess_abc",
		Tags:      []string{"production", "checkout"},
		Version:   "v2.1.0",
		Metadata: map[string]any{
			// Keys must be alphanumeric; values are capped at 200 chars.
			"feature": "summarizer",
		},
	}),
)
```

`TraceOptions` fields (verified in `options.go`): `TraceID`, `SpanID`,
`ParentID`, `UserID`, `SessionID`, `Tags`, `Metadata`, `Version`.

!!! note "Propagating across a call hierarchy"
    For values shared across many calls (e.g. one user session spanning several
    LLM requests), set them once on the context instead of per call:

    ```go
    ctx = llms.WithTraceContext(ctx, llms.NewTraceContext(
        llms.WithUserID("user_123"),
        llms.WithSessionID("sess_abc"),
        llms.WithTags("production"),
    ))
    ```

    Per-call `WithTrace` values override the context-level `TraceContext`. Use
    `llms.PropagateAttributes(ctx, ...)` to derive a child context that inherits
    user/session/tags/version/metadata from its parent.

---

## Structured logging

`llms.NewLoggingMiddleware(llm, logger)` wraps a client and calls a `Logger`
implementation on every request, response, and error. The SDK ships two
loggers — both **redact prompt/response content by default**.

### slog logger

`llms.NewSlogLogger` adapts the standard library `log/slog`:

```go
import (
	"log/slog"
	"os"

	llms "github.com/nocturnium/llm-go-sdk"
)

handler := slog.NewJSONHandler(os.Stdout, nil)
logger := llms.NewSlogLogger(slog.New(handler),
	llms.WithLogLevel(slog.LevelInfo),
	// Redaction is ON by default. The line below is the explicit default.
	llms.WithRedaction(true),
	llms.WithMaxLength(500),
)

client := llms.NewLoggingMiddleware(base, logger)
```

It emits `llm_request`, `llm_response`, and `llm_error` records. Response records
always include `provider`, `model`, `operation`, `duration`, `finish_reason`, and
token counts (`prompt_tokens`, `completion_tokens`, `total_tokens`); message and
content text are included **only when redaction is disabled**. Errors that are
`*llms.APIError` add `status_code`, `error_type`, and `error_code`.

### JSON-lines logger

`llms.NewJSONLogger` writes one JSON object per line to a write function you
supply:

```go
logger := llms.NewJSONLogger(
	func(b []byte) error { _, err := os.Stdout.Write(b); return err },
	// Redaction is ON by default; opt out explicitly if you need bodies.
	llms.WithJSONRedaction(false),
	llms.WithJSONMaxLength(2000),
)

client := llms.NewLoggingMiddleware(base, logger)
```

When redaction is on, `messages` and `content` are stripped from the serialized
`LogEntry`; otherwise `content` is truncated to the configured max length.

!!! tip "Opting out of redaction"
    To log full prompts/responses (development, debugging), pass
    `llms.WithRedaction(false)` (slog) or `llms.WithJSONRedaction(false)` (JSON).
    Treat these logs as sensitive.

### Custom loggers

`Logger` is a small interface — implement it to forward entries anywhere (a
metrics pipeline, a Langfuse ingestion API, etc.):

```go
type Logger interface {
	LogRequest(ctx context.Context, req *llms.LogEntry)
	LogResponse(ctx context.Context, resp *llms.LogEntry)
	LogError(ctx context.Context, entry *llms.LogEntry, err error)
}
```

`LogEntry` carries Langfuse-friendly fields (`TraceID`, `UserID`, `SessionID`,
`Tags`, `CostUSD`, `TimeToFirstToken`, …) and a
`(*LogEntry).ToLangfuseGeneration()` helper that converts an entry into a
Langfuse `GENERATION` map. Use `llms.NopLogger{}` to discard everything.

---

## Cost tracking

A `CostTracker` accumulates per-model token usage and an estimated USD cost. Wrap
a client with `NewCostMiddleware` to record automatically, or call
`tracker.Record(...)` yourself.

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
	base, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		log.Fatal(err)
	}

	tracker := llms.NewCostTracker()
	client := llms.NewCostMiddleware(base, tracker)

	for _, prompt := range []string{
		"What is the capital of France?",
		"Write a haiku about Go.",
	} {
		if _, err := llms.Call(context.Background(), client, prompt); err != nil {
			log.Printf("request failed: %v", err)
		}
	}

	// Aggregate totals.
	prompt, completion := tracker.GetTotalTokens()
	fmt.Printf("Prompt tokens:     %d\n", prompt)
	fmt.Printf("Completion tokens: %d\n", completion)
	fmt.Printf("Requests:          %d\n", tracker.GetTotalRequests())
	fmt.Printf("Estimated cost:    %s\n", llms.FormatCost(tracker.GetTotalCost()))

	// Per-model breakdown.
	for _, u := range tracker.Report() {
		fmt.Printf("%s/%s: %d req, %s\n",
			u.Provider, u.Model, u.Requests, llms.FormatCost(u.EstimatedCost))
	}
}
```

`NewCostMiddleware` records usage on `GenerateContent` and on `Stream` (using the
final usage chunk). `Call` on the middleware routes through `GenerateContent`
internally so it is tracked too.

### Tracker API

| Method | Returns | Notes |
| --- | --- | --- |
| `Record(provider Provider, model string, usage Usage)` | — | Add one request's usage; cost is computed from the pricing table. |
| `RecordEmbedding(provider, model, usage EmbeddingUsage)` | — | Convenience for embedding usage. |
| `Report()` | `[]ModelUsage` | Per-model usage snapshots (copies). |
| `GetTotalCost()` | `float64` | Sum of estimated cost across models. |
| `GetTotalTokens()` | `(prompt, completion int64)` | Aggregate token totals. |
| `GetTotalRequests()` | `int64` | Total requests recorded. |
| `GetUsage(provider, model)` | `*ModelUsage` | Usage for one model (`nil` if untracked). |
| `Reset()` | — | Clear all accumulated usage. |
| `SetPricing(provider, model, Pricing)` | — | Override or add pricing for a model. |
| `GetPricing(provider, model)` | `(Pricing, bool)` | Look up current pricing. |

All tracker methods are safe for concurrent use.

`ModelUsage` fields include `Provider`, `Model`, `PromptTokens`,
`CompletionTokens`, `CacheReadTokens`, `CacheCreationTokens`, `Requests`,
`EstimatedCost`, `FirstUsed`, and `LastUsed`.

### Pricing and custom rates

The SDK ships a `DefaultPricing` table (USD per 1M tokens) covering common OpenAI,
Anthropic, Gemini, and TogetherAI models. Models not in the table estimate to
`$0` — supply your own rates for everything else:

```go
custom := map[string]llms.Pricing{
	// key format is "provider:model"
	"openai:gpt-4o": {PromptPerMillion: 2.50, CompletionPerMillion: 10.00},
}

// Merge custom rates over the defaults at construction time...
tracker := llms.NewCostTracker(custom)

// ...or set/override a single model later.
tracker.SetPricing(llms.ProviderOpenAI, "my-finetune", llms.Pricing{
	PromptPerMillion:     5.00,
	CompletionPerMillion: 15.00,
})
```

For a one-off estimate without a tracker, use the package helpers:

```go
cost := llms.EstimateCost(llms.ProviderOpenAI, "gpt-4o", resp.Usage)
fmt.Println(llms.FormatCost(cost)) // e.g. "$0.0123" (4 decimals under 1 cent)
```

`llms.FormatCost` renders 4 decimal places for sub-cent values and 2 decimals
otherwise.

---

## Composing middleware

Middleware wrappers nest because each one is itself an `llms.LLM`. Apply them
inside-out — the outermost wrapper runs first. A typical production stack:

```go
base, err := openai.New(openai.WithModel("gpt-4o"))
if err != nil {
	log.Fatal(err)
}

// 1. Cost tracking closest to the provider.
tracker := llms.NewCostTracker()
costed := llms.NewCostMiddleware(base, tracker)

// 2. OpenTelemetry traces + metrics.
traced, err := llms.NewOTelMiddleware(costed)
if err != nil {
	log.Fatal(err)
}

// 3. Structured logging on the outside.
logged := llms.NewLoggingMiddleware(traced,
	llms.NewSlogLogger(slog.Default()),
)

// Use `logged` everywhere; it satisfies llms.LLM.
resp, err := logged.GenerateContent(ctx, messages)
```

!!! note "Combining with resilience"
    These wrappers compose cleanly with the resilience helpers
    (`llms.NewResilientClient`, `llms.NewRateLimitedClient`,
    `llms.NewFallbackChain`) covered in the resilience guide. A common ordering
    puts retries/rate limiting nearest the provider and observability outside, so
    each retry attempt is traced and counted.
