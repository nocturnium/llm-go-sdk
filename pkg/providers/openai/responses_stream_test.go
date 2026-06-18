package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

// WithResponsesAPI clients must stream via POST /responses and surface text deltas
// and the terminal finish/usage.
func TestResponsesAPI_StreamingEndToEnd(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(event, data string) {
			_, _ = w.Write([]byte("event: " + event + "\ndata: " + data + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		write("response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hel"}`)
		write("response.output_text.delta", `{"type":"response.output_text.delta","delta":"lo"}`)
		write("response.completed", `{"type":"response.completed","response":{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}}`)
	}))
	defer server.Close()

	client, err := New(WithAPIKey("k"), WithBaseURL(server.URL), WithResponsesAPI(), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}

	chunks, err := client.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text string
	var finish llms.FinishReason
	var total int
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		text += chunk.Content
		if chunk.FinishReason != "" {
			finish = chunk.FinishReason
		}
		if chunk.Usage != nil {
			total = chunk.Usage.TotalTokens
		}
	}

	if gotPath != "/responses" {
		t.Errorf("path = %s, want /responses", gotPath)
	}
	if text != "Hello" {
		t.Errorf("streamed text = %q, want Hello", text)
	}
	if finish != llms.FinishReasonStop {
		t.Errorf("finish = %q, want stop", finish)
	}
	if total != 5 {
		t.Errorf("usage total = %d, want 5", total)
	}
}
