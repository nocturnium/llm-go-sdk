package gemini

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/geminiapi"
)

func followupClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := New(WithAPIKey("test-api-key"), WithModel("gemini-2.5-flash"), WithBaseURL(server.URL), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBlockedPrompt_ReturnsModerationError(t *testing.T) {
	c := followupClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":7,"totalTokenCount":7}}`))
	})
	_, err := c.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	var mod *llms.ModerationError
	if !errors.As(err, &mod) || mod.Stage != llms.ModerationInput || !mod.Charged || mod.Reasons[0] != "SAFETY" || !errors.Is(err, llms.ErrContentFiltered) {
		t.Fatalf("err = %v", err)
	}
}

func TestBlockedPrompt_Stream(t *testing.T) {
	c := followupClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"},"usageMetadata":{"promptTokenCount":5,"totalTokenCount":5}}` + "\n\n"))
	})
	ch, err := c.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var last llms.StreamChunk
	for chunk := range ch {
		last = chunk
	}
	var mod *llms.ModerationError
	if !errors.As(last.Error, &mod) || !mod.Charged || last.Usage == nil || last.Usage.PromptTokens != 5 {
		t.Fatalf("last = %+v", last)
	}
}

func TestConvertResponse_NoCandidatesKeepsUsage(t *testing.T) {
	out := convertResponse(&geminiapi.GenerateContentResponse{PromptFeedback: &geminiapi.PromptFeedback{BlockReason: "SAFETY"}, UsageMetadata: &geminiapi.UsageMetadata{PromptTokenCount: 3, TotalTokenCount: 3}})
	if out.Usage.PromptTokens != 3 || out.FinishReason != llms.FinishReasonContentFilter {
		t.Fatalf("out = %+v", out)
	}
}

func TestConvertMessages_ToolResultNameFallsBackToID(t *testing.T) {
	contents, err := convertMessages([]llms.Message{{Role: llms.RoleTool, ToolCallID: "get_weather", Content: `{"temp":20}`}})
	if err != nil || len(contents) != 1 || contents[0].Parts[0].FunctionResponse.Name != "get_weather" {
		t.Fatalf("contents = %+v err = %v", contents, err)
	}
	contents, _ = convertMessages([]llms.Message{{Role: llms.RoleTool, ToolCallID: "call_1", Name: "explicit", Content: "x"}})
	if contents[0].Parts[0].FunctionResponse.Name != "explicit" {
		t.Fatal("explicit Name must win")
	}
}

func TestConvertMessages_UnparseableToolArgsRetained(t *testing.T) {
	msg := llms.Message{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{{ID: "f", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "f", Arguments: "not json"}}}}
	contents, err := convertMessages([]llms.Message{msg})
	if err != nil || contents[0].Parts[0].FunctionCall.Args["arguments"] != "not json" {
		t.Fatalf("contents = %+v err = %v", contents, err)
	}
	msg.ToolCalls[0].Function.Arguments = ""
	contents, _ = convertMessages([]llms.Message{msg})
	if contents[0].Parts[0].FunctionCall.Args != nil {
		t.Fatal("empty arguments must stay nil")
	}
}

func TestConvertMessages_URLImageRejected(t *testing.T) {
	msg := llms.Message{Role: llms.RoleUser, Parts: []llms.ContentPart{{Type: llms.PartTypeImage, Image: &llms.ImageContent{Source: llms.ImageSourceURL, Data: "https://example.com/a.png"}}}}
	if _, err := convertMessages([]llms.Message{msg}); !errors.Is(err, llms.ErrInvalidParameters) || !strings.Contains(err.Error(), "url") {
		t.Fatalf("err = %v", err)
	}
	c := followupClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request must not reach the server") })
	if _, err := c.GenerateContent(context.Background(), []llms.Message{msg}); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.Stream(context.Background(), []llms.Message{msg}); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatalf("stream err = %v", err)
	}
}
