package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/geminiapi"
)

func TestClient_GenerateContent_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("expected :generateContent in path, got %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "test-api-key" {
			t.Errorf("expected x-goog-api-key header")
		}

		// Parse request
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		contents, ok := req["contents"].([]any)
		if !ok || len(contents) == 0 {
			t.Error("expected contents in request")
		}

		// Send response
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "Hello! I'm Gemini."},
						},
						"role": "model",
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]any{
				"promptTokenCount":     10,
				"candidatesTokenCount": 5,
				"totalTokenCount":      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithModel("gemini-1.5-pro"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
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

	if resp.Content != "Hello! I'm Gemini." {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("unexpected prompt_tokens: %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("unexpected completion_tokens: %d", resp.Usage.CompletionTokens)
	}
}

func TestClient_GenerateContent_WithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify tools format
		tools, ok := req["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		// Gemini uses functionDeclarations inside tools
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Error("expected tool to be a map")
			return
		}
		declarations, ok := tool["functionDeclarations"].([]any)
		if !ok || len(declarations) == 0 {
			t.Error("expected functionDeclarations in tool")
		}

		// Respond with function call
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{
								"functionCall": map[string]any{
									"name": "get_weather",
									"args": map[string]any{"location": "Boston"},
								},
							},
						},
						"role": "model",
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]any{
				"promptTokenCount":     15,
				"candidatesTokenCount": 8,
				"totalTokenCount":      23,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	weatherTool := llms.NewFunctionTool("get_weather", "Get weather", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{"type": "string"},
		},
	})

	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "What's the weather in Boston?"}},
		llms.WithTools([]llms.Tool{weatherTool}),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if !resp.HasToolCalls() {
		t.Fatal("expected tool calls in response")
	}

	tc := resp.ToolCallByName("get_weather")
	if tc == nil {
		t.Fatal("expected get_weather tool call")
	}

	// Gemini uses function name as ID
	if tc.ID != "get_weather" {
		t.Errorf("unexpected tool call ID: %s", tc.ID)
	}

	type Args struct {
		Location string `json:"location"`
	}
	args, err := llms.ParseToolArguments[Args](*tc)
	if err != nil {
		t.Fatalf("failed to parse arguments: %v", err)
	}
	if args.Location != "Boston" {
		t.Errorf("unexpected location: %s", args.Location)
	}
}

func TestClient_GenerateContent_FunctionResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		contents, ok := req["contents"].([]any)
		if !ok {
			t.Error("expected contents to be an array")
			return
		}

		// Look for functionResponse in contents
		foundFunctionResponse := false
		for _, c := range contents {
			content, ok := c.(map[string]any)
			if !ok {
				continue
			}
			parts, ok := content["parts"].([]any)
			if !ok {
				continue
			}
			for _, p := range parts {
				part, ok := p.(map[string]any)
				if !ok {
					continue
				}
				if _, ok := part["functionResponse"]; ok {
					foundFunctionResponse = true
				}
			}
		}

		if !foundFunctionResponse {
			t.Error("expected functionResponse in request")
		}

		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "The weather in Boston is sunny and 72°F."},
						},
						"role": "model",
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]any{
				"promptTokenCount":     25,
				"candidatesTokenCount": 10,
				"totalTokenCount":      35,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Send a message with function response
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "What's the weather in Boston?"},
		{
			Role: llms.RoleAssistant,
			ToolCalls: []llms.ToolCall{
				{
					ID:   "get_weather",
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
			ToolCallID: "get_weather",
			Name:       "get_weather",
			Content:    `{"temperature": 72, "condition": "sunny"}`,
		},
	}

	resp, err := client.GenerateContent(context.Background(), messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != "The weather in Boston is sunny and 72°F." {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestClient_Stream_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming endpoint
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			t.Errorf("expected :streamGenerateContent in path, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Error("expected alt=sse query parameter")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected ResponseWriter to support Flusher")
			return
		}

		chunks := []string{
			`{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]}`,
			`{"candidates":[{"content":{"parts":[{"text":" there"}],"role":"model"}}]}`,
			`{"candidates":[{"content":{"parts":[{"text":"!"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8}}`,
		}

		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	chunks, err := client.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
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
		finalChunk = chunk
	}

	if content.String() != "Hello there!" {
		t.Errorf("unexpected content: %s", content.String())
	}
	if finalChunk.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", finalChunk.FinishReason)
	}
	if finalChunk.Usage == nil {
		t.Error("expected usage in final chunk")
	} else if finalChunk.Usage.TotalTokens != 8 {
		t.Errorf("unexpected total_tokens: %d", finalChunk.Usage.TotalTokens)
	}
}

func TestClient_Stream_RecoversPanic(t *testing.T) {
	originalReadStreamChunk := readStreamChunk
	readStreamChunk = func(_ *geminiapi.StreamReader) (*geminiapi.StreamChunk, error) {
		panic("test stream panic")
	}
	defer func() {
		readStreamChunk = originalReadStreamChunk
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Flusher")
		}
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]}` + "\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	chunks, err := client.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
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
	if !strings.Contains(streamErr.Error(), "gemini: panic during stream processing: test stream panic") {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
}

func TestClient_Stream_EmptySafetyFinishErrors(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
	}{
		{name: "safety", finishReason: "SAFETY"},
		{name: "recitation", finishReason: "RECITATION"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
					t.Errorf("expected :streamGenerateContent in path, got %s", r.URL.Path)
				}
				if r.URL.Query().Get("alt") != "sse" {
					t.Error("expected alt=sse query parameter")
				}

				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")

				flusher, ok := w.(http.Flusher)
				if !ok {
					t.Error("expected ResponseWriter to support Flusher")
					return
				}

				chunk := `{"candidates":[{"finishReason":"` + tc.finishReason + `"}]}`
				_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
				flusher.Flush()
			}))
			defer server.Close()

			client, err := New(
				WithAPIKey("test-api-key"),
				WithBaseURL(server.URL),
				WithAllowPrivateIPs(), WithAllowHTTP(),
			)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			chunks, err := client.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
			if err != nil {
				t.Fatalf("Stream failed: %v", err)
			}

			var content strings.Builder
			var errChunks int
			var totalChunks int
			var streamErr error

			for chunk := range chunks {
				totalChunks++
				content.WriteString(chunk.Content)
				if chunk.Error != nil {
					errChunks++
					streamErr = chunk.Error
				}
			}

			if content.String() != "" {
				t.Fatalf("expected no accumulated content, got %q", content.String())
			}
			if totalChunks != 1 {
				t.Fatalf("expected exactly one terminal chunk, got %d", totalChunks)
			}
			if errChunks != 1 {
				t.Fatalf("expected exactly one error chunk, got %d", errChunks)
			}
			msg := streamErr.Error()
			if !strings.Contains(msg, tc.finishReason) || !strings.Contains(msg, "no content") {
				t.Fatalf("expected error to mention %q and no content, got %q", tc.finishReason, msg)
			}
		})
	}
}

func TestClient_Stream_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Flusher")
		}

		// Send chunks slowly
		for i := 0; i < 100; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			chunk := `{"candidates":[{"content":{"parts":[{"text":"x"}],"role":"model"}}]}`
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	chunks, err := client.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var count int
	for chunk := range chunks {
		if chunk.Done {
			break
		}
		count++
	}

	if count >= 100 {
		t.Error("expected stream to be canceled before all chunks")
	}
}

func TestClient_GenerateContent_GenerationConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		config, ok := req["generationConfig"].(map[string]any)
		if !ok {
			t.Error("expected generationConfig to be a map")
			return
		}

		if config["temperature"] != 0.7 {
			t.Errorf("expected temperature=0.7, got %v", config["temperature"])
		}
		if config["maxOutputTokens"] != float64(1000) {
			t.Errorf("expected maxOutputTokens=1000, got %v", config["maxOutputTokens"])
		}

		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{{"text": "Response"}},
						"role":  "model",
					},
					"finishReason": "STOP",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
		llms.WithTemperature(0.7),
		llms.WithMaxTokens(1000),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}
}

func TestClient_ErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   map[string]any
	}{
		{
			name:       "invalid api key",
			statusCode: 400,
			response: map[string]any{
				"error": map[string]any{
					"code":    400,
					"message": "API key not valid",
					"status":  "INVALID_ARGUMENT",
				},
			},
		},
		{
			name:       "quota exceeded",
			statusCode: 429,
			response: map[string]any{
				"error": map[string]any{
					"code":    429,
					"message": "Resource exhausted",
					"status":  "RESOURCE_EXHAUSTED",
				},
			},
		},
		{
			name:       "model not found",
			statusCode: 404,
			response: map[string]any{
				"error": map[string]any{
					"code":    404,
					"message": "Model not found",
					"status":  "NOT_FOUND",
				},
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

			client, err := New(
				WithAPIKey("test-api-key"),
				WithBaseURL(server.URL),
				WithAllowPrivateIPs(), WithAllowHTTP(),
			)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			_, err = client.GenerateContent(
				context.Background(),
				[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
			)

			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestClient_JSONMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		config, ok := req["generationConfig"].(map[string]any)
		if !ok {
			t.Error("expected generationConfig to be a map")
			return
		}
		if config["responseMimeType"] != "application/json" {
			t.Errorf("expected responseMimeType=application/json, got %v", config["responseMimeType"])
		}

		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{{"text": `{"result": "success"}`}},
						"role":  "model",
					},
					"finishReason": "STOP",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Give me JSON"}},
		llms.WithJSONMode(),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != `{"result": "success"}` {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestClient_Call_Convenience(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)

		contents, ok := req["contents"].([]any)
		if !ok {
			t.Fatal("contents is not a []any")
		}
		if len(contents) != 1 {
			t.Errorf("expected 1 content, got %d", len(contents))
		}

		content, ok := contents[0].(map[string]any)
		if !ok {
			t.Fatal("content is not a map[string]any")
		}
		if content["role"] != "user" {
			t.Errorf("expected role=user, got %v", content["role"])
		}

		parts, ok := content["parts"].([]any)
		if !ok || len(parts) == 0 {
			t.Fatal("expected parts in content")
		}

		part, ok := parts[0].(map[string]any)
		if !ok {
			t.Fatal("part is not a map[string]any")
		}
		if part["text"] != "Hello" {
			t.Errorf("expected text=Hello, got %v", part["text"])
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{{"text": "Hi there!"}},
						"role":  "model",
					},
					"finishReason": "STOP",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	result, err := client.Call(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result != "Hi there!" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestClient_ValidationErrors(t *testing.T) {
	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL("https://example.com"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	t.Run("empty messages", func(t *testing.T) {
		_, err := client.GenerateContent(context.Background(), []llms.Message{})
		if err == nil {
			t.Fatal("expected error for empty messages")
		}
		if !errors.Is(err, llms.ErrEmptyMessages) {
			t.Errorf("expected ErrEmptyMessages, got %v", err)
		}
	})

	t.Run("invalid temperature", func(t *testing.T) {
		_, err := client.GenerateContent(
			context.Background(),
			[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
			llms.WithTemperature(3.0),
		)
		if err == nil {
			t.Fatal("expected error for invalid temperature")
		}
	})

	t.Run("invalid max_tokens", func(t *testing.T) {
		_, err := client.GenerateContent(
			context.Background(),
			[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
			llms.WithMaxTokens(-1),
		)
		if err == nil {
			t.Fatal("expected error for invalid max_tokens")
		}
	})
}

func TestClient_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":embedContent") {
			t.Errorf("expected :embedContent in path, got %s", r.URL.Path)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		resp := map[string]any{
			"embedding": map[string]any{
				"values": []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Embed(context.Background(), []string{"test text"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(resp.Embeddings) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(resp.Embeddings))
	}
	if len(resp.Embeddings[0].Vector) != 5 {
		t.Errorf("expected 5-dimensional vector, got %d", len(resp.Embeddings[0].Vector))
	}
	if resp.Embeddings[0].Vector[0] != 0.1 {
		t.Errorf("unexpected first value: %v", resp.Embeddings[0].Vector[0])
	}
}

func TestClient_EmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":batchEmbedContents") {
			t.Errorf("expected :batchEmbedContents in path, got %s", r.URL.Path)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		requests, ok := req["requests"].([]any)
		if !ok || len(requests) != 3 {
			t.Errorf("expected 3 requests, got %v", requests)
		}

		resp := map[string]any{
			"embeddings": []map[string]any{
				{"values": []float32{0.1, 0.2, 0.3}},
				{"values": []float32{0.4, 0.5, 0.6}},
				{"values": []float32{0.7, 0.8, 0.9}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Embed(context.Background(), []string{"text1", "text2", "text3"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(resp.Embeddings) != 3 {
		t.Fatalf("expected 3 embeddings, got %d", len(resp.Embeddings))
	}
}

func TestClient_EmbedQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify task type is set for queries
		if taskType, ok := req["taskType"].(string); ok {
			if taskType != "RETRIEVAL_QUERY" {
				t.Errorf("expected taskType=RETRIEVAL_QUERY, got %s", taskType)
			}
		}

		resp := map[string]any{
			"embedding": map[string]any{
				"values": []float32{0.1, 0.2, 0.3},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	vector, err := llms.EmbedQuery(context.Background(), client, "search query")
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	if len(vector) != 3 {
		t.Errorf("expected 3-dimensional vector, got %d", len(vector))
	}
}

func TestClient_EmbedDocuments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		resp := map[string]any{
			"embeddings": []map[string]any{
				{"values": []float32{0.1, 0.2}},
				{"values": []float32{0.3, 0.4}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	vectors, err := llms.EmbedDocuments(context.Background(), client, []string{"doc1", "doc2"})
	if err != nil {
		t.Fatalf("EmbedDocuments failed: %v", err)
	}

	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	if len(vectors[0]) != 2 {
		t.Errorf("expected 2-dimensional vectors, got %d", len(vectors[0]))
	}
}

func TestClient_GetCapabilities(t *testing.T) {
	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL("https://example.com"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	caps := client.Capabilities()

	// Gemini should support embeddings
	if !caps.Embeddings {
		t.Error("expected Embeddings capability to be true")
	}

	// Gemini should not support batch
	if caps.Batch {
		t.Error("expected Batch capability to be false")
	}
}

func TestConvertMessages(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
		{Role: llms.RoleAssistant, Content: "Hi there!"},
		{Role: llms.RoleUser, Content: "How are you?"},
	}

	result := convertMessages(messages)

	if len(result) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(result))
	}

	if result[0].Role != "user" {
		t.Errorf("expected role=user, got %s", result[0].Role)
	}
	if result[1].Role != "model" {
		t.Errorf("expected role=model, got %s", result[1].Role)
	}
	if len(result[0].Parts) != 1 || result[0].Parts[0].Text != "Hello" {
		t.Error("unexpected first message content")
	}
}

func TestConvertMessages_WithImage(t *testing.T) {
	messages := []llms.Message{
		{
			Role: llms.RoleUser,
			Parts: []llms.ContentPart{
				{Type: "text", Text: "What's in this image?"},
				{
					Type: "image",
					Image: &llms.ImageContent{
						MediaType: "image/png",
						Data:      "base64encodeddata",
						Source:    "base64",
					},
				},
			},
		},
	}

	result := convertMessages(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result))
	}

	if len(result[0].Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(result[0].Parts))
	}

	if result[0].Parts[0].Text != "What's in this image?" {
		t.Errorf("unexpected text part: %s", result[0].Parts[0].Text)
	}

	if result[0].Parts[1].InlineData == nil {
		t.Fatal("expected inline data for image")
	}
	if result[0].Parts[1].InlineData.MimeType != "image/png" {
		t.Errorf("unexpected mime type: %s", result[0].Parts[1].InlineData.MimeType)
	}
	if result[0].Parts[1].InlineData.Data != "base64encodeddata" {
		t.Errorf("unexpected image data: %s", result[0].Parts[1].InlineData.Data)
	}
}

func TestConvertRole(t *testing.T) {
	tests := []struct {
		input    llms.Role
		expected string
	}{
		{llms.RoleUser, "user"},
		{llms.RoleAssistant, "model"},
		{llms.RoleSystem, "user"}, // Gemini handles system differently
		{llms.RoleTool, "user"},   // Tool results from user
	}

	for _, tc := range tests {
		result := convertRole(tc.input)
		if result != tc.expected {
			t.Errorf("convertRole(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
}

func TestConvertTools(t *testing.T) {
	tools := []llms.Tool{
		llms.NewFunctionTool("get_weather", "Get the weather", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string"},
			},
		}),
		llms.NewFunctionTool("search", "Search the web", nil),
	}

	result := convertTools(tools)

	if len(result) != 1 {
		t.Fatalf("expected 1 tool (with multiple declarations), got %d", len(result))
	}

	if len(result[0].FunctionDeclarations) != 2 {
		t.Errorf("expected 2 function declarations, got %d", len(result[0].FunctionDeclarations))
	}

	if result[0].FunctionDeclarations[0].Name != "get_weather" {
		t.Errorf("unexpected function name: %s", result[0].FunctionDeclarations[0].Name)
	}
}

func TestConvertTools_Empty(t *testing.T) {
	result := convertTools([]llms.Tool{})
	if result != nil {
		t.Errorf("expected nil for empty tools, got %v", result)
	}
}

func TestConvertResponse_Empty(t *testing.T) {
	resp := convertResponse(&geminiapi.GenerateContentResponse{})

	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %s", resp.Content)
	}
}

func TestConvertResponse_WithToolCalls(t *testing.T) {
	resp := convertResponse(&geminiapi.GenerateContentResponse{
		Candidates: []geminiapi.Candidate{
			{
				Content: &geminiapi.Content{
					Parts: []geminiapi.Part{
						{
							FunctionCall: &geminiapi.FunctionCall{
								Name: "test_function",
								Args: map[string]any{"arg1": "value1"},
							},
						},
					},
				},
				FinishReason: "STOP",
			},
		},
		UsageMetadata: &geminiapi.UsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 5,
			TotalTokenCount:      15,
		},
	})

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}

	if resp.ToolCalls[0].Function.Name != "test_function" {
		t.Errorf("unexpected function name: %s", resp.ToolCalls[0].Function.Name)
	}

	if resp.Usage.TotalTokens != 15 {
		t.Errorf("unexpected total tokens: %d", resp.Usage.TotalTokens)
	}
}
