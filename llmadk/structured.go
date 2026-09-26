package llmadk

import (
	"fmt"
	"strings"

	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// setModelResponseName is the tool through which a model returns structured
// output while it also has other tools, the name ADK's own workaround uses.
const setModelResponseName = "set_model_response"

const setModelResponseDescription = "Set your final response using the required output schema. " +
	"Use this tool to provide your final structured answer instead of outputting text directly."

// setModelResponseInstruction is ADK's instruction for its set_model_response
// tool (internal/llminternal/outputschema_processor.go in ADK v2.4.0), copied
// verbatim because ADK keeps it in an internal package; a test pins it.
const setModelResponseInstruction = "IMPORTANT: You have access to other tools, but you must provide " +
	"your final response using the set_model_response tool with the " +
	"required structured format. After using any other tools needed " +
	"to complete the task, always call set_model_response with your " +
	"final answer in the specified schema format."

// structuredOutput translates the response MIME type and schema.
//
// A schema on its own becomes llms.WithJSONSchema. A schema together with
// tools, which ADK sends for every model not named gemini-*, is not passed down
// as both: several providers cannot honor the two at once (Anthropic enforces a
// schema by replacing the tools with one forced tool, so the agent's tools
// would vanish). The bridge instead offers the schema as a set_model_response
// tool and later turns the model's call to it into the final answer, the same
// workaround ADK applies for Gemini.
func (t *translator) structuredOutput(cfg *genai.GenerateContentConfig, tools []llms.Tool) ([]llms.Tool, error) {
	mime := strings.ToLower(strings.TrimSpace(cfg.ResponseMIMEType))
	var schemaSource any
	switch {
	case cfg.ResponseJsonSchema != nil:
		schemaSource = cfg.ResponseJsonSchema
	case cfg.ResponseSchema != nil:
		schemaSource = cfg.ResponseSchema
	}
	switch mime {
	case "", "text/plain", "application/json":
	default:
		return nil, t.reject("GenerateContentConfig.ResponseMIMEType", true, mime)
	}
	if schemaSource == nil {
		if mime == "application/json" {
			t.tr.callOptions = append(t.tr.callOptions, llms.WithJSONMode())
		}
		return tools, nil
	}
	schema, err := schemaJSON(schemaSource)
	if err != nil {
		return nil, fmt.Errorf("llmadk: response schema: %w", err)
	}
	t.tr.hasSchema = true
	// With function calling off (mode NONE) the tools cannot be called, so the
	// schema is enforced directly rather than through a tool.
	callsOff := cfg.ToolConfig != nil && cfg.ToolConfig.FunctionCallingConfig != nil &&
		cfg.ToolConfig.FunctionCallingConfig.Mode == genai.FunctionCallingConfigModeNone
	if len(tools) == 0 || callsOff {
		t.tr.callOptions = append(t.tr.callOptions, llms.WithJSONSchema("response", schema, false))
		return tools, nil
	}
	for _, tool := range tools {
		if tool.Function != nil && tool.Function.Name == setModelResponseName {
			return nil, fmt.Errorf("%w: a tool named %s collides with the tool the bridge uses for structured output", ErrUnsupportedTool, setModelResponseName)
		}
	}
	t.tr.structured = true
	t.appendSystem(setModelResponseInstruction)
	return append(tools, llms.Tool{
		Type: llms.ToolTypeFunction,
		Function: &llms.FunctionDefinition{
			Name:        setModelResponseName,
			Description: setModelResponseDescription,
			Parameters:  schema,
		},
	}), nil
}

// appendSystem adds text to the system message, creating it when absent.
func (t *translator) appendSystem(text string) {
	msgs := t.tr.messages
	if len(msgs) > 0 && msgs[0].Role == llms.RoleSystem {
		msgs[0].Content += "\n\n" + text
		return
	}
	t.tr.messages = append([]llms.Message{{Role: llms.RoleSystem, Content: text}}, msgs...)
}

// resolveStructured turns the model's set_model_response call into its final
// answer. A response whose only call is set_model_response becomes that call's
// arguments as text, finishing the turn. When the model called other tools as
// well, only those are kept, so the agent runs them and the model answers again
// with their results.
func resolveStructured(resp *llms.Response) {
	var kept []llms.ToolCall
	var answer *llms.ToolCall
	for i, tc := range resp.ToolCalls {
		if tc.Function != nil && tc.Function.Name == setModelResponseName {
			answer = &resp.ToolCalls[i]
			continue
		}
		kept = append(kept, tc)
	}
	if answer == nil {
		return
	}
	if len(kept) > 0 {
		resp.ToolCalls = kept
		return
	}
	resp.ToolCalls = nil
	resp.Content = answer.Function.Arguments
	resp.FinishReason = llms.FinishReasonStop
}
