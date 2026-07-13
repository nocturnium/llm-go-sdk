package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

func TestClient_GenerateContent_NormalToolCallsLeaveContentEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := req["tool_choice"]; ok {
			t.Fatal("normal tool call request unexpectedly forced tool_choice")
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		tools, ok := req["tools"].([]any)
		if !ok {
			t.Fatal("expected tools array")
		}
		if len(tools) != 1 {
			t.Fatalf("expected exactly 1 structured-output tool, got %d", len(tools))
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("tool is %T, want map[string]any", tools[0])
		}
		if tool["name"] != "person" {
			t.Fatalf("tool name = %v, want person", tool["name"])
		}
		assertJSONValueEqual(t, tool["input_schema"], schema)

		toolChoice, ok := req["tool_choice"].(map[string]any)
		if !ok {
			t.Fatal("expected tool_choice object")
		}
		if toolChoice["type"] != "tool" || toolChoice["name"] != "person" {
			t.Fatalf("tool_choice = %#v, want forced person tool", toolChoice)
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

	var wantValue any
	if err := json.NewDecoder(bytes.NewReader(want)).Decode(&wantValue); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	if !reflect.DeepEqual(got, wantValue) {
		t.Fatalf("JSON value = %#v, want %#v", got, wantValue)
	}
}
