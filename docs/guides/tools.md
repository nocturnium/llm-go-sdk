# Tool Calling & Agents

Tool calling (also called *function calling*) lets a model ask your program to
run a function and feed the result back into the conversation. The SDK supports
this at two levels:

- **Manual loop** — you define tools, pass them per call, read `resp.ToolCalls`,
  execute them yourself, append the results as `RoleTool` messages, and call the
  model again. Maximum control.
- **`llms.RunTools` agent loop** — a high-level helper that drives the whole
  read-execute-append-repeat cycle for you, given a `ToolRegistry` that maps tool
  names to Go handlers.

Both use the same building blocks, so it's worth understanding the manual loop
before reaching for `RunTools`.

## Defining a tool

A tool is created with `llms.NewFunctionTool`. The `parameters` argument is the
JSON Schema for the function's arguments — pass any Go value that marshals to the
schema you want (a `map[string]any` is the most common choice).

```go
package main

import (
	llms "github.com/nocturnium/llm-go-sdk/v4"
)

var weatherTool = llms.NewFunctionTool(
	"get_weather",
	"Get the current weather for a location",
	map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "The city and state, e.g. San Francisco, CA",
			},
			"unit": map[string]any{
				"type":        "string",
				"enum":        []string{"celsius", "fahrenheit"},
				"description": "Temperature unit",
			},
		},
		"required": []string{"location"},
	},
)
```

The returned `llms.Tool` is a small struct you can inspect or build by hand:

```go
type Tool struct {
	Type     ToolType            // always "function" for NewFunctionTool
	Function *FunctionDefinition
}

type FunctionDefinition struct {
	Name        string
	Description string
	Parameters  json.RawMessage
	Strict      bool
}
```

!!! tip "Generate the schema from a Go type"
    Instead of hand-writing the JSON Schema, you can derive it from a struct with
    `llms.SchemaFrom[T]()` (returns `json.RawMessage, error`) and pass the result
    as the `parameters` argument. See the [structured outputs guide](structured-outputs.md)
    for details on `SchemaFrom`.

## Passing tools to a call

Attach tools to any `GenerateContent` call with `llms.WithTools`. The model
either answers directly (in `resp.Content`) or asks to call one or more tools (in
`resp.ToolCalls`).

```go
resp, err := client.GenerateContent(ctx, messages,
	llms.WithTools([]llms.Tool{weatherTool}),
)
if err != nil {
	log.Fatal(err)
}

if resp.HasToolCalls() {
	for _, tc := range resp.ToolCalls {
		fmt.Printf("model wants to call %s with %s\n",
			tc.Function.Name, tc.Function.Arguments)
	}
}
```

Each `llms.ToolCall` carries:

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` | Unique id for this call — echo it back in your `RoleTool` message |
| `Type` | `llms.ToolType` | Always `"function"` today |
| `Function.Name` | `string` | The tool name the model chose |
| `Function.Arguments` | `string` | A JSON string of arguments — `json.Unmarshal` it |

Convenience accessors on `*llms.Response` help when you expect a specific call:

```go
resp.HasToolCalls()            // bool: did the model request any tool?
resp.ToolCall("call_abc123")   // *llms.ToolCall by id, or nil
resp.ToolCallByName("get_weather") // first call with that name, or nil
resp.ToolCallNames()           // []string of every requested tool name
```

### Controlling tool choice

By default the model decides whether to call a tool. Use the typed tool-choice
options to override that behaviour:

| Option | Effect |
|--------|--------|
| `llms.WithToolChoiceAuto()` | Model decides whether to call a tool (the default) |
| `llms.WithToolChoiceNone()` | Forbid tool calls; force a text answer |
| `llms.WithToolChoiceRequired()` | Force the model to call *some* tool |
| `llms.WithToolChoiceTool(name)` | Force the model to call the named tool |

```go
resp, err := client.GenerateContent(ctx, messages,
	llms.WithTools([]llms.Tool{weatherTool}),
	llms.WithToolChoiceTool("get_weather"), // must call get_weather
)
```

!!! note
    Not every provider supports every choice mode, and tool support can vary by
    model within a provider. Check `client.Capabilities()` (or the capability
    matrix in the package docs) before relying on tool calling for a given model.

## The manual tool loop

When the model returns tool calls, the protocol is:

1. Append the **assistant** message carrying the tool calls.
2. Execute each tool and append one **`RoleTool`** message per call, echoing the
   originating `ToolCallID`.
3. Call the model again so it can use the results.
4. Repeat until the model stops asking for tools (or you hit a safety cap).

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		log.Fatal(err)
	}

	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "What's the weather in Tokyo?"},
	}

	tools := []llms.Tool{weatherTool}

	for i := 0; i < 5; i++ { // safety cap to avoid an infinite loop
		resp, err := client.GenerateContent(ctx, messages, llms.WithTools(tools))
		if err != nil {
			log.Fatal(err)
		}

		if !resp.HasToolCalls() {
			fmt.Println("Assistant:", resp.Content)
			break
		}

		// 1. Append the assistant message that carries the tool calls.
		messages = append(messages, llms.Message{
			Role:      llms.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// 2. Execute each call and append a RoleTool result.
		for _, tc := range resp.ToolCalls {
			result := executeTool(tc.Function.Name, tc.Function.Arguments)

			messages = append(messages, llms.Message{
				Role:       llms.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,           // must match the call's ID
				Name:       tc.Function.Name,
			})
		}
		// 3. Loop: call the model again with the results in context.
	}
}

func executeTool(name, arguments string) string {
	switch name {
	case "get_weather":
		var args struct {
			Location string `json:"location"`
			Unit     string `json:"unit"`
		}
		_ = json.Unmarshal([]byte(arguments), &args)
		return fmt.Sprintf(`{"temperature": 22, "unit": "celsius", "condition": "sunny"}`)
	default:
		return `{"error": "unknown tool"}`
	}
}
```

### Building tool-result messages

You can construct the `RoleTool` message by hand as above, or use the helpers,
which JSON-encode the result and set `ToolCallID`/`Name` for you:

```go
// Any JSON-serializable value:
msg, err := llms.ToolResult(tc.ID, tc.Function.Name,
	map[string]any{"temperature": 72, "condition": "sunny"})

// A pre-formatted string:
msg := llms.ToolResultString(tc.ID, tc.Function.Name, `{"temperature":72}`)

// An error (some providers handle error results specially):
msg := llms.ToolResultError(tc.ID, tc.Function.Name, err)
```

`llms.ToolResult` returns an error only if `result` cannot be marshalled to JSON.

## The `RunTools` agent loop

The manual loop is repetitive: append assistant message, dispatch each call,
append results, call again. `llms.RunTools` does all of that for you. You supply
a `ToolRegistry` mapping tool names to Go handlers, and it runs the loop until the
model returns a final answer.

```go
func RunTools(
	ctx context.Context,
	llm LLM,
	messages []Message,
	registry *ToolRegistry,
	opts ...RunToolsOption,
) (*Response, []Message, error)
```

It returns the final `*llms.Response`, the **full transcript** (your original
messages plus every assistant, tool-call, and tool-result message that was
appended), and an error.

### Registering tools

Create a registry with `llms.NewToolRegistry()` and register each tool with its
handler. A handler has the signature `func(args json.RawMessage) (any, error)` —
whatever non-string value you return is JSON-encoded into the tool result; a
returned error is converted into a tool-result message so the model can react to
it rather than aborting the run.

```go
registry := llms.NewToolRegistry()

registry.Register(weatherTool, func(args json.RawMessage) (any, error) {
	var in struct {
		Location string `json:"location"`
		Unit     string `json:"unit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	return map[string]any{
		"location":    in.Location,
		"temperature": 22,
		"condition":   "sunny",
	}, nil
})
```

!!! tip "Typed handlers with `RegisterFunc`"
    To skip the manual `json.Unmarshal`, use the generic helper
    `llms.RegisterFunc[T]`, which parses arguments into `T` before calling your
    handler:

    ```go
    type WeatherArgs struct {
        Location string `json:"location"`
        Unit     string `json:"unit"`
    }

    llms.RegisterFunc(registry, weatherTool, func(in WeatherArgs) (any, error) {
        return map[string]any{"temperature": 22, "condition": "sunny"}, nil
    })
    ```

### Full runnable example

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		log.Fatal(err)
	}

	// 1. Define tools.
	weatherTool := llms.NewFunctionTool(
		"get_weather",
		"Get the current weather for a location",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{
					"type":        "string",
					"description": "City and state, e.g. San Francisco, CA",
				},
			},
			"required": []string{"location"},
		},
	)

	calculatorTool := llms.NewFunctionTool(
		"calculator",
		"Multiply two numbers",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "number"},
				"b": map[string]any{"type": "number"},
			},
			"required": []string{"a", "b"},
		},
	)

	// 2. Register handlers.
	registry := llms.NewToolRegistry()

	registry.Register(weatherTool, func(args json.RawMessage) (any, error) {
		var in struct {
			Location string `json:"location"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		return map[string]any{
			"location":    in.Location,
			"temperature": 22,
			"condition":   "sunny",
		}, nil
	})

	registry.Register(calculatorTool, func(args json.RawMessage) (any, error) {
		var in struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		return map[string]any{"result": in.A * in.B}, nil
	})

	// 3. Run the agent loop.
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a concise travel assistant."},
		{Role: llms.RoleUser, Content: "What's the weather in Paris, and what is 150 * 5?"},
	}

	resp, transcript, err := llms.RunTools(ctx, client, messages, registry,
		llms.WithMaxIterations(8),
		llms.WithToolConcurrency(4),
		llms.WithOnStep(func(iteration int, r *llms.Response) {
			fmt.Printf("[step %d] tool calls: %v\n", iteration, r.ToolCallNames())
		}),
	)
	if err != nil {
		if errors.Is(err, llms.ErrMaxIterations) {
			log.Printf("hit the iteration cap before finishing: %v", err)
		} else {
			log.Fatal(err)
		}
	}

	fmt.Println("\nFinal answer:", resp.Content)
	fmt.Printf("Transcript length: %d messages\n", len(transcript))
}
```

### How `RunTools` behaves

- **Tools are injected automatically.** `RunTools` calls `GenerateContent` with
  `WithTools(registry.Tools())` on every turn — you do not pass `WithTools`
  yourself. Forward per-turn options with `WithCallOptions`. `RunTools` prepends
  them before `WithTools(registry.Tools())`, so options like model, temperature,
  max tokens, reasoning, and response format are honored, while registry tools
  always take precedence for the tools field. Example:
  `llms.RunTools(ctx, client, messages, registry, llms.WithCallOptions(llms.WithModel("gpt-4o"), llms.WithTemperature(0)))`.
- **Concurrent, deterministic tool execution.** Within a single model turn, tool
  calls run concurrently (bounded by `WithToolConcurrency`, default 8), but the
  resulting `RoleTool` messages are appended in the same order as
  `resp.ToolCalls`, so the transcript is reproducible.
- **Errors become tool results.** If a handler returns an error — or dispatch
  fails because of a missing tool or malformed call — `RunTools` appends a
  tool-result message containing the error text instead of aborting, letting the
  model recover.
- **Context cancellation is respected.** If `ctx` is cancelled, `RunTools`
  returns promptly with `ctx.Err()`, the last response, and the transcript built
  so far. `RunTools` checks `ctx` before model calls and while
  scheduling/awaiting tool results, but a `ToolHandler` already running when
  cancellation occurs runs to completion — `ToolHandler` receives no context.
  Handlers that must abort on cancellation should capture a context via closure
  and check it themselves.

### `RunTools` options

| Option | Default | Description |
|--------|---------|-------------|
| `llms.WithMaxIterations(n)` | `10` | Max model-tool loop iterations. Values `<= 0` use the default. |
| `llms.WithToolConcurrency(n)` | `8` | Max tool calls executed concurrently per turn. Values `<= 0` use the default. |
| `llms.WithCallOptions(opts...)` | none | CallOptions applied to every model turn (model, temperature, max tokens, reasoning, response format, etc.). Applied before `WithTools(registry.Tools())`, so registry tools always take precedence for the tools field. |
| `llms.WithOnStep(fn)` | none | Callback `func(iteration int, resp *Response)` invoked once after each model response. `iteration` is 1-based. |

### Handling the iteration guard

If the model keeps asking for tools past `WithMaxIterations`, `RunTools` returns
the last response and transcript along with an error wrapping
`llms.ErrMaxIterations`. Detect it with `errors.Is`:

```go
resp, transcript, err := llms.RunTools(ctx, client, messages, registry,
	llms.WithMaxIterations(5),
)
if errors.Is(err, llms.ErrMaxIterations) {
	// The loop stopped early; resp and transcript are still usable.
	log.Printf("agent did not converge in 5 iterations")
}
```

## When to use which

!!! note "Manual loop vs. `RunTools`"
    Reach for **`RunTools`** for the common case: you have a set of self-contained
    tools and want the SDK to drive the loop, run tools concurrently, and produce
    a clean transcript. Drop down to the **manual loop** when you need to inspect
    or transform messages between turns, gate execution behind approval, apply
    different `CallOption`s per turn, or interleave streaming with tool calls.

## Driving a registry without the loop

`ToolRegistry` is also useful on its own, even outside `RunTools`. After a single
`GenerateContent` call you can execute every requested tool and get the
`RoleTool` messages back:

```go
resp, err := client.GenerateContent(ctx, messages,
	llms.WithTools(registry.Tools()))
if err != nil {
	log.Fatal(err)
}

// Execute all calls in resp and get one tool-result message per call.
toolMsgs, err := registry.HandleAll(resp)
if err != nil {
	log.Fatal(err)
}

messages = append(messages, llms.Message{
	Role:      llms.RoleAssistant,
	Content:   resp.Content,
	ToolCalls: resp.ToolCalls,
})
messages = append(messages, toolMsgs...)
// ...call the model again with the results.
```

`registry.Handle(tc)` does the same for a single `llms.ToolCall`, returning the
resulting `RoleTool` message.

## See also

- [Structured outputs](structured-outputs.md) — `GenerateTyped`, `SchemaFrom`, and JSON-schema-constrained responses.
- [Streaming](streaming.md) — incremental responses with `client.Stream`.
