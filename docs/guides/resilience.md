# Resilience

Network calls to LLM providers fail. APIs rate-limit you (HTTP 429), have transient
outages (HTTP 5xx), get overloaded, or simply time out. This guide covers the
opt-in resilience wrappers in `llms` that make your application robust against
these failures:

- **Retries with exponential backoff** — re-issue requests that hit transient errors.
- **Circuit breaker** — stop hammering a provider that is clearly down.
- **Client-side rate limiting** — pace your own requests to stay under provider quotas.
- **Fallback chains** — automatically switch to a backup provider/model when the
  primary fails, with time-based recovery.

!!! warning "Retries are OFF by default"
    The base provider clients (`openai.New(...)`, `anthropic.New(...)`, etc.) **do
    not retry** on their own. A single failed request returns its error immediately.
    Resilience is entirely opt-in: you get it by wrapping a client in one of the
    helpers below. Each wrapper implements the same `llms.LLM` interface, so it is a
    drop-in replacement for the client it wraps.

These helpers live in the `resilience` package; core types like `llms.Message`
remain in the root `llms` package:

```go
import (
	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/middleware/resilience"
)
```

---

## Retries: `NewResilientClient`

`resilience.NewResilientClient` wraps any `llms.LLM` with retry logic and a circuit
breaker. It satisfies `llms.LLM`, so you call `GenerateContent` / `Stream` on it
exactly as you would the underlying client.

```go
import (
	"context"
	"fmt"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/middleware/resilience"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/openai"
)

func main() {
	base, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		panic(err)
	}

	// Wrap with up to 3 retries (4 attempts total) on transient failures.
	client := resilience.NewResilientClient(base,
		resilience.WithMaxRetries(3),
	)

	resp, err := client.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "Hello!"},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(resp.Content)
}
```

### What gets retried

The default retry policy (`resilience.DefaultShouldRetry`) only retries errors that are
genuinely transient:

| Condition | Retried? |
|-----------|----------|
| HTTP 429 (rate limited) | yes |
| HTTP 500 / 502 / 503 / 504 (server errors) | yes |
| Other 4xx (e.g. 400, 401, 404) | no |
| `context.Canceled` / `context.DeadlineExceeded` | no |
| `resilience.ErrCircuitOpen` | no |

Retryable errors are detected via the typed `*llms.APIError` (which exposes
`StatusCode`, `Type`, `Code`, etc.). Client-side mistakes such as a bad API key or
an invalid request are returned immediately — retrying them would be pointless.

### Defaults and options

!!! note "ResilientClient defaults to retries enabled"
    Unlike the bare provider clients, a `ResilientClient` **does** retry by default.
    `NewResilientClient(base)` with no options uses `resilience.DefaultRetryConfig()`
    (3 attempts, 1s initial delay, 2x backoff) and a default circuit breaker.

`NewResilientClient` accepts these `resilience.ResilienceOption` values:

| Option | Effect |
|--------|--------|
| `WithMaxRetries(n int)` | Allow up to `n` retries (so `n+1` total attempts). |
| `WithRetryDelay(d time.Duration)` | Set the initial delay before the first retry. |
| `WithRetryConfig(cfg *RetryConfig)` | Replace the entire retry configuration. |
| `WithCircuitBreaker(cb *CircuitBreaker)` | Attach a custom circuit breaker. |
| `WithOnRetry(fn func(attempt int, err error, delay time.Duration))` | Callback invoked before each retry. |

The `WithOnRetry` callback is handy for logging or metrics:

```go
client := resilience.NewResilientClient(base,
	resilience.WithMaxRetries(5),
	resilience.WithRetryDelay(500*time.Millisecond),
	resilience.WithOnRetry(func(attempt int, err error, delay time.Duration) {
		fmt.Printf("retry #%d after %v (cause: %v)\n", attempt, delay, err)
	}),
)
```

### Tuning backoff with `RetryConfig`

For full control over backoff, build a `resilience.RetryConfig` and pass it via
`WithRetryConfig`. `resilience.DefaultRetryConfig()` returns a sensible baseline you can
copy and adjust:

```go
cfg := resilience.DefaultRetryConfig() // MaxAttempts 3, InitialDelay 1s,
                                        // MaxDelay 30s, BackoffFactor 2.0, Jitter 0.1
cfg.MaxAttempts = 5                     // total attempts, including the first
cfg.InitialDelay = 250 * time.Millisecond
cfg.MaxDelay = 10 * time.Second
cfg.BackoffFactor = 2.0                 // delay doubles each retry, capped at MaxDelay
cfg.Jitter = 0.2                        // ±20% randomization to avoid thundering herd

client := resilience.NewResilientClient(base, resilience.WithRetryConfig(cfg))
```

The fields of `RetryConfig` are:

| Field | Type | Meaning |
|-------|------|---------|
| `MaxAttempts` | `int` | Total attempts **including** the first call. Values `< 1` are clamped to 1, so at least one attempt always runs. |
| `InitialDelay` | `time.Duration` | Delay before the first retry. |
| `MaxDelay` | `time.Duration` | Upper bound on any single delay. |
| `BackoffFactor` | `float64` | Multiplier applied to the delay after each attempt. |
| `Jitter` | `float64` | Random jitter factor in `[0,1]` applied to each delay. |
| `ShouldRetry` | `func(error) bool` | Predicate deciding whether an error is retryable. |

When a provider returns a Retry-After value (surfaced on `*llms.APIError` as
`RetryAfter`), the retry waits at least that long — it overrides the computed
backoff when larger, still bounded by `MaxDelay`.

You can supply your own `ShouldRetry` to broaden or narrow what counts as
retryable. Note that the circuit breaker's notion of a "provider-health failure" is
determined internally (it counts 429/5xx responses and transport-level failures
such as connection refused, connection reset, EOF, and timeouts), independent of
any custom `ShouldRetry` predicate you install here.

!!! note "Streaming is not retried"
    Retries apply only to `Call`/`GenerateContent`. For `Stream`, the underlying
    client is invoked once: a failed open returns its error immediately (and
    records a breaker failure on transient errors), and a mid-stream failure is
    not retried. The breaker is still updated from the stream's final outcome.

---

## Circuit breaker: `NewCircuitBreaker`

A circuit breaker stops sending requests to a provider that is failing repeatedly,
giving it time to recover and protecting your app from piling up doomed calls. A
`ResilientClient` always has one (a default is created if you do not supply your
own).

The breaker has three states:

- **`CircuitClosed`** — normal operation; requests pass through.
- **`CircuitOpen`** — failure threshold exceeded; requests are rejected immediately
  with `resilience.ErrCircuitOpen` (this error is **not** retried).
- **`CircuitHalfOpen`** — after the reset timeout elapses, a limited number of probe
  requests are allowed through to test recovery. Enough successes close the circuit
  again; any failure re-opens it.

```go
cb := resilience.NewCircuitBreaker(
	resilience.WithMaxFailures(5),                  // open after 5 consecutive failures
	resilience.WithResetTimeout(30*time.Second),    // wait 30s before probing again
	resilience.WithOnStateChange(func(from, to resilience.CircuitState) {
		fmt.Printf("circuit: %s -> %s\n", from, to)
	}),
)

client := resilience.NewResilientClient(base,
	resilience.WithMaxRetries(3),
	resilience.WithCircuitBreaker(cb),
)
```

### Options and defaults

| Option | Default | Effect |
|--------|---------|--------|
| `WithMaxFailures(n int)` | 5 | Consecutive provider-health failures before opening. |
| `WithResetTimeout(d time.Duration)` | 30s | Time to stay open before allowing half-open probes. |
| `WithHalfOpenMax(n int)` | 3 | Probe requests allowed (and successes required to close) in half-open. |
| `WithOnStateChange(fn func(from, to CircuitState))` | — | Callback on every state transition. |

!!! note "Only transient failures trip the breaker"
    The breaker counts a failure toward opening **only** when the error indicates the
    provider itself is unhealthy (429/5xx responses, or transport-level failures
    such as connection refused, connection reset, EOF, and timeouts). Context cancellation, deadlines, other
    4xx errors, and `ErrCircuitOpen` do **not** trip it — so a burst of bad requests
    or canceled calls will not needlessly open the circuit on a healthy provider.

You can inspect or reset the breaker. `ResilientClient.CircuitBreaker()` returns the
breaker it is using:

```go
fmt.Println("state:", client.CircuitBreaker().State()) // closed / open / half-open
cb.Reset()                                              // force back to closed
```

---

## Client-side rate limiting: `NewRateLimitedClient`

Rather than waiting to be told you are over the limit (429), you can pace your own
requests with a token-bucket rate limiter. `resilience.NewRateLimitedClient` wraps an
`llms.LLM` and blocks (by default) until capacity is available.

```go
client := resilience.NewRateLimitedClient(base,
	resilience.WithRequestsPerMinute(60),     // cap at 60 requests/min
	resilience.WithTokensPerMinute(60_000),   // also cap at 60k tokens/min (optional)
)

resp, err := client.GenerateContent(ctx, messages)
```

### How token limiting works

- The **request** limiter is always active (default 60 req/min with a burst of 1,
  so requests are paced instead of releasing a full minute's quota immediately).
- The **token** limiter is only active when you set `WithTokensPerMinute(n)` with
  `n > 0`. Before each call it reserves an *estimate* of the tokens the request will
  consume (`WithTokenEstimate`, default 1000). After the call completes, the wrapper
  records the **actual** token usage from the response so the limiter self-corrects.

### Options and defaults

| Option | Default | Effect |
|--------|---------|--------|
| `WithRequestsPerMinute(n int)` | 60 | Max requests per minute. |
| `WithRequestBurst(n int)` | 1 | Requests allowed to burst at once. Default 1 paces requests instead of releasing a full minute's quota immediately. |
| `WithTokensPerMinute(n int)` | 0 (off) | Max tokens per minute; `0` disables token limiting. |
| `WithTokenBurst(n int)` | a full minute's budget (= TokensPerMinute) | Tokens allowed to burst at once; lower it for tighter client-side token pacing. |
| `WithTokenEstimate(n int)` | 1000 | Tokens reserved per request before the actual count is known. |
| `WithBlocking(b bool)` | `true` | If `true`, wait for capacity; if `false`, return an error immediately. |
| `WithWaitTimeout(d time.Duration)` | 30s | Max time to block when `WithBlocking(true)`. |

### Blocking vs. non-blocking

By default the limiter **blocks** until capacity frees up (or `WithWaitTimeout`
expires, yielding `resilience.ErrRateLimitTimeout`). Set `WithBlocking(false)` to fail
fast with `resilience.ErrRateLimitExceeded` instead:

```go
client := resilience.NewRateLimitedClient(base,
	resilience.WithRequestsPerMinute(30),
	resilience.WithBlocking(false), // return ErrRateLimitExceeded instead of waiting
)

resp, err := client.GenerateContent(ctx, messages)
if errors.Is(err, resilience.ErrRateLimitExceeded) {
	// shed load, queue for later, etc.
}
```

!!! tip "Provider defaults & shared limits"
    `resilience.NewProviderRateLimitedClient(base, ...)` seeds the limiter from
    conservative per-provider defaults (see `resilience.ProviderRateLimits`). To enforce a
    single quota across several clients, create one `*resilience.RateLimiter` and share it
    via `resilience.NewRateLimitedClientWithLimiter(base, limiter)`.

---

## Fallback chains: `NewFallbackChain`

A fallback chain tries multiple clients in order and returns the first success. Use
it to fail over from one provider/model to another when the primary is rate-limited,
overloaded, or down.

```go
import (
	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/middleware/resilience"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/openai"
)

primary, _ := openai.New(openai.WithModel("gpt-4o"))
backup, _ := anthropic.New(anthropic.WithModel("claude-sonnet-4-5"))

chain := resilience.NewFallbackChain([]llms.LLM{primary, backup},
	resilience.WithOnFallback(func(fromIdx, toIdx int, from, to llms.LLM, err error) {
		fmt.Printf("falling back from #%d (%s) to #%d (%s): %v\n",
			fromIdx, from.Provider(), toIdx, to.Provider(), err)
	}),
)

resp, err := chain.GenerateContent(ctx, messages)
```

### When does it fall back?

By default (the `DefaultFallbackSelector`), the chain advances to the next client on
errors that suggest the current provider is the problem:

- HTTP 429 (rate limited)
- HTTP 500 / 502 / 503 / 504 (server errors)
- HTTP 529 (overloaded — Anthropic)
- API error types `rate_limit_error`, `overloaded_error`, `server_error`
- Transport-level failures: connection refused, connection reset, EOF /
  unexpected EOF, and network timeouts (any net.Error)
- `resilience.ErrCircuitOpen` (the wrapped client's breaker is open)

On any **other** error (e.g. a 400 bad request), the chain stops and returns that
error rather than masking a real bug behind every backup. If all clients have been
tried and failed, it returns the last error encountered.

### Options

| Option | Default | Effect |
|--------|---------|--------|
| `WithOnFallback(fn func(fromIdx, toIdx int, from, to LLM, err error))` | — | Callback when advancing to the next client. |
| `WithOnSuccess(fn func(idx int, client LLM))` | — | Callback when a client succeeds. |
| `WithRecoveryAfter(d time.Duration)` | 30s | Cooldown before a failed client is probed again. |
| `WithFallbackSelector(s FallbackSelector)` | `DefaultFallbackSelector` | Customize which errors trigger fallback. |

Built-in selectors: `resilience.DefaultFallbackSelector{}` (the default, above),
`resilience.AlwaysFallbackSelector{}` (fall back on any error), and
`resilience.NeverFallbackSelector{}` (stop on the first error). Implement the
`resilience.FallbackSelector` interface for custom logic.

### Time-based recovery

The chain tracks per-client health. When a client fails, it is marked **unhealthy
for `WithRecoveryAfter`** (default 30s) and is skipped on subsequent calls — the
chain starts from the next healthy client, so you do not pay the latency of retrying
a known-bad provider on every request.

The recovery is "half-open" style:

- While unhealthy, a client is excluded from the candidate list.
- Once the `recoveryAfter` window elapses, the client becomes eligible again and is
  tried as a probe. A success marks it healthy again; a failure restarts its
  cooldown.
- **Failsafe:** if *every* client is currently in cooldown, the chain falls back to
  trying all clients anyway rather than returning "no clients available" — better a
  long shot than no shot.

```go
chain := resilience.NewFallbackChain([]llms.LLM{primary, backup},
	resilience.WithRecoveryAfter(60*time.Second), // keep a failed client out for a full minute
	resilience.WithOnFallback(func(fromIdx, toIdx int, from, to llms.LLM, err error) {
		log.Printf("primary unhealthy, using backup: %v", err)
	}),
	resilience.WithOnSuccess(func(idx int, client llms.LLM) {
		log.Printf("served by client #%d (%s)", idx, client.Provider())
	}),
)
```

You can also manage health manually: `chain.IsClientHealthy(idx)`,
`chain.SetClientHealthy(idx, healthy)`, and `chain.ResetHealth()`.

!!! tip "Weighted priorities"
    `resilience.NewWeightedFallbackChain(clients, weights, opts...)` builds a chain that
    tries higher-weighted clients first. It returns an error on invalid input; use
    `resilience.MustNewWeightedFallbackChain(...)` to panic instead.

---

## Composing the wrappers

Because every wrapper implements `llms.LLM`, you can stack them. A common,
production-grade arrangement is:

1. Rate-limit each provider client so you stay under its quota.
2. Make each one resilient (retries + circuit breaker) on transient errors.
3. Put the resilient clients behind a fallback chain for cross-provider failover.

```go
import (
	"context"
	"log"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/middleware/resilience"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/openai"
)

func buildClient() (llms.LLM, error) {
	openaiBase, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		return nil, err
	}
	anthropicBase, err := anthropic.New(anthropic.WithModel("claude-sonnet-4-5"))
	if err != nil {
		return nil, err
	}

	// 1. Rate-limit, then 2. make resilient — once per provider.
	primary := resilience.NewResilientClient(
		resilience.NewRateLimitedClient(openaiBase,
			resilience.WithRequestsPerMinute(60),
			resilience.WithTokensPerMinute(60_000),
		),
		resilience.WithMaxRetries(3),
		resilience.WithCircuitBreaker(resilience.NewCircuitBreaker(
			resilience.WithMaxFailures(5),
			resilience.WithResetTimeout(30*time.Second),
		)),
	)

	backup := resilience.NewResilientClient(
		resilience.NewRateLimitedClient(anthropicBase,
			resilience.WithRequestsPerMinute(60),
		),
		resilience.WithMaxRetries(2),
	)

	// 3. Fail over from primary to backup.
	return resilience.NewFallbackChain([]llms.LLM{primary, backup},
		resilience.WithRecoveryAfter(60*time.Second),
		resilience.WithOnFallback(func(fromIdx, toIdx int, from, to llms.LLM, err error) {
			log.Printf("fallback %s -> %s: %v", from.Provider(), to.Provider(), err)
		}),
	), nil
}

func main() {
	client, err := buildClient()
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "Summarize the theory of relativity in one sentence."},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(resp.Content)
}
```

!!! note "Order matters"
    Wrap rate limiting **inside** resilience so a retry still respects your local
    pacing, and put resilient clients **inside** the fallback chain so each provider
    exhausts its own retries before the chain fails over. The circuit breaker on each
    resilient client surfaces as `ErrCircuitOpen`, which the default fallback selector
    treats as a signal to move on to the next provider.

---

## Error reference

| Error | Source | Meaning |
|-------|--------|---------|
| `resilience.ErrCircuitOpen` | circuit breaker | Requests blocked while the circuit is open. |
| `resilience.ErrRateLimitExceeded` | rate limiter | Non-blocking limiter had no capacity. |
| `resilience.ErrRateLimitTimeout` | rate limiter | Blocking limiter exceeded `WithWaitTimeout`. |
| `resilience.ErrNoClientsAvailable` | fallback chain | The chain was constructed with no clients. |
| `resilience.ErrAllClientsFailed` | fallback chain | Sentinel for an exhausted chain. |

Provider HTTP errors are surfaced as `*llms.APIError`; use `errors.As` to inspect
`StatusCode`, `Type`, and `Code` when writing custom retry or fallback predicates.
