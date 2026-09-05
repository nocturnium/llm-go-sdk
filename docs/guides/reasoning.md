# Reasoning

Modern "reasoning" models (OpenAI o-series and GPT‑5, Anthropic extended
thinking, Gemini 2.5, Z.AI GLM, DeepSeek, Qwen) spend extra tokens on an internal
chain of thought before answering. The SDK exposes one cross-provider surface to
**request** reasoning and **read** it back, mapping onto each provider's native
parameter for you.

## Requesting reasoning

Use one of the call options:

```go
// Qualitative effort — maps to OpenAI reasoning_effort; providers that only take
// a token budget (Anthropic, Gemini) derive one automatically.
resp, err := client.GenerateContent(ctx, messages,
    llms.WithReasoningEffort(llms.ReasoningEffortHigh),
)

// Explicit thinking-token budget — honored by Anthropic and Gemini.
resp, err := client.GenerateContent(ctx, messages,
    llms.WithReasoningBudget(8192),
)

// Full control.
resp, err := client.GenerateContent(ctx, messages,
    llms.WithReasoning(llms.ReasoningConfig{
        Effort:       llms.ReasoningEffortMedium,
        BudgetTokens: 8192,
    }),
)
```

Effort levels are `ReasoningEffortMinimal`, `ReasoningEffortLow`,
`ReasoningEffortMedium`, and `ReasoningEffortHigh`. When a provider needs a token
budget but you only supplied an effort, the SDK derives one via
`llms.ReasoningBudgetForEffort`.

Anthropic's thinking request shape depends on the model generation, and the SDK
picks the right one from the model name:

- **Claude 4.5 and earlier** (Opus 4.1/4.5, Sonnet 4/4.5, Haiku 4.5, 3.x): sends
  `thinking: {type: "enabled", budget_tokens}`. The budget must be at least 1024
  tokens and `max_tokens` must exceed it (the SDK raises `max_tokens`
  automatically); `temperature`/`top_p` are omitted while thinking is on.
- **Opus 4.6 / Sonnet 4.6, Opus 4.7 / 4.8, Opus 5, Sonnet 5**: sends
  `thinking: {type: "adaptive"}` and maps the effort onto
  `output_config.effort` (`minimal` becomes `low`). `BudgetTokens` is ignored;
  the API rejects `budget_tokens` on 4.7 and later. An explicit
  `Enabled: false` sends `thinking: {type: "disabled"}`.
- **Fable / Mythos**: thinking is always on and the parameter is omitted; only
  `output_config.effort` is sent. `Enabled: false` is ignored.

Two Anthropic-only adjustments are applied silently on budget-based models
(Claude 4.5 and earlier) to keep requests valid: a forcing `tool_choice`
(required or a named tool) is softened to `auto` while thinking is on, and a
JSON-schema / JSON-mode request disables thinking, because those models reject
thinking alongside a forced tool. Adaptive thinking on 4.6 and later accepts a
forcing `tool_choice`, so nothing is changed there.

## Reading reasoning output

Reasoning is surfaced on the response, separate from the answer:

```go
if r := resp.ReasoningText(); r != "" {
    fmt.Println("reasoning:", r)
}
fmt.Println("answer:", resp.Content)

// Tokens spent reasoning (a subset of completion tokens), when reported.
fmt.Println("reasoning tokens:", resp.Usage.ReasoningTokens)
```

`resp.Reasoning` is a `*llms.ReasoningContent` (`nil` when the model produced no
reasoning) with `Content`, `Tokens`, and — for Anthropic — a `Signature` that
authenticates the thinking block for multi-turn use.

## Streaming

Reasoning streams independently of answer content, so you can render a
"thinking…" view as it arrives:

```go
stream, err := client.Stream(ctx, messages, llms.WithReasoningEffort(llms.ReasoningEffortHigh))
if err != nil {
    return err
}
for chunk := range stream {
    switch {
    case chunk.Reasoning != nil:
        fmt.Print(chunk.Reasoning.Content) // reasoning delta
    case chunk.Content != "":
        fmt.Print(chunk.Content) // answer delta
    }
}
```

## Capability detection

```go
caps := client.Capabilities()
if caps.Reasoning {
    // model supports reasoning controls
}
```

## Migration

The old `WithThinkingMode(bool)` option, the `Response.Thinking()` /
`StreamChunk.Thinking()` methods, and the `ThinkingContent` type alias were
**removed in v5**. Use `WithReasoning` (or `WithReasoningEffort` /
`WithReasoningBudget`), the `Reasoning` field, and `ReasoningContent`.
