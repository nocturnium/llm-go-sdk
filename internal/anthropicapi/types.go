// Package anthropicapi provides types and a client for the Anthropic Messages API.
//
// This package implements the Anthropic Messages API specification including:
//   - Request/response types for the /v1/messages endpoint
//   - Streaming with Server-Sent Events (message_start, content_block_delta, etc.)
//   - Tool calling with tool_use and tool_result content blocks
package anthropicapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

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
	OutputConfig  *OutputConfig   `json:"output_config,omitempty"`

	// ExtraBody carries caller-supplied fields merged into the top-level request
	// body by MarshalJSON. Standard fields always win over an ExtraBody key of the
	// same name.
	ExtraBody map[string]any `json:"-"`
}

// MarshalJSON merges ExtraBody into the top-level request object.
func (r MessagesRequest) MarshalJSON() ([]byte, error) {
	type plain MessagesRequest // no MarshalJSON: avoids recursion
	base, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	if len(r.ExtraBody) == 0 {
		return base, nil
	}
	var std map[string]json.RawMessage
	if err := json.Unmarshal(base, &std); err != nil {
		return nil, err
	}
	// A standard field wins even when it was omitted as a zero value (e.g. a
	// false Stream or an empty ToolChoice), so gate on every standard JSON name,
	// not only the ones present in the serialized map.
	known := standardJSONFields(reflect.TypeOf(plain(r)))
	m := make(map[string]json.RawMessage, len(r.ExtraBody)+16)
	for k, v := range r.ExtraBody {
		// Skip marshaling the ExtraBody value entirely: marshaling an
		// unmarshalable value under a key a standard field owns would fail the
		// whole request for a value that never reaches the wire.
		if known[k] {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("anthropic: marshal extra body field %q: %w", k, err)
		}
		m[k] = raw
	}
	for k, v := range std {
		m[k] = v
	}
	return json.Marshal(m)
}

// standardJSONFields caches the set of top-level JSON names the request struct
// declares. It lets MarshalJSON honor the documented precedence without
// inspecting the serialized map, which omitempty shrinks to non-zero fields.
var standardFields sync.Map // map[reflect.Type]map[string]bool

func standardJSONFields(typ reflect.Type) map[string]bool {
	if fields, ok := standardFields.Load(typ); ok {
		return fields.(map[string]bool)
	}
	fields := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		fields[name] = true
	}
	actual, _ := standardFields.LoadOrStore(typ, fields)
	return actual.(map[string]bool)
}

// ThinkingConfig configures Anthropic extended thinking. Type is one of
// "enabled" (with BudgetTokens, pre-4.6 models), "adaptive" (4.6+ models; the
// model decides how much to think) or "disabled".
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"` // "enabled" only
}

// OutputConfig carries output-shaping parameters. Effort controls thinking depth
// and overall token spend on models that support adaptive thinking.
type OutputConfig struct {
	Effort string `json:"effort,omitempty"` // "low" | "medium" | "high" | "xhigh" | "max"
}

// Message represents a message in the conversation.
type Message struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// ContentPart represents a part of the message content.
type ContentPart struct {
	Type string `json:"type"`

	Text   string       `json:"text,omitempty"`
	Source *ImageSource `json:"source,omitempty"`

	// For tool_use content
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// For tool_result content
	ToolUseID string             `json:"tool_use_id,omitempty"`
	Content   *ToolResultContent `json:"content,omitempty"`
	IsError   bool               `json:"is_error,omitempty"`

	// For thinking content (extended thinking). Signature authenticates the
	// thinking block and must be echoed back on a follow-up turn. Data carries an
	// encrypted redacted_thinking block.
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`

	// CacheControl marks this block as a prompt-caching breakpoint.
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ToolResultContent is the content of a tool_result block. Anthropic accepts
// either a plain string or an array of content blocks; the string form is the
// common case and array blocks are decoded so callers can inspect or forward
// them.
type ToolResultContent struct {
	Text     string
	Array    []ContentPart
	useArray bool
}

// MarshalJSON writes the string form when only Text is set, otherwise the array
// form.
func (c ToolResultContent) MarshalJSON() ([]byte, error) {
	if c.useArray || len(c.Array) > 0 {
		return json.Marshal(c.Array)
	}
	return json.Marshal(c.Text)
}

// UnmarshalJSON accepts the string and array forms Anthropic may return.
func (c *ToolResultContent) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*c = ToolResultContent{}
		return nil
	}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*c = ToolResultContent{Text: text}
		return nil
	}
	var parts []ContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	*c = ToolResultContent{Array: parts, useArray: true}
	return nil
}

// ImageSource represents the source of an image for Anthropic's vision API.
// Type "base64" carries MediaType+Data; type "url" carries URL.
type ImageSource struct {
	Type      string `json:"type"`                 // "base64" or "url"
	MediaType string `json:"media_type,omitempty"` // e.g., "image/png", "image/jpeg"
	Data      string `json:"data,omitempty"`       // Base64-encoded image data
	URL       string `json:"url,omitempty"`        // Publicly reachable image URL
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
	// MessageDelta is tagged `json:"-"`, so the alias unmarshal above never
	// clears it. A caller that reuses one StreamEvent across events would
	// otherwise see a stale stop-reason delta on a later non-message_delta
	// event.
	e.MessageDelta = nil

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
			// The alias unmarshal above also decoded this same "delta" object
			// into the content-delta field. Clear it so a message_delta event
			// does not present an empty content delta alongside the message
			// metadata; MessageDelta is the only delta this event carries.
			e.Delta = nil
		}
	}

	return nil
}

// MarshalJSON always writes a thinking block's "thinking" field. Recent models
// can return a thinking block whose text is empty, with only a signature, and
// the API rejects the block when it is replayed without the field ("thinking:
// Field required"), which omitempty would otherwise do.
func (p ContentPart) MarshalJSON() ([]byte, error) {
	type plain ContentPart
	if p.Type != "thinking" {
		return json.Marshal(plain(p))
	}
	return json.Marshal(struct {
		plain
		Thinking string `json:"thinking"`
	}{plain(p), p.Thinking})
}
