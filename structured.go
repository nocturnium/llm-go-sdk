package llms

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var schemaCache sync.Map

// SchemaFrom returns a JSON Schema derived from T using reflection.
// Struct fields are mapped from their JSON tags; fields tagged "-" are skipped.
// Fields without omitempty and non-pointer fields are marked required.
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

// GenerateTyped generates content with a JSON Schema response format and unmarshals
// the response content into T. If opts already include a json_schema response
// format, that schema is used; otherwise SchemaFrom[T] is applied automatically.
func GenerateTyped[T any](ctx context.Context, llm LLM, messages []Message, opts ...CallOption) (T, *Response, error) {
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

	resp, err := llm.GenerateContent(ctx, messages, callOpts...)
	if err != nil {
		return zero, resp, err
	}
	if resp == nil {
		return zero, nil, fmt.Errorf("llms: structured output response is nil")
	}

	var value T
	if err := json.Unmarshal([]byte(resp.Content), &value); err != nil {
		return zero, resp, fmt.Errorf("llms: structured output is not valid JSON: %w", err)
	}
	return value, resp, nil
}

func schemaForType(typ reflect.Type, seen map[reflect.Type]bool) (map[string]any, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
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
		valueSchema, err := schemaForType(typ.Elem(), seen)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "object", "additionalProperties": valueSchema}, nil
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

		name, omitEmpty, skip := jsonFieldName(field)
		if skip {
			continue
		}

		fieldSchema, err := schemaForType(field.Type, seen)
		if err != nil {
			return nil, err
		}
		properties[name] = fieldSchema

		if !omitEmpty && field.Type.Kind() != reflect.Pointer {
			required = append(required, name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema, nil
}

func jsonFieldName(field reflect.StructField) (string, bool, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	if tag == "" {
		return field.Name, false, false
	}

	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = field.Name
	}

	omitEmpty := false
	for _, part := range parts[1:] {
		if part == "omitempty" {
			omitEmpty = true
			break
		}
	}
	return name, omitEmpty, false
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
