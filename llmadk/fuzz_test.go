package llmadk

import (
	"encoding/json"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// FuzzTranslate feeds arbitrary genai requests, decoded from JSON, through the
// request translation: it must never panic, only translate or return an error.
func FuzzTranslate(f *testing.F) {
	f.Add([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	f.Add([]byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f","args":{"a":1}},"thoughtSignature":"bGxtZ28BeyJrIjoiY2FsbCJ9"}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{"x":null}}}]}],"config":{"thinkingConfig":{"thinkingBudget":-1},"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["f"]}}}}`))
	f.Add([]byte(`{"contents":[{"parts":[{"thought":true,"text":"t","partMetadata":{"llmgo.reasoning":42}},{"inlineData":{"mimeType":"image/png","data":"AA=="}}]}],"config":{"responseSchema":{"type":"OBJECT","properties":{"a":{"type":"ARRAY","items":{"type":"STRING"}}}},"responseMimeType":"application/json"}}`))
	m, err := NewModel(newFake())
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		var in struct {
			Contents []*genai.Content             `json:"contents"`
			Config   *genai.GenerateContentConfig `json:"config"`
		}
		if json.Unmarshal(raw, &in) != nil {
			return
		}
		_, _ = m.translate(&model.LLMRequest{Contents: in.Contents, Config: in.Config})
	})
}

// FuzzSchemaJSON checks that any genai schema converts to valid JSON.
func FuzzSchemaJSON(f *testing.F) {
	f.Add([]byte(`{"type":"OBJECT","nullable":true,"properties":{"a":{"type":"INTEGER","minimum":1}},"required":["a"],"anyOf":[{"type":"STRING"}]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var s genai.Schema
		if json.Unmarshal(raw, &s) != nil {
			return
		}
		out, err := schemaJSON(&s)
		if err != nil {
			return
		}
		if !json.Valid(out) {
			t.Errorf("invalid JSON %s for %s", out, raw)
		}
	})
}
