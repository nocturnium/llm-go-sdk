package llms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
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
	if !requiredSet["age"] || !requiredSet["tags"] || !requiredSet["note"] {
		t.Fatalf("expected omitempty and pointer fields to be required for strict mode, got %#v", requiredSet)
	}

	if decoded["additionalProperties"] != false {
		t.Fatalf("expected additionalProperties false, got %#v", decoded["additionalProperties"])
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
	if mock.opts.ResponseFormat.JSONSchema == nil || !mock.opts.ResponseFormat.JSONSchema.Strict {
		t.Fatal("expected GenerateTyped to pass a strict JSON schema")
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

type structuredStrictSchemaResult struct {
	Name   string                 `json:"name,omitempty"`
	Note   *string                `json:"note"`
	Nested structuredStrictNested `json:"nested"`
}

type structuredStrictNested struct {
	Count int `json:"count,omitempty"`
}

func TestSchemaFrom_StrictSchema(t *testing.T) {
	schema, err := SchemaFrom[structuredStrictSchemaResult]()
	if err != nil {
		t.Fatalf("SchemaFrom returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	requireAdditionalPropertiesFalse(t, decoded)
	requireRequiredFields(t, decoded, "name", "note", "nested")

	properties := schemaProperties(t, decoded)
	nested, ok := properties["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested object schema, got %#v", properties["nested"])
	}
	requireAdditionalPropertiesFalse(t, nested)
	requireRequiredFields(t, nested, "count")
}

type structuredStdlibSchemaResult struct {
	When time.Time       `json:"when"`
	Data []byte          `json:"data"`
	Raw  json.RawMessage `json:"raw"`
}

func TestSchemaFrom_StdlibFieldTypes(t *testing.T) {
	schema, err := SchemaFrom[structuredStdlibSchemaResult]()
	if err != nil {
		t.Fatalf("SchemaFrom returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	properties := schemaProperties(t, decoded)
	assertSchema(t, properties["when"], map[string]any{"type": "string", "format": "date-time"})
	assertSchema(t, properties["data"], map[string]any{"type": "string"})
	assertSchema(t, properties["raw"], map[string]any{})
}

type structuredTextMarshaler string

func (v structuredTextMarshaler) MarshalText() ([]byte, error) {
	return []byte(v), nil
}

type structuredJSONMarshaler struct{}

func (*structuredJSONMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"custom"`), nil
}

type structuredMarshalerSchemaResult struct {
	Text structuredTextMarshaler `json:"text"`
	JSON structuredJSONMarshaler `json:"json"`
}

func TestSchemaFrom_MarshalerTypes(t *testing.T) {
	schema, err := SchemaFrom[structuredMarshalerSchemaResult]()
	if err != nil {
		t.Fatalf("SchemaFrom returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	properties := schemaProperties(t, decoded)
	assertSchema(t, properties["text"], map[string]any{"type": "string"})
	assertSchema(t, properties["json"], map[string]any{"type": "string"})
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties object, got %#v", schema["properties"])
	}
	return properties
}

func requireAdditionalPropertiesFalse(t *testing.T, schema map[string]any) {
	t.Helper()
	if schema["additionalProperties"] != false {
		t.Fatalf("expected additionalProperties false, got %#v", schema["additionalProperties"])
	}
}

func requireRequiredFields(t *testing.T, schema map[string]any, names ...string) {
	t.Helper()
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("expected required array, got %#v", schema["required"])
	}
	requiredSet := make(map[string]bool)
	for _, value := range required {
		requiredSet[value.(string)] = true
	}
	for _, name := range names {
		if !requiredSet[name] {
			t.Fatalf("expected required field %q, got %#v", name, requiredSet)
		}
	}
	if len(requiredSet) != len(names) {
		t.Fatalf("expected required fields %#v, got %#v", names, requiredSet)
	}
}

func assertSchema(t *testing.T, actual any, expected map[string]any) {
	t.Helper()
	actualMap, ok := actual.(map[string]any)
	if !ok {
		t.Fatalf("expected schema object, got %#v", actual)
	}
	if len(actualMap) != len(expected) {
		t.Fatalf("expected schema %#v, got %#v", expected, actualMap)
	}
	for key, expectedValue := range expected {
		if actualMap[key] != expectedValue {
			t.Fatalf("expected schema %#v, got %#v", expected, actualMap)
		}
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
