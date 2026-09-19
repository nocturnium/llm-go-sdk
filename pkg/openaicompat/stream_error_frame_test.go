package openaicompat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// TestStream_MidStreamErrorFrameSurfaces pins that an error object sent
// mid-stream reaches the caller. OpenRouter, vLLM and LiteLLM emit one and
// then [DONE], which used to read as a clean finish with partial content.
func TestStream_MidStreamErrorFrameSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"half \"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"upstream died\",\"type\":\"server_error\",\"code\":502}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		w.(http.Flusher).Flush()
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

	var streamErr error
	for chunk := range chunks {
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
	}

	if streamErr == nil {
		t.Fatal("a mid-stream error frame was reported as a clean finish")
	}
	if !strings.Contains(streamErr.Error(), "upstream died") {
		t.Errorf("error = %v, want it to carry the provider's message", streamErr)
	}
}
