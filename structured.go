package llms

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

var schemaCache sync.Map

// SchemaFrom returns a JSON Schema derived from T using reflection.
// Struct fields are mapped from their JSON tags; fields tagged "-" are skipped.
// All non-skipped fields are marked required for strict JSON Schema mode.
func SchemaFrom[T any]() (json.RawMessage, error) {
	typ := reflect.TypeOf((*T)(nil)).Elem()
	if cached, ok := schemaCache.Load(typ); ok {
		raw, _ := cached.(json.RawMessage)
		return cloneRawMessage(raw), nil
	}

	schema, err := schemaForType(typ, make(map[reflect.Type]bool))
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("llms: marshal schema for %s: %w", typ.String(), err)
	}

	raw := json.RawMessage(data)
	actual, _ := schemaCache.LoadOrStore(typ, raw)
	stored, _ := actual.(json.RawMessage)
	return cloneRawMessage(stored), nil
}

// RepairOptions configures the self-repair loop of GenerateTypedWithRepair.
type RepairOptions struct {
	// MaxRepairAttempts is the number of additional model calls to make after the
	// first response fails to parse into T. 0 means no model-driven repair (local
	// JSON extraction is still attempted). Each repair turn feeds the model its
	// previous output and the parse error and asks for corrected JSON.
	MaxRepairAttempts int
}

// GenerateTyped generates content with a JSON Schema response format and unmarshals
// the response content into T. If opts already include a json_schema response
// format, that schema is used; otherwise SchemaFrom[T] is applied automatically.
//
// If the model wraps its JSON in markdown fences or surrounding prose, GenerateTyped
// transparently extracts the embedded JSON before unmarshaling. For model-driven
// retries on persistent failures, use GenerateTypedWithRepair.
//
// The auto-generated schema is emitted in OpenAI strict mode. Fields whose Go type
// has no closed JSON Schema shape — json.RawMessage, interface{}, and maps with
// arbitrary values — are mapped to an unconstrained schema ({}), which some strict
// validators reject. For structs containing such fields, supply a hand-authored
// schema via WithJSONSchema instead of relying on the auto-strict path.
func GenerateTyped[T any](ctx context.Context, llm LLM, messages []Message, opts ...CallOption) (T, *Response, error) {
	return generateTyped[T](ctx, llm, messages, RepairOptions{}, opts...)
}

// GenerateTypedWithRepair behaves like GenerateTyped but, when the response fails
// to parse into T, sends the model its invalid output plus the parse error and asks
// it to return corrected JSON, up to repair.MaxRepairAttempts times. Local JSON
// extraction (stripping code fences/prose) is attempted on every response before a
// repair turn is spent, so malformed-wrapper cases cost no extra model calls.
//
// Transport errors are returned immediately and never trigger a repair turn.
func GenerateTypedWithRepair[T any](ctx context.Context, llm LLM, messages []Message, repair RepairOptions, opts ...CallOption) (T, *Response, error) {
	return generateTyped[T](ctx, llm, messages, repair, opts...)
}

func generateTyped[T any](ctx context.Context, llm LLM, messages []Message, repair RepairOptions, opts ...CallOption) (T, *Response, error) {
	var zero T

	applied := ApplyOptions(opts...)
	callOpts := opts
	if applied.ResponseFormat == nil || applied.ResponseFormat.Type != ResponseFormatJSONSchema {
		schema, err := SchemaFrom[T]()
		if err != nil {
			return zero, nil, err
		}
		callOpts = append(append([]CallOption(nil), opts...), WithJSONSchema(schemaNameForType(reflect.TypeOf((*T)(nil)).Elem()), schema, true))
	}

	attempts := max(repair.MaxRepairAttempts+1, 1)

	convo := append([]Message(nil), messages...)
	var lastResp *Response
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		resp, err := llm.GenerateContent(ctx, convo, callOpts...)
		if err != nil {
			return zero, resp, err
		}
		if resp == nil {
			return zero, nil, fmt.Errorf("llms: structured output response is nil")
		}
		lastResp = resp

		value, perr := parseTyped[T](resp.Content)
		if perr == nil {
			return value, resp, nil
		}
		lastErr = perr

		// Queue a repair turn unless this was the final attempt.
		if attempt < attempts-1 {
			convo = append(convo,
				Message{Role: RoleAssistant, Content: resp.Content},
				Message{Role: RoleUser, Content: repairPrompt(perr)},
			)
		}
	}

	return zero, lastResp, fmt.Errorf("llms: structured output invalid after %d attempt(s): %w", attempts, lastErr)
}

// parseTyped unmarshals content into T, first directly and then, if that fails,
// from JSON embedded in markdown fences or surrounding prose.
func parseTyped[T any](content string) (T, error) {
	var value T
	if err := json.Unmarshal([]byte(content), &value); err == nil {
		return value, nil
	}
	if extracted, ok := extractJSON(content); ok {
		var v T
		if err := json.Unmarshal([]byte(extracted), &v); err == nil {
			return v, nil
		}
	}
	var zero T
	return zero, fmt.Errorf("llms: structured output is not valid JSON")
}

// repairPrompt is the correction instruction sent to the model on a repair turn.
func repairPrompt(parseErr error) string {
	return "Your previous reply could not be parsed as JSON matching the required schema (" +
		parseErr.Error() + "). Reply again with ONLY the corrected JSON object — no prose, " +
		"no markdown code fences, no explanation."
}

// extractJSON finds the first balanced JSON object or array embedded in s,
// ignoring braces inside strings. It handles markdown code fences and prose
// around the JSON. The boolean is false when no balanced JSON value is found.
func extractJSON(s string) (string, bool) {
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return "", false
	}

	open := s[start]
	closeCh := byte('}')
	if open == '[' {
		closeCh = ']'
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// skip structural characters inside strings
		case c == open:
			depth++
		case c == closeCh:
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}

func schemaForType(typ reflect.Type, seen map[reflect.Type]bool) (map[string]any, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	if schema, ok := specialSchemaForType(typ); ok {
		return schema, nil
	}

	if seen[typ] {
		return map[string]any{}, nil
	}

	switch typ.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Slice, reflect.Array:
		itemSchema, err := schemaForType(typ.Elem(), seen)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": itemSchema}, nil
	case reflect.Map:
		// A map has arbitrary string keys, which OpenAI strict mode cannot express
		// (it requires additionalProperties:false on every object). Emit an
		// unconstrained schema — matching json.RawMessage/interface{} and the
		// documented GenerateTyped behavior — rather than an additionalProperties
		// subschema that strict validators reject with a 400.
		return map[string]any{}, nil
	case reflect.Struct:
		return schemaForStruct(typ, seen)
	case reflect.Interface:
		return map[string]any{}, nil
	default:
		return map[string]any{}, nil
	}
}

func schemaForStruct(typ reflect.Type, seen map[reflect.Type]bool) (map[string]any, error) {
	seen[typ] = true
	defer delete(seen, typ)

	properties := make(map[string]any)
	required := make([]string, 0)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}

		// Flatten embedded (anonymous) structs without an explicit json name,
		// mirroring encoding/json field promotion: the model must return the
		// promoted fields at the parent level or json.Unmarshal silently drops
		// them. An embedded field WITH a json name falls through to be treated as
		// a normal named field.
		if field.Anonymous {
			promoted, ok, err := mergeEmbeddedSchema(field, properties, seen)
			if err != nil {
				return nil, err
			}
			if ok {
				required = appendUnique(required, promoted...)
				continue
			}
		}

		name, skip := jsonFieldName(field)
		if skip {
			continue
		}

		fieldSchema, err := schemaForType(field.Type, seen)
		if err != nil {
			return nil, err
		}
		properties[name] = fieldSchema
		required = appendUnique(required, name)
	}

	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema, nil
}

// mergeEmbeddedSchema flattens an anonymous embedded struct field into the
// parent's properties, mirroring encoding/json promotion, and returns the
// promoted field names for the required list. ok is false when the field is not a
// promotable embedded struct — it has an explicit json name (then it is a normal
// named field) or its underlying type is not a struct — in which case the caller
// handles it as a normal field. Outer fields already present shadow promoted ones
// (shallower wins), matching encoding/json.
func mergeEmbeddedSchema(field reflect.StructField, properties map[string]any, seen map[reflect.Type]bool) (promoted []string, ok bool, err error) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return nil, true, nil // embedded but explicitly skipped
	}
	if tag != "" && strings.Split(tag, ",")[0] != "" {
		return nil, false, nil // has an explicit json name → normal named field
	}

	ft := field.Type
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if ft.Kind() != reflect.Struct {
		return nil, false, nil // embedded interface or named non-struct → normal field
	}
	// An embedded type with a special (non-object) schema — time.Time,
	// json.Marshaler/TextMarshaler, json.RawMessage — marshals as a scalar and
	// must NOT be flattened into its internal fields; treat it as a normal named
	// field so schemaForType applies the special schema.
	if _, special := specialSchemaForType(ft); special {
		return nil, false, nil
	}
	// Recursion guard: schemaForStruct does not itself check seen (that guard
	// lives in schemaForType), so a self- or mutually-recursive embedded struct
	// would recurse forever and fatally overflow the stack. Fall back to the
	// normal field path, which routes through schemaForType's seen guard and
	// terminates with an unconstrained {}.
	if seen[ft] {
		return nil, false, nil
	}

	embedded, err := schemaForStruct(ft, seen)
	if err != nil {
		return nil, true, err
	}
	if props, hasProps := embedded["properties"].(map[string]any); hasProps {
		for k, v := range props {
			if _, exists := properties[k]; !exists {
				properties[k] = v
				promoted = append(promoted, k)
			}
		}
	}
	return promoted, true, nil
}

// appendUnique appends each name to dst only if not already present.
func appendUnique(dst []string, names ...string) []string {
	for _, n := range names {
		found := false
		for _, existing := range dst {
			if existing == n {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, n)
		}
	}
	return dst
}

func specialSchemaForType(typ reflect.Type) (map[string]any, bool) {
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}, true
	}
	if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
		return map[string]any{"type": "string"}, true
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return map[string]any{"type": "string", "format": "date-time"}, true
	}
	if implementsMarshaler(typ) {
		return map[string]any{"type": "string"}, true
	}
	return nil, false
}

func implementsMarshaler(typ reflect.Type) bool {
	jsonMarshaler := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshaler := reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	return typ.Implements(jsonMarshaler) ||
		reflect.PointerTo(typ).Implements(jsonMarshaler) ||
		typ.Implements(textMarshaler) ||
		reflect.PointerTo(typ).Implements(textMarshaler)
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	if tag == "" {
		return field.Name, false
	}

	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = field.Name
	}
	return name, false
}

func schemaNameForType(typ reflect.Type) string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Name() != "" {
		return typ.Name()
	}
	return "response"
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}
