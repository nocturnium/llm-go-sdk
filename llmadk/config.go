package llmadk

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// reject fails the request for field, unless the caller ignored it and it is
// not HARD.
func (t *translator) reject(field Field, isHard bool, detail string) error {
	if !isHard && t.opts.ignore[field] {
		return nil
	}
	if detail != "" {
		return fmt.Errorf("%w: %s (%s)", ErrUnsupportedConfigField, field, detail)
	}
	return fmt.Errorf("%w: %s", ErrUnsupportedConfigField, field)
}

// config translates a GenerateContentConfig. Every field is translated,
// ignored or rejected; see fieldHandling for the table.
func (t *translator) config(cfg *genai.GenerateContentConfig) error {
	add := func(o llms.CallOption) { t.tr.callOptions = append(t.tr.callOptions, o) }

	if cfg.Temperature != nil {
		add(llms.WithTemperature(float64(*cfg.Temperature)))
	}
	if cfg.TopP != nil {
		add(llms.WithTopP(float64(*cfg.TopP)))
	}
	if cfg.MaxOutputTokens > 0 {
		add(llms.WithMaxTokens(int(cfg.MaxOutputTokens)))
	}
	if len(cfg.StopSequences) > 0 {
		add(llms.WithStopWords(cfg.StopSequences))
	}
	if cfg.PresencePenalty != nil {
		add(llms.WithPresencePenalty(float64(*cfg.PresencePenalty)))
	}
	if cfg.FrequencyPenalty != nil {
		add(llms.WithFrequencyPenalty(float64(*cfg.FrequencyPenalty)))
	}
	if cfg.CandidateCount > 1 {
		return t.reject("GenerateContentConfig.CandidateCount", true, fmt.Sprintf("%d candidates", cfg.CandidateCount))
	}
	if len(cfg.ResponseModalities) > 0 {
		for _, m := range cfg.ResponseModalities {
			if !strings.EqualFold(m, string(genai.ModalityText)) {
				return t.reject("GenerateContentConfig.ResponseModalities", true, m)
			}
		}
	}

	// Presence checks for fields llm-go-sdk has no equivalent for. Rejection
	// keys on presence, so a zero value that cannot itself be a setting (false,
	// "", nil) passes, the same rule ADK's own OpenAI model follows.
	rejects := []struct {
		field Field
		set   bool
	}{
		{FieldTopK, cfg.TopK != nil},
		{FieldSeed, cfg.Seed != nil},
		{FieldResponseLogprobs, cfg.ResponseLogprobs},
		{FieldLogprobs, cfg.Logprobs != nil},
		{FieldSafetySettings, len(cfg.SafetySettings) > 0},
		{FieldCachedContent, cfg.CachedContent != ""},
		{FieldMediaResolution, cfg.MediaResolution != ""},
		{FieldSpeechConfig, cfg.SpeechConfig != nil},
		{FieldAudioTimestamp, cfg.AudioTimestamp},
		{FieldImageConfig, cfg.ImageConfig != nil},
		{FieldRoutingConfig, cfg.RoutingConfig != nil},
		{FieldModelSelectionConfig, cfg.ModelSelectionConfig != nil},
		{FieldModelArmorConfig, cfg.ModelArmorConfig != nil},
		{FieldEnableEnhancedCivicAnswers, cfg.EnableEnhancedCivicAnswers != nil},
		{FieldServiceTier, cfg.ServiceTier != ""},
		{FieldAudioTranscriptionConfig, cfg.AudioTranscriptionConfig != nil},
	}
	for _, r := range rejects {
		if r.set {
			if err := t.reject(r.field, false, ""); err != nil {
				return err
			}
		}
	}
	// Labels are request metadata for Google's billing; ADK Python sets one per
	// agent, so rejecting them would break every agent the day ADK Go does too.

	if err := t.httpOptions(cfg.HTTPOptions); err != nil {
		return err
	}
	if err := t.thinking(cfg.ThinkingConfig); err != nil {
		return err
	}
	tools, err := t.tools(cfg.Tools)
	if err != nil {
		return err
	}
	tools, err = t.structuredOutput(cfg, tools)
	if err != nil {
		return err
	}
	tools, err = t.toolConfig(cfg.ToolConfig, tools)
	if err != nil {
		return err
	}
	if len(tools) > 0 {
		t.tr.hasTools = true
		add(llms.WithTools(tools))
	}
	return nil
}

// httpOptions honors a per-request timeout and ignores headers, which are
// addressed to Google's backend and must not reach another provider. The other
// options configure Google's client and have no meaning here.
func (t *translator) httpOptions(h *genai.HTTPOptions) error {
	if h == nil {
		return nil
	}
	if h.Timeout != nil && *h.Timeout > 0 {
		t.tr.timeout = *h.Timeout
	}
	rest := *h
	rest.Timeout = nil
	rest.Headers = nil
	if !reflect.ValueOf(rest).IsZero() {
		return t.reject(FieldHTTPOptions, false, "only Timeout and Headers are supported")
	}
	return nil
}

var thinkingEfforts = map[genai.ThinkingLevel]llms.ReasoningEffort{
	genai.ThinkingLevelMinimal: llms.ReasoningEffortMinimal,
	genai.ThinkingLevelLow:     llms.ReasoningEffortLow,
	genai.ThinkingLevelMedium:  llms.ReasoningEffortMedium,
	genai.ThinkingLevelHigh:    llms.ReasoningEffortHigh,
}

// thinking translates a ThinkingConfig, following the precedence ADK's own
// OpenAI model uses: a level wins over a budget unless the level is
// UNSPECIFIED, which defers to a budget and otherwise means medium.
// IncludeThoughts decides only whether thought text is shown; it does not turn
// reasoning on, since for Gemini it never did.
func (t *translator) thinking(tc *genai.ThinkingConfig) error {
	if tc == nil {
		return nil
	}
	t.tr.hideThoughts = !tc.IncludeThoughts
	if tc.ThinkingBudget != nil && *tc.ThinkingBudget < -1 {
		return fmt.Errorf("%w: ThinkingConfig.ThinkingBudget %d", ErrUnsupportedConfigField, *tc.ThinkingBudget)
	}
	level := tc.ThinkingLevel
	var rc *llms.ReasoningConfig
	switch {
	case level != "" && level != genai.ThinkingLevelUnspecified:
		effort, ok := thinkingEfforts[level]
		if !ok {
			return fmt.Errorf("%w: ThinkingConfig.ThinkingLevel %q", ErrUnsupportedConfigField, level)
		}
		rc = &llms.ReasoningConfig{Effort: effort}
	case tc.ThinkingBudget != nil:
		switch budget := *tc.ThinkingBudget; budget {
		case 0:
			off := false
			rc = &llms.ReasoningConfig{Enabled: &off}
		case -1:
			// The caller asked the model to decide how much to think.
			on := true
			rc = &llms.ReasoningConfig{Enabled: &on}
		default:
			rc = &llms.ReasoningConfig{BudgetTokens: int(budget)}
		}
	case level == genai.ThinkingLevelUnspecified:
		rc = &llms.ReasoningConfig{Effort: llms.ReasoningEffortMedium}
	}
	if rc != nil {
		t.tr.callOptions = append(t.tr.callOptions, llms.WithReasoning(*rc))
	}
	return nil
}

// tools translates the function declarations of every tool. Gemini's server
// tools have no provider-neutral equivalent and are rejected, except Google
// Search, which maps onto the provider's own web search with WithWebSearch.
func (t *translator) tools(tools []*genai.Tool) ([]llms.Tool, error) {
	var out []llms.Tool
	for i, tool := range tools {
		if tool == nil {
			continue
		}
		if name := serverTool(tool); name != "" {
			return nil, fmt.Errorf("%w: Tool.%s (tools[%d]) has no equivalent outside Gemini", ErrUnsupportedTool, name, i)
		}
		if tool.GoogleSearch != nil || tool.GoogleSearchRetrieval != nil {
			if t.opts.webSearch {
				// Results are requested so they can be reported to ADK as
				// grounding metadata, which is what GoogleSearch returns.
				t.tr.callOptions = append(t.tr.callOptions, llms.WithWebSearch(llms.WebSearchConfig{Enabled: true, IncludeResults: true}))
			} else if !t.opts.ignore[FieldGoogleSearch] {
				return nil, fmt.Errorf("%w: %s (use WithWebSearch for a provider that searches, or WithIgnore)", ErrUnsupportedTool, FieldGoogleSearch)
			}
		}
		for _, decl := range tool.FunctionDeclarations {
			if decl == nil {
				continue
			}
			converted, err := functionDeclaration(decl)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		}
	}
	return out, nil
}

// serverTool names the first Gemini-only server tool in tool, if any.
func serverTool(tool *genai.Tool) string {
	switch {
	case tool.Retrieval != nil:
		return "Retrieval"
	case tool.ComputerUse != nil:
		return "ComputerUse"
	case tool.FileSearch != nil:
		return "FileSearch"
	case tool.GoogleMaps != nil:
		return "GoogleMaps"
	case tool.CodeExecution != nil:
		return "CodeExecution"
	case tool.EnterpriseWebSearch != nil:
		return "EnterpriseWebSearch"
	case tool.ParallelAISearch != nil:
		return "ParallelAISearch"
	case tool.URLContext != nil:
		return "URLContext"
	case len(tool.MCPServers) > 0:
		return "MCPServers"
	case tool.ExaAISearch != nil:
		return "ExaAISearch"
	}
	return ""
}

// functionDeclaration converts one declaration. Parameters wins over
// ParametersJsonSchema when both are set, as in ADK's own OpenAI model. The
// response schemas describe the tool's output, which no provider is told.
func functionDeclaration(decl *genai.FunctionDeclaration) (llms.Tool, error) {
	if decl.Behavior == genai.BehaviorNonBlocking {
		return llms.Tool{}, fmt.Errorf("%w: function %q is NON_BLOCKING, a Live API behavior", ErrUnsupportedTool, decl.Name)
	}
	var params json.RawMessage
	var err error
	if decl.Parameters != nil {
		params, err = schemaJSON(decl.Parameters)
	} else {
		params, err = schemaJSON(decl.ParametersJsonSchema)
	}
	if err != nil {
		return llms.Tool{}, fmt.Errorf("llmadk: function %q parameters: %w", decl.Name, err)
	}
	if len(params) == 0 || string(params) == jsonNull {
		params = emptyObjectSchema
	}
	return llms.Tool{
		Type: llms.ToolTypeFunction,
		Function: &llms.FunctionDefinition{
			Name:        decl.Name,
			Description: decl.Description,
			Parameters:  params,
		},
	}, nil
}

// toolConfig translates the function-calling mode. Restricting the allowed
// functions filters the tool list, which every provider understands, rather
// than relying on a provider-specific allow list; the bridge's own
// set_model_response tool is always kept.
func (t *translator) toolConfig(tc *genai.ToolConfig, tools []llms.Tool) ([]llms.Tool, error) {
	if tc == nil {
		return tools, nil
	}
	if tc.RetrievalConfig != nil {
		if err := t.reject(FieldRetrievalConfig, false, ""); err != nil {
			return nil, err
		}
	}
	if tc.IncludeServerSideToolInvocations != nil && *tc.IncludeServerSideToolInvocations {
		if err := t.reject(FieldServerSideToolInvocations, false, ""); err != nil {
			return nil, err
		}
	}
	fc := tc.FunctionCallingConfig
	if fc == nil {
		return tools, nil
	}
	add := func(o llms.CallOption) { t.tr.callOptions = append(t.tr.callOptions, o) }
	names := fc.AllowedFunctionNames
	switch fc.Mode {
	case "", genai.FunctionCallingConfigModeUnspecified, genai.FunctionCallingConfigModeAuto:
		return filterTools(tools, names), nil
	case genai.FunctionCallingConfigModeNone:
		add(llms.WithToolChoiceNone())
		return tools, nil
	case genai.FunctionCallingConfigModeAny:
		switch {
		case len(names) == 1 && !t.tr.structured:
			add(llms.WithToolChoiceTool(names[0]))
			return tools, nil
		default:
			add(llms.WithToolChoiceRequired())
			return filterTools(tools, names), nil
		}
	case genai.FunctionCallingConfigModeValidated:
		// Schema-validated calling has no provider-neutral equivalent; when the
		// caller ignores that, the mode degrades to AUTO.
		if err := t.reject(FieldValidatedFunctionCalling, false, "VALIDATED"); err != nil {
			return nil, err
		}
		return filterTools(tools, names), nil
	default:
		return nil, fmt.Errorf("%w: FunctionCallingConfig.Mode %q", ErrUnsupportedConfigField, fc.Mode)
	}
}

// filterTools keeps the tools named in allowed, and the bridge's
// set_model_response tool; an empty allow list keeps them all.
func filterTools(tools []llms.Tool, allowed []string) []llms.Tool {
	if len(allowed) == 0 {
		return tools
	}
	keep := map[string]bool{setModelResponseName: true}
	for _, name := range allowed {
		keep[name] = true
	}
	var out []llms.Tool
	for _, tool := range tools {
		if tool.Function != nil && keep[tool.Function.Name] {
			out = append(out, tool)
		}
	}
	return out
}
