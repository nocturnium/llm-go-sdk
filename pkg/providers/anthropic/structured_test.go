package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestClient_GenerateContent_NormalToolCallsLeaveContentEmpty(t *testing.T) {
	var handlerErr error
	var handlerMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			handlerMu.Lock()
			handlerErr = fmt.Errorf("decode request: %w", err)
			handlerMu.Unlock()
			return
		}
		if _, ok := req["tool_choice"]; ok {
			handlerMu.Lock()
			handlerErr = errors.New("normal tool call request unexpectedly forced tool_choice")
			handlerMu.Unlock()
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{
					"type":  "tool_use",
					"id":    "toolu_weather",
					"name":  "get_weather",
					"input": json.RawMessage(`{"location":"Boston"}`),
				},
				{
					"type":  "tool_use",
					"id":    "toolu_time",
					"name":  "get_time",
					"input": json.RawMessage(`{"timezone":"America/New_York"}`),
				},
			},
			"stop_reason": "tool_use",
			"usage":       map[string]any{"input_tokens": 25, "output_tokens": 15},
		})
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	tools := []llms.Tool{
		llms.NewFunctionTool("get_weather", "Get weather", map[string]any{
			"type":       "object",
			"properties": map[string]any{"location": map[string]any{"type": "string"}},
		}),
		llms.NewFunctionTool("get_time", "Get time", map[string]any{
			"type":       "object",
			"properties": map[string]any{"timezone": map[string]any{"type": "string"}},
		}),
	}
	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Use tools"}},
		llms.WithTools(tools),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}
	handlerMu.Lock()
	gotHandlerErr := handlerErr
	handlerMu.Unlock()
	if gotHandlerErr != nil {
		t.Fatal(gotHandlerErr)
	}

	if resp.Content != "" {
		t.Fatalf("normal tool calls should leave Content empty, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
	assertToolCall(t, resp.ToolCalls[0], "toolu_weather", "get_weather", `{"location":"Boston"}`)
	assertToolCall(t, resp.ToolCalls[1], "toolu_time", "get_time", `{"timezone":"America/New_York"}`)
}

func TestClient_GenerateContent_JSONSchemaForcesAnthropicToolAndReturnsInputAsContent(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name","age"],"additionalProperties":false}`)
	wantInput := `{"name":"Ada","age":37}`

	var handlerErr error
	var handlerMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			notifyHandlerError(&handlerMu, &handlerErr, fmt.Errorf("decode request: %w", err))
			return
		}

		tools, ok := req["tools"].([]any)
		if !ok {
			notifyHandlerError(&handlerMu, &handlerErr, errors.New("expected tools array"))
			return
		}
		if len(tools) != 1 {
			notifyHandlerError(&handlerMu, &handlerErr, fmt.Errorf("expected exactly 1 structured-output tool, got %d", len(tools)))
			return
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			notifyHandlerError(&handlerMu, &handlerErr, fmt.Errorf("tool is %T, want map[string]any", tools[0]))
			return
		}
		if tool["name"] != "person" {
			notifyHandlerError(&handlerMu, &handlerErr, fmt.Errorf("tool name = %v, want person", tool["name"]))
			return
		}
		if err := jsonValueEqual(tool["input_schema"], schema); err != nil {
			notifyHandlerError(&handlerMu, &handlerErr, err)
			return
		}

		toolChoice, ok := req["tool_choice"].(map[string]any)
		if !ok {
			notifyHandlerError(&handlerMu, &handlerErr, errors.New("expected tool_choice object"))
			return
		}
		if toolChoice["type"] != "tool" || toolChoice["name"] != "person" {
			notifyHandlerError(&handlerMu, &handlerErr, fmt.Errorf("tool_choice = %#v, want forced person tool", toolChoice))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{
					"type":  "tool_use",
					"id":    "toolu_structured",
					"name":  "person",
					"input": json.RawMessage(wantInput),
				},
			},
			"stop_reason": "tool_use",
			"usage":       map[string]any{"input_tokens": 25, "output_tokens": 15},
		})
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Return a person"}},
		llms.WithJSONSchema("person", schema, true),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}
	handlerMu.Lock()
	gotHandlerErr := handlerErr
	handlerMu.Unlock()
	if gotHandlerErr != nil {
		t.Fatal(gotHandlerErr)
	}

	if resp.Content != wantInput {
		t.Fatalf("structured-output Content = %q, want %q", resp.Content, wantInput)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	assertToolCall(t, resp.ToolCalls[0], "toolu_structured", "person", wantInput)
}

func assertToolCall(t *testing.T, got llms.ToolCall, wantID, wantName, wantArgs string) {
	t.Helper()

	if got.ID != wantID {
		t.Errorf("tool call ID = %q, want %q", got.ID, wantID)
	}
	if got.Function == nil {
		t.Fatal("tool call Function is nil")
	}
	if got.Function.Name != wantName {
		t.Errorf("tool call name = %q, want %q", got.Function.Name, wantName)
	}
	if got.Function.Arguments != wantArgs {
		t.Errorf("tool call args = %q, want %q", got.Function.Arguments, wantArgs)
	}
}

func assertJSONValueEqual(t *testing.T, got any, want json.RawMessage) {
	t.Helper()
	if err := jsonValueEqual(got, want); err != nil {
		t.Fatal(err)
	}
}

// notifyHandlerError records the first failure a test HTTP handler observes;
// handlers run on their own goroutine, where t.Fatal is invalid.
func notifyHandlerError(mu *sync.Mutex, dst *error, err error) {
	mu.Lock()
	if *dst == nil {
		*dst = err
	}
	mu.Unlock()
}

func jsonValueEqual(got any, want json.RawMessage) error {
	var wantValue any
	if err := json.NewDecoder(bytes.NewReader(want)).Decode(&wantValue); err != nil {
		return fmt.Errorf("decode expected JSON: %w", err)
	}
	if !reflect.DeepEqual(got, wantValue) {
		return fmt.Errorf("JSON value = %#v, want %#v", got, wantValue)
	}
	return nil
}
