package llms

import "encoding/json"

// FinishReason describes why a model stopped generating output.
type FinishReason string

const (
	// FinishReasonStop indicates normal completion.
	FinishReasonStop FinishReason = "stop"
	// FinishReasonLength indicates the model hit a token or length limit.
	FinishReasonLength FinishReason = "length"
	// FinishReasonToolCalls indicates the model requested one or more tool calls.
	FinishReasonToolCalls FinishReason = "tool_calls"
	// FinishReasonContentFilter indicates generation stopped due to content filtering.
	FinishReasonContentFilter FinishReason = "content_filter"
)

// Response represents the response from an LLM
type Response struct {
	// ID is the provider's identifier for this response, when one is returned
	// (e.g. the OpenAI chat completion id, or the Responses API response id). It
	// is the handle used to chain server-side conversation state — pass it as the
	// previous response on the next turn (see the openai provider's Responses-API
	// options). Empty when the provider does not return an id.
	ID string `json:"id,omitempty"`

	Content string `json:"content,omitempty"`
	// Reasoning is the model's reasoning/chain-of-thought output, when the
	// provider/model supports it (OpenAI o-series, Anthropic extended thinking,
	// Gemini, Z.AI GLM, DeepSeek, Qwen). Nil otherwise.
	Reasoning *ReasoningContent `json:"reasoning,omitempty"`
	// Thinking is the former name of Reasoning, populated to the same value.
	//
	// Deprecated: use Reasoning. Retained for backward compatibility; it is not
	// serialized (Reasoning is the canonical JSON field) and is repopulated from
	// Reasoning when a Response is unmarshaled from JSON. Removed in a future major
	// version.
	Thinking      *ReasoningContent `json:"-"`
	FinishReason  FinishReason      `json:"finish_reason,omitempty"`
	Usage         Usage             `json:"usage"`
	ToolCalls     []ToolCall        `json:"tool_calls,omitempty"`     // Tool calls requested by the model
	SearchResults []SearchResult    `json:"search_results,omitempty"` // Web search results when WebSearch.IncludeResults is true
}

// UnmarshalJSON restores the deprecated Thinking alias from the canonical
// Reasoning JSON field.
func (r *Response) UnmarshalJSON(data []byte) error {
	type responseAlias Response
	var alias responseAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*r = Response(alias)
	r.Thinking = r.Reasoning
	return nil
}

// SetReasoning sets the canonical Reasoning field and the deprecated Thinking
// alias to the same value, so callers reading either field observe the result.
// Providers use this to surface reasoning output without duplicating assignments.
func (r *Response) SetReasoning(rc *ReasoningContent) {
	r.Reasoning = rc
	r.Thinking = rc
}

// ReasoningText returns the model's reasoning text, or "" if the response has no
// reasoning content.
func (r *Response) ReasoningText() string {
	if r == nil || r.Reasoning == nil {
		return ""
	}
	return r.Reasoning.Content
}

// Usage represents token usage information.
//
// Token semantics are normalized across providers so cost is computed uniformly:
//   - PromptTokens counts input tokens billed at the standard input rate and
//     EXCLUDES CacheReadTokens and CacheCreationTokens.
//   - CompletionTokens counts generated output tokens and INCLUDES ReasoningTokens.
//   - CacheReadTokens / CacheCreationTokens count input tokens served from / written
//     to a prompt cache (billed at the provider's cache-read / cache-write rates).
//   - ReasoningTokens is the subset of CompletionTokens spent on internal reasoning,
//     when the provider reports it separately (zero otherwise).
type Usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
	ReasoningTokens     int `json:"reasoning_tokens,omitempty"`
}

// StreamChunk represents a chunk of streamed content
type StreamChunk struct {
	// Content is the text content in this chunk
	Content string `json:"content,omitempty"`

	// Reasoning contains the model's reasoning ("thinking") content in this chunk,
	// when the provider/model supports it.
	Reasoning *ReasoningContent `json:"reasoning,omitempty"`

	// Thinking is the former name of Reasoning, populated to the same value.
	//
	// Deprecated: use Reasoning. Retained for backward compatibility; it is not
	// serialized (Reasoning is the canonical JSON field) and is repopulated from
	// Reasoning when a StreamChunk is unmarshaled from JSON. Removed in a future
	// major version.
	Thinking *ReasoningContent `json:"-"`

	// ToolCalls contains any tool calls in this chunk (may be partial)
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// FinishReason is set on the final chunk
	FinishReason FinishReason `json:"finish_reason,omitempty"`

	// Usage is only populated on the final chunk (if available)
	Usage *Usage `json:"usage,omitempty"`

	// Error is set if an error occurred during streaming
	Error error `json:"-"`

	// Done indicates this is the final chunk
	Done bool `json:"done,omitempty"`
}

// UnmarshalJSON restores the deprecated Thinking alias from the canonical
// Reasoning JSON field.
func (c *StreamChunk) UnmarshalJSON(data []byte) error {
	type streamChunkAlias StreamChunk
	var alias streamChunkAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*c = StreamChunk(alias)
	c.Thinking = c.Reasoning
	return nil
}
