package llms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type structuredTestResult struct {
	Name   string   `json:"name"`
	Age    int      `json:"age,omitempty"`
	Active bool     `json:"active"`
	Tags   []string `json:"tags,omitempty"`
	Note   *string  `json:"note"`
	Hidden string   `json:"-"`
}

func TestSchemaFrom(t *testing.T) {
	schema, err := SchemaFrom[structuredTestResult]()
	if err != nil {
		t.Fatalf("SchemaFrom returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	if decoded["type"] != "object" {
		t.Fatalf("expected object schema, got %#v", decoded["type"])
	}

	properties, ok := decoded["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties object, got %#v", decoded["properties"])
	}
	for _, name := range []string{"name", "age", "active", "tags", "note"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("expected property %q in schema", name)
		}
	}
	if _, ok := properties["Hidden"]; ok {
		t.Fatal("json:- field should not be included")
	}

	required, ok := decoded["required"].([]any)
	if !ok {
		t.Fatalf("expected required array, got %#v", decoded["required"])
	}
	requiredSet := make(map[string]bool)
	for _, value := range required {
		requiredSet[value.(string)] = true
	}
	if !requiredSet["name"] || !requiredSet["active"] {
		t.Fatalf("expected name and active to be required, got %#v", requiredSet)
	}
	if requiredSet["age"] || requiredSet["tags"] || requiredSet["note"] {
		t.Fatalf("omitempty and pointer fields should not be required, got %#v", requiredSet)
	}

	cached, err := SchemaFrom[structuredTestResult]()
	if err != nil {
		t.Fatalf("second SchemaFrom returned error: %v", err)
	}
	if string(schema) != string(cached) {
		t.Fatalf("cached schema changed:\nfirst:  %s\nsecond: %s", schema, cached)
	}
}

func TestGenerateTyped(t *testing.T) {
	mock := &structuredMockLLM{
		response: &Response{Content: `{"name":"Ada","active":true}`},
	}

	value, resp, err := GenerateTyped[structuredTestResult](
		context.Background(),
		mock,
		[]Message{{Role: RoleUser, Content: "extract"}},
	)
	if err != nil {
		t.Fatalf("GenerateTyped returned error: %v", err)
	}
	if resp != mock.response {
		t.Fatal("expected raw response to be returned")
	}
	if value.Name != "Ada" || !value.Active {
		t.Fatalf("unexpected typed value: %#v", value)
	}
	if mock.opts == nil || mock.opts.ResponseFormat == nil {
		t.Fatal("expected GenerateTyped to pass a response format")
	}
	if mock.opts.ResponseFormat.Type != ResponseFormatJSONSchema {
		t.Fatalf("expected json_schema response format, got %q", mock.opts.ResponseFormat.Type)
	}
}

func TestGenerateTyped_InvalidJSON(t *testing.T) {
	mock := &structuredMockLLM{
		response: &Response{Content: `not json`},
	}

	_, resp, err := GenerateTyped[structuredTestResult](
		context.Background(),
		mock,
		[]Message{{Role: RoleUser, Content: "extract"}},
	)
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
	if resp != mock.response {
		t.Fatal("expected raw response to be returned with error")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("expected clear invalid JSON error, got %v", err)
	}
}

type structuredMockLLM struct {
	response *Response
	opts     *CallOptions
}

func (m *structuredMockLLM) Call(context.Context, string, ...CallOption) (string, error) {
	return "", nil
}

func (m *structuredMockLLM) GenerateContent(_ context.Context, _ []Message, opts ...CallOption) (*Response, error) {
	m.opts = ApplyOptions(opts...)
	return m.response, nil
}

func (m *structuredMockLLM) Stream(context.Context, []Message, ...CallOption) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

func (m *structuredMockLLM) Provider() Provider {
	return ProviderOpenAI
}

func (m *structuredMockLLM) Model() string {
	return "test-model"
}
