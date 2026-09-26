package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// geminiServer answers every request with body and records the last request.
func geminiServer(t *testing.T, body string) (*Client, *string) {
	t.Helper()
	var last string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		last = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	c, err := New(WithAPIKey("k"), WithBaseURL(server.URL), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}
	return c, &last
}

func TestGenerateContent_ToolCallIDs(t *testing.T) {
	t.Run("native ID is kept and repeated names get distinct IDs", func(t *testing.T) {
		c, _ := geminiServer(t, `{"modelVersion":"gemini-reported","candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[
			{"functionCall":{"id":"fc_native","name":"get_weather","args":{"city":"a"}}},
			{"functionCall":{"name":"get_weather","args":{"city":"b"}}},
			{"functionCall":{"name":"get_weather","args":{"city":"c"}}}
		]}}]}`)
		resp, err := c.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "weather"}})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ToolCalls[0].ID != "fc_native" {
			t.Errorf("native ID = %q, want fc_native", resp.ToolCalls[0].ID)
		}
		seen := map[string]bool{}
		for _, tc := range resp.ToolCalls {
			if tc.ID == "" || tc.ID == "get_weather" || seen[tc.ID] {
				t.Errorf("IDs not unique minted IDs: %+v", resp.ToolCalls)
			}
			seen[tc.ID] = true
		}
		if resp.Provider != llms.ProviderGemini || resp.Model != c.Model() || resp.ModelVersion != "gemini-reported" {
			t.Errorf("identity = %q/%q/%q", resp.Provider, resp.Model, resp.ModelVersion)
		}
	})

	t.Run("tool result with only an ID is named after its call", func(t *testing.T) {
		c, last := geminiServer(t, `{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":"ok"}]}}]}`)
		msgs := []llms.Message{
			{Role: llms.RoleUser, Content: "weather"},
			{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{
				{ID: "abc123XYZ", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "get_weather", Arguments: "{}"}},
			}},
			{Role: llms.RoleTool, ToolCallID: "abc123XYZ", Content: `{"temp":1}`},
			{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{
				{ID: "legacy_name", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "legacy_name", Arguments: "{}"}},
			}},
			{Role: llms.RoleTool, ToolCallID: "unmatched_fn", Content: `{"x":2}`},
		}
		if _, err := c.GenerateContent(context.Background(), msgs); err != nil {
			t.Fatal(err)
		}
		var req struct {
			Contents []struct {
				Parts []struct {
					FunctionResponse *struct {
						Name string `json:"name"`
					} `json:"functionResponse"`
				} `json:"parts"`
			} `json:"contents"`
		}
		if err := json.Unmarshal([]byte(*last), &req); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, content := range req.Contents {
			for _, p := range content.Parts {
				if p.FunctionResponse != nil {
					names = append(names, p.FunctionResponse.Name)
				}
			}
		}
		if len(names) != 2 || names[0] != "get_weather" || names[1] != "unmatched_fn" {
			t.Errorf("function response names = %v, want [get_weather unmatched_fn]", names)
		}
	})
}

func TestGenerateContent_DropsForeignSignatures(t *testing.T) {
	c, last := geminiServer(t, `{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":"ok"}]}}]}`)
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "q"},
		{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{
			{ID: "own1abcde", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "f", Arguments: "{}"},
				Signature: "own-sig", SignatureProvider: llms.ProviderGemini},
			{ID: "for1abcde", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "g", Arguments: "{}"},
				Signature: "foreign-sig", SignatureProvider: llms.ProviderAnthropic},
		}},
		{Role: llms.RoleTool, ToolCallID: "own1abcde", Name: "f", Content: "{}"},
		{Role: llms.RoleTool, ToolCallID: "for1abcde", Name: "g", Content: "{}"},
	}
	if _, err := c.GenerateContent(context.Background(), msgs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(*last, "own-sig") {
		t.Errorf("own signature missing: %s", *last)
	}
	if strings.Contains(*last, "foreign-sig") {
		t.Errorf("foreign signature sent: %s", *last)
	}
}

func TestStream_IdentityAndToolCallContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range []string{
			`{"modelVersion":"gemini-reported","candidates":[{"content":{"role":"model","parts":[{"text":"thinking","thought":true}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"f","args":{}},"thoughtSignature":"sig"}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"f","args":{"n":2}}}]},"finishReason":"STOP"}]}`,
		} {
			_, _ = w.Write([]byte("data: " + ev + "\n\n"))
		}
	}))
	defer server.Close()
	c, err := New(WithAPIKey("k"), WithBaseURL(server.URL), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := c.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "go"}})
	if err != nil {
		t.Fatal(err)
	}
	var final llms.StreamChunk
	for ch := range stream {
		if ch.Error != nil {
			t.Fatal(ch.Error)
		}
		if !ch.Done && len(ch.ToolCalls) > 0 {
			t.Errorf("non-final chunk carries tool calls: %+v", ch.ToolCalls)
		}
		if ch.Done {
			final = ch
		}
	}
	if final.Provider != llms.ProviderGemini || final.Model != c.Model() || final.ModelVersion != "gemini-reported" {
		t.Errorf("final identity = %q/%q/%q", final.Provider, final.Model, final.ModelVersion)
	}
	if len(final.ToolCalls) != 2 || final.ToolCalls[0].ID == final.ToolCalls[1].ID || final.ToolCalls[0].ID == "f" {
		t.Errorf("final tool call IDs = %+v, want two distinct minted IDs", final.ToolCalls)
	}
	if final.ToolCalls[0].SignatureProvider != llms.ProviderGemini || final.ToolCalls[1].SignatureProvider != "" {
		t.Errorf("signature stamps = %q/%q", final.ToolCalls[0].SignatureProvider, final.ToolCalls[1].SignatureProvider)
	}
}

// TestGenerateContent_EchoesCallIDsToGemini3 checks that requests to Gemini 3
// carry each call's ID on the replayed functionCall and on its functionResponse,
// which Gemini 3 pairs by ID, and that earlier models, which never issued IDs,
// are sent none.
func TestGenerateContent_EchoesCallIDsToGemini3(t *testing.T) {
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "q"},
		{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{
			{ID: "fc_one", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "f", Arguments: "{}"}},
			{ID: "fc_two", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "f", Arguments: "{}"}},
		}},
		{Role: llms.RoleTool, ToolCallID: "fc_one", Content: "{}"},
		{Role: llms.RoleTool, ToolCallID: "fc_two", Content: "{}"},
	}
	for _, tt := range []struct {
		model string
		want  bool
	}{{"gemini-3-pro", true}, {"gemini-2.5-flash", false}} {
		c, last := geminiServer(t, `{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":"ok"}]}}]}`)
		if _, err := c.GenerateContent(context.Background(), msgs, llms.WithModel(tt.model)); err != nil {
			t.Fatal(err)
		}
		var req struct {
			Contents []struct {
				Parts []struct {
					FunctionCall     *struct{ ID string } `json:"functionCall"`
					FunctionResponse *struct{ ID string } `json:"functionResponse"`
				} `json:"parts"`
			} `json:"contents"`
		}
		if err := json.Unmarshal([]byte(*last), &req); err != nil {
			t.Fatal(err)
		}
		var calls, responses []string
		for _, content := range req.Contents {
			for _, p := range content.Parts {
				if p.FunctionCall != nil {
					calls = append(calls, p.FunctionCall.ID)
				}
				if p.FunctionResponse != nil {
					responses = append(responses, p.FunctionResponse.ID)
				}
			}
		}
		want := []string{"", ""}
		if tt.want {
			want = []string{"fc_one", "fc_two"}
		}
		if strings.Join(calls, ",") != strings.Join(want, ",") || strings.Join(responses, ",") != strings.Join(want, ",") {
			t.Errorf("%s: call IDs %v, response IDs %v, want %v on both", tt.model, calls, responses, want)
		}
	}
}

// TestGenerateContent_ToolSchemasAreJSONSchema checks that tool parameters go
// in parametersJsonSchema. Gemini's parameters field takes an OpenAPI subset and
// answers additionalProperties, present in every schema generated from a Go
// struct, with a 400.
func TestGenerateContent_ToolSchemasAreJSONSchema(t *testing.T) {
	c, last := geminiServer(t, `{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":"ok"}]}}]}`)
	tool := llms.NewFunctionTool("f", "f", map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"q": map[string]any{"type": "string"}},
	})
	if _, err := c.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "q"}}, llms.WithTools([]llms.Tool{tool})); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Tools []struct {
			FunctionDeclarations []map[string]any `json:"functionDeclarations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(*last), &req); err != nil {
		t.Fatal(err)
	}
	decl := req.Tools[0].FunctionDeclarations[0]
	if _, ok := decl["parameters"]; ok {
		t.Error("tool schema sent in parameters")
	}
	schema, _ := decl["parametersJsonSchema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Errorf("parametersJsonSchema = %v", decl["parametersJsonSchema"])
	}
}
