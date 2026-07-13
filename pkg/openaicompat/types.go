package openaicompat

// This file defines the OpenAI-compatible wire types (requests, responses,
// streaming chunks, embeddings, and model listings). The package-level
// documentation lives in doc.go.

import (
	"encoding/json"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

const (
	contentTypeText = "text"
)

// ChatCompletionRequest represents a chat completion request
type ChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	// Temperature and TopP are pointers so an explicit 0.0 is serialized while an
	// unset (nil) value is omitted, letting the provider apply its own default.
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	// MaxCompletionTokens is the OpenAI reasoning-model replacement for max_tokens.
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
	Stop                []string        `json:"stop,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	StreamOptions       *StreamOptions  `json:"stream_options,omitempty"`
	Tools               []Tool          `json:"tools,omitempty"`
	ToolChoice          any             `json:"tool_choice,omitempty"`
	ResponseFormat      *ResponseFormat `json:"response_format,omitempty"`
	// ReasoningEffort maps to the OpenAI reasoning_effort parameter
	// ("minimal"/"low"/"medium"/"high") for reasoning models. Empty omits it.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// ExtraBody contains additional parameters merged at the top level of the JSON request.
	// Used for provider-specific extensions like LoRAX adapter_id.
	// These fields are flattened into the request JSON, not nested under "extra_body".
	ExtraBody map[string]any `json:"-"` // Excluded from default marshaling, handled by MarshalJSON
}

// StreamOptions specifies options for streaming chat completions.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ResponseFormat specifies the response format
type ResponseFormat struct {
	Type       string              `json:"type"` // "text", "json_object", or "json_schema"
	JSONSchema *ResponseJSONSchema `json:"json_schema,omitempty"`
}

// ResponseJSONSchema specifies the OpenAI-compatible json_schema response format.
type ResponseJSONSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      bool            `json:"strict,omitempty"`
}

// MarshalJSON implements custom JSON marshaling for ChatCompletionRequest.
// It merges ExtraBody fields at the top level of the JSON output.
func (r ChatCompletionRequest) MarshalJSON() ([]byte, error) {
	// Create a map to hold the JSON structure
	m := make(map[string]any)

	// Add standard fields
	m["model"] = r.Model
	m["messages"] = r.Messages

	if r.Temperature != nil {
		m["temperature"] = *r.Temperature
	}
	if r.MaxTokens != nil {
		m["max_tokens"] = *r.MaxTokens
	}
	if r.MaxCompletionTokens != nil {
		m["max_completion_tokens"] = *r.MaxCompletionTokens
	}
	if r.TopP != nil {
		m["top_p"] = *r.TopP
	}
	if r.FrequencyPenalty != nil {
		m["frequency_penalty"] = *r.FrequencyPenalty
	}
	if r.PresencePenalty != nil {
		m["presence_penalty"] = *r.PresencePenalty
	}
	if len(r.Stop) > 0 {
		m["stop"] = r.Stop
	}
	if r.Stream {
		m["stream"] = r.Stream
	}
	if r.StreamOptions != nil {
		m["stream_options"] = r.StreamOptions
	}
	if len(r.Tools) > 0 {
		m["tools"] = r.Tools
	}
	if r.ToolChoice != nil {
		m["tool_choice"] = r.ToolChoice
	}
	if r.ResponseFormat != nil {
		m["response_format"] = r.ResponseFormat
	}
	if r.ReasoningEffort != "" {
		m["reasoning_effort"] = r.ReasoningEffort
	}

	// Merge ExtraBody fields at the top level
	for k, v := range r.ExtraBody {
		m[k] = v
	}

	return jsonMarshal(m)
}

// jsonMarshal is a variable to allow testing with custom marshaler
var jsonMarshal = jsonMarshalImpl

func jsonMarshalImpl(v any) ([]byte, error) {
	return json.Marshal(v)
}

// ChatMessage represents a message in the chat.
// Content can be either a string or a slice of ContentPart for vision models.
type ChatMessage struct {
	Role             string     `json:"role"`
	ContentValue     any        `json:"content,omitempty"`           // string or []ContentPart
	ReasoningContent string     `json:"reasoning_content,omitempty"` // Z.AI GLM models return reasoning in this field
	Reasoning        string     `json:"reasoning,omitempty"`         // Synthetic.new / Qwen Thinking models return reasoning in this field
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
}

// ReasoningText returns the reasoning content from whichever field is populated.
// Providers use different field names: "reasoning_content" (Z.AI GLM) vs "reasoning" (Synthetic/Qwen).
func (m *ChatMessage) ReasoningText() string {
	if m.ReasoningContent != "" {
		return m.ReasoningContent
	}
	return m.Reasoning
}

// ContentPart represents a part of a multi-modal message
type ContentPart struct {
	Type     string    `json:"type"` // "text" or "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents an image URL in a content part
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", or "high"
}

// Tool represents a tool definition
type Tool struct {
	Type     string              `json:"type"`
	Function *FunctionDefinition `json:"function,omitempty"`
}

// FunctionDefinition defines a function that can be called
type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      bool   `json:"strict,omitempty"`
}

// ToolCall represents a tool call from the model
type ToolCall struct {
	Index    *int          `json:"index,omitempty"` // Used in streaming to identify which tool call to update
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function *FunctionCall `json:"function,omitempty"`
}

// FunctionCall contains the function name and arguments
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionResponse represents a chat completion response
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice represents a response choice
type Choice struct {
	Index        int          `json:"index"`
	Message      *ChatMessage `json:"message,omitempty"`
	Delta        *ChatMessage `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details,omitempty"`
	// PromptCacheHitTokens is DeepSeek's cache-hit count (its alternative to
	// prompt_tokens_details.cached_tokens).
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens,omitempty"`
}

// cacheReadTokens returns the number of prompt tokens served from cache,
// reconciling the OpenAI (prompt_tokens_details.cached_tokens) and DeepSeek
// (prompt_cache_hit_tokens) shapes.
func (u *Usage) cacheReadTokens() int {
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens
	}
	return u.PromptCacheHitTokens
}

// reasoningTokens returns the number of reasoning tokens reported in
// completion_tokens_details, or zero.
func (u *Usage) reasoningTokens() int {
	if u.CompletionTokensDetails != nil {
		return u.CompletionTokensDetails.ReasoningTokens
	}
	return 0
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// ToolChoiceFunction is used when specifying a specific function
type ToolChoiceFunction struct {
	Type     string            `json:"type"` // "function"
	Function *FunctionSelector `json:"function"`
}

// FunctionSelector specifies which function to call
type FunctionSelector struct {
	Name string `json:"name"`
}

// RawContent extracts string content from a ChatMessage without falling back to
// reasoning content. Use this for visible response text, especially streaming
// deltas where reasoning must remain separate from content.
func (m *ChatMessage) RawContent() string {
	if m == nil {
		return ""
	}

	var result string

	if m.ContentValue != nil {
		switch c := m.ContentValue.(type) {
		case string:
			result = c
		case []any:
			// Handle JSON-decoded array of content parts
			for _, part := range c {
				if partMap, ok := part.(map[string]any); ok {
					if text, ok := partMap["text"].(string); ok {
						result += text
					}
				}
			}
		case []ContentPart:
			for _, part := range c {
				if part.Type == contentTypeText {
					result += part.Text
				}
			}
		}
	}

	return result
}

// Content extracts string content from a ChatMessage.
// Content can be either a string or []ContentPart for vision models.
// Falls back to reasoning content if Content is empty (Z.AI GLM / Synthetic Thinking models).
// Returns empty string if no content is available.
func (m *ChatMessage) Content() string {
	result := m.RawContent()

	// Fall back to reasoning content if Content is empty
	// Handles both "reasoning_content" (Z.AI GLM) and "reasoning" (Synthetic/Qwen Thinking)
	if result == "" {
		if reasoning := m.ReasoningText(); reasoning != "" {
			return reasoning
		}
	}

	return result
}

// EmbeddingRequest represents a request to the embeddings API
type EmbeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"` // "float" or "base64"
	Dimensions     int      `json:"dimensions,omitempty"`
	User           string   `json:"user,omitempty"`
}

// EmbeddingResponse represents the response from the embeddings API
type EmbeddingResponse struct {
	Object string          `json:"object"` // "list"
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  EmbeddingUsage  `json:"usage"`
}

// EmbeddingData represents a single embedding in the response
type EmbeddingData struct {
	Object    string    `json:"object"` // "embedding"
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// EmbeddingUsage represents token usage for embeddings
type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ModelResponse represents a single model in the API response
type ModelResponse struct {
	ID            string   `json:"id"`
	Object        string   `json:"object"`
	Created       int64    `json:"created"`
	OwnedBy       string   `json:"owned_by,omitempty"`
	Type          string   `json:"type,omitempty"`           // TogetherAI: chat, language, code, image, embedding, moderation, rerank
	DisplayName   string   `json:"display_name,omitempty"`   // TogetherAI
	Organization  string   `json:"organization,omitempty"`   // TogetherAI
	Link          string   `json:"link,omitempty"`           // TogetherAI
	License       string   `json:"license,omitempty"`        // TogetherAI
	ContextLength int      `json:"context_length,omitempty"` // TogetherAI
	Pricing       *Pricing `json:"pricing,omitempty"`        // TogetherAI
}

// Pricing contains cost information for a model (TogetherAI specific).
type Pricing = llms.Pricing

// ModelsListResponse represents the response from the models list API
type ModelsListResponse struct {
	Object string          `json:"object"` // "list"
	Data   []ModelResponse `json:"data"`
}
