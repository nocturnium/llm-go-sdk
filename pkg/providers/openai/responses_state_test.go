package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// The response id must be surfaced on llms.Response so callers can chain turns.
func TestResponsesAPI_SurfacesResponseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_abc","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`))
	}))
	defer server.Close()

	client, err := New(WithAPIKey("k"), WithBaseURL(server.URL), WithResponsesAPI(), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "resp_abc" {
		t.Errorf("Response.ID = %q, want resp_abc", resp.ID)
	}
}

// WithPreviousResponseID and WithStore must be sent in the request body so a
// caller can chain server-side conversation state.
func TestResponsesAPI_ChainingOptions(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_2","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	client, err := New(WithAPIKey("k"), WithBaseURL(server.URL), WithResponsesAPI(), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GenerateContent(context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "again"}},
		WithPreviousResponseID("resp_abc"),
		WithStore(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	if body["previous_response_id"] != "resp_abc" {
		t.Errorf("previous_response_id = %v, want resp_abc", body["previous_response_id"])
	}
	if body["store"] != true {
		t.Errorf("store = %v, want true", body["store"])
	}
}
