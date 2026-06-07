package llms

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Pricing represents the cost per million tokens for a model.
type Pricing struct {
	PromptPerMillion     float64 // Cost per 1M prompt tokens in USD
	CompletionPerMillion float64 // Cost per 1M completion tokens in USD
}

// DefaultPricing contains known pricing for common models (as of Dec 2024).
// Prices are in USD per 1 million tokens.
var DefaultPricing = map[string]Pricing{
	// OpenAI
	"openai:gpt-4o":                 {2.50, 10.00},
	"openai:gpt-4o-2024-11-20":      {2.50, 10.00},
	"openai:gpt-4o-2024-08-06":      {2.50, 10.00},
	"openai:gpt-4o-mini":            {0.15, 0.60},
	"openai:gpt-4o-mini-2024-07-18": {0.15, 0.60},
	"openai:gpt-4-turbo":            {10.00, 30.00},
	"openai:gpt-4":                  {30.00, 60.00},
	"openai:gpt-3.5-turbo":          {0.50, 1.50},
	"openai:o1":                     {15.00, 60.00},
	"openai:o1-mini":                {3.00, 12.00},

	// OpenAI Embeddings
	"openai:text-embedding-3-small": {0.02, 0.0},
	"openai:text-embedding-3-large": {0.13, 0.0},
	"openai:text-embedding-ada-002": {0.10, 0.0},

	// Anthropic
	"anthropic:claude-3-5-sonnet-20241022": {3.00, 15.00},
	"anthropic:claude-3-5-sonnet-20240620": {3.00, 15.00},
	"anthropic:claude-3-5-haiku-20241022":  {0.80, 4.00},
	"anthropic:claude-3-opus-20240229":     {15.00, 75.00},
	"anthropic:claude-3-sonnet-20240229":   {3.00, 15.00},
	"anthropic:claude-3-haiku-20240307":    {0.25, 1.25},

	// Google Gemini
	"gemini:gemini-2.0-flash-exp": {0.0, 0.0}, // Free during preview
	"gemini:gemini-2.0-flash":     {0.075, 0.30},
	"gemini:gemini-1.5-flash":     {0.075, 0.30},
	"gemini:gemini-1.5-flash-8b":  {0.0375, 0.15},
	"gemini:gemini-1.5-pro":       {1.25, 5.00},
	"gemini:gemini-1.0-pro":       {0.50, 1.50},
	"gemini:text-embedding-004":   {0.0, 0.0}, // Free

	// TogetherAI (varies by model, these are examples)
	"togetherai:meta-llama/Llama-3.3-70B-Instruct-Turbo":       {0.88, 0.88},
	"togetherai:meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo": {3.50, 3.50},
	"togetherai:meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo":  {0.88, 0.88},
	"togetherai:meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo":   {0.18, 0.18},
	"togetherai:mistralai/Mixtral-8x7B-Instruct-v0.1":          {0.60, 0.60},
}

// ModelUsage tracks usage for a specific model.
type ModelUsage struct {
	Provider            Provider  `json:"provider"`
	Model               string    `json:"model"`
	PromptTokens        int64     `json:"prompt_tokens"`
	CompletionTokens    int64     `json:"completion_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	Requests            int64     `json:"requests"`
	EstimatedCost       float64   `json:"estimated_cost_usd"`
	FirstUsed           time.Time `json:"first_used"`
	LastUsed            time.Time `json:"last_used"`
}

// CostTracker tracks token usage and estimated costs across providers/models.
//
// Thread-safety: All methods are safe for concurrent use. The tracker uses
// an RWMutex internally to allow concurrent reads (GetUsage, GetTotalCost,
// GetAllUsage) while serializing writes (Record, RecordEmbedding).
type CostTracker struct {
	mu      sync.RWMutex
	usage   map[string]*ModelUsage
	pricing map[string]Pricing
}

// NewCostTracker creates a new cost tracker with optional custom pricing.
func NewCostTracker(customPricing ...map[string]Pricing) *CostTracker {
	// Merge default and custom pricing
	pricing := make(map[string]Pricing)
	for k, v := range DefaultPricing {
		pricing[k] = v
	}
	for _, cp := range customPricing {
		for k, v := range cp {
			pricing[k] = v
		}
	}

	return &CostTracker{
		usage:   make(map[string]*ModelUsage),
		pricing: pricing,
	}
}

// maxSafeTokenCount is the maximum token count before we risk integer overflow.
// At this point, we've processed over 4 quadrillion tokens which would cost
// billions of dollars - so this is effectively unreachable in practice.
const maxSafeTokenCount = 1<<62 - 1

// Record records token usage for a request.
// If token counts approach overflow (extremely unlikely), the record is skipped
// to prevent data corruption.
//
// Lock contention optimization: Cost calculation is performed before acquiring
// the write lock to minimize lock hold time under high concurrency.
func (t *CostTracker) Record(provider Provider, model string, usage Usage) {
	key := t.makeKey(provider, model)
	now := time.Now()

	// Pre-calculate cost OUTSIDE the lock to minimize lock hold time.
	// Pricing data is read-only after initialization, so this is safe.
	var cost float64
	t.mu.RLock()
	if pricing, ok := t.pricing[key]; ok {
		promptCost := float64(usage.PromptTokens) / 1_000_000 * pricing.PromptPerMillion
		completionCost := float64(usage.CompletionTokens) / 1_000_000 * pricing.CompletionPerMillion
		cost = promptCost + completionCost
	}
	t.mu.RUnlock()

	// Now acquire write lock for minimal duration
	t.mu.Lock()
	defer t.mu.Unlock()

	u, ok := t.usage[key]
	if !ok {
		u = &ModelUsage{
			Provider:  provider,
			Model:     model,
			FirstUsed: now,
		}
		t.usage[key] = u
	}

	// Check for potential overflow before adding (extremely conservative).
	// In practice, this would require processing quadrillions of tokens.
	if u.PromptTokens > maxSafeTokenCount || u.CompletionTokens > maxSafeTokenCount {
		// Counters have reached safe maximum - skip this record to prevent overflow.
		// In production, consider rotating to a new tracking window or alerting.
		u.LastUsed = now
		return
	}

	u.PromptTokens += int64(usage.PromptTokens)
	u.CompletionTokens += int64(usage.CompletionTokens)
	u.CacheReadTokens += int64(usage.CacheReadTokens)
	u.CacheCreationTokens += int64(usage.CacheCreationTokens)
	u.Requests++
	u.LastUsed = now
	u.EstimatedCost += cost
}

// RecordEmbedding records usage for an embedding request.
func (t *CostTracker) RecordEmbedding(provider Provider, model string, usage EmbeddingUsage) {
	t.Record(provider, model, Usage{
		PromptTokens: usage.PromptTokens,
		TotalTokens:  usage.TotalTokens,
	})
}

// GetUsage returns usage for a specific model.
func (t *CostTracker) GetUsage(provider Provider, model string) *ModelUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := t.makeKey(provider, model)
	if u, ok := t.usage[key]; ok {
		// Return a copy
		usageCopy := *u
		return &usageCopy
	}
	return nil
}

// GetTotalCost returns the total estimated cost across all models.
func (t *CostTracker) GetTotalCost() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var total float64
	for _, u := range t.usage {
		total += u.EstimatedCost
	}
	return total
}

// GetTotalTokens returns total prompt and completion tokens.
func (t *CostTracker) GetTotalTokens() (prompt, completion int64) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, u := range t.usage {
		prompt += u.PromptTokens
		completion += u.CompletionTokens
	}
	return
}

// GetTotalRequests returns total number of requests.
func (t *CostTracker) GetTotalRequests() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var total int64
	for _, u := range t.usage {
		total += u.Requests
	}
	return total
}

// Report returns usage for all tracked models.
func (t *CostTracker) Report() []ModelUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]ModelUsage, 0, len(t.usage))
	for _, u := range t.usage {
		result = append(result, *u)
	}
	return result
}

// Reset clears all usage data.
func (t *CostTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.usage = make(map[string]*ModelUsage)
}

// SetPricing sets or updates pricing for a model.
func (t *CostTracker) SetPricing(provider Provider, model string, pricing Pricing) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pricing[t.makeKey(provider, model)] = pricing
}

// GetPricing returns pricing for a model.
func (t *CostTracker) GetPricing(provider Provider, model string) (Pricing, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.pricing[t.makeKey(provider, model)]
	return p, ok
}

func (t *CostTracker) makeKey(provider Provider, model string) string {
	return fmt.Sprintf("%s:%s", provider, model)
}

// CostMiddleware wraps an LLM with cost tracking.
type CostMiddleware struct {
	llm     LLM
	tracker *CostTracker
}

// NewCostMiddleware creates a new cost tracking middleware.
func NewCostMiddleware(llm LLM, tracker *CostTracker) *CostMiddleware {
	if tracker == nil {
		tracker = NewCostTracker()
	}
	return &CostMiddleware{
		llm:     llm,
		tracker: tracker,
	}
}

// Call wraps the LLM's Call method with cost tracking.
func (m *CostMiddleware) Call(ctx context.Context, prompt string, options ...CallOption) (string, error) {
	// Call doesn't provide token usage, use GenerateContent internally
	messages := []Message{{Role: RoleUser, Content: prompt}}
	resp, err := m.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// GenerateContent wraps the LLM's GenerateContent method with cost tracking.
func (m *CostMiddleware) GenerateContent(ctx context.Context, messages []Message, options ...CallOption) (*Response, error) {
	resp, err := m.llm.GenerateContent(ctx, messages, options...)
	if err != nil {
		return nil, err
	}

	m.tracker.Record(m.llm.Provider(), m.llm.Model(), resp.Usage)
	return resp, nil
}

// Stream wraps the LLM's Stream method with cost tracking.
func (m *CostMiddleware) Stream(ctx context.Context, messages []Message, options ...CallOption) (<-chan StreamChunk, error) {
	stream, err := m.llm.Stream(ctx, messages, options...)
	if err != nil {
		return nil, err
	}

	opts := ApplyOptions(options...)
	var lastUsage *Usage

	return WrapStreamWithFinalizer(ctx, stream, opts,
		func(chunk StreamChunk) StreamChunk {
			if chunk.Usage != nil {
				lastUsage = chunk.Usage
			}
			return chunk
		},
		func() {
			if lastUsage != nil {
				m.tracker.Record(m.llm.Provider(), m.llm.Model(), *lastUsage)
			}
		},
	), nil
}

// Provider returns the underlying LLM's provider.
func (m *CostMiddleware) Provider() Provider {
	return m.llm.Provider()
}

// Model returns the underlying LLM's model.
func (m *CostMiddleware) Model() string {
	return m.llm.Model()
}

// Unwrap returns the underlying LLM.
func (m *CostMiddleware) Unwrap() LLM {
	return m.llm
}

// Tracker returns the cost tracker.
func (m *CostMiddleware) Tracker() *CostTracker {
	return m.tracker
}

// Ensure CostMiddleware implements LLM.
var _ LLM = (*CostMiddleware)(nil)

// EstimateCost calculates the estimated cost for a given usage.
func EstimateCost(provider Provider, model string, usage Usage) float64 {
	key := fmt.Sprintf("%s:%s", provider, model)
	pricing, ok := DefaultPricing[key]
	if !ok {
		return 0
	}

	promptCost := float64(usage.PromptTokens) / 1_000_000 * pricing.PromptPerMillion
	completionCost := float64(usage.CompletionTokens) / 1_000_000 * pricing.CompletionPerMillion
	return promptCost + completionCost
}

// FormatCost formats a cost value as a USD string.
func FormatCost(cost float64) string {
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
