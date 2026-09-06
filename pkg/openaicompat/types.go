package openaicompat

// This file defines the OpenAI-compatible wire types (requests, responses,
// streaming chunks, embeddings, and model listings). The package-level
// documentation lives in doc.go.

import (
	"encoding/json"

	llms "github.com/nocturnium/llm-go-sdk/v6"
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

// ImageGenerationRequest is the JSON image generation request. ExtraBody overrides standard fields.
type ImageGenerationRequest struct {
	Model             string         `json:"model"`
	Prompt            string         `json:"prompt"`
	N                 int            `json:"n,omitempty"`
	Size              string         `json:"size,omitempty"`
	Quality           string         `json:"quality,omitempty"`
	OutputFormat      string         `json:"output_format,omitempty"`
	OutputCompression *int           `json:"output_compression,omitempty"`
	Background        string         `json:"background,omitempty"`
	Moderation        string         `json:"moderation,omitempty"`
	Stream            bool           `json:"stream,omitempty"`
	PartialImages     *int           `json:"partial_images,omitempty"`
	User              string         `json:"user,omitempty"`
	ResponseFormat    string         `json:"response_format,omitempty"`
	ExtraBody         map[string]any `json:"-"`
}

// MarshalJSON merges additional body keys last.
func (r ImageGenerationRequest) MarshalJSON() ([]byte, error) {
	type plain ImageGenerationRequest
	return marshalMediaExtra(plain(r), r.ExtraBody)
}

func marshalMediaExtra(value any, extra map[string]any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range extra {
		fields[key] = value
	}
	return json.Marshal(fields)
}

// ImageEditRequest contains multipart fields and inline image uploads.
type ImageEditRequest struct {
	Model         string            `json:"model"`
	Prompt        string            `json:"prompt"`
	N             int               `json:"n,omitempty"`
	Size          string            `json:"size,omitempty"`
	Quality       string            `json:"quality,omitempty"`
	OutputFormat  string            `json:"output_format,omitempty"`
	InputFidelity string            `json:"input_fidelity,omitempty"`
	Images        []llms.MediaInput `json:"-"`
	Mask          *llms.MediaInput  `json:"-"`
	ExtraBody     map[string]any    `json:"-"`
}

// ImageData describes one generated image.
type ImageData struct {
	MediaType     string `json:"media_type,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	URL           string `json:"url,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// ImageUsage describes image or audio token accounting.
type ImageUsage struct {
	// InputCharacters is reported by character-billed speech services.
	InputCharacters    *int     `json:"input_characters,omitempty"`
	PromptTokens       int      `json:"prompt_tokens,omitempty"`
	CompletionTokens   int      `json:"completion_tokens,omitempty"`
	Cost               *float64 `json:"cost,omitempty"`
	InputTokens        int      `json:"input_tokens,omitempty"`
	OutputTokens       int      `json:"output_tokens,omitempty"`
	TotalTokens        int      `json:"total_tokens,omitempty"`
	InputTokensDetails *struct {
		TextTokens  int `json:"text_tokens,omitempty"`
		ImageTokens int `json:"image_tokens,omitempty"`
	} `json:"input_tokens_details,omitempty"`
}

// ImageResponse is a generated image response.
type ImageResponse struct {
	Created int64       `json:"created,omitempty"`
	Data    []ImageData `json:"data"`
	Usage   *ImageUsage `json:"usage,omitempty"`
}

// ImageStreamEvent is an image generation or edit SSE event; Err is a transport/decoding error.
type ImageStreamEvent struct {
	Type              string      `json:"type,omitempty"`
	B64JSON           string      `json:"b64_json,omitempty"`
	PartialImageIndex int         `json:"partial_image_index,omitempty"`
	Usage             *ImageUsage `json:"usage,omitempty"`
	Err               error       `json:"-"`
}

// SpeechRequest requests binary audio or SSE audio deltas.
type SpeechRequest struct {
	ExtraBody      map[string]any `json:"-"`
	Model          string         `json:"model"`
	Input          string         `json:"input"`
	Voice          string         `json:"voice"`
	Instructions   string         `json:"instructions,omitempty"`
	ResponseFormat string         `json:"response_format,omitempty"`
	Speed          *float64       `json:"speed,omitempty"`
	StreamFormat   string         `json:"stream_format,omitempty"`
}

// MarshalJSON merges extensions while reserving all standard speech fields.
func (r SpeechRequest) MarshalJSON() ([]byte, error) {
	type plain SpeechRequest
	extra := make(map[string]any, len(r.ExtraBody))
	for k, v := range r.ExtraBody {
		switch k {
		case "model", "input", "voice", "instructions", "response_format", "speed", "stream_format":
		default:
			extra[k] = v
		}
	}
	if len(extra) == 0 {
		return json.Marshal(plain(r))
	}
	return marshalMediaExtra(plain(r), extra)
}

// SpeechStreamEvent contains base64 audio or terminal usage; Err reports stream failures.
type SpeechStreamEvent struct {
	Type  string      `json:"type,omitempty"`
	Audio string      `json:"audio,omitempty"`
	Usage *ImageUsage `json:"usage,omitempty"`
	Err   error       `json:"-"`
}

// TranscriptionRequest contains multipart transcription fields and an inline audio file.
type TranscriptionRequest struct {
	File                   llms.MediaInput `json:"-"`
	Model                  string          `json:"model"`
	Language               string          `json:"language,omitempty"`
	Prompt                 string          `json:"prompt,omitempty"`
	ResponseFormat         string          `json:"response_format,omitempty"`
	Temperature            *float64        `json:"temperature,omitempty"`
	TimestampGranularities []string        `json:"timestamp_granularities,omitempty"`
	Stream                 bool            `json:"stream,omitempty"`
	Include                []string        `json:"include,omitempty"`
	ExtraBody              map[string]any  `json:"-"`
}

// TranscriptionResponse covers JSON, verbose JSON, diarized JSON, and text responses.
type TranscriptionResponse struct {
	Text     string                 `json:"text"`
	Language string                 `json:"language,omitempty"`
	Duration float64                `json:"duration,omitempty"`
	Segments []TranscriptionSegment `json:"segments,omitempty"`
	Words    []TranscriptionWord    `json:"words,omitempty"`
	Usage    *TranscriptionUsage    `json:"usage,omitempty"`
}

// TranscriptionUsage reports either duration billing or token billing.
type TranscriptionUsage struct {
	Cost              *float64 `json:"cost,omitempty"`
	Type              string   `json:"type"`
	Seconds           float64  `json:"seconds,omitempty"`
	TotalTokens       int      `json:"total_tokens,omitempty"`
	InputTokens       int      `json:"input_tokens,omitempty"`
	InputTokenDetails struct {
		TextTokens  int `json:"text_tokens,omitempty"`
		AudioTokens int `json:"audio_tokens,omitempty"`
	} `json:"input_token_details,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// TranscriptionSegment is one timed, optionally speaker-labeled segment.
type TranscriptionSegment struct {
	// ID is an integer on verbose_json responses and a string such as
	// "seg_0" on diarized_json responses, so it is kept as raw JSON.
	ID      json.RawMessage `json:"id,omitempty"`
	Start   float64         `json:"start"`
	End     float64         `json:"end"`
	Text    string          `json:"text"`
	Speaker string          `json:"speaker,omitempty"`
}

// TranscriptionWord is one timed word.
type TranscriptionWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// VideoCreateRequest submits a video job; Seconds is a wire string.
type VideoCreateRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	Seconds        string `json:"seconds,omitempty"`
	Size           string `json:"size,omitempty"`
	InputReference string `json:"input_reference,omitempty"`
}

// VideoObject describes a submitted video job.
type VideoObject struct {
	ID        string      `json:"id"`
	Object    string      `json:"object,omitempty"`
	Status    string      `json:"status"`
	Progress  *float64    `json:"progress,omitempty"`
	CreatedAt int64       `json:"created_at,omitempty"`
	ExpiresAt int64       `json:"expires_at,omitempty"`
	Model     string      `json:"model,omitempty"`
	Seconds   string      `json:"seconds,omitempty"`
	Size      string      `json:"size,omitempty"`
	Error     *VideoError `json:"error,omitempty"`
}

// VideoError describes a failed video job.
type VideoError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
