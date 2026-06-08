// Package anthropicapi provides types and a client for the Anthropic Messages API.
//
// This package implements the Anthropic Messages API specification including:
//   - Request/response types for the /v1/messages endpoint
//   - Streaming with Server-Sent Events (message_start, content_block_delta, etc.)
//   - Tool calling with tool_use and tool_result content blocks
package anthropicapi

import "encoding/json"

// MessagesRequest represents a request to the Anthropic Messages API.
type MessagesRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []Message     `json:"messages"`
	System    []SystemBlock `json:"system,omitempty"`
	// Temperature and TopP are pointers so an explicit 0.0 is serialized while an
	// unset (nil) value is omitted, letting the model apply its own default.
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	Tools         []Tool          `json:"tools,omitempty"`
	ToolChoice    any             `json:"tool_choice,omitempty"`
	Thinking      *ThinkingConfig `json:"thinking,omitempty"`
}

// ThinkingConfig enables Anthropic extended thinking with an explicit token
// budget. When set, the model emits thinking content blocks before its answer.
type ThinkingConfig struct {
	Type         string `json:"type"`          // "enabled"
	BudgetTokens int    `json:"budget_tokens"` // tokens the model may spend thinking
}

// Message represents a message in the conversation.
type Message struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// ContentPart represents a part of the message content.
type ContentPart struct {
	Type string `json:"type"`

	// For text content
	Text string `json:"text,omitempty"`

	// For image content
	Source *ImageSource `json:"source,omitempty"`

	// For tool_use content
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// For tool_result content
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	// For thinking content (extended thinking). Signature authenticates the
	// thinking block and must be echoed back on a follow-up turn. Data carries an
	// encrypted redacted_thinking block.
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`

	// CacheControl marks this block as a prompt-caching breakpoint.
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageSource represents the source of an image for Anthropic's vision API.
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // e.g., "image/png", "image/jpeg"
	Data      string `json:"data"`       // Base64-encoded image data
}

// Tool represents a tool definition for Anthropic.
type Tool struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	InputSchema  any           `json:"input_schema"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// CacheControl marks a content block as a prompt-caching breakpoint.
type CacheControl struct {
	Type string `json:"type"`          // "ephemeral"
	TTL  string `json:"ttl,omitempty"` // "" (5m default) or "1h"
}

// SystemBlock is a system-prompt text block that can carry a cache breakpoint.
type SystemBlock struct {
	Type         string        `json:"type"` // "text"
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// MessagesResponse represents the response from the Messages API.
type MessagesResponse struct {
	ID           string        `json:"id"`
	Type         string        `json:"type"`
	Role         string        `json:"role"`
	Content      []ContentPart `json:"content"`
	Model        string        `json:"model"`
	StopReason   string        `json:"stop_reason"`
	StopSequence string        `json:"stop_sequence,omitempty"`
	Usage        Usage         `json:"usage"`
}

// Usage represents token usage.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// StreamEvent represents an event from the streaming API.
type StreamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index,omitempty"`

	// For message_start
	Message *MessagesResponse `json:"message,omitempty"`

	// For content_block_start
	ContentBlock *ContentPart `json:"content_block,omitempty"`

	// For content_block_delta
	Delta *StreamDelta `json:"delta,omitempty"`

	// For message_delta (parsed from "delta" field when type is "message_delta")
	MessageDelta *MessageDelta `json:"-"`

	// For usage updates
	Usage *Usage `json:"usage,omitempty"`
}

// StreamDelta represents delta content in a stream.
type StreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	// Thinking and Signature carry extended-thinking deltas (delta types
	// "thinking_delta" and "signature_delta").
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// MessageDelta represents message-level delta in a stream.
type MessageDelta struct {
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

// ToolChoiceAuto lets the model decide whether to use tools.
type ToolChoiceAuto struct {
	Type string `json:"type"` // "auto"
}

// ToolChoiceAny forces the model to use a tool.
type ToolChoiceAny struct {
	Type string `json:"type"` // "any"
}

// ToolChoiceTool forces the model to use a specific tool.
type ToolChoiceTool struct {
	Type string `json:"type"` // "tool"
	Name string `json:"name"`
}

// ModelInfo represents a single model in the Anthropic API response.
type ModelInfo struct {
	ID          string `json:"id"`           // Unique model identifier
	Type        string `json:"type"`         // Always "model"
	DisplayName string `json:"display_name"` // Human-readable name
	CreatedAt   string `json:"created_at"`   // RFC 3339 datetime
}

// ModelsListResponse represents the response from listing models.
type ModelsListResponse struct {
	Data    []ModelInfo `json:"data"`
	HasMore bool        `json:"has_more"`
	FirstID string      `json:"first_id"`
	LastID  string      `json:"last_id"`
}

// ModelsListParams represents query parameters for listing models.
type ModelsListParams struct {
	Limit    int    `json:"limit,omitempty"`     // 1-1000, default 20
	AfterID  string `json:"after_id,omitempty"`  // Cursor for next page
	BeforeID string `json:"before_id,omitempty"` // Cursor for previous page
}

// UnmarshalJSON implements custom unmarshaling for StreamEvent to handle
// the "delta" field which can be either StreamDelta or MessageDelta depending
// on the event type.
func (e *StreamEvent) UnmarshalJSON(data []byte) error {
	// First, unmarshal into a type alias to avoid recursion
	type streamEventAlias StreamEvent
	var alias streamEventAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	*e = StreamEvent(alias)

	// For message_delta events, the "delta" field contains MessageDelta data
	if e.Type == "message_delta" {
		var raw struct {
			Delta json.RawMessage `json:"delta"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		if len(raw.Delta) > 0 {
			var msgDelta MessageDelta
			if err := json.Unmarshal(raw.Delta, &msgDelta); err != nil {
				return err
			}
			e.MessageDelta = &msgDelta
		}
	}

	return nil
}
