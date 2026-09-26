package openaicompat_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

const reportedModel = "engine-chat-model-2026-01-01"

func toolCallResponse(calls ...openaicompat.ToolCall) openaicompat.ChatCompletionResponse {
	return openaicompat.ChatCompletionResponse{
		ID:    "chatcmpl-tools",
		Model: reportedModel,
		Choices: []openaicompat.Choice{{
			Message:      &openaicompat.ChatMessage{Role: "assistant", ToolCalls: calls},
			FinishReason: "tool_calls",
		}},
	}
}

func fn(name string) *openaicompat.FunctionCall {
	return &openaicompat.FunctionCall{Name: name, Arguments: "{}"}
}

func TestBaseProvider_GenerateContentIdentity(t *testing.T) {
	server := testutil.NewMockOpenAICompatibleServer(testutil.WithChatCompletionResponse(
		toolCallResponse(
			openaicompat.ToolCall{ID: "", Type: "function", Function: fn("a")},
			openaicompat.ToolCall{ID: "dup", Type: "function", Function: fn("b")},
			openaicompat.ToolCall{ID: "dup", Type: "function", Function: fn("c")},
		),
	))
	defer server.Close()
	provider := newTestBaseProvider(server)
	msgs := []llms.Message{{Role: llms.RoleUser, Content: "hi"}}

	t.Run("default model", func(t *testing.T) {
		resp, err := provider.GenerateContent(context.Background(), msgs)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Provider != engineProvider || resp.Model != engineModel || resp.ModelVersion != reportedModel {
			t.Errorf("identity = %q/%q/%q, want %q/%q/%q", resp.Provider, resp.Model, resp.ModelVersion,
				engineProvider, engineModel, reportedModel)
		}
		ids := map[string]bool{}
		for _, tc := range resp.ToolCalls {
			if tc.ID == "" || ids[tc.ID] {
				t.Errorf("tool call IDs not unique and non-empty: %+v", resp.ToolCalls)
			}
			ids[tc.ID] = true
		}
		if resp.ToolCalls[1].ID != "dup" {
			t.Errorf("first occurrence of a server ID was replaced: %q", resp.ToolCalls[1].ID)
		}
	})

	t.Run("model override is the requested model", func(t *testing.T) {
		resp, err := provider.GenerateContent(context.Background(), msgs, llms.WithModel("other-model"))
		if err != nil {
			t.Fatal(err)
		}
		if resp.Model != "other-model" {
			t.Errorf("Model = %q, want the requested other-model", resp.Model)
		}
	})
}

// TestBaseProvider_StreamContract checks the StreamChunk contract every provider
// keeps: tool calls arrive complete on the final chunk only, and the final chunk
// carries the served identity.
func TestBaseProvider_StreamContract(t *testing.T) {
	index := 0
	server := testutil.NewMockOpenAICompatibleServer(testutil.WithStreamResponse(
		streamContentChunk("hello"),
		streamToolCallChunk(openaicompat.ToolCall{Index: &index, Type: "function", Function: &openaicompat.FunctionCall{Name: "f", Arguments: `{"a":`}}),
		streamToolCallChunk(openaicompat.ToolCall{Index: &index, Function: &openaicompat.FunctionCall{Arguments: `1}`}}),
		streamFinishChunk("tool_calls", nil),
	))
	defer server.Close()
	provider := newTestBaseProvider(server)

	stream, err := provider.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var final llms.StreamChunk
	for chunk := range stream {
		if !chunk.Done && len(chunk.ToolCalls) > 0 {
			t.Errorf("non-final chunk carries tool calls: %+v", chunk.ToolCalls)
		}
		if chunk.Done {
			final = chunk
		}
	}
	if final.Provider != engineProvider || final.Model != engineModel || final.ModelVersion != engineModel {
		t.Errorf("final identity = %q/%q/%q", final.Provider, final.Model, final.ModelVersion)
	}
	if len(final.ToolCalls) != 1 || final.ToolCalls[0].ID == "" || final.ToolCalls[0].Function.Arguments != `{"a":1}` {
		t.Errorf("final tool calls = %+v, want one complete call with a minted ID", final.ToolCalls)
	}
}

// TestBaseProvider_ResponsesDropsForeignReasoning checks that encrypted
// reasoning another provider produced never reaches the Responses API, while
// the provider's own replays.
func TestBaseProvider_ResponsesDropsForeignReasoning(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	client := openaicompat.NewClient(openaicompat.ClientConfig{BaseURL: server.URL, APIKey: "k", AllowPrivateIPs: true, AllowHTTP: true})
	p := openaicompat.NewBaseProvider(client, openaicompat.ProviderConfig{
		Provider:        llms.ProviderOpenAI,
		DefaultModel:    "gpt",
		UseResponsesAPI: true,
	})
	reasoning := func(provider llms.Provider, id string) *llms.ReasoningContent {
		return &llms.ReasoningContent{
			Provider: provider,
			Metadata: map[string]any{openaicompat.MetadataKeyResponsesReasoning: []openaicompat.ResponsesReasoningItem{
				{ID: id, EncryptedContent: "E-" + id},
			}},
		}
	}
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "q1"},
		{Role: llms.RoleAssistant, Content: "a1", Reasoning: reasoning(llms.ProviderOpenAI, "rs_own")},
		{Role: llms.RoleUser, Content: "q2"},
		{Role: llms.RoleAssistant, Content: "a2", Reasoning: reasoning(llms.ProviderAzure, "rs_foreign")},
		{Role: llms.RoleUser, Content: "q3"},
	}
	if _, err := p.GenerateContent(context.Background(), msgs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "rs_own") {
		t.Errorf("own reasoning item missing from request: %s", body)
	}
	if strings.Contains(body, "rs_foreign") {
		t.Errorf("foreign reasoning item sent: %s", body)
	}
}
