package openaicompat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// TestStream_TruncatedWithoutDoneReportsError pins that a connection dropped
// mid-generation is reported. Both [DONE] and a dropped connection surface as
// io.EOF from the reader, so the clean-finish branch used to hand the caller
// half an answer with a nil error.
func TestStream_TruncatedWithoutDoneReportsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"half an \"}}]}\n\n"))
		w.(http.Flusher).Flush()
		// Close without [DONE] and without a finish_reason.
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "test", AllowHTTP: true, AllowPrivateIPs: true})
	stream, err := client.CreateChatCompletionStream(context.Background(), &ChatCompletionRequest{
		Model:    "test-model",
		Messages: []ChatMessage{{Role: "user", ContentValue: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletionStream: %v", err)
	}

	chunks := make(chan llms.StreamChunk, 8)
	sender := llms.NewStreamSender(context.Background(), chunks, 0)
	go ProcessStream(context.Background(), stream, chunks, sender, "test", nil)

	var content string
	var streamErr error
	for chunk := range chunks {
		content += chunk.Content
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
	}

	if streamErr == nil {
		t.Fatalf("truncated stream reported no error, content = %q", content)
	}
	if !errors.Is(streamErr, io.ErrUnexpectedEOF) {
		t.Errorf("error = %v, want it to wrap io.ErrUnexpectedEOF", streamErr)
	}
}
