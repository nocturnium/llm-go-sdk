# Google ADK Go

The `llmadk` module runs any llm-go-sdk provider as the model behind a
[Google ADK Go](https://github.com/google/adk-go) agent. ADK itself ships models
for Gemini and for OpenAI's Responses API; with `llmadk` an ADK agent can use
Anthropic, OpenAI chat completions, Mistral, Groq, OpenRouter, a local Ollama
server or any other provider in this SDK, wrapped in any of its middleware
(fallback, retries, rate limits, caching, cost tracking, tracing).

```bash
go get github.com/nocturnium/llm-go-sdk/llmadk
```

`llmadk` is a separate module so that ADK's dependencies and its Go version
requirement stay out of the core SDK. It is versioned on its own (`llmadk/v0.x`)
while ADK and genai are pre-1.0.

## Quick start

```go
claude, err := anthropic.New() // or any provider, or llms.New("openai", llms.Config{})
if err != nil {
    log.Fatal(err)
}
model, err := llmadk.NewModel(claude)
if err != nil {
    log.Fatal(err)
}
agent, err := llmagent.New(llmagent.Config{
    Name:        "assistant",
    Model:       model,
    Instruction: "You are a helpful assistant.",
    Tools:       []tool.Tool{weatherTool},
})
```

A complete program is in `llmadk/examples/agent`.

Tools registered in an `llms.ToolRegistry` can be handed to an ADK agent with
`llmadk.Tools(registry)`. Their handlers run inside ADK's tool flow, and an
error a handler returns reaches ADK's error callbacks.

## Options

| Option | Effect |
|---|---|
| `WithName(name)` | The model name ADK sees; defaults to the provider's model. A name starting `gemini-` makes ADK treat the model as Gemini. |
| `WithCallOptions(...)` | Call options applied to every request (a request's own settings win), such as `openai.WithReasoningRoundTrip()`. |
| `WithIgnore(fields...)` | Drop named genai fields instead of rejecting them, for configs written for Gemini (`FieldSafetySettings`, `FieldTopK`, ...). |
| `WithWebSearch()` | Map ADK's `GoogleSearch` tool onto the provider's web search (Z.AI today). |
| `WithCapabilityChecks()` | Reject requests for tools, images or a JSON schema from a provider that reports not supporting them. Off by default, because several providers report `false` for features that depend on the model. |
| `WithTimeout(d)` | Bound each model call; a request's `HTTPOptions.Timeout` wins. |

## How requests are translated

Every field of ADK's request is **translated**, **ignored** because it cannot
change the result, or **rejected** with an error naming it
(`ErrUnsupportedConfigField`, `ErrUnsupportedPart`, `ErrUnsupportedTool`).
Nothing is dropped silently, and a test fails whenever genai adds a field the
bridge has not classified.

- **Translated:** temperature, top-p, max output tokens, stop sequences,
  penalties, system instruction, function declarations (Gemini schemas are
  converted to JSON Schema), function-calling mode and allowed functions, response
  MIME type and schema, thinking config (level wins over budget; budget 0 turns
  thinking off, -1 lets the model decide), `HTTPOptions.Timeout`, and a
  per-request model name.
- **Ignored:** `Labels`, `HTTPOptions.Headers` (addressed to Google's backend),
  the output schemas of function declarations, `StreamFunctionCallArguments`
  (calls always arrive whole).
- **Rejected, can be ignored with `WithIgnore`:** top-k, seed, logprobs, safety
  settings, cached content, speech, image and routing configs, service tier,
  `VALIDATED` function calling, and Google Search without `WithWebSearch`.
- **Always rejected:** more than one candidate, non-text response modalities,
  Gemini's server tools (code execution, URL context, maps, file search,
  retrieval, computer use), and parts llm-go-sdk cannot carry: audio, video,
  PDFs, `gs://` or Gemini Files API URIs, executable code.
- **Content:** text, images (inline or `http(s)` URLs), and text-like inline
  data such as an artifact attached by ADK's `load_artifacts` tool.

## Behavior worth knowing

- **Tool-call IDs are unique across the session.** ADK pairs each call with its
  result by ID over the whole history, so an ID a provider repeats between turns
  (or omits) would attach one call's result to another. The bridge keeps a
  provider's ID only when it cannot collide and mints one otherwise.
- **Unanswered calls get a pending result.** ADK records no result for a
  long-running tool until it finishes, and OpenAI- and Anthropic-style APIs
  reject a call without one, so the bridge sends
  `{"status":"pending",...}` for it.
- **Structured output with tools.** ADK sends an output schema together with the
  agent's tools for non-Gemini models, which several providers cannot honor at
  once (Anthropic enforces a schema by replacing the tools). The bridge offers
  the schema as a `set_model_response` tool, as ADK does for Gemini, and turns the
  model's call to it into the final JSON answer. Text is not streamed as partial
  events in that mode.
- **Reasoning round-trips.** Anthropic thinking signatures, OpenAI encrypted
  reasoning and Gemini thought signatures are stored with the session and sent
  back to the provider that produced them; every provider drops reasoning another
  provider produced, so a fallback chain or a model switch does not fail on it.
  With thoughts hidden (no `ThinkingConfig`, or `IncludeThoughts` false) the
  thought text is still stored, base64-encoded in the part's metadata, because
  Anthropic needs it back with its signature: it is not shown as a thought, but
  it is present in session storage and in ADK's event stream.
- **Usage.** Token counts follow genai's accounting (cached tokens inside the
  prompt count, thought tokens outside the candidates), so ADK's history
  compaction sees the real prompt size. Anthropic does not report thinking
  tokens separately, so they count as candidates. Cost, when the provider reports
  it, is in `CustomMetadata["llmgo.cost_usd"]`.
- **Moving a session to ADK's own Gemini model.** Gemini's signatures are stored
  as Gemini issued them, other providers' reasoning goes in part metadata, and a
  call Gemini did not make carries Google's `skip_thought_signature_validator`
  placeholder, so an agent can switch from a bridged model to `gemini.NewModel`
  mid-session. On Vertex AI, ADK's Gemini model refuses part metadata, so a
  session holding another provider's reasoning cannot move to it there.
- **Gemini model names.** Gemini 3 pairs calls by ID and validates signatures.
  The SDK's Gemini provider detects Gemini 3 from a versioned name
  (`gemini-3.5-flash`) to decide whether to send call IDs; an alias such as
  `gemini-flash-latest` is treated as older, so pin a versioned model. (The
  bridge itself needs no detection: it marks every unsigned first call.)

## Compatibility matrix

**live** means verified against the provider's API by the release gate
(`make -C llmadk integration`) and replayed from its recording in CI;
**unit** means covered by tests with a scripted provider; **n/a** means the
provider has no such feature.

| Feature | OpenAI chat | OpenAI Responses | Anthropic | Gemini | OpenRouter | Other OpenAI-compatible |
|---|---|---|---|---|---|---|
| Text and tool loop | live | live | live (Haiku 4.5, Sonnet 5) | live | live | Z.AI (coding endpoint): live; others: unit |
| Streaming tool loop | live | live | live | live | live | Z.AI (coding endpoint): live; others: unit |
| Parallel calls to the same tool | live | live | live | live | live | unit |
| Same tool across turns, IDs stay paired | unit | unit | unit | unit | unit | unit |
| Reasoning round-trip with tools | n/a (chat refuses reasoning with tools) | live | live | live | n/a (no reasoning passback) | unit |
| Structured output with tools | live | unit | live | live | unit | unit |
| Switch into the provider mid tool loop | unit | unit | live (manual, adaptive, always-on thinking) | live (Gemini 3) | unit | unit |
| Session moved to ADK's Gemini model | unit | unit | live | live | unit | unit |
| Long-running tool left unanswered | unit | unit | unit | unit | unit | unit |
| Web search (`GoogleSearch`) | n/a | n/a | n/a | n/a | n/a | Z.AI: unit (request and non-streaming results only; streaming drops search results; not live: the coding endpoint does not run web search) |
| Images | unit | unit | unit | unit | unit | unit |

Mistral, Groq, Cerebras, DeepSeek and Perplexity run through the same
OpenAI-compatible path as the "Other" column; their provider-specific rules
(Mistral's nine-character tool-call IDs, for one) are covered by the core SDK's
tests, not by live runs.

The evidence for each live run is in `llmadk/testdata/live/`.
