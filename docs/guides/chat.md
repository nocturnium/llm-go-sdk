# Chat & Messages

This guide covers the everyday request/response flow: building a slice of
messages, calling the model with `GenerateContent`, tuning the call with
options, and reading the `Response` back. Streaming and tool calling have their
own guides — here we focus on plain text generation and multi-turn conversation.

!!! info "Imports used on this page"
    ```go
    import (
        llms "github.com/nocturnium/llm-go-sdk/v4"
        "github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"
    )
    ```
    Every provider follows the same pattern; swap the `openai` import and
    constructor for any other provider and the rest of the code is identical.

---

## Messages

A conversation is an ordered `[]llms.Message`. Each message has a `Role` and
either simple text (`Content`) or multi-part content (`Parts`, used for vision —
see the vision guide).

```go
type Message struct {
    Role       Role          // who is speaking
    Content    string        // simple text content
    Parts      []ContentPart // multi-part content (text + images)
    ToolCalls  []ToolCall    // tool calls requested by the assistant
    ToolCallID string        // ID of the tool call a tool message answers
    Name       string        // tool name (for tool-role messages)
}
```

### Roles

There are four roles, each a typed `llms.Role` constant:

| Constant | Value | Use it for |
| --- | --- | --- |
| `llms.RoleSystem` | `system` | Instructions / persona. Must be **first** in the slice. |
| `llms.RoleUser` | `user` | Input from the human. |
| `llms.RoleAssistant` | `assistant` | The model's previous replies (replayed for context). |
| `llms.RoleTool` | `tool` | A tool-call result (requires `ToolCallID`). |

A minimal two-message conversation:

```go
messages := []llms.Message{
    {Role: llms.RoleSystem, Content: "You are a concise assistant. Answer in one sentence."},
    {Role: llms.RoleUser, Content: "What is the capital of France?"},
}
```

!!! note "Message validation rules"
    The SDK validates messages before sending. The slice must be non-empty,
    every message needs a role, a `system` message may appear only once and only
    at the start, every `tool` message needs a `ToolCallID`, and consecutive
    same-role messages (other than tool messages) are rejected by many provider
    APIs. By default the SDK **merges** consecutive same-role messages for you
    before validating; disable that with `llms.WithDisableMessageMerging()`.

### Reading text out of a message

Use `Message.Text()` to get the text regardless of whether it was stored in
`Content` or assembled from `Parts`:

```go
text := messages[1].Text() // "What is the capital of France?"
```

---

## GenerateContent

`GenerateContent` is the core call on every client. It takes a context, the
message slice, and zero or more `CallOption`s:

```go
GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error)
```

A complete example:

```go
client, err := openai.New(
    openai.WithModel("gpt-4o"),
    openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
)
if err != nil {
    log.Fatal(err)
}

messages := []llms.Message{
    {Role: llms.RoleSystem, Content: "You are a helpful assistant."},
    {Role: llms.RoleUser, Content: "Explain goroutines in two sentences."},
}

resp, err := client.GenerateContent(ctx, messages,
    llms.WithTemperature(0.7),
    llms.WithMaxTokens(512),
)
if err != nil {
    log.Fatal(err)
}

fmt.Println(resp.Content)
```

---

## Call options

Options are functional arguments passed after the messages. They override the
defaults baked into the client for this one call.

### Generation parameters

```go
resp, err := client.GenerateContent(ctx, messages,
    llms.WithTemperature(0.2),         // sampling temperature
    llms.WithMaxTokens(1024),          // output token cap
    llms.WithTopP(0.9),                // nucleus sampling
    llms.WithStopWords([]string{"\n\n"}), // stop sequences
    llms.WithModel("gpt-4o-mini"),     // override the client's default model
)
```

!!! tip "Temperature 0 is honored"
    `WithTemperature`, `WithMaxTokens`, and `WithTopP` store their values behind
    pointers, so an explicit `0` is sent to the provider verbatim rather than
    being treated as "unset". `llms.WithTemperature(0)` really requests
    deterministic-as-possible sampling; omitting the option entirely lets the
    model apply its own default.

### CallOptions reference

The options most relevant to chat generation. (Tool, JSON, streaming, and trace
options are covered in their respective guides.)

| Option | Signature | Effect |
| --- | --- | --- |
| `WithModel` | `WithModel(model string)` | Override the client's default model for this call. |
| `WithTemperature` | `WithTemperature(temp float64)` | Sampling temperature; `0` is honored. |
| `WithMaxTokens` | `WithMaxTokens(tokens int)` | Maximum output tokens. |
| `WithTopP` | `WithTopP(topP float64)` | Nucleus-sampling probability mass; `0` is honored. |
| `WithStopWords` | `WithStopWords(words []string)` | Stop sequences that end generation. |
| `WithFrequencyPenalty` | `WithFrequencyPenalty(p float64)` | Penalize repeated tokens (provider-dependent). |
| `WithPresencePenalty` | `WithPresencePenalty(p float64)` | Penalize tokens already present (provider-dependent). |
| `WithDisableMessageMerging` | `WithDisableMessageMerging()` | Do not merge consecutive same-role messages before sending. |
| `WithEstimateTokens` | `WithEstimateTokens()` | Estimate usage counts when the provider does not return them. |
| `WithTools` | `WithTools(tools []llms.Tool)` | Make tools available (see the tools guide). |
| `WithJSONMode` | `WithJSONMode()` | Request JSON output (see the structured-outputs guide). |
| `WithJSONSchema` | `WithJSONSchema(name string, schema json.RawMessage, strict bool)` | Constrain output to a JSON Schema. |
| `WithTrace` | `WithTrace(t llms.TraceOptions)` | Attach per-call trace context (see the observability guide). |

!!! warning "Provider support varies"
    Not every provider honors every parameter. Frequency/presence penalties, for
    example, are silently ignored by some backends, and parameters like
    `top_p` may behave differently per model. Check the capability matrix and
    `client.Capabilities()` when in doubt.

---

## The Response

`GenerateContent` returns a `*llms.Response`:

```go
type Response struct {
    Content       string            // the generated text
    Reasoning     *ReasoningContent // reasoning output; nil if unsupported
    FinishReason  FinishReason      // why generation stopped
    Usage         Usage             // token accounting
    ToolCalls     []ToolCall        // tool calls the model requested
    SearchResults []SearchResult    // web-search grounding results, if requested
}
```

`Thinking`/`ThinkingContent` is a deprecated alias retained for backward
compatibility; use `Reasoning`/`ReasoningContent` in new code.

### Content

The primary text output. For a plain chat call this is all you usually need:

```go
fmt.Println(resp.Content)
```

### FinishReason

A typed `llms.FinishReason` telling you why the model stopped. Compare against
the constants rather than raw strings:

| Constant | Value | Meaning |
| --- | --- | --- |
| `llms.FinishReasonStop` | `stop` | Normal completion (or a stop word was hit). |
| `llms.FinishReasonLength` | `length` | Hit the token / length limit — output is truncated. |
| `llms.FinishReasonToolCalls` | `tool_calls` | The model wants to call one or more tools. |
| `llms.FinishReasonContentFilter` | `content_filter` | Generation was halted by a content filter. |

```go
if resp.FinishReason == llms.FinishReasonLength {
    log.Println("output was truncated — consider raising WithMaxTokens")
}
```

### Usage

Token accounting for the call. Cache fields are populated only when the provider
reports prompt caching (for example Anthropic's `cache_control`).

```go
type Usage struct {
    PromptTokens        int
    CompletionTokens    int
    TotalTokens         int
    CacheReadTokens     int
    CacheCreationTokens int
    ReasoningTokens     int
}
```

```go
fmt.Printf("prompt=%d completion=%d total=%d\n",
    resp.Usage.PromptTokens,
    resp.Usage.CompletionTokens,
    resp.Usage.TotalTokens,
)
```

!!! note "When usage is missing"
    Some providers omit token counts. Pass `llms.WithEstimateTokens()` to have
    the SDK estimate them locally when the provider returns zeros.

### Reasoning

For reasoning-capable models (e.g. Z.AI GLM, OpenAI o-series, DeepSeek),
`Reasoning` carries the chain-of-thought separately from the answer. It is a
**pointer** and is `nil` when the provider does not surface reasoning, so always
nil-check it:

```go
if resp.Reasoning != nil {
    fmt.Println("reasoning:", resp.Reasoning.Content)
    fmt.Println("reasoning tokens:", resp.Reasoning.Tokens)
}
fmt.Println(resp.ReasoningText()) // safe empty string when no reasoning is present
```

`ReasoningContent` has `Content string`, `Tokens int`, `Signature string` (used
by Anthropic extended thinking), and a `Metadata map[string]any` for
provider-specific detail. `ThinkingContent` is a deprecated alias.

---

## The `llms.Call` helper

For the simplest case — one user prompt, just the text back —
`llms.Call` is a package-level helper (not a method) that wraps the prompt in a
single user message and returns `resp.Content`:

```go
answer, err := llms.Call(ctx, client, "Write a haiku about Go.")
if err != nil {
    log.Fatal(err)
}
fmt.Println(answer)
```

It accepts the same `CallOption`s as `GenerateContent`:

```go
answer, err := llms.Call(ctx, client, "Summarize the CAP theorem.",
    llms.WithTemperature(0),
    llms.WithMaxTokens(120),
)
```

!!! info "Signature"
    ```go
    func Call(ctx context.Context, llm llms.LLM, prompt string, options ...llms.CallOption) (string, error)
    ```
    Use it when you only need the final text. When you need the finish reason,
    usage, tool calls, or reasoning, call `GenerateContent` and read the full
    `Response`.

---

## Multi-turn conversations

There is no hidden server-side session: the client is stateless, so you carry
the conversation by appending each turn to the slice and sending the whole
history every time. Append the assistant's reply, then the next user message.

```go
ctx := context.Background()

client, err := openai.New(
    openai.WithModel("gpt-4o"),
    openai.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
)
if err != nil {
    log.Fatal(err)
}

// Seed the conversation.
messages := []llms.Message{
    {Role: llms.RoleSystem, Content: "You are a helpful travel assistant."},
    {Role: llms.RoleUser, Content: "I'm planning a trip to Japan in spring."},
}

// Turn 1.
resp, err := client.GenerateContent(ctx, messages)
if err != nil {
    log.Fatal(err)
}
fmt.Println("Assistant:", resp.Content)

// Record the reply, then ask a follow-up that depends on it.
messages = append(messages,
    llms.Message{Role: llms.RoleAssistant, Content: resp.Content},
    llms.Message{Role: llms.RoleUser, Content: "Which week has the best cherry blossoms?"},
)

// Turn 2 — the model now has the full context.
resp, err = client.GenerateContent(ctx, messages)
if err != nil {
    log.Fatal(err)
}
fmt.Println("Assistant:", resp.Content)
```

!!! tip "Keep the history tidy"
    - Append the assistant turn from `resp.Content` (or the full message,
      including any `resp.ToolCalls`, when using tools).
    - Keep exactly one system message at the front.
    - Long histories cost tokens on every turn — trim or summarize old turns to
      stay within the model's context window and your budget.

---

## Inspecting the client

Three accessors let you introspect a client at runtime — handy for logging,
routing, and capability checks:

```go
fmt.Println(client.Provider())     // e.g. "openai"
fmt.Println(client.Model())        // the configured default model
caps := client.Capabilities()      // llms.Capabilities{Streaming, Tools, Vision, ...}
if caps.Tools {
    // safe to pass WithTools on this client
}
```

---

## Error handling

`GenerateContent` returns a typed error on failure. Use `errors.As` to inspect
HTTP-level details such as the status code:

```go
resp, err := client.GenerateContent(ctx, messages)
if err != nil {
    var apiErr *llms.APIError
    if errors.As(err, &apiErr) {
        log.Printf("API error %d: %s", apiErr.StatusCode, apiErr.Message)
    } else {
        log.Printf("request failed: %v", err)
    }
    return
}
```

!!! note "Providers do not retry by default"
    A transient `429` or `5xx` surfaces immediately as an error. To add
    automatic retries, backoff, circuit breaking, or fallback, wrap the client
    with the resilience helpers — see the resilience guide.
