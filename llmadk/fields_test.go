package llmadk

import (
	"errors"
	"reflect"
	"testing"

	"google.golang.org/genai"
)

// A self-referential schema has no JSON Schema form; conversion must return an
// error rather than recursing until the stack overflows.
func TestSchemaJSON_RejectsCyclicSchema(t *testing.T) {
	s := &genai.Schema{Type: genai.TypeArray}
	s.Items = s
	if _, err := schemaJSON(s); err == nil {
		t.Fatal("cyclic schema converted without error")
	} else if !errors.Is(err, ErrUnsupportedConfigField) {
		t.Errorf("err = %v, want ErrUnsupportedConfigField", err)
	}

	prop := &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{}}
	prop.Properties["self"] = prop
	if _, err := schemaJSON(prop); err == nil {
		t.Fatal("cyclic property schema converted without error")
	}
}

// TestFieldHandlingCoversGenai is the tripwire for genai releases: every
// exported field of the request types the bridge reads must be classified in
// fieldHandling. A new field fails this test until someone decides whether the
// bridge translates, ignores or rejects it, so nothing is dropped silently.
func TestFieldHandlingCoversGenai(t *testing.T) {
	types := []any{
		genai.GenerateContentConfig{}, genai.HTTPOptions{}, genai.Content{}, genai.Part{},
		genai.FunctionCall{}, genai.FunctionResponse{}, genai.Tool{}, genai.FunctionDeclaration{},
		genai.ToolConfig{}, genai.FunctionCallingConfig{}, genai.ThinkingConfig{}, genai.Schema{},
	}
	seen := map[string]bool{}
	for _, v := range types {
		rt := reflect.TypeOf(v)
		for i := range rt.NumField() {
			f := rt.Field(i)
			if !f.IsExported() {
				continue
			}
			key := rt.Name() + "." + f.Name
			seen[key] = true
			if _, ok := fieldHandling[key]; !ok {
				t.Errorf("genai field %s is not classified in fieldHandling", key)
			}
		}
	}
	for key := range fieldHandling {
		if !seen[key] {
			t.Errorf("fieldHandling classifies %s, which genai no longer has", key)
		}
	}
}

// TestIgnorableFieldsAreRejectedByDefault keeps the Field constants and the
// table in step: every constant names a field the table rejects.
func TestIgnorableFieldsAreRejectedByDefault(t *testing.T) {
	for _, f := range []Field{
		FieldTopK, FieldSeed, FieldResponseLogprobs, FieldLogprobs, FieldSafetySettings, FieldCachedContent,
		FieldMediaResolution, FieldSpeechConfig, FieldAudioTimestamp, FieldImageConfig, FieldRoutingConfig,
		FieldModelSelectionConfig, FieldModelArmorConfig, FieldEnableEnhancedCivicAnswers, FieldServiceTier,
		FieldAudioTranscriptionConfig, FieldGoogleSearch, FieldRetrievalConfig, FieldServerSideToolInvocations,
	} {
		if h, ok := fieldHandling[string(f)]; !ok || h != rejected {
			t.Errorf("%s: handling %v, want rejected", f, h)
		}
	}
}
