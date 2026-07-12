package llms

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ToolType describes the kind of tool exposed to a model.
type ToolType string

const (
	// ToolTypeFunction identifies a function-calling tool.
	ToolTypeFunction ToolType = "function"
)

// Tool represents a tool that can be called by the model
type Tool struct {
	Type     ToolType            `json:"type"`
	Function *FunctionDefinition `json:"function,omitempty"`
}

// FunctionDefinition defines a function that can be called by the model
type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}

// ToolCall represents a tool call requested by the model
type ToolCall struct {
	ID       string        `json:"id"`
	Type     ToolType      `json:"type"`
	Function *FunctionCall `json:"function,omitempty"`
	// Signature is an opaque, provider-issued token that authenticates the
	// reasoning that produced this tool call. Gemini 2.5+ returns a
	// thoughtSignature on the functionCall part when thinking is enabled; it
	// must be echoed back on the following turn's tool-call part or the model
	// loses its reasoning context (and can reject the request). Empty for
	// providers that do not issue per-call signatures. This is the tool-call
	// analog of ReasoningContent.Signature.
	Signature string `json:"signature,omitempty"`
}

// FunctionCall contains the function name and arguments
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolChoiceType specifies the model's tool selection behavior.
type ToolChoiceType string

const (
	// ToolChoiceAuto lets the model decide whether to call tools.
	ToolChoiceAuto ToolChoiceType = "auto"
	// ToolChoiceNone prevents the model from calling tools.
	ToolChoiceNone ToolChoiceType = "none"
	// ToolChoiceRequired forces the model to call a tool.
	ToolChoiceRequired ToolChoiceType = "required"
)

// ToolChoice specifies which tool the model should use.
type ToolChoice struct {
	Type     ToolChoiceType     `json:"type"`
	Function *FunctionReference `json:"function,omitempty"`
}

// FunctionReference references a specific function by name
type FunctionReference struct {
	Name string `json:"name"`
}

// NewFunctionTool creates a new function tool with the given definition
func NewFunctionTool(name, description string, parameters any) Tool {
	return Tool{
		Type: ToolTypeFunction,
		Function: &FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  marshalFunctionParameters(parameters),
		},
	}
}

func marshalFunctionParameters(parameters any) json.RawMessage {
	if parameters == nil {
		return nil
	}
	switch p := parameters.(type) {
	case json.RawMessage:
		return append(json.RawMessage(nil), p...)
	case []byte:
		return append(json.RawMessage(nil), p...)
	default:
		data, err := json.Marshal(parameters)
		if err != nil {
			return nil
		}
		return data
	}
}

// ParseToolArguments parses the JSON arguments from a tool call into the specified type.
// Returns an error if the tool call has no function or if JSON parsing fails.
//
// Example:
//
//	type WeatherArgs struct {
//	    Location string `json:"location"`
//	    Unit     string `json:"unit"`
//	}
//
//	args, err := llms.ParseToolArguments[WeatherArgs](resp.ToolCalls[0])
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Location: %s\n", args.Location)
func ParseToolArguments[T any](tc ToolCall) (T, error) {
	var result T

	if tc.Function == nil {
		return result, fmt.Errorf("%w: tool call has no function", ErrToolCallFailed)
	}

	if tc.Function.Arguments == "" {
		return result, fmt.Errorf("%w: tool call has empty arguments", ErrToolCallFailed)
	}

	if err := json.Unmarshal([]byte(tc.Function.Arguments), &result); err != nil {
		return result, fmt.Errorf("%w: failed to parse arguments: %w", ErrInvalidToolResponse, err)
	}

	return result, nil
}

// ParseToolArgumentsMap parses tool call arguments into a map.
// Useful when the argument structure is not known at compile time.
func ParseToolArgumentsMap(tc ToolCall) (map[string]any, error) {
	return ParseToolArguments[map[string]any](tc)
}

// ToolResult creates a tool message with the result of a tool call.
// The result can be any JSON-serializable value.
//
// Example:
//
//	result := map[string]any{"temperature": 72, "condition": "sunny"}
//	msg, err := llms.ToolResult(toolCall.ID, toolCall.Function.Name, result)
func ToolResult(toolCallID, toolName string, result any) (Message, error) {
	var content string

	switch v := result.(type) {
	case string:
		content = v
	case []byte:
		content = string(v)
	default:
		data, err := json.Marshal(result)
		if err != nil {
			return Message{}, fmt.Errorf("%w: failed to marshal result: %w", ErrInvalidToolResponse, err)
		}
		content = string(data)
	}

	return Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
		Name:       toolName,
	}, nil
}

// ToolResultString creates a tool message with a string result.
func ToolResultString(toolCallID, toolName, result string) Message {
	return Message{
		Role:       RoleTool,
		Content:    result,
		ToolCallID: toolCallID,
		Name:       toolName,
	}
}

// ToolResultError creates a tool message indicating an error occurred.
// Some providers handle error results differently from success results.
func ToolResultError(toolCallID, toolName string, err error) Message {
	return Message{
		Role:       RoleTool,
		Content:    fmt.Sprintf("Error: %v", err),
		ToolCallID: toolCallID,
		Name:       toolName,
	}
}

// HasToolCalls returns true if the response contains any tool calls.
func (r *Response) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// ToolCall returns the tool call with the given ID, or nil if not found.
func (r *Response) ToolCall(id string) *ToolCall {
	for i := range r.ToolCalls {
		if r.ToolCalls[i].ID == id {
			return &r.ToolCalls[i]
		}
	}
	return nil
}

// ToolCallByName returns the first tool call with the given function name, or nil if not found.
func (r *Response) ToolCallByName(name string) *ToolCall {
	for i := range r.ToolCalls {
		if r.ToolCalls[i].Function != nil && r.ToolCalls[i].Function.Name == name {
			return &r.ToolCalls[i]
		}
	}
	return nil
}

// ToolCallNames returns the names of all tool calls in the response.
func (r *Response) ToolCallNames() []string {
	names := make([]string, 0, len(r.ToolCalls))
	for _, tc := range r.ToolCalls {
		if tc.Function != nil {
			names = append(names, tc.Function.Name)
		}
	}
	return names
}

// ToolHandler is a function that handles a tool call and returns a result.
// The result will be JSON-encoded if it's not already a string.
type ToolHandler func(ctx context.Context, args json.RawMessage) (any, error)

// ToolRegistry manages a collection of tool handlers.
//
// Thread-safety: All methods are safe for concurrent use. The registry uses
// an RWMutex to protect the handlers map and tools slice, allowing concurrent
// reads (Handle, Tools) while serializing writes (Register).
type ToolRegistry struct {
	mu       sync.RWMutex
	handlers map[string]ToolHandler
	tools    []Tool
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		handlers: make(map[string]ToolHandler),
		tools:    make([]Tool, 0),
	}
}

// Register adds a tool and its handler to the registry.
func (r *ToolRegistry) Register(tool Tool, handler ToolHandler) {
	if tool.Function == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[tool.Function.Name] = handler
	r.tools = append(r.tools, tool)
}

// RegisterFunc is a convenience method to register a tool with a typed handler.
// The handler function receives parsed arguments and returns a result.
//
// Example:
//
//	registry.RegisterFunc(weatherTool, func(ctx context.Context, args WeatherArgs) (any, error) {
//	    return map[string]any{"temperature": 72}, nil
//	})
func RegisterFunc[T any](r *ToolRegistry, tool Tool, handler func(context.Context, T) (any, error)) {
	if tool.Function == nil {
		return
	}

	wrappedHandler := func(ctx context.Context, args json.RawMessage) (any, error) {
		var parsed T
		if err := json.Unmarshal(args, &parsed); err != nil {
			return nil, fmt.Errorf("failed to parse arguments: %w", err)
		}
		return handler(ctx, parsed)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[tool.Function.Name] = wrappedHandler
	r.tools = append(r.tools, tool)
}

// Tools returns a copy of all registered tools for use with WithTools.
// A copy is returned to prevent external modification.
func (r *ToolRegistry) Tools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, len(r.tools))
	copy(result, r.tools)
	return result
}

// Handle executes the appropriate handler for a tool call and returns a tool message.
func (r *ToolRegistry) Handle(ctx context.Context, tc ToolCall) (Message, error) {
	if tc.Function == nil {
		return Message{}, fmt.Errorf("%w: tool call has no function", ErrToolCallFailed)
	}

	// Look up handler under read lock
	r.mu.RLock()
	handler, ok := r.handlers[tc.Function.Name]
	r.mu.RUnlock()

	if !ok {
		return Message{}, fmt.Errorf("%w: %s", ErrToolNotFound, tc.Function.Name)
	}

	// Execute handler outside lock to allow concurrent tool execution
	result, err := handler(ctx, json.RawMessage(tc.Function.Arguments))
	if err != nil {
		return ToolResultError(tc.ID, tc.Function.Name, err), nil
	}

	// Handle nil result explicitly - return empty string result
	if result == nil {
		return ToolResultString(tc.ID, tc.Function.Name, ""), nil
	}

	return ToolResult(tc.ID, tc.Function.Name, result)
}

// HandleAll executes handlers for all tool calls in a response and returns tool messages.
func (r *ToolRegistry) HandleAll(ctx context.Context, resp *Response) ([]Message, error) {
	messages := make([]Message, 0, len(resp.ToolCalls))

	for _, tc := range resp.ToolCalls {
		msg, err := r.Handle(ctx, tc)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}

	return messages, nil
}
