package openaicompat

import (
	"context"
	"io"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/httpclient"
)

// sseBody renders a sequence of (event, data) pairs as an SSE stream body.
func sseBody(events [][2]string) io.ReadCloser {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("event: ")
		b.WriteString(e[0])
		b.WriteString("\ndata: ")
		b.WriteString(e[1])
		b.WriteString("\n\n")
	}
	return io.NopCloser(strings.NewReader(b.String()))
}

// drainResponsesStream runs ProcessResponsesStream over a canned body and collects
// the streamed text, reasoning, final tool calls, usage, finish reason, and error.
func drainResponsesStream(t *testing.T, body io.ReadCloser) (text, reasoning string, final llms.StreamChunk, gotErr error) {
	t.Helper()
	reader := &ResponsesStreamReader{sse: httpclient.NewSSEReader(body)}
	chunks := make(chan llms.StreamChunk, 16)
	sender := llms.NewStreamSender(context.Background(), chunks, 0)

	go ProcessResponsesStream(context.Background(), reader, chunks, sender, "openai", nil)

	for chunk := range chunks {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
		text += chunk.Content
		if chunk.Reasoning != nil {
			reasoning += chunk.Reasoning.Content
		}
		if len(chunk.ToolCalls) > 0 || chunk.Usage != nil || chunk.FinishReason != "" {
			final = chunk
		}
	}
	return text, reasoning, final, gotErr
}

func TestProcessResponsesStream_TextReasoningCompleted(t *testing.T) {
	completed := `{"type":"response.completed","response":{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`
	body := sseBody([][2]string{
		{"response.reasoning_summary_text.delta", `{"type":"response.reasoning_summary_text.delta","delta":"think"}`},
		{"response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hel"}`},
		{"response.output_text.delta", `{"type":"response.output_text.delta","delta":"lo"}`},
		{"response.completed", completed},
	})

	text, reasoning, final, err := drainResponsesStream(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Hello" {
		t.Errorf("streamed text = %q, want Hello", text)
	}
	if reasoning != "think" {
		t.Errorf("streamed reasoning = %q, want think", reasoning)
	}
	if final.FinishReason != llms.FinishReasonStop {
		t.Errorf("finish = %q, want stop", final.FinishReason)
	}
	if final.Usage == nil || final.Usage.TotalTokens != 12 {
		t.Errorf("final usage = %+v, want total 12", final.Usage)
	}
}

func TestProcessResponsesStream_ToolCallsFromCompleted(t *testing.T) {
	completed := `{"type":"response.completed","response":{"id":"r","status":"completed","output":[{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}]}}`
	body := sseBody([][2]string{{"response.completed", completed}})

	_, _, final, err := drainResponsesStream(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if final.FinishReason != llms.FinishReasonToolCalls {
		t.Errorf("finish = %q, want tool_calls", final.FinishReason)
	}
	if len(final.ToolCalls) != 1 || final.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("tool calls = %+v", final.ToolCalls)
	}
}

func TestProcessResponsesStream_ErrorEvent(t *testing.T) {
	body := sseBody([][2]string{
		{"response.output_text.delta", `{"type":"response.output_text.delta","delta":"partial"}`},
		{"error", `{"type":"error","message":"rate limited","code":"rate_limit"}`},
	})
	text, _, _, err := drainResponsesStream(t, body)
	if err == nil {
		t.Fatal("expected a terminal error chunk")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %v, want it to mention 'rate limited'", err)
	}
	if text != "partial" {
		t.Errorf("text before error = %q, want partial", text)
	}
}

func TestProcessResponsesStream_EOFWithoutCompleted(t *testing.T) {
	body := sseBody([][2]string{
		{"response.output_text.delta", `{"type":"response.output_text.delta","delta":"hi"}`},
	})
	text, _, final, err := drainResponsesStream(t, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hi" {
		t.Errorf("text = %q, want hi", text)
	}
	if final.FinishReason != llms.FinishReasonStop {
		t.Errorf("finish = %q, want stop on clean EOF", final.FinishReason)
	}
}
