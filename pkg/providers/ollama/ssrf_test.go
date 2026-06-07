package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recordingTransport returns a canned chat-completion response and records the
// URL it was asked to fetch.
type recordingTransport struct {
	hit bool
	url string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.hit = true
	rt.url = req.URL.String()
	body, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ok"}}},
	})
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    req,
	}, nil
}

// TestSSRF_DefaultLocalhostNotBlocked proves ollama.New() with its default
// localhost base URL passes SSRF validation out of the box (loopback + plain
// HTTP allowed by default for the local provider).
func TestSSRF_DefaultLocalhostNotBlocked(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "") // keep the localhost default deterministic

	rt := &recordingTransport{}
	client, err := New(WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Call(context.Background(), "hi"); err != nil {
		t.Fatalf("default localhost base URL was unexpectedly blocked: %v", err)
	}
	if !rt.hit {
		t.Fatal("expected request to reach the transport for default localhost base URL")
	}
	if !strings.HasPrefix(rt.url, "http://localhost:11434/") {
		t.Fatalf("expected request to localhost:11434, got %q", rt.url)
	}
}

// TestSSRF_ExplicitLocalhostBaseURLPasses proves an explicit
// http://localhost:11434 base URL passes validation.
func TestSSRF_ExplicitLocalhostBaseURLPasses(t *testing.T) {
	rt := &recordingTransport{}
	client, err := New(
		WithBaseURL("http://localhost:11434/v1"),
		WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Call(context.Background(), "hi"); err != nil {
		t.Fatalf("explicit localhost base URL was unexpectedly blocked: %v", err)
	}
	if !rt.hit {
		t.Fatal("expected request to reach the transport for explicit localhost base URL")
	}
}
