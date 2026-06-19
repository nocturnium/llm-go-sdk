package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

func TestClient_GenerateContent_JSONSchemaResponseFormatWireRequest(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name","age"],"additionalProperties":false}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		config, ok := req["generationConfig"].(map[string]any)
		if !ok {
			t.Fatal("expected generationConfig object")
		}
		if config["responseMimeType"] != "application/json" {
			t.Fatalf("responseMimeType = %v, want application/json", config["responseMimeType"])
		}
		geminiAssertJSONValueEqual(t, config["responseSchema"], schema)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{{"text": `{"name":"Ada","age":37}`}},
						"role":  "model",
					},
					"finishReason": "STOP",
				},
			},
		})
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Return a person"}},
		llms.WithJSONSchema("person", schema, true),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}
	if resp.Content != `{"name":"Ada","age":37}` {
		t.Fatalf("Content = %q, want JSON response", resp.Content)
	}
}

func geminiAssertJSONValueEqual(t *testing.T, got any, want json.RawMessage) {
	t.Helper()

	var wantValue any
	if err := json.NewDecoder(bytes.NewReader(want)).Decode(&wantValue); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	if !reflect.DeepEqual(got, wantValue) {
		t.Fatalf("JSON value = %#v, want %#v", got, wantValue)
	}
}
