package openaicompat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateChatCompletionStream_DoesNotMutateCallerRequest pins that the
// caller's request survives a streaming call unchanged. Setting Stream on it
// raced any concurrent unary call sharing the request, and a request reused
// afterwards sent "stream":true to the non-streaming endpoint.
func TestCreateChatCompletionStream_DoesNotMutateCallerRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "test", AllowHTTP: true, AllowPrivateIPs: true})
	req := &ChatCompletionRequest{Model: "m", Messages: []ChatMessage{{Role: "user", ContentValue: "hi"}}}

	if _, err := client.CreateChatCompletionStream(context.Background(), req); err != nil {
		t.Fatalf("CreateChatCompletionStream: %v", err)
	}
	if req.Stream {
		t.Error("the caller's request was left with Stream set")
	}
}

// TestNewClient_CopiesHeaders pins that the caller's header map is not
// retained: every provider derived from one config shared it, so a later write
// by one reached the others.
func TestNewClient_CopiesHeaders(t *testing.T) {
	shared := map[string]string{"X-Team": "a"}
	client := NewClient(ClientConfig{BaseURL: "https://example.com", APIKey: "test", Headers: shared})

	shared["X-Team"] = "b"

	if got := client.getHeaders()["X-Team"]; got != "a" {
		t.Errorf("header = %q, want %q: the client retained the caller's map", got, "a")
	}
}
