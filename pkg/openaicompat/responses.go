package openaicompat

// This file implements the OpenAI Responses API (POST /responses) — the stateful
// successor to /chat/completions. It defines the Responses wire types, the client
// method, and the converters between llms types and the Responses request/response
// shape. Streaming and full server-side conversation chaining are intentionally
// out of scope here (see docs/roadmap.md, Track A).

import (
	"context"
	"net/http"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/internal/httpclient"
)

// --- Request types ---

// ResponsesRequest is a POST /responses request body.
type ResponsesRequest struct {
	Model           string               `json:"model"`
	Input           []ResponsesInputItem `json:"input"`
	Instructions    string               `json:"instructions,omitempty"`
	MaxOutputTokens *int                 `json:"max_output_tokens,omitempty"`
	Temperature     *float64             `json:"temperature,omitempty"`
	TopP            *float64             `json:"top_p,omitempty"`
	Tools           []ResponsesTool      `json:"tools,omitempty"`
	ToolChoice      any                  `json:"tool_choice,omitempty"`
	Text            *ResponsesText       `json:"text,omitempty"`
	Reasoning       *ResponsesReasoning  `json:"reasoning,omitempty"`
	// Store and PreviousResponseID drive server-side conversation state. They are
	// populated from CallOptions.ExtraBody ("store", "previous_response_id"); full
	// chaining ergonomics are a follow-up (see docs/roadmap.md).
	Store              *bool  `json:"store,omitempty"`
	PreviousResponseID string `json:"previous_response_id,omitempty"`
	Stream             bool   `json:"stream,omitempty"`
}

// ResponsesInputItem is one item in the Responses `input` array. A single struct
// represents all input item kinds (message, function_call, function_call_output);
// omitempty keeps the unused fields out of the wire form.
type ResponsesInputItem struct {
	Type string `json:"type"` // "message" | "function_call" | "function_call_output"

	// message
	Role    string                 `json:"role,omitempty"`
	Content []ResponsesContentPart `json:"content,omitempty"`

	// function_call (assistant tool call) / function_call_output (tool result)
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

// ResponsesContentPart is a typed content part of an input message.
type ResponsesContentPart struct {
	Type     string `json:"type"` // "input_text" | "output_text" | "input_image"
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// ResponsesTool is a Responses-API tool definition. Unlike chat completions,
// function fields are flattened (not nested under "function").
type ResponsesTool struct {
	Type        string `json:"type"` // "function"
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      bool   `json:"strict,omitempty"`
}

// ResponsesText carries the structured-output format (the Responses equivalent of
// chat completions' response_format).
type ResponsesText struct {
	Format *ResponsesFormat `json:"format,omitempty"`
}

// ResponsesFormat is the text.format object.
type ResponsesFormat struct {
	Type   string `json:"type"` // "text" | "json_object" | "json_schema"
	Name   string `json:"name,omitempty"`
	Schema []byte `json:"schema,omitempty"`
	Strict bool   `json:"strict,omitempty"`
}

// ResponsesReasoning configures reasoning effort for reasoning models.
type ResponsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

// --- Response types ---

// ResponsesResponse is a POST /responses response body.
type ResponsesResponse struct {
	ID                string                `json:"id"`
	Object            string                `json:"object"`
	Model             string                `json:"model"`
	Status            string                `json:"status"` // completed | incomplete | failed | ...
	Output            []ResponsesOutputItem `json:"output"`
	Usage             *ResponsesUsage       `json:"usage,omitempty"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// ResponsesOutputItem is one item in the response `output` array.
type ResponsesOutputItem struct {
	Type string `json:"type"` // "message" | "function_call" | "reasoning"

	// message
	Role    string                   `json:"role,omitempty"`
	Content []ResponsesOutputContent `json:"content,omitempty"`

	// function_call
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// reasoning
	Summary []ResponsesSummaryPart `json:"summary,omitempty"`
}

// ResponsesOutputContent is a content part of an output message.
type ResponsesOutputContent struct {
	Type    string `json:"type"` // "output_text" | "refusal"
	Text    string `json:"text,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}

// ResponsesSummaryPart is a reasoning summary fragment.
type ResponsesSummaryPart struct {
	Type string `json:"type"` // "summary_text"
	Text string `json:"text,omitempty"`
}

// ResponsesUsage is the Responses-API usage payload.
type ResponsesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details,omitempty"`
}

func (u *ResponsesUsage) cachedTokens() int {
	if u.InputTokensDetails != nil {
		return u.InputTokensDetails.CachedTokens
	}
	return 0
}

func (u *ResponsesUsage) reasoningTokens() int {
	if u.OutputTokensDetails != nil {
		return u.OutputTokensDetails.ReasoningTokens
	}
	return 0
}

// --- Client ---

// CreateResponse sends a non-streaming Responses API request to POST /responses.
func (c *Client) CreateResponse(ctx context.Context, req *ResponsesRequest) (*ResponsesResponse, error) {
	headers := c.getHeaders()
	var response ResponsesResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.buildURL("/responses"),
		Headers: headers,
		Body:    req,
	}, &response)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

// --- Converters ---

// BuildResponsesRequest builds a ResponsesRequest from llms types. System messages
// become the top-level `instructions`; remaining messages become `input` items.
func BuildResponsesRequest(model string, messages []llms.Message, opts *llms.CallOptions, stream bool) *ResponsesRequest {
	input, instructions := convertMessagesToResponsesInput(messages)

	req := &ResponsesRequest{
		Model:           model,
		Input:           input,
		Instructions:    instructions,
		MaxOutputTokens: opts.MaxTokens,
		Temperature:     opts.Temperature,
		TopP:            opts.TopP,
		Stream:          stream,
	}

	// Reasoning models reject temperature/top_p. max_output_tokens is universal in
	// the Responses API, so no max_tokens/max_completion_tokens split is needed.
	if isOpenAIReasoningModel(model) {
		req.Temperature = nil
		req.TopP = nil
	}

	if len(opts.Tools) > 0 {
		req.Tools = convertResponsesTools(opts.Tools)
	}
	if opts.ToolChoice != nil {
		req.ToolChoice = convertToolChoice(opts.ToolChoice)
	}
	if opts.ResponseFormat != nil {
		req.Text = &ResponsesText{Format: convertResponsesFormat(opts.ResponseFormat)}
	}
	if opts.Reasoning != nil && opts.Reasoning.Effort != "" {
		req.Reasoning = &ResponsesReasoning{Effort: string(opts.Reasoning.Effort)}
	}

	applyResponsesState(req, opts.ExtraBody)
	return req
}

// applyResponsesState reads server-side conversation-state hints from ExtraBody:
// "store" (bool) and "previous_response_id" (string).
func applyResponsesState(req *ResponsesRequest, extra map[string]any) {
	if len(extra) == 0 {
		return
	}
	if v, ok := extra["store"].(bool); ok {
		req.Store = &v
	}
	if v, ok := extra["previous_response_id"].(string); ok && v != "" {
		req.PreviousResponseID = v
	}
}

// convertMessagesToResponsesInput maps llms messages onto Responses input items,
// returning the items and the concatenated system-message text (instructions).
func convertMessagesToResponsesInput(messages []llms.Message) ([]ResponsesInputItem, string) {
	var items []ResponsesInputItem
	var instructions string

	for _, msg := range messages {
		switch msg.Role {
		case llms.RoleSystem:
			if instructions != "" && msg.Content != "" {
				instructions += "\n\n"
			}
			instructions += msg.Content

		case llms.RoleTool:
			items = append(items, ResponsesInputItem{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: msg.Content,
			})

		case llms.RoleAssistant:
			if parts := responsesMessageContent(msg, "output_text"); len(parts) > 0 {
				items = append(items, ResponsesInputItem{
					Type:    "message",
					Role:    string(msg.Role),
					Content: parts,
				})
			}
			for _, tc := range msg.ToolCalls {
				item := ResponsesInputItem{Type: "function_call", CallID: tc.ID}
				if tc.Function != nil {
					item.Name = tc.Function.Name
					item.Arguments = tc.Function.Arguments
				}
				items = append(items, item)
			}

		default: // user (and any other role) → input message
			items = append(items, ResponsesInputItem{
				Type:    "message",
				Role:    string(msg.Role),
				Content: responsesMessageContent(msg, "input_text"),
			})
		}
	}
	return items, instructions
}

// responsesMessageContent builds the content parts for a message, using textType
// ("input_text" for user, "output_text" for assistant) for text and "input_image"
// for image parts.
func responsesMessageContent(msg llms.Message, textType string) []ResponsesContentPart {
	if len(msg.Parts) == 0 {
		if msg.Content == "" {
			return nil
		}
		return []ResponsesContentPart{{Type: textType, Text: msg.Content}}
	}

	var parts []ResponsesContentPart
	for _, p := range msg.Parts {
		switch p.Type {
		case llms.PartTypeText:
			parts = append(parts, ResponsesContentPart{Type: textType, Text: p.Text})
		case llms.PartTypeImage:
			if p.Image != nil {
				parts = append(parts, ResponsesContentPart{Type: "input_image", ImageURL: formatImageURL(p.Image)})
			}
		}
	}
	return parts
}

func convertResponsesTools(tools []llms.Tool) []ResponsesTool {
	result := make([]ResponsesTool, 0, len(tools))
	for _, t := range tools {
		rt := ResponsesTool{Type: string(t.Type)}
		if t.Function != nil {
			rt.Name = t.Function.Name
			rt.Description = t.Function.Description
			rt.Parameters = t.Function.Parameters
			rt.Strict = t.Function.Strict
		}
		result = append(result, rt)
	}
	return result
}

func convertResponsesFormat(format *llms.ResponseFormat) *ResponsesFormat {
	if format == nil {
		return nil
	}
	rf := &ResponsesFormat{Type: string(format.Type)}
	if format.Type == llms.ResponseFormatJSONSchema && format.JSONSchema != nil {
		rf.Name = format.JSONSchema.Name
		rf.Schema = append([]byte(nil), format.JSONSchema.Schema...)
		rf.Strict = format.JSONSchema.Strict
	}
	return rf
}

// ConvertResponsesResponse maps a Responses API response to the neutral llms.Response.
func ConvertResponsesResponse(resp *ResponsesResponse) *llms.Response {
	result := &llms.Response{ID: resp.ID}

	var content, reasoning string
	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" {
					content += c.Text
				}
			}
		case "function_call":
			result.ToolCalls = append(result.ToolCalls, llms.ToolCall{
				ID:   item.CallID,
				Type: llms.ToolTypeFunction,
				Function: &llms.FunctionCall{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		case "reasoning":
			for _, s := range item.Summary {
				reasoning += s.Text
			}
		}
	}

	result.Content = content
	if reasoning != "" {
		result.SetReasoning(&llms.ReasoningContent{Content: reasoning})
	}
	result.FinishReason = responsesFinishReason(resp, len(result.ToolCalls) > 0)

	if resp.Usage != nil {
		result.Usage = convertResponsesUsage(resp.Usage)
		if result.Reasoning != nil && result.Usage.ReasoningTokens > 0 {
			result.Reasoning.Tokens = result.Usage.ReasoningTokens
		}
	}
	return result
}

// responsesFinishReason derives a neutral finish reason from the response status
// and whether tool calls were produced.
func responsesFinishReason(resp *ResponsesResponse, hasToolCalls bool) llms.FinishReason {
	if hasToolCalls {
		return llms.FinishReasonToolCalls
	}
	if resp.Status == "incomplete" && resp.IncompleteDetails != nil &&
		resp.IncompleteDetails.Reason == "max_output_tokens" {
		return llms.FinishReasonLength
	}
	return llms.FinishReasonStop
}

// convertResponsesUsage maps Responses usage to llms.Usage, normalizing so
// PromptTokens excludes the cache-read subset (input_tokens is cache-inclusive).
func convertResponsesUsage(u *ResponsesUsage) llms.Usage {
	cacheRead := u.cachedTokens()
	prompt := u.InputTokens - cacheRead
	if prompt < 0 {
		prompt = 0
	}
	return llms.Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
		CacheReadTokens:  cacheRead,
		ReasoningTokens:  u.reasoningTokens(),
	}
}
