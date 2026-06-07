package llms

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
	Content       string           `json:"content,omitempty"`
	Thinking      *ThinkingContent `json:"thinking,omitempty"` // nil if provider doesn't support reasoning
	FinishReason  FinishReason     `json:"finish_reason,omitempty"`
	Usage         Usage            `json:"usage"`
	ToolCalls     []ToolCall       `json:"tool_calls,omitempty"`     // Tool calls requested by the model
	SearchResults []SearchResult   `json:"search_results,omitempty"` // Web search results when WebSearch.IncludeResults is true
}

// ThinkingContent represents the model's reasoning/chain-of-thought output.
// Supported by providers like Z.AI (GLM), OpenAI (o1), and DeepSeek.
type ThinkingContent struct {
	// Content is the model's reasoning text
	Content string `json:"content,omitempty"`

	// Tokens is the number of tokens used for reasoning (if reported separately)
	// Zero if not available from the provider
	Tokens int `json:"tokens,omitempty"`

	// Metadata contains provider-specific details about the reasoning
	// Examples: thinking mode used, reasoning steps, confidence scores
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
}

// StreamChunk represents a chunk of streamed content
type StreamChunk struct {
	// Content is the text content in this chunk
	Content string `json:"content,omitempty"`

	// Thinking contains reasoning content in this chunk
	Thinking *ThinkingContent `json:"thinking,omitempty"`

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
