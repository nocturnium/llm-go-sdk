package llmadk

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// jsonNull is the JSON encoding of null, which a caller can hand over as a
// schema or as arguments meaning "none".
const jsonNull = "null"

// emptyObjectSchema is the parameter schema of a function that takes no
// arguments. Providers reject a missing schema more often than an empty one.
var emptyObjectSchema = json.RawMessage(`{"type":"object","properties":{}}`)

// schemaJSON renders one of the schema forms ADK puts in a request as JSON
// Schema. functiontool sets a *jsonschema.Schema, ADK's set_model_response a
// *genai.Schema, and hand-written declarations may use a map or raw JSON. A
// genai.Schema is converted rather than marshaled, because its JSON form uses
// Gemini's uppercase types ("OBJECT") and fields OpenAI-style APIs reject.
func schemaJSON(v any) (json.RawMessage, error) {
	switch s := v.(type) {
	case nil:
		return nil, nil
	case *genai.Schema:
		if s == nil {
			return nil, nil
		}
		return json.Marshal(genaiSchema(s))
	case genai.Schema:
		return json.Marshal(genaiSchema(&s))
	case json.RawMessage:
		return s, nil
	case []byte:
		return json.RawMessage(s), nil
	case string:
		return json.RawMessage(s), nil
	default:
		// *jsonschema.Schema and map[string]any marshal to JSON Schema as is.
		b, err := json.Marshal(s)
		if err != nil {
			return nil, fmt.Errorf("%w: schema of type %T: %w", ErrUnsupportedConfigField, v, err)
		}
		return b, nil
	}
}

// genaiSchema converts a genai.Schema, Gemini's OpenAPI subset, to a JSON
// Schema object. PropertyOrdering and Example have no JSON Schema meaning a
// provider acts on and are dropped.
func genaiSchema(s *genai.Schema) map[string]any {
	out := map[string]any{}
	typ := jsonSchemaType(s.Type)
	switch {
	case typ != "" && s.Nullable != nil && *s.Nullable:
		out["type"] = []string{typ, "null"}
	case typ != "":
		out["type"] = typ
	}
	if s.Title != "" {
		out["title"] = s.Title
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if s.Pattern != "" {
		out["pattern"] = s.Pattern
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Default != nil {
		out["default"] = s.Default
	}
	if s.Items != nil {
		out["items"] = genaiSchema(s.Items)
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for name, p := range s.Properties {
			if p != nil {
				props[name] = genaiSchema(p)
			}
		}
		out["properties"] = props
	} else if typ == "object" {
		out["properties"] = map[string]any{}
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	if len(s.AnyOf) > 0 {
		anyOf := make([]any, 0, len(s.AnyOf))
		for _, a := range s.AnyOf {
			if a != nil {
				anyOf = append(anyOf, genaiSchema(a))
			}
		}
		out["anyOf"] = anyOf
	}
	setInt := func(key string, v *int64) {
		if v != nil {
			out[key] = *v
		}
	}
	setInt("minItems", s.MinItems)
	setInt("maxItems", s.MaxItems)
	setInt("minLength", s.MinLength)
	setInt("maxLength", s.MaxLength)
	setInt("minProperties", s.MinProperties)
	setInt("maxProperties", s.MaxProperties)
	if s.Minimum != nil {
		out["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		out["maximum"] = *s.Maximum
	}
	return out
}

// jsonSchemaType maps a genai type to its JSON Schema name; unspecified maps
// to "", leaving the schema untyped.
func jsonSchemaType(t genai.Type) string {
	if t == "" || t == genai.TypeUnspecified {
		return ""
	}
	return strings.ToLower(string(t))
}
