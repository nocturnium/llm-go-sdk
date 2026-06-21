package openaicompat

import (
	"encoding/json"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

func TestBuildChatRequest_JSONSchemaResponseFormat(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	opts := llms.ApplyOptions(llms.WithJSONSchema("person", schema, true))

	req := BuildChatRequest("test-model", []llms.Message{{Role: llms.RoleUser, Content: "extract"}}, opts, false)
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	responseFormat, ok := decoded["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("expected response_format object, got %#v", decoded["response_format"])
	}
	if responseFormat["type"] != string(llms.ResponseFormatJSONSchema) {
		t.Fatalf("expected json_schema type, got %#v", responseFormat["type"])
	}

	jsonSchema, ok := responseFormat["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected json_schema object, got %#v", responseFormat["json_schema"])
	}
	if jsonSchema["name"] != "person" {
		t.Fatalf("expected schema name person, got %#v", jsonSchema["name"])
	}
	if jsonSchema["strict"] != true {
		t.Fatalf("expected strict true, got %#v", jsonSchema["strict"])
	}

	wireSchema, ok := jsonSchema["schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested schema object, got %#v", jsonSchema["schema"])
	}
	if wireSchema["type"] != "object" {
		t.Fatalf("expected object schema, got %#v", wireSchema["type"])
	}
}
