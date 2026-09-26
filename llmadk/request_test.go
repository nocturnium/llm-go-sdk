package llmadk

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func newTestModel(t *testing.T, opts ...Option) *Model {
	t.Helper()
	m, err := NewModel(newFake(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func user(parts ...*genai.Part) *genai.Content {
	return &genai.Content{Role: string(genai.RoleUser), Parts: parts}
}

func modelTurn(parts ...*genai.Part) *genai.Content {
	return &genai.Content{Role: string(genai.RoleModel), Parts: parts}
}

func text(s string) *genai.Part { return &genai.Part{Text: s} }

func callPart(id, name string, args map[string]any) *genai.Part {
	return &genai.Part{FunctionCall: &genai.FunctionCall{ID: id, Name: name, Args: args}}
}

func responsePart(id, name string, resp map[string]any) *genai.Part {
	return &genai.Part{FunctionResponse: &genai.FunctionResponse{ID: id, Name: name, Response: resp}}
}

func translateOK(t *testing.T, m *Model, req *model.LLMRequest) *translation {
	t.Helper()
	tr, err := m.translate(req)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	return tr
}

func TestTranslate_Contents(t *testing.T) {
	m := newTestModel(t)

	t.Run("system, user, model and tool turns", func(t *testing.T) {
		tr := translateOK(t, m, &model.LLMRequest{
			Config: &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{text("be brief"), text("be kind")}}},
			Contents: []*genai.Content{
				user(text("hi "), text("there")),
				modelTurn(text("calling"), callPart("call_abc123", "f", map[string]any{"x": 1.0})),
				user(responsePart("call_abc123", "f", map[string]any{"y": 2.0}), text("and now?")),
			},
		})
		got := tr.messages
		if len(got) != 5 {
			t.Fatalf("messages = %+v", got)
		}
		if got[0].Role != llms.RoleSystem || got[0].Content != "be brief\n\nbe kind" {
			t.Errorf("system = %+v", got[0])
		}
		if got[1].Role != llms.RoleUser || got[1].Content != "hi there" {
			t.Errorf("user = %+v", got[1])
		}
		if got[2].Content != "calling" || got[2].ToolCalls[0].ID != "call_abc123" || got[2].ToolCalls[0].Function.Arguments != `{"x":1}` {
			t.Errorf("assistant = %+v", got[2])
		}
		if got[3].Role != llms.RoleTool || got[3].ToolCallID != "call_abc123" || got[3].Content != `{"y":2}` || got[3].Name != "f" {
			t.Errorf("tool result = %+v", got[3])
		}
		if got[4].Role != llms.RoleUser || got[4].Content != "and now?" {
			t.Errorf("tool results must precede the user's text: %+v", got[4])
		}
	})

	t.Run("empty role is user and user-role thoughts are dropped", func(t *testing.T) {
		tr := translateOK(t, m, &model.LLMRequest{Contents: []*genai.Content{
			{Parts: []*genai.Part{{Text: "foreign thought", Thought: true}, text("question")}},
		}})
		if len(tr.messages) != 1 || tr.messages[0].Role != llms.RoleUser || tr.messages[0].Content != "question" {
			t.Errorf("messages = %+v", tr.messages)
		}
	})

	t.Run("images and text artifacts", func(t *testing.T) {
		tr := translateOK(t, m, &model.LLMRequest{Contents: []*genai.Content{user(
			text("look"),
			&genai.Part{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{1, 2}}},
			&genai.Part{FileData: &genai.FileData{MIMEType: "image/jpeg", FileURI: "https://example.com/a.jpg"}},
			&genai.Part{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte("notes"), DisplayName: "notes.txt"}},
		)}})
		parts := tr.messages[0].Parts
		if len(parts) != 4 || parts[1].Image.Data != "AQI=" || parts[2].Image.Data != "https://example.com/a.jpg" ||
			parts[3].Text != "[notes.txt]\nnotes" {
			t.Errorf("parts = %+v", parts)
		}
		if !tr.hasImages {
			t.Error("hasImages not set")
		}
	})

	rejects := map[string]*genai.Part{
		"pdf":            {InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("x")}},
		"gs uri":         {FileData: &genai.FileData{MIMEType: "image/png", FileURI: "gs://b/o.png"}},
		"files api":      {FileData: &genai.FileData{MIMEType: "image/png", FileURI: "https://generativelanguage.googleapis.com/v1beta/files/x"}},
		"audio file":     {FileData: &genai.FileData{MIMEType: "audio/wav", FileURI: "https://example.com/a.wav"}},
		"code execution": {ExecutableCode: &genai.ExecutableCode{Code: "print(1)"}},
		"media response": {FunctionResponse: &genai.FunctionResponse{Name: "f", Parts: []*genai.FunctionResponsePart{{}}}},
	}
	for name, part := range rejects {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := m.translate(&model.LLMRequest{Contents: []*genai.Content{user(part)}})
			if !errors.Is(err, ErrUnsupportedPart) {
				t.Errorf("err = %v, want ErrUnsupportedPart", err)
			}
		})
	}

	t.Run("rejects media in a model turn", func(t *testing.T) {
		_, err := m.translate(&model.LLMRequest{Contents: []*genai.Content{
			modelTurn(&genai.Part{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{1}}}),
		}})
		if !errors.Is(err, ErrUnsupportedPart) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("nil and empty requests", func(t *testing.T) {
		if _, err := m.translate(nil); !errors.Is(err, ErrRequestNil) {
			t.Errorf("nil: %v", err)
		}
		if _, err := m.translate(&model.LLMRequest{}); !errors.Is(err, ErrNoContents) {
			t.Errorf("empty: %v", err)
		}
	})
}

// TestTranslate_ToolCallIDs covers history whose calls have no ID, as ADK sends
// it after stripping its own "adk-" IDs for providers that return none.
func TestTranslate_ToolCallIDs(t *testing.T) {
	m := newTestModel(t)
	req := &model.LLMRequest{Contents: []*genai.Content{
		user(text("q")),
		modelTurn(callPart("", "f", nil), callPart("", "f", nil), callPart("", "g", nil)),
		user(responsePart("", "f", map[string]any{"n": 1.0}), responsePart("", "f", map[string]any{"n": 2.0}), responsePart("", "g", nil)),
	}}
	first := translateOK(t, m, req).messages
	second := translateOK(t, m, req).messages

	calls := first[1].ToolCalls
	ids := map[string]bool{}
	for _, tc := range calls {
		if !nineAlnum(tc.ID) || ids[tc.ID] {
			t.Errorf("synthesized IDs not distinct nine-char IDs: %+v", calls)
		}
		ids[tc.ID] = true
	}
	for i, tc := range calls {
		if first[2+i].ToolCallID != tc.ID {
			t.Errorf("result %d paired with %q, want %q", i, first[2+i].ToolCallID, tc.ID)
		}
		if second[1].ToolCalls[i].ID != tc.ID {
			t.Errorf("IDs not deterministic: %q then %q", tc.ID, second[1].ToolCalls[i].ID)
		}
	}
	if first[2].Content != `{"n":1}` || first[3].Content != `{"n":2}` {
		t.Errorf("same-name results paired out of order: %q %q", first[2].Content, first[3].Content)
	}
}

func nineAlnum(s string) bool {
	if len(s) != 9 {
		return false
	}
	for _, c := range s {
		isAlnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !isAlnum {
			return false
		}
	}
	return true
}

func TestRepairToolResults(t *testing.T) {
	call := func(ids ...string) llms.Message {
		msg := llms.Message{Role: llms.RoleAssistant}
		for _, id := range ids {
			msg.ToolCalls = append(msg.ToolCalls, toolCall(id, "f", "{}"))
		}
		return msg
	}
	result := func(id, content string) llms.Message {
		return llms.Message{Role: llms.RoleTool, ToolCallID: id, Content: content}
	}
	got, dropped := repairToolResults([]llms.Message{
		result("stray", "before any call"),
		{Role: llms.RoleUser, Content: "q"},
		call("a", "b", "c"),
		result("c", "c1"),
		result("a", "a1"),
		result("zzz", "answers nothing"),
		result("a", "a2"),
		{Role: llms.RoleUser, Content: "next"},
	})
	if dropped != 2 {
		t.Errorf("dropped = %d, want the 2 orphans", dropped)
	}
	want := []string{"q", "", "a2", pendingResult, "c1", "next"}
	if len(got) != len(want) {
		t.Fatalf("got %d messages: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Content != w {
			t.Errorf("message %d = %q, want %q", i, got[i].Content, w)
		}
	}
	if got[3].ToolCallID != "b" {
		t.Errorf("placeholder answers %q, want b", got[3].ToolCallID)
	}
}

func TestTranslate_Config(t *testing.T) {
	contents := []*genai.Content{user(text("q"))}
	f32 := func(v float32) *float32 { return &v }
	i32 := func(v int32) *int32 { return &v }

	t.Run("translated sampling fields", func(t *testing.T) {
		timeout := 3 * time.Second
		tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{
			Temperature: f32(0.5), TopP: f32(0.9), MaxOutputTokens: 100, StopSequences: []string{"END"},
			PresencePenalty: f32(0.1), FrequencyPenalty: f32(0.2),
			Labels:      map[string]string{"adk_agent": "a"},
			HTTPOptions: &genai.HTTPOptions{Timeout: &timeout, Headers: map[string][]string{"x-goog": {"1"}}},
		}})
		o := llms.ApplyOptions(tr.callOptions...)
		if *o.Temperature != 0.5 || *o.TopP != float64(float32(0.9)) || *o.MaxTokens != 100 || o.StopWords[0] != "END" ||
			*o.PresencePenalty != float64(float32(0.1)) || *o.FrequencyPenalty != float64(float32(0.2)) {
			t.Errorf("options = %+v", o)
		}
		if tr.timeout != timeout {
			t.Errorf("timeout = %v", tr.timeout)
		}
	})

	rejected := map[Field]*genai.GenerateContentConfig{
		FieldTopK:           {TopK: f32(40)},
		FieldSeed:           {Seed: i32(7)},
		FieldSafetySettings: {SafetySettings: []*genai.SafetySetting{{}}},
		FieldServiceTier:    {ServiceTier: "flex"},
		FieldHTTPOptions:    {HTTPOptions: &genai.HTTPOptions{APIVersion: "v1"}},
	}
	for field, cfg := range rejected {
		t.Run("rejects "+string(field), func(t *testing.T) {
			_, err := newTestModel(t).translate(&model.LLMRequest{Contents: contents, Config: cfg})
			if !errors.Is(err, ErrUnsupportedConfigField) || !strings.Contains(err.Error(), string(field)) {
				t.Errorf("err = %v, want ErrUnsupportedConfigField naming %s", err, field)
			}
			if _, err := newTestModel(t, WithIgnore(field)).translate(&model.LLMRequest{Contents: contents, Config: cfg}); err != nil {
				t.Errorf("with WithIgnore: %v", err)
			}
		})
	}

	hard := map[string]*genai.GenerateContentConfig{
		"CandidateCount":     {CandidateCount: 2},
		"ResponseModalities": {ResponseModalities: []string{"AUDIO"}},
		"ResponseMIMEType":   {ResponseMIMEType: "text/x.enum"},
	}
	for name, cfg := range hard {
		t.Run("hard "+name, func(t *testing.T) {
			m := newTestModel(t, WithIgnore(Field("GenerateContentConfig."+name)))
			if _, err := m.translate(&model.LLMRequest{Contents: contents, Config: cfg}); !errors.Is(err, ErrUnsupportedConfigField) {
				t.Errorf("err = %v, want a rejection WithIgnore cannot lift", err)
			}
		})
	}

	t.Run("text modality is accepted", func(t *testing.T) {
		translateOK(t, newTestModel(t), &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{ResponseModalities: []string{"TEXT"}}})
	})

	t.Run("model override", func(t *testing.T) {
		tr := translateOK(t, newTestModel(t), &model.LLMRequest{Model: "other", Contents: contents})
		if llms.ApplyOptions(tr.callOptions...).Model != "other" {
			t.Error("request model not applied")
		}
		tr = translateOK(t, newTestModel(t), &model.LLMRequest{Model: "fake-model", Contents: contents})
		if llms.ApplyOptions(tr.callOptions...).Model != "" {
			t.Error("the model's own name should not become an override")
		}
	})
}

func TestTranslate_Thinking(t *testing.T) {
	contents := []*genai.Content{user(text("q"))}
	i32 := func(v int32) *int32 { return &v }
	tests := []struct {
		name string
		tc   *genai.ThinkingConfig
		want *llms.ReasoningConfig
		hide bool
	}{
		{"nil hides thoughts", nil, nil, true},
		{"include only shows thoughts", &genai.ThinkingConfig{IncludeThoughts: true}, nil, false},
		{"level", &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelHigh}, &llms.ReasoningConfig{Effort: llms.ReasoningEffortHigh}, true},
		{"level beats budget", &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow, ThinkingBudget: i32(9000)}, &llms.ReasoningConfig{Effort: llms.ReasoningEffortLow}, true},
		{"unspecified defers to budget", &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelUnspecified, ThinkingBudget: i32(2048)}, &llms.ReasoningConfig{BudgetTokens: 2048}, true},
		{"unspecified alone is medium", &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelUnspecified}, &llms.ReasoningConfig{Effort: llms.ReasoningEffortMedium}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{ThinkingConfig: tt.tc}})
			got := llms.ApplyOptions(tr.callOptions...).Reasoning
			if (got == nil) != (tt.want == nil) || got != nil && (got.Effort != tt.want.Effort || got.BudgetTokens != tt.want.BudgetTokens) {
				t.Errorf("reasoning = %+v, want %+v", got, tt.want)
			}
			if tr.hideThoughts != tt.hide {
				t.Errorf("hideThoughts = %v, want %v", tr.hideThoughts, tt.hide)
			}
		})
	}
	for budget, enabled := range map[int32]bool{0: false, -1: true} {
		tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingBudget: i32(budget)}}})
		got := llms.ApplyOptions(tr.callOptions...).Reasoning
		if got == nil || got.Enabled == nil || *got.Enabled != enabled {
			t.Errorf("budget %d: reasoning = %+v, want Enabled=%v", budget, got, enabled)
		}
	}
	if _, err := newTestModel(t).translate(&model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingBudget: i32(-2)}}}); !errors.Is(err, ErrUnsupportedConfigField) {
		t.Errorf("budget -2: %v", err)
	}
}

func TestTranslate_Tools(t *testing.T) {
	contents := []*genai.Content{user(text("q"))}
	decl := func(name string) *genai.FunctionDeclaration {
		return &genai.FunctionDeclaration{Name: name, Description: name + " tool", Parameters: &genai.Schema{
			Type: genai.TypeObject, Properties: map[string]*genai.Schema{"q": {Type: genai.TypeString}}, Required: []string{"q"},
		}}
	}
	tools := []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{decl("a"), decl("b"), decl("c")}}}
	names := func(tr *translation) []string {
		var out []string
		for _, tool := range llms.ApplyOptions(tr.callOptions...).Tools {
			out = append(out, tool.Function.Name)
		}
		return out
	}

	t.Run("declarations and schema", func(t *testing.T) {
		tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{Tools: tools}})
		got := llms.ApplyOptions(tr.callOptions...).Tools
		if len(got) != 3 || string(got[0].Function.Parameters) != `{"properties":{"q":{"type":"string"}},"required":["q"],"type":"object"}` {
			t.Errorf("tools = %+v %s", got, got[0].Function.Parameters)
		}
	})

	modes := []struct {
		name  string
		fc    *genai.FunctionCallingConfig
		tools []string
		mode  llms.ToolChoiceMode
		tool  string
	}{
		{"auto with allow list filters", &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAuto, AllowedFunctionNames: []string{"b"}}, []string{"b"}, "", ""},
		{"none", &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeNone}, []string{"a", "b", "c"}, llms.ToolChoiceNone, ""},
		{"any", &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny}, []string{"a", "b", "c"}, llms.ToolChoiceRequired, ""},
		{"any one", &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny, AllowedFunctionNames: []string{"c"}}, []string{"a", "b", "c"}, llms.ToolChoiceTool, "c"},
		{"any several", &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny, AllowedFunctionNames: []string{"a", "c"}}, []string{"a", "c"}, llms.ToolChoiceRequired, ""},
	}
	for _, tt := range modes {
		t.Run(tt.name, func(t *testing.T) {
			tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{
				Tools: tools, ToolConfig: &genai.ToolConfig{FunctionCallingConfig: tt.fc},
			}})
			if got := strings.Join(names(tr), ","); got != strings.Join(tt.tools, ",") {
				t.Errorf("tools = %s, want %v", got, tt.tools)
			}
			choice := llms.ApplyOptions(tr.callOptions...).ToolChoice
			if tt.mode == "" && choice != nil || tt.mode != "" && (choice == nil || choice.Mode != tt.mode || choice.Tool != tt.tool) {
				t.Errorf("tool choice = %+v, want %s %s", choice, tt.mode, tt.tool)
			}
		})
	}

	t.Run("validated is rejected unless ignored", func(t *testing.T) {
		cfg := &genai.GenerateContentConfig{Tools: tools, ToolConfig: &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeValidated}}}
		if _, err := newTestModel(t).translate(&model.LLMRequest{Contents: contents, Config: cfg}); !errors.Is(err, ErrUnsupportedConfigField) {
			t.Errorf("err = %v", err)
		}
		translateOK(t, newTestModel(t, WithIgnore(FieldValidatedFunctionCalling)), &model.LLMRequest{Contents: contents, Config: cfg})
	})

	t.Run("server tools", func(t *testing.T) {
		for _, tool := range []*genai.Tool{{CodeExecution: &genai.ToolCodeExecution{}}, {URLContext: &genai.URLContext{}}} {
			if _, err := newTestModel(t).translate(&model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{tool}}}); !errors.Is(err, ErrUnsupportedTool) {
				t.Errorf("err = %v", err)
			}
		}
		search := &genai.GenerateContentConfig{Tools: []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}}}
		if _, err := newTestModel(t).translate(&model.LLMRequest{Contents: contents, Config: search}); !errors.Is(err, ErrUnsupportedTool) {
			t.Errorf("google search without WithWebSearch: %v", err)
		}
		tr := translateOK(t, newTestModel(t, WithWebSearch()), &model.LLMRequest{Contents: contents, Config: search})
		if ws := llms.ApplyOptions(tr.callOptions...).WebSearch; ws == nil || !ws.Enabled || !ws.IncludeResults {
			t.Errorf("web search = %+v", ws)
		}
	})

	t.Run("non-blocking functions are rejected", func(t *testing.T) {
		d := decl("x")
		d.Behavior = genai.BehaviorNonBlocking
		if _, err := newTestModel(t).translate(&model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{d}}}}}); !errors.Is(err, ErrUnsupportedTool) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestTranslate_StructuredOutput(t *testing.T) {
	contents := []*genai.Content{user(text("q"))}
	schema := &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"answer": {Type: genai.TypeString}}}

	t.Run("schema alone is a JSON schema", func(t *testing.T) {
		tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{
			ResponseMIMEType: "application/json", ResponseSchema: schema,
		}})
		rf := llms.ApplyOptions(tr.callOptions...).ResponseFormat
		if rf == nil || rf.JSONSchema == nil || !strings.Contains(string(rf.JSONSchema.Schema), `"type":"object"`) || tr.structured {
			t.Errorf("response format = %+v structured=%v", rf, tr.structured)
		}
	})

	t.Run("schema with tools becomes set_model_response", func(t *testing.T) {
		tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{
			ResponseMIMEType: "application/json", ResponseSchema: schema,
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup"}}}},
			ToolConfig: &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAny, AllowedFunctionNames: []string{"lookup"},
			}},
		}})
		o := llms.ApplyOptions(tr.callOptions...)
		if o.ResponseFormat != nil {
			t.Error("schema passed down alongside tools")
		}
		if !tr.structured || len(o.Tools) != 2 || o.Tools[1].Function.Name != setModelResponseName {
			t.Errorf("tools = %+v", o.Tools)
		}
		if o.ToolChoice == nil || o.ToolChoice.Mode != llms.ToolChoiceRequired {
			t.Errorf("a single allowed tool must not be forced while set_model_response is offered: %+v", o.ToolChoice)
		}
		if tr.messages[0].Role != llms.RoleSystem || tr.messages[0].Content != setModelResponseInstruction {
			t.Errorf("system = %+v", tr.messages[0])
		}
	})

	t.Run("name collision", func(t *testing.T) {
		_, err := newTestModel(t).translate(&model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{
			ResponseSchema: schema,
			Tools:          []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: setModelResponseName}}}},
		}})
		if !errors.Is(err, ErrUnsupportedTool) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("JSON without a schema", func(t *testing.T) {
		tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: contents, Config: &genai.GenerateContentConfig{ResponseMIMEType: "application/json"}})
		if rf := llms.ApplyOptions(tr.callOptions...).ResponseFormat; rf == nil || rf.Type != llms.ResponseFormatJSONObject {
			t.Errorf("response format = %+v", rf)
		}
	})
}

// TestSetModelResponseInstructionMatchesADK reads ADK's own instruction for its
// set_model_response tool from the ADK source in the module cache and compares
// it with the copy here, so an ADK release that rewords it fails this test.
func TestSetModelResponseInstructionMatchesADK(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "google.golang.org/adk/v2").Output()
	if err != nil {
		t.Skipf("cannot locate the ADK module: %v", err)
	}
	path := filepath.Join(strings.TrimSpace(string(out)), "internal", "llminternal", "outputschema_processor.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var adk string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "instructionForProcessor" {
			return true
		}
		adk = concatLiterals(t, spec.Values[0])
		return false
	})
	if adk == "" {
		t.Fatalf("instructionForProcessor not found in %s", path)
	}
	if setModelResponseInstruction != adk {
		t.Errorf("instruction drifted from ADK:\n got %q\nwant %q", setModelResponseInstruction, adk)
	}
}

// concatLiterals evaluates a constant expression of string literals joined by +.
func concatLiterals(t *testing.T, e ast.Expr) string {
	t.Helper()
	switch v := e.(type) {
	case *ast.BasicLit:
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			t.Fatal(err)
		}
		return s
	case *ast.BinaryExpr:
		return concatLiterals(t, v.X) + concatLiterals(t, v.Y)
	case *ast.ParenExpr:
		return concatLiterals(t, v.X)
	}
	t.Fatalf("unexpected expression %T", e)
	return ""
}

// TestTranslate_StructuredWithCallsOff checks that with function calling off
// the schema is enforced directly, not through a tool the model may not call.
func TestTranslate_StructuredWithCallsOff(t *testing.T) {
	tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: []*genai.Content{user(text("q"))}, Config: &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   &genai.Schema{Type: genai.TypeObject},
		Tools:            []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup"}}}},
		ToolConfig:       &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeNone}},
	}})
	o := llms.ApplyOptions(tr.callOptions...)
	if tr.structured || o.ResponseFormat == nil {
		t.Errorf("structured=%v responseFormat=%+v, want the schema enforced directly", tr.structured, o.ResponseFormat)
	}
	for _, tool := range o.Tools {
		if tool.Function.Name == setModelResponseName {
			t.Error("set_model_response offered with function calling off")
		}
	}
}

// TestTranslate_IDlessResponsesPairWithinTheirTurn covers an ID-less call left
// open (a long-running tool in a session that began on a model without IDs)
// followed later by an ID-less call to the same tool: the later result must
// answer the later call, not the open one.
func TestTranslate_IDlessResponsesPairWithinTheirTurn(t *testing.T) {
	tr := translateOK(t, newTestModel(t), &model.LLMRequest{Contents: []*genai.Content{
		user(text("start")),
		modelTurn(callPart("", "job", map[string]any{"n": 1.0})),
		user(text("again")),
		modelTurn(callPart("", "job", map[string]any{"n": 2.0})),
		user(responsePart("", "job", map[string]any{"done": 2.0})),
	}})
	msgs := tr.messages
	first, second := msgs[1].ToolCalls[0].ID, msgs[4].ToolCalls[0].ID
	if msgs[2].ToolCallID != first || msgs[2].Content != pendingResult {
		t.Errorf("open call answered with %+v, want the pending placeholder", msgs[2])
	}
	if msgs[5].ToolCallID != second || msgs[5].Content != `{"done":2}` {
		t.Errorf("later result = %+v, want it paired with the later call %s", msgs[5], second)
	}
}
