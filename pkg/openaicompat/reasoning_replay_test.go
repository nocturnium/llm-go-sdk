package openaicompat_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// replayServer records each request body and answers with a plain completion,
// or with a one-chunk stream when the request asks to stream.
func replayServer(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(b)
		mu.Unlock()
		if strings.Contains(string(b), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"c\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	return server, func() string {
		mu.Lock()
		defer mu.Unlock()
		return body
	}
}

func replayProvider(server *httptest.Server, replay bool) *openaicompat.BaseProvider {
	client := openaicompat.NewClient(openaicompat.ClientConfig{BaseURL: server.URL, APIKey: "k", AllowPrivateIPs: true, AllowHTTP: true})
	p := openaicompat.NewBaseProvider(client, openaicompat.ProviderConfig{
		Provider:        llms.ProviderOpenRouter,
		DefaultModel:    "m",
		ReplayReasoning: replay,
	})
	return &p
}

// A conversation with the provider's own reasoning on one assistant turn and
// another provider's on the next.
var replayConversation = []llms.Message{
	{Role: llms.RoleUser, Content: "q1"},
	{Role: llms.RoleAssistant, Content: "a1", Reasoning: &llms.ReasoningContent{Content: "I should read app.go first."}},
	{Role: llms.RoleUser, Content: "q2"},
	{Role: llms.RoleAssistant, Content: "a2", Reasoning: &llms.ReasoningContent{Content: "foreign thought", Provider: llms.ProviderAnthropic}},
	{Role: llms.RoleUser, Content: "q3"},
}

// With replay on, an assistant turn's own reasoning goes back in its
// "reasoning" field and another provider's does not; with it off, the default,
// no reasoning is sent at all.
func TestBaseProvider_ReplaysOwnReasoningOnlyWhenAsked(t *testing.T) {
	server, body := replayServer(t)

	if _, err := replayProvider(server, true).GenerateContent(context.Background(), replayConversation); err != nil {
		t.Fatal(err)
	}
	got := body()
	if !strings.Contains(got, `"reasoning":"I should read app.go first."`) {
		t.Errorf("own reasoning missing from the request: %s", got)
	}
	if strings.Contains(got, "foreign thought") || strings.Count(got, `"reasoning":`) != 1 {
		t.Errorf("reasoning sent beyond the one own assistant turn: %s", got)
	}

	if _, err := replayProvider(server, false).GenerateContent(context.Background(), replayConversation); err != nil {
		t.Fatal(err)
	}
	if got := body(); strings.Contains(got, `"reasoning":`) {
		t.Errorf("reasoning sent by a provider that did not ask to replay it: %s", got)
	}
}

// The streaming request is built the same way.
func TestBaseProvider_StreamReplaysReasoningWhenAsked(t *testing.T) {
	server, body := replayServer(t)
	chunks, err := replayProvider(server, true).Stream(context.Background(), replayConversation)
	if err != nil {
		t.Fatal(err)
	}
	received := 0
	for range chunks {
		received++
	}
	if received == 0 {
		t.Fatal("the stream delivered no chunks")
	}
	if got := body(); !strings.Contains(got, `"reasoning":"I should read app.go first."`) {
		t.Errorf("own reasoning missing from the streaming request: %s", got)
	}
}
