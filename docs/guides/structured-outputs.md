# Structured Outputs

Structured outputs let you constrain a model's response to valid JSON — either
free-form JSON (`json_object` mode) or JSON that conforms to a specific JSON
Schema (`json_schema` mode). The SDK builds on these primitives with a generic
helper, `GenerateTyped[T]`, that derives a schema from a Go struct, requests
strict JSON, and unmarshals the result directly into a typed value.

All of the building blocks live in the root `llms` package:

```go
import (
    llms "github.com/nocturnium/llm-go-sdk/v2"
    "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
)
```

## The four pieces

| API | What it does |
| --- | --- |
| `llms.WithJSONMode()` | Call option requesting a JSON object (no schema). |
| `llms.WithJSONSchema(name, schema, strict)` | Call option requesting JSON constrained by a JSON Schema. |
| `llms.SchemaFrom[T]()` | Derives a `json.RawMessage` JSON Schema from a Go struct via reflection. |
| `llms.GenerateTyped[T](ctx, client, messages, opts...)` | Generates, constrains, and unmarshals into a typed `T` in one call. |

## `WithJSONMode()` — JSON without a schema

`WithJSONMode()` asks the provider to emit a syntactically valid JSON object,
but does not enforce any particular shape. The model decides the keys. Use this
when you want JSON but the structure is loose or you will validate it yourself.

```go
resp, err := client.GenerateContent(ctx, messages, llms.WithJSONMode())
if err != nil {
    return err
}

var data map[string]any
if err := json.Unmarshal([]byte(resp.Content), &data); err != nil {
    return fmt.Errorf("model did not return valid JSON: %w", err)
}
```

!!! tip "Prompt the model to produce JSON"
    JSON object mode guarantees *syntactically* valid JSON, but most providers
    still expect your prompt to describe the desired fields. Spell out the
    structure you want in the system or user message.

Under the hood this sets `ResponseFormat.Type` to `llms.ResponseFormatJSONObject`
(the wire value `"json_object"`).

## `WithJSONSchema()` — JSON constrained by a schema

`WithJSONSchema(name string, schema json.RawMessage, strict bool)` requests
output that conforms to an explicit JSON Schema. The `name` labels the schema,
`schema` is the raw JSON Schema document, and `strict` requests strict
conformance where the provider supports it.

```go
schema := json.RawMessage(`{
  "type": "object",
  "properties": {
    "sentiment": {"type": "string"},
    "confidence": {"type": "number"}
  },
  "required": ["sentiment", "confidence"]
}`)

resp, err := client.GenerateContent(ctx, messages,
    llms.WithJSONSchema("Sentiment", schema, true),
)
if err != nil {
    return err
}

var out struct {
    Sentiment  string  `json:"sentiment"`
    Confidence float64 `json:"confidence"`
}
if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
    return err
}
```

This sets `ResponseFormat.Type` to `llms.ResponseFormatJSONSchema` (wire value
`"json_schema"`).

## `SchemaFrom[T]()` — derive a schema from a struct

Writing JSON Schema by hand is tedious. `SchemaFrom[T]()` generates one from a
Go struct using reflection, so your struct stays the single source of truth.

```go
type Person struct {
    Name  string `json:"name"`
    Age   int    `json:"age"`
    Email string `json:"email,omitempty"`
}

schema, err := llms.SchemaFrom[Person]()
if err != nil {
    return err
}
// schema is a json.RawMessage you can pass to WithJSONSchema.
```

Reflection rules:

- Field names come from the `json` tag; fields without a tag use the Go field name.
- Fields tagged `json:"-"` are skipped.
- Unexported fields are skipped.
- Every non-skipped field is marked **required**, and every object emits
  `additionalProperties: false`, so the schema is OpenAI strict-compatible.
  `omitempty` and pointer fields are NOT treated as optional. If you need
  optional fields, supply a hand-authored schema via `WithJSONSchema`.
- Go kinds map to JSON Schema types: integers → `integer`, floats → `number`,
  `bool` → `boolean`, `string` → `string`, slices/arrays → `array`, maps →
  `object` with `additionalProperties`, nested structs → nested `object`.
- `time.Time` → `string` (format `date-time`); `[]byte` → `string`; types
  implementing `json.Marshaler`/`encoding.TextMarshaler` → `string`. Types with
  no closed shape — `json.RawMessage`, `interface{}`, and maps with arbitrary
  values — map to an unconstrained `{}`, which OpenAI strict validators reject.
  For structs containing such fields, supply a hand-authored schema via
  `WithJSONSchema` instead of the auto-strict `GenerateTyped` path.

For `Person` above, all three of `name`, `age`, and `email` are marked required
(`omitempty` no longer makes a field optional), and the object schema gets
`additionalProperties: false`. To make `email` truly optional, hand-author a
schema and pass it via `WithJSONSchema`.

!!! note "Schemas are cached"
    `SchemaFrom[T]()` caches the generated schema per type and returns a copy on
    each call, so repeated use is cheap.

## `GenerateTyped[T]()` — the one-call path

`GenerateTyped[T]` ties the pieces together. It:

1. Checks whether you already supplied a `json_schema` response format. If not,
   it calls `SchemaFrom[T]()` and applies `WithJSONSchema(...)` automatically
   (with `strict` set to `true` and the schema name derived from the type name).
2. Calls `GenerateContent`.
3. Unmarshals `Response.Content` into a `T`.

It returns the typed value, the raw `*llms.Response` (for usage, finish reason,
etc.), and an error.

```go
func GenerateTyped[T any](
    ctx context.Context,
    llm LLM,
    messages []Message,
    opts ...CallOption,
) (T, *Response, error)
```

### Full runnable example

```go
package main

import (
    "context"
    "fmt"
    "log"

    llms "github.com/nocturnium/llm-go-sdk/v2"
    "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
)

// Recipe is both the target type and the source of the JSON Schema.
type Recipe struct {
    Name        string   `json:"name"`
    PrepMinutes int      `json:"prep_minutes"`
    Ingredients []string `json:"ingredients"`
    Vegetarian  bool     `json:"vegetarian"`
}

func main() {
    ctx := context.Background()

    // Reads OPENAI_API_KEY from the environment.
    client, err := openai.New(openai.WithModel("gpt-4o"))
    if err != nil {
        log.Fatal(err)
    }

    messages := []llms.Message{
        {Role: llms.RoleSystem, Content: "Extract structured recipe data from the user's request."},
        {Role: llms.RoleUser, Content: "A quick tomato pasta with garlic, basil, and olive oil."},
    }

    // The schema is derived from Recipe, sent as a strict json_schema response
    // format, and the JSON response is decoded straight into the struct.
    recipe, resp, err := llms.GenerateTyped[Recipe](ctx, client, messages,
        llms.WithTemperature(0), // 0 is honored — deterministic extraction
    )
    if err != nil {
        log.Fatalf("structured generation failed: %v", err)
    }

    fmt.Printf("Recipe: %s\n", recipe.Name)
    fmt.Printf("Prep:   %d min\n", recipe.PrepMinutes)
    fmt.Printf("Veg:    %v\n", recipe.Vegetarian)
    fmt.Printf("Items:  %v\n", recipe.Ingredients)
    fmt.Printf("Tokens: %d\n", resp.Usage.TotalTokens)
}
```

### Bring your own schema

If you pass a `json_schema` response format yourself, `GenerateTyped` uses it
verbatim instead of deriving one from `T` — useful when you want a hand-tuned
schema (descriptions, enums, constraints) but still want typed decoding:

```go
schema := json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "prep_minutes": {"type": "integer", "minimum": 0},
    "ingredients": {"type": "array", "items": {"type": "string"}},
    "vegetarian": {"type": "boolean"}
  },
  "required": ["name", "prep_minutes", "ingredients", "vegetarian"],
  "additionalProperties": false
}`)

recipe, _, err := llms.GenerateTyped[Recipe](ctx, client, messages,
    llms.WithJSONSchema("Recipe", schema, true),
)
```

### Error handling

`GenerateTyped` returns a descriptive error when the model's output is not valid
JSON for `T`:

```go
recipe, resp, err := llms.GenerateTyped[Recipe](ctx, client, messages)
if err != nil {
    // err wraps the underlying provider error, a nil response, or
    // "structured output is not valid JSON" with the json.Unmarshal cause.
    log.Fatal(err)
}
_ = resp // resp is non-nil whenever the provider returned a response
```

## Provider support

The SDK targets one structured-output contract across 18 providers, but the
mechanism differs by provider:

| Provider family | Mechanism |
| --- | --- |
| OpenAI-compatible (openai, azure, groq, deepseek, mistral, fireworks, togetherai, and others built on `pkg/openaicompat`) | Native `response_format` with `json_object` and `json_schema` (including `strict`). |
| Gemini (native) | Maps `json_schema` to `responseSchema` + `responseMimeType: application/json`; `json_object` maps to `responseMimeType` only. |
| Anthropic (native) | No `response_format` field. For `json_schema` the SDK **forces a single tool** whose `input_schema` is your schema, sets `tool_choice` to that tool, and exposes the tool input as `Response.Content`. |

Because of these differences, prefer `WithJSONSchema` / `GenerateTyped` over
`WithJSONMode` when you need a guaranteed shape: the schema path has a defined
behavior on every provider, including the Anthropic forced-tool fallback.

!!! warning "JSON object mode is not universal"
    Bare `WithJSONMode()` (no schema) is best-effort and is not meaningfully
    enforced by every backend. For Anthropic in particular, structured output is
    driven by the schema path. When in doubt, supply a schema.

### Checking support at runtime

Providers advertise structured-output support through the `JSONMode` capability
flag:

```go
if client.Capabilities().JSONMode {
    // Safe to request JSON output.
}
```

You can also use the package-level `llms.HasCapability` helper, which takes a
predicate over `llms.Capabilities`:

```go
if llms.HasCapability(client, func(c llms.Capabilities) bool { return c.JSONMode }) {
    // ...
}
```

## Tips

- **Use `WithTemperature(0)`** for extraction and classification tasks — a value
  of `0` is honored by the SDK and yields more deterministic structured output.
- `SchemaFrom`/`GenerateTyped` emit a strict schema in which every field is
  required. There is no struct-tag way to mark a field optional; if you need
  optional fields, author the schema by hand and pass it with `WithJSONSchema`.
- **Inspect `resp.FinishReason`** (typed `llms.FinishReason`) — a value of
  `llms.FinishReasonLength` means the JSON was likely truncated; raise
  `WithMaxTokens`.
- **Keep your struct authoritative.** Deriving the schema from `T` with
  `GenerateTyped` keeps the prompt contract and the decode target in sync.

## See also

- [Tools and Function Calling](tools.md) for the agent loop (`llms.RunTools`) —
  structured outputs and tool calling are complementary.
- The `llms.Response` type for usage, finish reason, and tool-call accessors.
