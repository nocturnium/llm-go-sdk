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

// Response represents the response from an LLM.
//
// Its read methods take a pointer receiver and tolerate a nil Response;
// SetReasoning writes to the receiver, so it needs a real one.
type Response struct {
	// ID is the provider's identifier for this response, when one is returned
	// (e.g. the OpenAI chat completion id, or the Responses API response id). It
	// is the handle used to chain server-side conversation state, pass it as the
	// previous response on the next turn (see the openai provider's Responses-API
	// options). Empty when the provider does not return an id.
	ID string `json:"id,omitempty"`

	Content string `json:"content,omitempty"`
	// Reasoning is the model's reasoning/chain-of-thought output, when the
	// provider/model supports it (OpenAI o-series, Anthropic extended thinking,
	// Gemini, Z.AI GLM, DeepSeek, Qwen). Nil otherwise.
	Reasoning     *ReasoningContent `json:"reasoning,omitempty"`
	FinishReason  FinishReason      `json:"finish_reason,omitempty"`
	Usage         Usage             `json:"usage"`
	ToolCalls     []ToolCall        `json:"tool_calls,omitempty"`     // Tool calls requested by the model
	SearchResults []SearchResult    `json:"search_results,omitempty"` // Web search results when WebSearch.IncludeResults is true

	// ServiceTier names the capacity tier that served this request, for
	// providers that sell more than one grade of capacity per model and report
	// which one ran: "default", "flex" or "priority" (OpenAI, OpenRouter).
	// Empty when the provider reports nothing, which includes every provider
	// without tiers. A stream carries the served tier on its final
	// [StreamChunk] instead.
	//
	// It reports what served the request, not what was asked for: a priority
	// request can fall back to another endpoint and is then billed at that
	// endpoint's rate. Requesting a tier is provider-specific (see
	// openrouter.WithServiceTier); pricing it is [WithPricingMode].
	ServiceTier string `json:"service_tier,omitempty"`

	// Provider and Model identify what served this request: the provider that
	// answered and the model ID that was requested of it (a [WithModel] override,
	// else the client's default). Model is the requested ID, never the snapshot
	// the API reports back, so it compares equal to the configured model; the
	// reported snapshot is ModelVersion. Middleware that routes between clients,
	// such as a fallback chain, passes the serving client's values through, so
	// these may differ from the Provider and Model methods of the LLM that was
	// called. Empty when the answering client does not report them.
	Provider Provider `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	// ModelVersion is the model version the provider reported serving (for
	// example a dated snapshot of an alias), when it reports one.
	ModelVersion string `json:"model_version,omitempty"`

	// Adjustments names the changes a provider made to the request so that it
	// would be accepted, such as suspending extended thinking for one turn whose
	// history no longer carries a replayable thinking block. Each entry is a
	// stable dotted name ("anthropic.thinking_suspended"). Empty when the request
	// was sent as given.
	Adjustments []string `json:"adjustments,omitempty"`
}

// SetReasoning sets the canonical Reasoning field. Providers use this to
// surface reasoning output without duplicating assignments.
func (r *Response) SetReasoning(rc *ReasoningContent) {
	r.Reasoning = rc
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

	// Cost is the charge in USD the provider reported for this request, nil when
	// it reported none. It is what was billed rather than what a rate card
	// predicts, so [CostTracker] banks it in preference to its own estimate, and
	// in preference to a card registered with SetPricing.
	//
	// Providers report it only on request: see openrouter.WithUsageAccounting.
	Cost *float64 `json:"cost,omitempty"`
}

// StreamChunk represents a chunk of streamed content.
// StreamChunk methods use pointer receivers for nil-safety.
type StreamChunk struct {
	// Content is the text content in this chunk
	Content string `json:"content,omitempty"`

	// Reasoning contains the model's reasoning ("thinking") content in this chunk,
	// when the provider/model supports it.
	Reasoning *ReasoningContent `json:"reasoning,omitempty"`

	// ToolCalls holds the stream's tool calls. Every provider in this module
	// delivers them complete, arguments and all, on the final (Done) chunk only;
	// chunks before it carry none. Middleware that re-emits a stream preserves
	// that, so a consumer reads tool calls from the Done chunk (as
	// [CollectStream] does).
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// FinishReason is set on the final chunk
	FinishReason FinishReason `json:"finish_reason,omitempty"`

	// Usage is only populated on the final chunk (if available)
	Usage *Usage `json:"usage,omitempty"`

	// ServiceTier names the capacity tier that served the request, for providers
	// that sell more than one grade of capacity per model and report which one
	// ran. It is carried on the final chunk, matching [Response.ServiceTier].
	ServiceTier string `json:"service_tier,omitempty"`

	// Provider, Model, ModelVersion and Adjustments are carried on the final
	// chunk and mean what they mean on [Response].
	Provider     Provider `json:"provider,omitempty"`
	Model        string   `json:"model,omitempty"`
	ModelVersion string   `json:"model_version,omitempty"`
	Adjustments  []string `json:"adjustments,omitempty"`

	// Error is set if an error occurred during streaming
	Error error `json:"-"`

	// Done indicates this is the final chunk
	Done bool `json:"done,omitempty"`
}
