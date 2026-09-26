package llmadk

import (
	"encoding/json"
	"strings"
	"testing"
)

// checkWireRequest checks a request body a provider client sent during fixture
// replay against the rules its API enforces, the rules live runs taught the
// bridge and the SDK: a regression in request building fails replay instead of
// only failing against the live API.
func checkWireRequest(t *testing.T, family string, body []byte) {
	t.Helper()
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Errorf("%s request is not JSON: %v", family, err)
		return
	}
	switch {
	case strings.HasPrefix(family, "gemini"):
		checkGeminiRequest(t, req)
	case strings.HasPrefix(family, "anthropic"):
		checkAnthropicRequest(t, req)
	case family == "openai-chat" || family == "openrouter" || family == "zai":
		checkChatRequest(t, req)
	case family == "openai-responses":
		checkResponsesRequest(t, req)
	}
}

func list(v any) []any            { l, _ := v.([]any); return l }
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }

// checkGeminiRequest: tool schemas go in parametersJsonSchema (the OpenAPI
// parameters field rejects additionalProperties), and on Gemini 3 the first
// function call of every model turn carries a thought signature.
func checkGeminiRequest(t *testing.T, req map[string]any) {
	t.Helper()
	for _, tool := range list(req["tools"]) {
		for _, d := range list(object(tool)["functionDeclarations"]) {
			decl := object(d)
			if _, ok := decl["parameters"]; ok {
				t.Errorf("gemini: function %v declared with parameters, which rejects JSON Schema", decl["name"])
			}
		}
	}
	for _, c := range list(req["contents"]) {
		content := object(c)
		if content["role"] != "model" {
			continue
		}
		for _, p := range list(content["parts"]) {
			part := object(p)
			if part["functionCall"] == nil {
				continue
			}
			if sig, _ := part["thoughtSignature"].(string); sig == "" {
				t.Errorf("gemini: first function call of a model turn has no thoughtSignature: %v", part["functionCall"])
			}
			break
		}
	}
}

// checkAnthropicRequest: a thinking block always carries its thinking field,
// and every tool_use is answered by a tool_result before the next assistant
// message (Anthropic joins consecutive user messages into one turn).
func checkAnthropicRequest(t *testing.T, req map[string]any) {
	t.Helper()
	msgs := list(req["messages"])
	for i, m := range msgs {
		msg := object(m)
		var uses []string
		for _, b := range list(msg["content"]) {
			block := object(b)
			switch block["type"] {
			case "thinking":
				if _, ok := block["thinking"]; !ok {
					t.Error("anthropic: thinking block without its thinking field")
				}
			case "tool_use":
				id, _ := block["id"].(string)
				uses = append(uses, id)
			}
		}
		if len(uses) == 0 {
			continue
		}
		answered := map[string]bool{}
		for j := i + 1; j < len(msgs) && object(msgs[j])["role"] == "user"; j++ {
			for _, b := range list(object(msgs[j])["content"]) {
				if id, _ := object(b)["tool_use_id"].(string); id != "" {
					answered[id] = true
				}
			}
		}
		for _, id := range uses {
			if !answered[id] {
				t.Errorf("anthropic: tool_use %s is not answered before the next assistant turn", id)
			}
		}
	}
}

// checkChatRequest: every assistant tool call is answered by a tool message
// before the next non-tool message.
func checkChatRequest(t *testing.T, req map[string]any) {
	t.Helper()
	msgs := list(req["messages"])
	for i, m := range msgs {
		msg := object(m)
		calls := list(msg["tool_calls"])
		if len(calls) == 0 {
			continue
		}
		answered := map[string]bool{}
		for j := i + 1; j < len(msgs) && object(msgs[j])["role"] == "tool"; j++ {
			if id, _ := object(msgs[j])["tool_call_id"].(string); id != "" {
				answered[id] = true
			}
		}
		for _, c := range calls {
			if id, _ := object(c)["id"].(string); !answered[id] {
				t.Errorf("chat: tool call %s is not answered", id)
			}
		}
	}
}

// checkResponsesRequest: every function_call input item is answered by a
// function_call_output with its call_id, and a replayed reasoning item carries
// its encrypted content and the summary array the API requires.
func checkResponsesRequest(t *testing.T, req map[string]any) {
	t.Helper()
	outputs := map[string]bool{}
	for _, it := range list(req["input"]) {
		item := object(it)
		if item["type"] == "function_call_output" {
			id, _ := item["call_id"].(string)
			outputs[id] = true
		}
	}
	for _, it := range list(req["input"]) {
		item := object(it)
		switch item["type"] {
		case "function_call":
			if id, _ := item["call_id"].(string); !outputs[id] {
				t.Errorf("responses: function_call %s has no function_call_output", id)
			}
		case "reasoning":
			if enc, _ := item["encrypted_content"].(string); enc == "" {
				t.Error("responses: replayed reasoning item without encrypted_content")
			}
			if _, ok := item["summary"]; !ok {
				t.Error("responses: replayed reasoning item without its summary array")
			}
		}
	}
}
