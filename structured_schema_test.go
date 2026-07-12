package llms

import (
	"encoding/json"
	"testing"
	"time"
)

// TestSchemaFrom_RecursiveEmbeddedDoesNotOverflow guards against a fatal stack
// overflow: an anonymous, self-referential embedded struct must terminate (the
// cyclic embed degrades to an unconstrained schema) rather than recurse forever.
func TestSchemaFrom_RecursiveEmbeddedDoesNotOverflow(t *testing.T) {
	type Node struct {
		*Node
		Value string `json:"value"`
	}
	raw, err := SchemaFrom[Node]()
	if err != nil {
		t.Fatalf("SchemaFrom[Node]: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("empty schema")
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["value"]; !ok {
		t.Errorf("own field 'value' missing from recursive-embed schema: %v", props)
	}
}

// TestSchemaFrom_EmbeddedMarshalerStructIsScalar documents that a struct
// embedding a json.Marshaler type (e.g. time.Time) promotes MarshalJSON to the
// outer struct, so json.Marshal produces a scalar — the schema must be that
// scalar, and the embedded type must never be flattened into an object (which
// would diverge from what the wire actually carries).
func TestSchemaFrom_EmbeddedMarshalerStructIsScalar(t *testing.T) {
	type Doc struct {
		time.Time
		Title string `json:"title"`
	}
	raw, err := SchemaFrom[Doc]()
	if err != nil {
		t.Fatalf("SchemaFrom: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if schema["type"] != "string" {
		t.Errorf("struct embedding time.Time marshals as a string scalar; schema = %v", schema)
	}
	if _, hasProps := schema["properties"]; hasProps {
		t.Errorf("embedded-Marshaler struct must not be flattened into an object: %v", schema)
	}
}

// TestSchemaFrom_MapEmitsUnconstrained pins that a map field maps to an
// unconstrained {} (as documented), not an additionalProperties subschema that
// OpenAI strict mode rejects with a 400.
func TestSchemaFrom_MapEmitsUnconstrained(t *testing.T) {
	type WithMap struct {
		Labels map[string]string `json:"labels"`
	}
	raw, err := SchemaFrom[WithMap]()
	if err != nil {
		t.Fatalf("SchemaFrom: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)
	labels, _ := props["labels"].(map[string]any)
	if labels == nil {
		t.Fatalf("no labels property: %s", raw)
	}
	if _, hasAP := labels["additionalProperties"]; hasAP {
		t.Errorf("map field emitted additionalProperties (strict-invalid): %v", labels)
	}
	if _, hasType := labels["type"]; hasType {
		t.Errorf("map field should be unconstrained {}, got %v", labels)
	}
	if len(labels) != 0 {
		t.Errorf("map field should be unconstrained {}, got %v", labels)
	}
}

// TestSchemaFrom_FlattensEmbeddedStruct pins that an anonymous embedded struct is
// flattened into the parent (mirroring encoding/json promotion), so the schema's
// shape matches what json.Unmarshal expects and no promoted field is silently
// dropped.
func TestSchemaFrom_FlattensEmbeddedStruct(t *testing.T) {
	type Meta struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	type Doc struct {
		Meta
		Title string `json:"title"`
	}
	raw, err := SchemaFrom[Doc]()
	if err != nil {
		t.Fatalf("SchemaFrom: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)

	if _, nested := props["Meta"]; nested {
		t.Errorf("embedded struct not flattened (nested Meta present): %v", props)
	}
	for _, want := range []string{"id", "version", "title"} {
		if _, ok := props[want]; !ok {
			t.Errorf("field %q not present at top level: %v", want, props)
		}
	}

	// required must include the promoted fields and the parent field.
	reqSet := map[string]bool{}
	if reqs, ok := schema["required"].([]any); ok {
		for _, r := range reqs {
			if s, ok := r.(string); ok {
				reqSet[s] = true
			}
		}
	}
	for _, want := range []string{"id", "version", "title"} {
		if !reqSet[want] {
			t.Errorf("required missing %q: %v", want, schema["required"])
		}
	}

	// The schema's flat shape is exactly what json.Unmarshal into Doc expects:
	// a model obeying the schema returns flat JSON, and nothing is dropped.
	var d Doc
	if err := json.Unmarshal([]byte(`{"id":"x","version":3,"title":"t"}`), &d); err != nil {
		t.Fatalf("flat round-trip unmarshal: %v", err)
	}
	if d.ID != "x" || d.Version != 3 || d.Title != "t" {
		t.Errorf("flat round-trip dropped data: %+v", d)
	}
}

// TestSchemaFrom_OuterShadowsPromotedEmbeddedField pins that an outer field
// shadows a promoted embedded field of the same json name (shallower wins,
// matching encoding/json) and appears exactly once in required (dedup).
func TestSchemaFrom_OuterShadowsPromotedEmbeddedField(t *testing.T) {
	type Meta struct {
		ID string `json:"id"`
	}
	type Doc struct {
		Meta
		ID string `json:"id"` // shadows Meta.ID (shallower wins)
	}
	raw, err := SchemaFrom[Doc]()
	if err != nil {
		t.Fatalf("SchemaFrom: %v", err)
	}
	var schema map[string]any
	_ = json.Unmarshal(raw, &schema)

	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["id"]; !ok {
		t.Errorf("shadowed field 'id' missing: %v", props)
	}
	count := 0
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			if r == "id" {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("'id' should appear exactly once in required (dedup), got %d: %v", count, schema["required"])
	}
}

// TestSchemaFrom_EmbeddedWithJSONNameIsNamedField pins that an embedded struct
// carrying an explicit json name is treated as a normal named field (not
// flattened), matching encoding/json.
func TestSchemaFrom_EmbeddedWithJSONNameIsNamedField(t *testing.T) {
	type Meta struct {
		ID string `json:"id"`
	}
	type Doc struct {
		Meta  `json:"meta"`
		Title string `json:"title"`
	}
	raw, err := SchemaFrom[Doc]()
	if err != nil {
		t.Fatalf("SchemaFrom: %v", err)
	}
	var schema map[string]any
	_ = json.Unmarshal(raw, &schema)
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["meta"]; !ok {
		t.Errorf("named-embedded struct should appear as %q object: %v", "meta", props)
	}
	if _, ok := props["id"]; ok {
		t.Errorf("named-embedded struct must NOT be flattened: %v", props)
	}
}
