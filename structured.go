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
// has no closed JSON Schema shape, json.RawMessage, interface{}, and maps with
// arbitrary values, are mapped to an unconstrained schema ({}), which some strict
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
	directErr := json.Unmarshal([]byte(content), &value)
	if directErr == nil {
		return value, nil
	}
	// Keep the decoder's own diagnosis: it names the field and the types that
	// did not match, which is what the repair turn has to tell the model. A
	// constant string sent the model back to fix "invalid JSON" when the JSON
	// was valid and only a field's type was wrong.
	reason := directErr
	for _, extracted := range extractJSONCandidates(content) {
		var v T
		extractedErr := json.Unmarshal([]byte(extracted), &v)
		if extractedErr == nil {
			return v, nil
		}
		reason = extractedErr
	}
	// A scalar T (string, number, bool) has no braces to scan for, but the
	// response may still be a quoted scalar inside a markdown fence or prose.
	if extracted, ok := extractScalarJSONOutsideStructures(content); ok {
		var v T
		if err := json.Unmarshal([]byte(extracted), &v); err == nil {
			return v, nil
		}
	}
	var zero T
	return zero, fmt.Errorf("llms: structured output does not match the schema: %w", reason)
}

// extractScalarJSON finds the first valid JSON scalar (string, number, bool, or
// null) embedded in s. Strings are found by their quotes; bare scalars are tried
// at each position that could begin one. It returns ok=false when none parses.
func extractScalarJSON(s string) (string, bool) {
	return extractScalarJSONOutsideStructures(s)
}

// extractScalarJSONOutsideStructures finds a scalar only outside balanced JSON
// objects and arrays. Without that boundary a string schema could consume an
// object key (for example `"name"` from `{"name":"Ada"}`) and report success
// with a value that is not the response.
func extractScalarJSONOutsideStructures(s string) (string, bool) {
	for i := 0; i < len(s); {
		if s[i] == '{' || s[i] == '[' {
			if candidate, ok := balancedJSONAt(s, i); ok {
				i += len(candidate)
				continue
			}
			// An unbalanced structure means the response is malformed. Do not
			// scan inside it and mistake an object key for a scalar result.
			return "", false
		}
		switch s[i] {
		case '"':
			if candidate, ok := quotedJSONAt(s, i); ok {
				return candidate, true
			}
		case 't', 'f', 'n', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			end := i
			for end < len(s) && !isJSONScalarDelimiter(s[end]) {
				end++
			}
			if end > i {
				candidate := s[i:end]
				if json.Valid([]byte(candidate)) {
					return candidate, true
				}
			}
		}
		i++
	}
	return "", false
}

func isJSONScalarDelimiter(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == '}' || c == ']' || c == '`'
}

// quotedJSONAt scans a JSON string starting at start and returns it when it is a
// well-formed string literal.
func quotedJSONAt(s string, start int) (string, bool) {
	escaped := false
	for i := start + 1; i < len(s); i++ {
		switch {
		case escaped:
			escaped = false
		case s[i] == '\\':
			escaped = true
		case s[i] == '"':
			candidate := s[start : i+1]
			if json.Valid([]byte(candidate)) {
				return candidate, true
			}
			return "", false
		}
	}
	return "", false
}

// repairPrompt is the correction instruction sent to the model on a repair turn.
func repairPrompt(parseErr error) string {
	return "Your previous reply could not be parsed as JSON matching the required schema (" +
		parseErr.Error() + "). Reply again with ONLY the corrected JSON value, no prose, " +
		"no markdown code fences, no explanation."
}

// extractJSON finds the first balanced JSON object or array embedded in s that
// also parses as JSON, ignoring braces inside strings. It handles markdown code
// fences and prose around the JSON. Candidates are tried left to right: prose
// such as "Here is [the JSON] {"name":"Ada"}" has a balanced but invalid first
// bracket pair, and the scanner must continue to the valid object after it. The
// boolean is false when no balanced, parseable JSON value is found.
func extractJSON(s string) (string, bool) {
	candidates := extractJSONCandidates(s)
	if len(candidates) == 0 {
		return "", false
	}
	return candidates[0], true
}

// extractJSONCandidates returns every balanced JSON object or array embedded in
// s that parses as JSON, in left-to-right order. Callers that unmarshal into a
// concrete type must try each one: a valid but unrelated value earlier in prose
// must not mask the value that actually matches the schema.
func extractJSONCandidates(s string) []string {
	var candidates []string
	for i := 0; i < len(s); i++ {
		if s[i] != '{' && s[i] != '[' {
			continue
		}
		if candidate, ok := balancedJSONAt(s, i); ok {
			if json.Valid([]byte(candidate)) {
				candidates = append(candidates, candidate)
			}
		}
	}
	return candidates
}

// balancedJSONAt scans from a bracketing byte at start and returns the shortest
// balanced object/array beginning there, ignoring structural characters inside
// string literals.
func balancedJSONAt(s string, start int) (string, bool) {
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

// JSON Schema keyword and type names used when building a schema from a Go type.
const (
	schemaKeyType    = "type"
	schemaTypeString = "string"
)

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
		return map[string]any{schemaKeyType: "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return map[string]any{schemaKeyType: "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{schemaKeyType: "number"}, nil
	case reflect.String:
		return map[string]any{schemaKeyType: schemaTypeString}, nil
	case reflect.Slice, reflect.Array:
		// A recursive slice type (type S []S, or a cycle reached through slice
		// elements) would otherwise recurse forever: the seen guard is set only
		// for structs, so a slice never marks itself. Mark it for the duration of
		// the element lookup so the cycle terminates with an unconstrained schema.
		seen[typ] = true
		itemSchema, err := schemaForType(typ.Elem(), seen)
		delete(seen, typ)
		if err != nil {
			return nil, err
		}
		return map[string]any{schemaKeyType: "array", "items": itemSchema}, nil
	case reflect.Map:
		// A map has arbitrary string keys, which OpenAI strict mode cannot express
		// (it requires additionalProperties:false on every object). Emit an
		// unconstrained schema, matching json.RawMessage/interface{} and the
		// documented GenerateTyped behavior, rather than an additionalProperties
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
	properties, _, err := schemaForStructDepth(typ, seen)
	if err != nil {
		return nil, err
	}
	required := make([]string, 0, len(properties))
	for name := range properties {
		required = appendUnique(required, name)
	}
	schema := map[string]any{
		schemaKeyType:          "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema, nil
}

// schemaField is one JSON-named property discovered while walking a struct.
// Depth is relative to the struct at the root of the current walk: direct fields
// are depth 0, fields promoted through one embedding are depth 1, and so on.
// Count records how many fields were found at the shallowest depth; more than one
// is ambiguous and encoding/json drops the name.
type schemaField struct {
	schema map[string]any
	depth  int
	count  int
}

// schemaForStructDepth returns the object properties for typ and the depth at
// which each property is promoted relative to typ. encoding/json chooses the
// shallowest field and treats equal-depth duplicates as ambiguous, so callers
// must preserve these depths when merging an embedded struct.
func schemaForStructDepth(typ reflect.Type, seen map[reflect.Type]bool) (map[string]any, map[string]int, error) {
	seen[typ] = true
	defer delete(seen, typ)

	fields := make(map[string]schemaField)

	add := func(name string, depth int, fieldSchema map[string]any) {
		prior, ok := fields[name]
		switch {
		case !ok || depth < prior.depth:
			fields[name] = schemaField{schema: fieldSchema, depth: depth, count: 1}
		case depth == prior.depth:
			prior.count++
			fields[name] = prior
		}
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			// An unexported anonymous struct is not itself reachable, but
			// encoding/json still promotes its exported fields to the parent, so
			// the schema must promote them too or the model will omit data that
			// Unmarshal would have accepted. Any other unexported field is
			// invisible to encoding/json and must stay out of the schema.
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if !field.Anonymous || embedded.Kind() != reflect.Struct {
				continue
			}
		}

		// Flatten embedded (anonymous) structs without an explicit json name,
		// mirroring encoding/json field promotion: the model must return the
		// promoted fields at the parent level or json.Unmarshal silently drops
		// them. An embedded field WITH a json name falls through to be treated as
		// a normal named field.
		if field.Anonymous {
			props, depths, ok, err := mergeEmbeddedSchema(field, seen)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				for k, v := range props {
					property, isMap := v.(map[string]any)
					if !isMap {
						continue
					}
					add(k, depths[k]+1, property)
				}
				continue
			}
		}

		name, skip := jsonFieldName(field)
		if skip {
			continue
		}

		fieldSchema, err := schemaForType(field.Type, seen)
		if err != nil {
			return nil, nil, err
		}
		// The json ",string" option makes encoding/json emit the field as a
		// quoted JSON string regardless of the Go type, so the model must be told
		// to return a string here or its numeric/bool value fails to decode.
		if jsonFieldStringOption(field) {
			fieldSchema = map[string]any{schemaKeyType: schemaTypeString}
		}
		add(name, 0, fieldSchema)
	}

	properties := make(map[string]any, len(fields))
	depths := make(map[string]int, len(fields))
	for name, field := range fields {
		if field.count > 1 {
			continue
		}
		properties[name] = field.schema
		depths[name] = field.depth
	}
	return properties, depths, nil
}

// mergeEmbeddedSchema returns the properties an anonymous embedded struct
// promotes, mirroring encoding/json promotion. ok is false when the field is not
// a promotable embedded struct, it has an explicit json name (then it is a
// normal named field) or its underlying type is not a struct, in which case the
// caller handles it as a normal field. The caller merges the returned properties
// so it can resolve same-depth conflicts, which encoding/json treats as
// ambiguous.
func mergeEmbeddedSchema(field reflect.StructField, seen map[reflect.Type]bool) (props map[string]any, depths map[string]int, ok bool, err error) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return nil, nil, true, nil // embedded but explicitly skipped
	}
	if tag != "" && strings.Split(tag, ",")[0] != "" {
		return nil, nil, false, nil // has an explicit json name, so a normal named field
	}

	ft := field.Type
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if ft.Kind() != reflect.Struct {
		return nil, nil, false, nil // embedded interface or named non-struct, so a normal field
	}
	// An embedded type with a special (non-object) schema, time.Time,
	// json.Marshaler/TextMarshaler, json.RawMessage, marshals as a scalar and
	// must stay unflattened rather than expand into its internal fields; treat it as a normal named
	// field so schemaForType applies the special schema.
	if _, special := specialSchemaForType(ft); special {
		return nil, nil, false, nil
	}
	// Recursion guard: schemaForStruct does not itself check seen (that guard
	// lives in schemaForType), so a self- or mutually-recursive embedded struct
	// would recurse forever and fatally overflow the stack. Fall back to the
	// normal field path, which routes through schemaForType's seen guard and
	// terminates with an unconstrained {}.
	if seen[ft] {
		return nil, nil, false, nil
	}

	props, depths, err = schemaForStructDepth(ft, seen)
	if err != nil {
		return nil, nil, true, err
	}
	return props, depths, true, nil
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
		return map[string]any{schemaKeyType: schemaTypeString}, true
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return map[string]any{schemaKeyType: schemaTypeString, "format": "date-time"}, true
	}
	// json.Number round-trips a JSON number, not a string, even though it
	// implements both Marshaler and Unmarshaler.
	if typ == reflect.TypeOf(json.Number("")) {
		return map[string]any{schemaKeyType: "number"}, true
	}
	// encoding/json decodes a type implementing only TextUnmarshaler from a
	// quoted string. There is no marshaler to inspect, but the decode path pins
	// the wire form, so describe it as a string rather than emitting an object
	// schema the SDK's own unmarshal would reject.
	if implementsTextUnmarshaler(typ) && !implementsJSONMarshaler(typ) {
		return map[string]any{schemaKeyType: schemaTypeString}, true
	}
	// A custom marshaler is described as a string only when it round-trips a
	// quoted string and its marshal output is actually a JSON string. A write-only
	// marshaler, or one emitting an object/array, must keep the structural schema
	// that encoding/json can decode from an object.
	if implementsMarshaler(typ) && implementsUnmarshaler(typ) && marshalsAsJSONString(typ) {
		return map[string]any{schemaKeyType: schemaTypeString}, true
	}
	return nil, false
}

func implementsMarshaler(typ reflect.Type) bool {
	return implementsJSONMarshaler(typ) || implementsTextMarshaler(typ)
}

func implementsJSONMarshaler(typ reflect.Type) bool {
	jsonMarshaler := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	return typ.Implements(jsonMarshaler) ||
		reflect.PointerTo(typ).Implements(jsonMarshaler)
}

// implementsJSONUnmarshaler reports whether a value of typ can be decoded from
// the JSON a custom JSON marshaler produces.
func implementsJSONUnmarshaler(typ reflect.Type) bool {
	jsonUnmarshaler := reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	return typ.Implements(jsonUnmarshaler) ||
		reflect.PointerTo(typ).Implements(jsonUnmarshaler)
}

func implementsTextMarshaler(typ reflect.Type) bool {
	textMarshaler := reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	return typ.Implements(textMarshaler) ||
		reflect.PointerTo(typ).Implements(textMarshaler)
}

// marshalsAsJSONString probes a marshaler's zero value. The reflection guard
// keeps a panic inside a third-party MarshalJSON from escaping into schema
// generation; an unprobeable type simply keeps its structural schema.
func marshalsAsJSONString(typ reflect.Type) (result bool) {
	defer func() {
		if recover() != nil {
			result = false
		}
	}()
	v := reflect.New(typ).Elem()
	m, ok := v.Interface().(json.Marshaler)
	if !ok {
		if !reflect.PointerTo(typ).Implements(reflect.TypeOf((*json.Marshaler)(nil)).Elem()) {
			return false
		}
		v = reflect.New(typ)
		m = v.Interface().(json.Marshaler)
	}
	data, err := m.MarshalJSON()
	if err != nil || len(data) == 0 {
		return false
	}
	return data[0] == '"'
}

// implementsUnmarshaler reports whether a value of typ can be decoded from the
// JSON a custom marshaler produces.
func implementsUnmarshaler(typ reflect.Type) bool {
	textUnmarshaler := reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	return implementsJSONUnmarshaler(typ) ||
		typ.Implements(textUnmarshaler) ||
		reflect.PointerTo(typ).Implements(textUnmarshaler)
}

// implementsTextUnmarshaler reports whether encoding/json can decode a value of
// typ from a quoted JSON string through TextUnmarshaler.
func implementsTextUnmarshaler(typ reflect.Type) bool {
	textUnmarshaler := reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	return typ.Implements(textUnmarshaler) ||
		reflect.PointerTo(typ).Implements(textUnmarshaler)
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

// jsonFieldStringOption reports whether the field's json tag carries the
// ",string" option, which makes encoding/json emit and expect a quoted string.
func jsonFieldStringOption(field reflect.StructField) bool {
	parts := strings.Split(field.Tag.Get("json"), ",")
	for _, opt := range parts[1:] {
		if opt == "string" {
			return true
		}
	}
	return false
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
