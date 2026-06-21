package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

// WithResponsesAPI should route GenerateContent through POST /responses end to end.
func TestWithResponsesAPI_RoutesEndToEnd(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`))
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithResponsesAPI(),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := client.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if gotPath != "/responses" {
		t.Errorf("path = %s, want /responses", gotPath)
	}
	if !strings.Contains(gotBody, `"input"`) {
		t.Errorf("body missing input items: %s", gotBody)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q", resp.Content)
	}
}

// Without the option, the default path stays /chat/completions.
func TestWithoutResponsesAPI_UsesChatCompletions(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","choices":[{"message":{"role":"assistant","content":"hey"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}}); err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %s, want /chat/completions", gotPath)
	}
}
