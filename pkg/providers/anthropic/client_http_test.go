package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/anthropicapi"
)

const (
	testClaudeSonnet = "claude-3-5-sonnet-20240620"
)

func TestClient_GenerateContent_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected /v1/messages, got %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-api-key" {
			t.Errorf("expected x-api-key header, got %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}

		// Parse request
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req["model"] != testClaudeSonnet {
			t.Errorf("expected model=claude-sonnet-4-20250514, got %v", req["model"])
		}
		if _, ok := req["system"]; ok {
			t.Error("system should be omitted when no system message is provided")
		}

		// Send response
		resp := map[string]any{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "text",
					"text": "Hello! How can I assist you today?",
				},
			},
			"model":       testClaudeSonnet,
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":  12,
				"output_tokens": 9,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
		WithModel(testClaudeSonnet),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := client.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != "Hello! How can I assist you today?" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 12 {
		t.Errorf("unexpected prompt_tokens: %d", resp.Usage.PromptTokens)
	}
}

func TestClient_GenerateContent_WithSystemMessage(t *testing.T) {
	var capturedSystem string
	var capturedCacheControl string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Capture system message
		if system, ok := req["system"].([]any); ok && len(system) > 0 {
			if block, ok := system[0].(map[string]any); ok {
				if text, ok := block["text"].(string); ok {
					capturedSystem = text
				}
				if cacheControl, ok := block["cache_control"].(map[string]any); ok {
					if cacheType, ok := cacheControl["type"].(string); ok {
						capturedCacheControl = cacheType
					}
				}
			}
		}

		// Verify messages don't include system
		messages, ok := req["messages"].([]any)
		if !ok {
			t.Fatal("messages is not a []any")
		}
		for _, m := range messages {
			msg, ok := m.(map[string]any)
			if !ok {
				t.Fatal("message is not a map[string]any")
			}
			if msg["role"] == "system" {
				t.Error("system message should not be in messages array")
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "I am a helpful assistant."},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 20, "output_tokens": 5},
		})
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a helpful assistant."},
		{Role: llms.RoleUser, Content: "Who are you?"},
	}

	_, err := client.GenerateContent(context.Background(), messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if capturedSystem != "You are a helpful assistant." {
		t.Errorf("expected system message, got: %s", capturedSystem)
	}
	if capturedCacheControl != "ephemeral" {
		t.Errorf("expected cache_control.type=ephemeral, got: %s", capturedCacheControl)
	}
}

func TestClient_GenerateContent_WithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Verify tools
		tools, ok := req["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		// Respond with tool use
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{
					"type":  "tool_use",
					"id":    "toolu_123",
					"name":  "get_weather",
					"input": map[string]any{"location": "Boston"},
				},
			},
			"stop_reason": "tool_use",
			"usage":       map[string]any{"input_tokens": 25, "output_tokens": 15},
		})
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	weatherTool := llms.NewFunctionTool("get_weather", "Get weather", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{"type": "string"},
		},
	})

	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "What's the weather?"}},
		llms.WithTools([]llms.Tool{weatherTool}),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if !resp.HasToolCalls() {
		t.Fatal("expected tool calls")
	}

	if resp.FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason=tool_calls, got %s", resp.FinishReason)
	}

	tc := resp.ToolCallByName("get_weather")
	if tc == nil {
		t.Fatal("expected get_weather tool call")
	}

	if tc.ID != "toolu_123" {
		t.Errorf("unexpected tool call ID: %s", tc.ID)
	}
}

func TestClient_GenerateContent_ToolResult(t *testing.T) {
	var capturedMessages []any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		messages, ok := req["messages"].([]any)
		if !ok {
			t.Fatal("messages is not a []any")
		}
		capturedMessages = messages

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_124",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "The weather in Boston is sunny."},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 30, "output_tokens": 8},
		})
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "What's the weather?"},
		{
			Role:    llms.RoleAssistant,
			Content: "",
			ToolCalls: []llms.ToolCall{
				{
					ID:   "toolu_123",
					Type: "function",
					Function: &llms.FunctionCall{
						Name:      "get_weather",
						Arguments: `{"location": "Boston"}`,
					},
				},
			},
		},
		{
			Role:       llms.RoleTool,
			ToolCallID: "toolu_123",
			Name:       "get_weather",
			Content:    `{"temperature": 72, "condition": "sunny"}`,
		},
	}

	resp, err := client.GenerateContent(context.Background(), messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != "The weather in Boston is sunny." {
		t.Errorf("unexpected content: %s", resp.Content)
	}

	// Verify tool result was properly formatted
	if len(capturedMessages) < 3 {
		t.Errorf("expected 3 messages, got %d", len(capturedMessages))
	}
}

func TestClient_Stream_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req["stream"] != true {
			t.Error("expected stream=true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Flusher")
		}

		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","content":[],"model":"claude-3-5-sonnet-20240620","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there!"}}`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":3}}`,
			`event: message_stop
data: {"type":"message_stop"}`,
		}

		for _, event := range events {
			_, _ = w.Write([]byte(event + "\n\n"))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	chunks, err := client.Stream(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
	)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var content strings.Builder
	var finalChunk llms.StreamChunk

	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		content.WriteString(chunk.Content)
		if chunk.Done {
			finalChunk = chunk
		}
	}

	if content.String() != "Hello there!" {
		t.Errorf("unexpected content: %s", content.String())
	}
	if finalChunk.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", finalChunk.FinishReason)
	}
}

func TestClient_Stream_RecoversPanic(t *testing.T) {
	originalReadStreamEvent := readStreamEvent
	readStreamEvent = func(_ *anthropicapi.StreamReader) (*anthropicapi.StreamEvent, error) {
		panic("test stream panic")
	}
	defer func() {
		readStreamEvent = originalReadStreamEvent
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Flusher")
		}
		_, _ = w.Write([]byte(`event: message_stop
data: {"type":"message_stop"}` + "\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	chunks, err := client.Stream(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
	)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var streamErr error
	for chunk := range chunks {
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
	}

	if streamErr == nil {
		t.Fatal("expected terminal stream error")
	}
	if !strings.Contains(streamErr.Error(), "anthropic: panic during stream processing: test stream panic") {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
}

func TestClient_Stream_WithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Flusher")
		}

		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","content":[],"model":"claude-3-5-sonnet-20240620","stop_reason":null,"usage":{"input_tokens":15,"output_tokens":0}}}`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_123","name":"get_weather"}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":" \"Boston\"}"}}`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":10}}`,
			`event: message_stop
data: {"type":"message_stop"}`,
		}

		for _, event := range events {
			_, _ = w.Write([]byte(event + "\n\n"))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	chunks, err := client.Stream(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Weather?"}},
	)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var finalChunk llms.StreamChunk
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		if chunk.Done {
			finalChunk = chunk
		}
	}

	if len(finalChunk.ToolCalls) == 0 {
		t.Fatal("expected tool calls in final chunk")
	}

	tc := finalChunk.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("unexpected function name: %s", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"location": "Boston"}` {
		t.Errorf("unexpected arguments: %s", tc.Function.Arguments)
	}
}

func TestClient_ErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   map[string]any
	}{
		{
			name:       "rate limited",
			statusCode: 429,
			response: map[string]any{
				"type":    "error",
				"error":   map[string]any{"type": "rate_limit_error", "message": "Rate limit exceeded"},
				"message": "Rate limit exceeded",
			},
		},
		{
			name:       "authentication error",
			statusCode: 401,
			response: map[string]any{
				"type":    "error",
				"error":   map[string]any{"type": "authentication_error", "message": "Invalid API key"},
				"message": "Invalid API key",
			},
		},
		{
			name:       "overloaded",
			statusCode: 529,
			response: map[string]any{
				"type":    "error",
				"error":   map[string]any{"type": "overloaded_error", "message": "API is overloaded"},
				"message": "API is overloaded",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_ = json.NewEncoder(w).Encode(tc.response)
			}))
			defer server.Close()

			client, _ := New(
				WithAPIKey("test-api-key"),
				WithBaseURL(server.URL+"/v1"),
				WithAllowPrivateIPs(), WithAllowHTTP(),
			)

			_, err := client.GenerateContent(
				context.Background(),
				[]llms.Message{{Role: llms.RoleUser, Content: "Hello"}},
			)

			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestClient_MaxTokensDefault(t *testing.T) {
	var capturedMaxTokens float64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		maxTokens, ok := req["max_tokens"].(float64)
		if !ok {
			t.Fatal("max_tokens is not a float64")
		}
		capturedMaxTokens = maxTokens

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_123",
			"type":        "message",
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": "Hi"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 5, "output_tokens": 1},
		})
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL+"/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	// Anthropic requires max_tokens - verify we set a default
	_, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if capturedMaxTokens == 0 {
		t.Error("expected max_tokens to be set")
	}
}

// TestClient_Stream_ThinkingSignature is the end-to-end guard for the streamed
// extended-thinking round-trip: a stream that emits thinking_delta then
// signature_delta must deliver the signature to the caller. It is load-bearing
// for BOTH fixes — the terminal-signature emit (anthropic.go) and CollectStream's
// signature preservation (streaming.go).
func TestClient_Stream_ThinkingSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		events := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude","usage":{"input_tokens":10,"output_tokens":0}}}`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think"}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG-XYZ"}}`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			`event: message_stop
data: {"type":"message_stop"}`,
		}
		for _, e := range events {
			_, _ = w.Write([]byte(e + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	client, _ := New(WithAPIKey("k"), WithBaseURL(server.URL+"/v1"), WithAllowPrivateIPs(), WithAllowHTTP())
	chunks, err := client.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	res, err := llms.CollectStream(chunks)
	if err != nil {
		t.Fatalf("CollectStream: %v", err)
	}
	if res.Reasoning == nil || res.Reasoning.Signature != "SIG-XYZ" {
		t.Errorf("streamed thinking signature not delivered: %+v", res.Reasoning)
	}
	if res.Reasoning != nil && res.Reasoning.Content != "Let me think" {
		t.Errorf("reasoning content = %q, want 'Let me think'", res.Reasoning.Content)
	}
}
