package llmadk

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// Tools exposes every tool in reg to ADK. Each handler runs inside ADK's tool
// flow: an error it returns is reported as a tool error (ADK's error callbacks
// see it), a map result is the tool's response, and any other result is
// wrapped as {"result": value}.
//
// ADK validates a call's arguments against the tool's schema before the handler
// runs, which is stricter than llms.RunTools, which hands the raw arguments to
// the handler. A schema that jsonschema-go cannot resolve is reported here,
// naming the tool.
func Tools(reg *llms.ToolRegistry) ([]tool.Tool, error) {
	if reg == nil {
		return nil, nil
	}
	defs := reg.Tools()
	out := make([]tool.Tool, 0, len(defs))
	for _, def := range defs {
		if def.Function == nil {
			continue
		}
		name := def.Function.Name
		handler, ok := reg.Handler(name)
		if !ok {
			return nil, fmt.Errorf("llmadk: tool %q has no handler", name)
		}
		schema, err := inputSchema(def.Function.Parameters)
		if err != nil {
			return nil, fmt.Errorf("llmadk: tool %q schema: %w", name, err)
		}
		t, err := functiontool.New(functiontool.Config{
			Name:        name,
			Description: def.Function.Description,
			InputSchema: schema,
		}, bridgeHandler(handler))
		if err != nil {
			return nil, fmt.Errorf("llmadk: tool %q: %w", name, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// inputSchema parses a tool's JSON Schema and checks that it resolves, so a
// broken schema fails when the tools are built rather than on the first call.
func inputSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	schema := &jsonschema.Schema{Type: "object"}
	if len(raw) > 0 && string(raw) != jsonNull {
		if err := json.Unmarshal(raw, schema); err != nil {
			return nil, err
		}
	}
	if _, err := schema.Resolve(nil); err != nil {
		return nil, err
	}
	return schema, nil
}

// bridgeHandler adapts an llms.ToolHandler to a functiontool handler.
func bridgeHandler(h llms.ToolHandler) functiontool.Func[map[string]any, map[string]any] {
	return func(ctx agent.Context, args map[string]any) (map[string]any, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("llmadk: tool arguments: %w", err)
		}
		result, err := h(ctx, raw)
		if err != nil {
			return nil, err
		}
		if m, ok := result.(map[string]any); ok {
			return m, nil
		}
		return map[string]any{"result": result}, nil
	}
}
