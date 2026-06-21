package openaicompat_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/testutil"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/openaicompat"
)

const (
	engineProvider = llms.Provider("engine-test")
	engineModel    = "engine-chat-model"
	embeddingModel = "engine-embedding-model"
)

func TestEngineGenerateContent(t *testing.T) {
	tests := []struct {
		name     string
		response openaicompat.ChatCompletionResponse
		want     *llms.Response
	}{
		{
			name: "success maps content finish reason and usage",
			response: openaicompat.ChatCompletionResponse{
				ID:    "chatcmpl-engine",
				Model: engineModel,
				Choices: []openaicompat.Choice{
					{
						Index: 0,
						Message: &openaicompat.ChatMessage{
							Role:         "assistant",
							ContentValue: "engine response",
						},
						FinishReason: "stop",
					},
				},
				Usage: &openaicompat.Usage{
					PromptTokens:     11,
					CompletionTokens: 7,
					TotalTokens:      18,
					PromptTokensDetails: &struct {
						CachedTokens int `json:"cached_tokens"`
					}{CachedTokens: 3},
				},
			},
			want: &llms.Response{
				Content:      "engine response",
				FinishReason: "stop",
				Usage: llms.Usage{
					// PromptTokens excludes the 3 cache-read tokens (11 - 3 = 8).
					PromptTokens:     8,
					CompletionTokens: 7,
					TotalTokens:      18,
					CacheReadTokens:  3,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := testutil.NewMockOpenAICompatibleServer(
				testutil.WithChatCompletionResponse(tt.response),
			)
			defer server.Close()
			provider := newTestBaseProvider(server)

			resp, err := provider.GenerateContent(context.Background(), []llms.Message{
				{Role: llms.RoleUser, Content: "hello"},
			})
			if err != nil {
				t.Fatalf("GenerateContent() error = %v", err)
			}

			if resp.Content != tt.want.Content {
				t.Fatalf("Content = %q, want %q", resp.Content, tt.want.Content)
			}
			if resp.FinishReason != tt.want.FinishReason {
				t.Fatalf("FinishReason = %q, want %q", resp.FinishReason, tt.want.FinishReason)
			}
			if resp.Usage != tt.want.Usage {
				t.Fatalf("Usage = %+v, want %+v", resp.Usage, tt.want.Usage)
			}

			req := server.LastRequest()
			if req.Method != http.MethodPost || req.Path != "/chat/completions" {
				t.Fatalf("request = %s %s, want POST /chat/completions", req.Method, req.Path)
			}
			if req.Body["model"] != engineModel {
				t.Fatalf("request model = %v, want %s", req.Body["model"], engineModel)
			}
			messages, ok := req.Body["messages"].([]any)
			if !ok || len(messages) != 1 {
				t.Fatalf("request messages = %#v, want one message", req.Body["messages"])
			}
			first, ok := messages[0].(map[string]any)
			if !ok {
				t.Fatalf("request message type = %T, want map[string]any", messages[0])
			}
			if first["role"] != "user" || first["content"] != "hello" {
				t.Fatalf("request message = %#v, want user hello", first)
			}
		})
	}
}

func TestEngineGenerateContent_APIError(t *testing.T) {
	server := testutil.NewMockOpenAICompatibleServer(
		testutil.WithErrorResponse(http.StatusBadRequest, map[string]any{
			"error": map[string]any{
				"message": "invalid model",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
				"param":   "model",
			},
		}),
	)
	defer server.Close()
	provider := newTestBaseProvider(server)

	_, err := provider.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "hello"},
	})
	if err == nil {
		t.Fatal("GenerateContent() error = nil, want API error")
	}
	var apiErr *llms.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As(*llms.APIError) = false for %T: %v", err, err)
	}
	if apiErr.Provider != engineProvider {
		t.Fatalf("Provider = %q, want %q", apiErr.Provider, engineProvider)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
	if apiErr.Code != "model_not_found" || apiErr.Type != "invalid_request_error" || apiErr.Param != "model" {
		t.Fatalf("unexpected API error fields: %+v", apiErr)
	}
}

func TestEngineGenerateContent_EmptyChoices(t *testing.T) {
	server := testutil.NewMockOpenAICompatibleServer(
		testutil.WithChatCompletionResponse(openaicompat.ChatCompletionResponse{
			ID:      "chatcmpl-empty",
			Model:   engineModel,
			Choices: nil,
		}),
	)
	defer server.Close()
	provider := newTestBaseProvider(server)

	resp, err := provider.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "hello"},
	})
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	if resp == nil {
		t.Fatal("GenerateContent() response = nil, want empty response")
	}
	if resp.Content != "" || resp.FinishReason != "" || resp.Usage != (llms.Usage{}) {
		t.Fatalf("response = %+v, want empty response", resp)
	}
}

func TestEngineStreamContentUsageAndTerminal(t *testing.T) {
	server := testutil.NewMockOpenAICompatibleServer(
		testutil.WithStreamResponse(
			streamContentChunk("Hello"),
			streamContentChunk(" world"),
			streamFinishChunk("stop", &openaicompat.Usage{
				PromptTokens:     4,
				CompletionTokens: 2,
				TotalTokens:      6,
			}),
		),
	)
	defer server.Close()
	provider := newTestBaseProvider(server)

	stream, err := provider.Stream(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "hello"},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var content strings.Builder
	var terminal []llms.StreamChunk
	for chunk := range stream {
		if chunk.Content != "" {
			content.WriteString(chunk.Content)
		}
		if chunk.Done || chunk.Error != nil {
			terminal = append(terminal, chunk)
		}
	}

	if content.String() != "Hello world" {
		t.Fatalf("stream content = %q, want %q", content.String(), "Hello world")
	}
	if len(terminal) != 1 {
		t.Fatalf("terminal chunks = %d, want 1: %+v", len(terminal), terminal)
	}
	final := terminal[0]
	if final.Error != nil {
		t.Fatalf("terminal error = %v, want nil", final.Error)
	}
	if !final.Done {
		t.Fatal("terminal Done = false, want true")
	}
	if final.FinishReason != "stop" {
		t.Fatalf("terminal FinishReason = %q, want stop", final.FinishReason)
	}
	if final.Usage == nil || *final.Usage != (llms.Usage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6}) {
		t.Fatalf("terminal Usage = %+v, want prompt=4 completion=2 total=6", final.Usage)
	}

	req := server.LastRequest()
	if req.Body["stream"] != true {
		t.Fatalf("request stream = %v, want true", req.Body["stream"])
	}
}

func TestEngineStreamToolCallsMergedByIndex(t *testing.T) {
	index := 0
	server := testutil.NewMockOpenAICompatibleServer(
		testutil.WithStreamResponse(
			streamToolCallChunk(openaicompat.ToolCall{
				Index: &index,
				ID:    "call_1",
				Type:  "function",
				Function: &openaicompat.FunctionCall{
					Name:      "get_weather",
					Arguments: `{"city":`,
				},
			}),
			streamToolCallChunk(openaicompat.ToolCall{
				Index: &index,
				Function: &openaicompat.FunctionCall{
					Arguments: `"Paris"}`,
				},
			}),
			streamFinishChunk("tool_calls", nil),
		),
	)
	defer server.Close()
	provider := newTestBaseProvider(server)

	stream, err := provider.Stream(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "weather"},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	final := drainOneTerminal(t, stream)
	if final.Error != nil {
		t.Fatalf("terminal error = %v, want nil", final.Error)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("terminal ToolCalls = %+v, want one call", final.ToolCalls)
	}
	call := final.ToolCalls[0]
	if call.ID != "call_1" || call.Type != "function" {
		t.Fatalf("tool call identity = %+v, want call_1 function", call)
	}
	if call.Function == nil {
		t.Fatal("tool call Function = nil, want function call")
	}
	if call.Function.Name != "get_weather" || call.Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("tool call function = %+v, want merged arguments", call.Function)
	}
	if final.FinishReason != "tool_calls" {
		t.Fatalf("FinishReason = %q, want tool_calls", final.FinishReason)
	}
}

func TestEngineStreamMidStreamContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := testutil.NewMockOpenAICompatibleServer(
		testutil.WithStreamResponse(streamContentChunk("first")),
		testutil.WithStreamHeldOpenAfterChunks(),
	)
	defer server.Close()
	provider := newTestBaseProvider(server)

	stream, err := provider.Stream(ctx, []llms.Message{
		{Role: llms.RoleUser, Content: "hello"},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	first := receiveChunk(t, stream)
	if first.Content != "first" {
		t.Fatalf("first chunk Content = %q, want first", first.Content)
	}
	cancel()

	final := receiveChunk(t, stream)
	if final.Error == nil {
		t.Fatalf("terminal Error = nil, want cancellation error: %+v", final)
	}
	if !final.Done {
		t.Fatal("terminal Done = false, want true")
	}
	if extra, ok := receiveOptionalChunk(stream); ok && (extra.Done || extra.Error != nil) {
		t.Fatalf("received extra terminal chunk: %+v", extra)
	}
}

func TestEngineEmbed(t *testing.T) {
	server := testutil.NewMockOpenAICompatibleServer(
		testutil.WithEmbeddingsResponse(openaicompat.EmbeddingResponse{
			Object: "list",
			Model:  embeddingModel,
			Data: []openaicompat.EmbeddingData{
				{Object: "embedding", Index: 0, Embedding: []float32{0.1, 0.2}},
				{Object: "embedding", Index: 1, Embedding: []float32{0.3, 0.4}},
			},
			Usage: openaicompat.EmbeddingUsage{
				PromptTokens: 5,
				TotalTokens:  5,
			},
		}),
	)
	defer server.Close()
	provider := newTestBaseProvider(server)

	resp, err := provider.Embed(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if resp.Model != embeddingModel {
		t.Fatalf("Model = %q, want %q", resp.Model, embeddingModel)
	}
	if resp.Usage.PromptTokens != 5 || resp.Usage.TotalTokens != 5 {
		t.Fatalf("Usage = %+v, want prompt=5 total=5", resp.Usage)
	}
	if len(resp.Embeddings) != 2 {
		t.Fatalf("Embeddings length = %d, want 2", len(resp.Embeddings))
	}
	if resp.Embeddings[0].Index != 0 || !equalFloat64s(resp.Embeddings[0].Vector, []float32{0.1, 0.2}) {
		t.Fatalf("first embedding = %+v, want index 0 vector [0.1 0.2]", resp.Embeddings[0])
	}

	req := server.LastRequest()
	if req.Method != http.MethodPost || req.Path != "/embeddings" {
		t.Fatalf("request = %s %s, want POST /embeddings", req.Method, req.Path)
	}
	if req.Body["model"] != embeddingModel {
		t.Fatalf("request model = %v, want %s", req.Body["model"], embeddingModel)
	}
	input, ok := req.Body["input"].([]any)
	if !ok || len(input) != 2 || input[0] != "one" || input[1] != "two" {
		t.Fatalf("request input = %#v, want [one two]", req.Body["input"])
	}
}

func newTestBaseProvider(server *testutil.MockOpenAICompatibleServer) *openaicompat.BaseProvider {
	client := openaicompat.NewClient(openaicompat.ClientConfig{
		BaseURL:         server.URL(),
		APIKey:          "test-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})
	provider := openaicompat.NewBaseProvider(client, openaicompat.ProviderConfig{
		Provider:              engineProvider,
		ProviderName:          string(engineProvider),
		DefaultModel:          engineModel,
		DefaultEmbeddingModel: embeddingModel,
		Capabilities: llms.Capabilities{
			Streaming:  true,
			Tools:      true,
			Embeddings: true,
		},
	})
	return &provider
}

func streamContentChunk(content string) openaicompat.StreamChunk {
	return openaicompat.StreamChunk{
		ID:    "chatcmpl-stream",
		Model: engineModel,
		Choices: []openaicompat.Choice{
			{
				Index: 0,
				Delta: &openaicompat.ChatMessage{ContentValue: content},
			},
		},
	}
}

func streamToolCallChunk(call openaicompat.ToolCall) openaicompat.StreamChunk {
	return openaicompat.StreamChunk{
		ID:    "chatcmpl-stream",
		Model: engineModel,
		Choices: []openaicompat.Choice{
			{
				Index: 0,
				Delta: &openaicompat.ChatMessage{
					ToolCalls: []openaicompat.ToolCall{call},
				},
			},
		},
	}
}

func streamFinishChunk(reason string, usage *openaicompat.Usage) openaicompat.StreamChunk {
	return openaicompat.StreamChunk{
		ID:    "chatcmpl-stream",
		Model: engineModel,
		Choices: []openaicompat.Choice{
			{
				Index:        0,
				Delta:        &openaicompat.ChatMessage{},
				FinishReason: reason,
			},
		},
		Usage: usage,
	}
}

func drainOneTerminal(t *testing.T, stream <-chan llms.StreamChunk) llms.StreamChunk {
	t.Helper()
	var terminals []llms.StreamChunk
	for chunk := range stream {
		if chunk.Done || chunk.Error != nil {
			terminals = append(terminals, chunk)
		}
	}
	if len(terminals) != 1 {
		t.Fatalf("terminal chunks = %d, want 1: %+v", len(terminals), terminals)
	}
	return terminals[0]
}

func receiveChunk(t *testing.T, stream <-chan llms.StreamChunk) llms.StreamChunk {
	t.Helper()
	select {
	case chunk, ok := <-stream:
		if !ok {
			t.Fatal("stream closed before expected chunk")
		}
		return chunk
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream chunk")
	}
	return llms.StreamChunk{}
}

func receiveOptionalChunk(stream <-chan llms.StreamChunk) (llms.StreamChunk, bool) {
	select {
	case chunk, ok := <-stream:
		return chunk, ok
	case <-time.After(100 * time.Millisecond):
		return llms.StreamChunk{}, false
	}
}

func equalFloat64s(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
