package llms

import (
	"context"
	"encoding/json"
	"reflect"
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
		response: &Response{Content: `{"name":"Ada","age":36,"active":true,"tags":[],"note":null}`},
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
	if !strings.Contains(err.Error(), "does not match the schema") {
		t.Fatalf("expected a schema-mismatch error, got %v", err)
	}
	// The decoder's own diagnosis has to survive: the repair turn quotes it.
	if !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("expected the decoder's reason to be preserved, got %v", err)
	}
}

// A scalar T must accept a fenced JSON string; extractJSON only scans objects
// and arrays, so a string result previously failed even though it was valid JSON.
func TestGenerateTyped_FencedScalar(t *testing.T) {
	mock := &structuredMockLLM{response: &Response{Content: "```json\n\"Ada\"\n```"}}
	got, _, err := GenerateTyped[string](context.Background(), mock, []Message{{Role: RoleUser, Content: "name"}})
	if err != nil {
		t.Fatalf("GenerateTyped[string] failed: %v", err)
	}
	if got != "Ada" {
		t.Errorf("got %q, want Ada", got)
	}
}

// T = any has no struct schema, and json.Unmarshal into an interface that
// receives JSON null leaves a nil interface. reflect.TypeOf then returns nil,
// so the struct-field check must not dereference it.
func TestParseTyped_InterfaceTargetNull(t *testing.T) {
	got, err := parseTyped[any]("null")
	if err != nil {
		t.Fatalf("parseTyped[any](null) failed: %v", err)
	}
	if got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}

// The generated schema strips pointers and describes the pointee, so a null
// response does not satisfy it. GenerateTyped[*T] must reject null rather than
// returning a nil value that violates the advertised schema.
func TestParseTyped_PointerTargetRejectsNull(t *testing.T) {
	if _, err := parseTyped[*structuredTestResult](`null`); err == nil {
		t.Fatal("parseTyped[*T](null) succeeded, want a schema mismatch")
	}
	got, err := parseTyped[*structuredTestResult](`{"name":"Ada","age":36,"active":true,"tags":[],"note":null}`)
	if err != nil {
		t.Fatalf("parseTyped[*T] object failed: %v", err)
	}
	if got == nil || got.Name != "Ada" {
		t.Fatalf("got %+v", got)
	}
}

// json.Unmarshal matches struct fields case-insensitively, so a response using
// "Name" for a field tagged json:"name" decodes successfully. The struct
// candidate check must accept it too rather than forcing an unnecessary repair.
func TestParseTyped_CaseInsensitiveFieldMatch(t *testing.T) {
	got, err := parseTyped[structuredTestResult](`{"Name":"Ada","Age":36,"Active":true,"Tags":[],"Note":null}`)
	if err != nil {
		t.Fatalf("parseTyped case-insensitive field failed: %v", err)
	}
	if got.Name != "Ada" {
		t.Errorf("got %#v, want Name=Ada", got)
	}
}

// A struct with fields whose JSON names differ only by case must not let one
// key satisfy both. "foo" and "Foo" fold to one lower-case bucket, so a
// response naming just one of them used to satisfy both required fields and
// decode the other to its zero value.
func TestParseTyped_CaseOnlyDistinctFieldsRequireBothNames(t *testing.T) {
	type mixedCase struct {
		Lower string `json:"foo"`
		Upper string `json:"Foo"`
	}
	if _, err := parseTyped[mixedCase](`{"foo":"x"}`); err == nil {
		t.Error("object naming only foo accepted, want a missing-field error for Foo")
	}
	if _, err := parseTyped[mixedCase](`{"foo":"x","Foo":"y"}`); err != nil {
		t.Fatalf("object naming both case-distinct fields rejected: %v", err)
	}
	got, err := parseTyped[mixedCase](`{"foo":"x","Foo":"y"}`)
	if err != nil || got.Lower != "x" || got.Upper != "y" {
		t.Errorf("got %+v, err %v, want {x y}", got, err)
	}
}

// When the caller supplies their own JSON Schema, that schema is the contract.
// The generated schema marks every Go field required, so enforcing it here
// rejected a response the caller's schema deliberately made optional.
func TestGenerateTyped_CallerSchemaIsNotForcedToGeneratedShape(t *testing.T) {
	llm := &repairScriptLLM{responses: []string{`{"name":"Ada"}`}}
	_, _, err := GenerateTyped[structuredTestResult](
		context.Background(), llm,
		[]Message{{Role: RoleUser, Content: "x"}},
		WithJSONSchema("partial", json.RawMessage(`{"type":"object"}`), false),
	)
	if err != nil {
		t.Fatalf("GenerateTyped with a caller schema rejected a partial object: %v", err)
	}
	// Without a caller schema the generated required list still applies.
	llm2 := &repairScriptLLM{responses: []string{`{"name":"Ada"}`}}
	if _, _, err := GenerateTyped[structuredTestResult](
		context.Background(), llm2,
		[]Message{{Role: RoleUser, Content: "x"}},
	); err == nil {
		t.Error("GenerateTyped without a caller schema accepted a partial object, want a missing-field error")
	}
}

// A caller schema that permits JSON null for a concrete Go type is the contract
// the model was given. The generated schema describes a string as non-null, so
// its null check used to reject a response the caller's nullable schema allows.
func TestGenerateTyped_CallerSchemaMayPermitNull(t *testing.T) {
	nullable := json.RawMessage(`{"type":["string","null"]}`)
	llm := &repairScriptLLM{responses: []string{`null`}}
	got, _, err := GenerateTyped[string](
		context.Background(), llm,
		[]Message{{Role: RoleUser, Content: "x"}},
		WithJSONSchema("nullable", nullable, false),
	)
	if err != nil {
		t.Fatalf("GenerateTyped with a nullable caller schema rejected null: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want the zero string", got)
	}
	// Without a caller schema the generated non-null string schema still rejects
	// null rather than decoding it to a false success.
	llm2 := &repairScriptLLM{responses: []string{`null`}}
	if _, _, err := GenerateTyped[string](
		context.Background(), llm2,
		[]Message{{Role: RoleUser, Content: "x"}},
	); err == nil {
		t.Error("GenerateTyped without a caller schema accepted null, want a schema mismatch")
	}
}

// Prose can contain a valid scalar before the intended one (a bool before a
// quoted answer). Extraction must try every scalar candidate so a later one
// that matches T is not missed.
func TestParseTyped_TriesEveryScalarCandidate(t *testing.T) {
	got, err := parseTyped[string](`The answer is true, specifically "Ada"`)
	if err != nil {
		t.Fatalf("parseTyped[string] failed: %v", err)
	}
	if got != "Ada" {
		t.Errorf("got %q, want Ada", got)
	}
}

// json.Unmarshal accepts null into a non-pointer scalar, slice, array, or map
// and leaves a zero value. The generated schema describes each as non-null, so
// parsing must reject null rather than reporting a schema match.
func TestParseTyped_NonPointerScalarRejectsNull(t *testing.T) {
	if _, err := parseTyped[string]("null"); err == nil {
		t.Error("parseTyped[string](null) succeeded, want a schema mismatch")
	}
	if _, err := parseTyped[int]("null"); err == nil {
		t.Error("parseTyped[int](null) succeeded, want a schema mismatch")
	}
	if _, err := parseTyped[[]string]("null"); err == nil {
		t.Error("parseTyped[[]string](null) succeeded, want a schema mismatch")
	}
}

// schemaForType strips pointers, so *map[string]any and *any emit the same
// unconstrained `{}` schema as their pointees. A null is valid for them, and
// rejecting it would contradict the advertised schema.
func TestParseTyped_PointerToUnconstrainedAcceptsNull(t *testing.T) {
	if _, err := parseTyped[*map[string]any]("null"); err != nil {
		t.Errorf("parseTyped[*map[string]any](null) failed: %v", err)
	}
	if _, err := parseTyped[*any]("null"); err != nil {
		t.Errorf("parseTyped[*any](null) failed: %v", err)
	}
}

// A bare scalar embedded in prose ends at punctuation the JSON delimiter set
// does not include (`true.`), so extraction must still recover the valid prefix.
func TestParseTyped_BareScalarWithTrailingPunctuation(t *testing.T) {
	got, err := parseTyped[bool]("The answer is true.")
	if err != nil {
		t.Fatalf("parseTyped[bool] failed: %v", err)
	}
	if !got {
		t.Errorf("got %v, want true", got)
	}
	n, err := parseTyped[int]("The answer is 42.")
	if err != nil {
		t.Fatalf("parseTyped[int] failed: %v", err)
	}
	if n != 42 {
		t.Errorf("got %d, want 42", n)
	}
}

type structuredPromotedMeta struct {
	ID string `json:"id"`
}

// The generated schema marks every JSON-visible field required (omitempty and
// pointers included). Validation must reject a partial object rather than
// returning a zero value the schema forbids, both at the top level and inside
// nested objects and object slices.
func TestParseTyped_MissingRequiredFieldsRejected(t *testing.T) {
	if _, err := parseTyped[structuredTestResult](`{"name":"Ada"}`); err == nil {
		t.Error("partial object accepted, want a missing-field error")
	}
	if _, err := parseTyped[structuredTestResult](`{"name":"Ada","age":36,"active":true,"note":null}`); err == nil {
		t.Error("object missing tags accepted, want a missing-field error")
	}
	type nested struct {
		Inner structuredTestResult `json:"inner"`
	}
	if _, err := parseTyped[nested](`{"inner":{"name":"Ada"}}`); err == nil {
		t.Error("nested partial object accepted, want a missing-field error")
	}
	if _, err := parseTyped[[]structuredTestResult](`[{"name":"Ada"}]`); err == nil {
		t.Error("partial object in slice accepted, want a missing-field error")
	}
	if _, err := parseTyped[[][]structuredTestResult](`[[{"name":"Ada"}]]`); err == nil {
		t.Error("partial object in nested slice accepted, want a missing-field error")
	}
}

// json.RawMessage emits an unconstrained schema, so a null is valid for it even
// though it is not an interface or map.
func TestParseTyped_RawMessageAcceptsNull(t *testing.T) {
	if _, err := parseTyped[json.RawMessage]("null"); err != nil {
		t.Errorf("parseTyped[json.RawMessage](null) failed: %v", err)
	}
}

// The generated object schema sets additionalProperties:false, so a response
// naming a property the struct cannot populate violates the advertised contract
// even though json.Unmarshal would silently ignore it. Validation must reject it
// so the repair turn runs.
func TestParseTyped_RejectsUnexpectedProperties(t *testing.T) {
	if _, err := parseTyped[structuredTestResult](`{"name":"Ada","age":36,"active":true,"tags":[],"note":null,"unexpected":true}`); err == nil {
		t.Error("object with an unexpected property accepted, want a schema mismatch")
	}
	// A case-insensitive match is still a valid field, mirroring json.Unmarshal.
	if _, err := parseTyped[structuredTestResult](`{"Name":"Ada","Age":36,"Active":true,"Tags":[],"Note":null}`); err != nil {
		t.Errorf("case-insensitive fields rejected: %v", err)
	}
	// A nested unexpected property is rejected too.
	type nested struct {
		Count int `json:"count"`
	}
	type outer struct {
		Inner nested `json:"inner"`
	}
	if _, err := parseTyped[outer](`{"inner":{"count":1,"bogus":2}}`); err == nil {
		t.Error("nested object with an unexpected property accepted, want a schema mismatch")
	}
}

// json.Unmarshal accepts null into a nested non-pointer scalar and leaves a
// zero value, so `{"nested":{"count":null}}` would satisfy a schema that
// describes count as a non-null integer. Nested validation must reject it the
// same way the top-level and extracted-scalar paths do. A nested pointer field
// still accepts null because nil is its natural encoding.
func TestParseTyped_NestedNonNullableRejectsNull(t *testing.T) {
	type nested struct {
		Count int `json:"count"`
	}
	type outer struct {
		Name   string  `json:"name"`
		Note   *string `json:"note"`
		Nested nested  `json:"nested"`
	}
	if _, err := parseTyped[outer](`{"name":"Ada","note":null,"nested":{"count":null}}`); err == nil {
		t.Error("nested null count accepted, want a schema mismatch")
	}
	got, err := parseTyped[outer](`{"name":"Ada","note":null,"nested":{"count":7}}`)
	if err != nil {
		t.Fatalf("valid nested object failed: %v", err)
	}
	if got.Nested.Count != 7 || got.Note != nil {
		t.Errorf("got %+v, want Count=7 Note=nil", got)
	}

	type inSlice struct {
		Items []nested `json:"items"`
	}
	if _, err := parseTyped[inSlice](`{"items":[{"count":null}]}`); err == nil {
		t.Error("null inside an object slice accepted, want a schema mismatch")
	}
}

type structuredPromotedDoc struct {
	structuredPromotedMeta
	Name string `json:"name"`
}

// encoding/json promotes a field from an embedded struct, so a response naming
// the promoted JSON name decodes correctly. Validation must recognize the
// promoted name too, or it rejects a response the decoder would have populated.
func TestParseTyped_EmbeddedPromotedField(t *testing.T) {
	got, err := parseTyped[structuredPromotedDoc](`{"id":"x","name":"Ada"}`)
	if err != nil {
		t.Fatalf("parseTyped promoted field failed: %v", err)
	}
	if got.ID != "x" || got.Name != "Ada" {
		t.Errorf("got %+v, want ID=x Name=Ada", got)
	}
}

// A field declared directly on the outer struct shadows an embedded field with
// the same JSON name, and encoding/json decodes into the outer field. Recursive
// validation must pick the same field, or it applies the embedded field's schema
// to a value the decoder never sends there and rejects valid output.
func TestParseTyped_ShallowerFieldShadowsEmbedded(t *testing.T) {
	type inner struct {
		Name struct {
			ID string `json:"id"`
		} `json:"name"`
	}
	type outer struct {
		inner
		Name map[string]any `json:"name"`
	}
	// The outer map permits any object, so an arbitrary key must be accepted.
	if _, err := parseTyped[outer](`{"name":{"anything":1}}`); err != nil {
		t.Errorf("shadowing map field rejected: %v", err)
	}

	// The exported embedded form takes the promotion branch that used to win
	// over the shallower outer field.
	type outerExported struct {
		structuredPromotedShadow
		Name map[string]any `json:"name"`
	}
	if _, err := parseTyped[outerExported](`{"name":{"anything":1}}`); err != nil {
		t.Errorf("shadowing map field rejected for exported embed: %v", err)
	}
}

type structuredPromotedShadow struct {
	Name structuredShadowInner `json:"name"`
}

type structuredShadowInner struct {
	ID string `json:"id"`
}

type emptySchemaTarget struct{}

type allHiddenSchemaTarget struct {
	Hidden string `json:"-"`
}

// A struct with no JSON-visible fields has an object schema that permits only
// `{}`. Validation must accept that empty object rather than rejecting the one
// valid representation.
func TestParseTyped_EmptyStructAcceptsEmptyObject(t *testing.T) {
	if _, err := parseTyped[emptySchemaTarget](`{}`); err != nil {
		t.Errorf("empty struct: %v", err)
	}
	if _, err := parseTyped[allHiddenSchemaTarget](`{}`); err != nil {
		t.Errorf("all-hidden struct: %v", err)
	}
	if _, err := parseTyped[emptySchemaTarget](`{"x":1}`); err == nil {
		t.Error("empty struct accepted a non-empty object")
	}
}

// A nested object inside a valid enclosing JSON value is part of that value,
// not a separate top-level payload. Descending into the envelope would let an
// unrelated wrapper satisfy the schema with the nested object and return data
// the model did not present as the answer.
func TestParseTyped_RejectsNestedEnvelopeObject(t *testing.T) {
	_, err := parseTyped[structuredTestResult](`{"wrapper":{"name":"Ada","age":36}}`)
	if err == nil {
		t.Fatal("parseTyped accepted a nested object inside an unrelated envelope")
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

func (*structuredJSONMarshaler) UnmarshalJSON([]byte) error { return nil }

// structuredWriteOnlyMarshaler can produce JSON but cannot read it back.
type structuredWriteOnlyMarshaler struct{ Field string }

func (structuredWriteOnlyMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"written"`), nil
}

// structuredTextOnlyUnmarshaler can decode a quoted string but has no marshal
// method of either kind. encoding/json accepts it from a string, so the schema
// must ask for one rather than emitting an object schema the decode rejects.
type structuredTextOnlyUnmarshaler struct{ Value string }

func (v *structuredTextOnlyUnmarshaler) UnmarshalText(data []byte) error {
	v.Value = string(data)
	return nil
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

// TestSchemaFrom_TextOnlyUnmarshalerIsString pins the decode path: a type with
// only TextUnmarshaler is populated by encoding/json from a JSON string, so an
// object schema would make the SDK's own unmarshal fail.
func TestSchemaFrom_TextOnlyUnmarshalerIsString(t *testing.T) {
	type result struct {
		Value structuredTextOnlyUnmarshaler `json:"value"`
	}
	schema, err := SchemaFrom[result]()
	if err != nil {
		t.Fatalf("SchemaFrom returned error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	assertSchema(t, schemaProperties(t, decoded)["value"], map[string]any{"type": "string"})

	// The generated shape must actually decode through encoding/json.
	var out result
	if err := json.Unmarshal([]byte(`{"value":"hello"}`), &out); err != nil {
		t.Fatalf("string value rejected by the decoder: %v", err)
	}
	if out.Value.Value != "hello" {
		t.Errorf("decoded TextUnmarshaler = %#v, want hello", out.Value)
	}
}

// TestSchemaFrom_PromotedMarshalerIsString pins that a struct embedding a
// scalar marshaler (time.Time) is described as its scalar wire form, not as an
// object. encoding/json promotes the embedded MarshalJSON, so an object schema
// would not match what the type actually emits.
func TestSchemaFrom_PromotedMarshalerIsString(t *testing.T) {
	type result struct {
		When structuredEmbeddedTime `json:"when"`
	}
	schema, err := SchemaFrom[result]()
	if err != nil {
		t.Fatalf("SchemaFrom returned error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	assertSchema(t, schemaProperties(t, decoded)["when"], map[string]any{"type": "string"})
}

type structuredEmbeddedTime struct{ time.Time }

// A shallower promoted field shadows a deeper field with the same JSON name, so
// the schema must keep the shallow one rather than dropping the name as though
// the two were equally ambiguous.
func TestSchemaFrom_ShallowEmbeddedFieldWins(t *testing.T) {
	type Deep struct {
		Name string `json:"name"`
	}
	type Mid struct {
		Deep
	}
	type Shallow struct {
		Name int `json:"name"`
	}
	type Doc struct {
		Mid
		Shallow
	}
	schema, err := SchemaFrom[Doc]()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatal(err)
	}
	assertSchema(t, schemaProperties(t, decoded)["name"], map[string]any{"type": "integer"})
}

// TestSchemaFrom_JSONNumberIsNumeric pins that json.Number is described as a
// JSON number. It implements both marshaler interfaces, but its wire form is a
// number, so a string schema makes the model return a quoted value that
// json.Number.UnmarshalJSON rejects.
func TestSchemaFrom_JSONNumberIsNumeric(t *testing.T) {
	type result struct {
		Value json.Number `json:"value"`
	}
	schema, err := SchemaFrom[result]()
	if err != nil {
		t.Fatalf("SchemaFrom: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	assertSchema(t, schemaProperties(t, decoded)["value"], map[string]any{"type": "number"})
}

// TestSchemaFrom_WriteOnlyMarshalerKeepsStructuralSchema pins that a type which
// marshals to a string but cannot unmarshal one is described structurally. The
// schema used to ask the model for a string the SDK then failed to decode.
func TestSchemaFrom_WriteOnlyMarshalerKeepsStructuralSchema(t *testing.T) {
	type result struct {
		Value structuredWriteOnlyMarshaler `json:"value"`
	}

	schema, err := SchemaFrom[result]()
	if err != nil {
		t.Fatalf("SchemaFrom returned error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	value, ok := schemaProperties(t, decoded)["value"].(map[string]any)
	if !ok {
		t.Fatalf("value property is not an object")
	}
	if value[schemaKeyType] == schemaTypeString {
		t.Error("a write-only marshaler was described as a string the SDK cannot decode")
	}
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
		// reflect.DeepEqual, not !=: a nested schema value is itself a map, and
		// comparing maps with != panics. DeepEqual handles scalars and nested
		// objects alike.
		if !reflect.DeepEqual(actualMap[key], expectedValue) {
			t.Fatalf("expected schema %#v, got %#v", expected, actualMap)
		}
	}
}

type structuredMockLLM struct {
	response *Response
	opts     *CallOptions
}

// embeddedCustomUnmarshaler consumes the whole outer object through the
// promoted UnmarshalJSON, so its exported fields are never decoded
// individually. The schema must not advertise them as flattened properties.
type embeddedCustomUnmarshaler struct{ V string }

func (e *embeddedCustomUnmarshaler) UnmarshalJSON([]byte) error { e.V = "custom"; return nil }

func TestSchemaFrom_EmbeddedCustomUnmarshalerNotFlattened(t *testing.T) {
	type outer struct {
		embeddedCustomUnmarshaler
		Other string `json:"other"`
	}
	schema, err := schemaForType(reflect.TypeOf(outer{}), map[reflect.Type]bool{})
	if err != nil {
		t.Fatalf("schemaForType: %v", err)
	}
	// The promoted UnmarshalJSON consumes the whole outer object, so no
	// structural schema describes what the decoder accepts. It must emit an
	// unconstrained schema rather than advertising V or other as required keys.
	if len(schema) != 0 {
		t.Fatalf("expected an unconstrained schema, got %#v", schema)
	}

	// Validation must agree: an object the custom decoder accepts is not
	// rejected for missing the structural fields the schema no longer advertises.
	if _, err := parseTyped[outer](`{"anything":1}`); err != nil {
		t.Errorf("custom-decoded object rejected: %v", err)
	}
}

type unexportedEmbedInner struct {
	A      string `json:"a"`
	hidden string
}

type unexportedEmbedOuter struct {
	unexportedEmbedInner
}

// Two embedded structs that promote the same JSON name are ambiguous to
// encoding/json, which drops both. The schema must drop the field too rather
// than ask the model for a value Unmarshal silently discards. The type is built
// with reflect.StructOf because go vet rejects a literal struct with duplicate
// JSON tags, which is exactly the shape under test.
// An unexported anonymous struct still promotes its exported fields through
// encoding/json, so the schema must include them.
func TestSchemaFrom_UnexportedEmbeddedStructPromoted(t *testing.T) {
	schema, err := schemaForType(reflect.TypeOf(unexportedEmbedOuter{}), map[reflect.Type]bool{})
	if err != nil {
		t.Fatalf("schemaForType: %v", err)
	}
	properties := schemaProperties(t, schema)
	if _, ok := properties["a"]; !ok {
		t.Fatalf("promoted exported field missing from schema: %#v", properties)
	}
	if _, ok := properties["hidden"]; ok {
		t.Fatalf("unexported field leaked into schema: %#v", properties)
	}
}

// TestSchemaFrom_UnexportedPointerEmbeddedStructPromoted pins the pointer form
// of promotion. encoding/json promotes exported fields through an unexported
// anonymous *inner, so the schema must include them as well.
func TestSchemaFrom_UnexportedPointerEmbeddedStructPromoted(t *testing.T) {
	type outer struct {
		*unexportedEmbedInner
	}
	schema, err := schemaForType(reflect.TypeOf(outer{}), map[reflect.Type]bool{})
	if err != nil {
		t.Fatalf("schemaForType: %v", err)
	}
	properties := schemaProperties(t, schema)
	if _, ok := properties["a"]; !ok {
		t.Fatalf("promoted exported field missing from schema: %#v", properties)
	}
}

// Two direct fields sharing a JSON name are ambiguous to encoding/json; the
// schema must drop the name rather than require a value Unmarshal discards.
func TestSchemaFrom_DuplicateDirectJSONNamesDropped(t *testing.T) {
	// reflect.StructOf avoids a literal duplicate json tag, which go vet flags in
	// source even though the ambiguity is exactly what this test exercises.
	dup := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeOf(""), Tag: `json:"x"`},
		{Name: "B", Type: reflect.TypeOf(""), Tag: `json:"x"`},
		{Name: "C", Type: reflect.TypeOf(""), Tag: `json:"keep"`},
	})
	schema, err := schemaForType(dup, map[reflect.Type]bool{})
	if err != nil {
		t.Fatalf("schemaForType: %v", err)
	}
	properties := schemaProperties(t, schema)
	if _, ok := properties["x"]; ok {
		t.Errorf("ambiguous name x kept in schema: %#v", properties)
	}
	if _, ok := properties["keep"]; !ok {
		t.Errorf("unambiguous field missing: %#v", properties)
	}
	// The schema builder's required list stores names in its own []string form;
	// assert membership directly rather than through the []any helper.
	required, _ := schema["required"].([]string)
	if len(required) != 1 || required[0] != "keep" {
		t.Errorf("required = %v, want [keep]", required)
	}
}

func TestSchemaFrom_AmbiguousEmbeddedFieldsDropped(t *testing.T) {
	embedded := reflect.StructOf([]reflect.StructField{{
		Name: "Name",
		Type: reflect.TypeOf(""),
		Tag:  `json:"name"`,
	}})
	top := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: embedded, Anonymous: true},
		{Name: "B", Type: embedded, Anonymous: true},
		{Name: "Other", Type: reflect.TypeOf(""), Tag: `json:"other"`},
	})

	schema, err := schemaForType(top, map[reflect.Type]bool{})
	if err != nil {
		t.Fatalf("schemaForType returned error: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing properties: %#v", schema)
	}
	if _, ok := properties["name"]; ok {
		t.Fatalf("ambiguous promoted field %q must be dropped, got %#v", "name", properties["name"])
	}
	if _, ok := properties["other"]; !ok {
		t.Fatalf("non-ambiguous field %q missing from schema", "other")
	}
	if required, ok := schema["required"].([]string); ok {
		for _, r := range required {
			if r == "name" {
				t.Fatalf("ambiguous field must not be required, got %#v", required)
			}
		}
	}
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
