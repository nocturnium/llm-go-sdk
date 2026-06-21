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
	// CacheReadPerMillion is the cost per 1M cache-read (cache-hit) tokens. Zero
	// falls back to the standard prompt rate (a conservative estimate).
	CacheReadPerMillion float64
	// CacheWritePerMillion is the cost per 1M cache-write (cache-creation) tokens.
	// Zero falls back to the standard prompt rate.
	CacheWritePerMillion float64
}

// cacheReadRate returns the effective cache-read rate, defaulting to the prompt
// rate when no cache-specific rate is configured.
func (p Pricing) cacheReadRate() float64 {
	if p.CacheReadPerMillion > 0 {
		return p.CacheReadPerMillion
	}
	return p.PromptPerMillion
}

// cacheWriteRate returns the effective cache-write rate, defaulting to the prompt
// rate when no cache-specific rate is configured.
func (p Pricing) cacheWriteRate() float64 {
	if p.CacheWritePerMillion > 0 {
		return p.CacheWritePerMillion
	}
	return p.PromptPerMillion
}

// cost computes the total USD cost for the given usage under this pricing,
// accounting for discounted cache-read and cache-write tokens. PromptTokens is
// assumed to exclude cache tokens (see the Usage contract).
func (p Pricing) cost(usage Usage) float64 {
	const perMillion = 1_000_000.0
	return float64(usage.PromptTokens)/perMillion*p.PromptPerMillion +
		float64(usage.CompletionTokens)/perMillion*p.CompletionPerMillion +
		float64(usage.CacheReadTokens)/perMillion*p.cacheReadRate() +
		float64(usage.CacheCreationTokens)/perMillion*p.cacheWriteRate()
}

// DefaultPricing is the single source of truth for built-in token pricing.
// Prices are in USD per 1 million tokens. Provider model metadata derives its
// displayed token pricing from this table via ModelTokenPricing; do not duplicate
// token Input/Output pricing literals in provider known-model tables. Cache
// rates are set where the provider charges a distinct cache-read/cache-write
// rate (e.g. Anthropic read ≈0.1×, write ≈1.25×; OpenAI cached read ≈0.5×);
// when omitted, cache reads/writes fall back to the prompt rate.
//
// Coverage is intentionally partial: models without an entry have no built-in
// pricing data, so cost lookups report them as unknown (the pricing lookup
// returns ok=false) rather than guessing a price. Register accurate pricing for
// such models via the cost tracker's custom-pricing API instead of relying on a
// silent $0.00.
//
// Some providers are deliberately left unpriced so lookups keep returning
// ok=false: ollama and llamacpp run locally/free and have no per-token list price
// (N/A); Cerebras no longer lists the default llama3.1-70b model on its public
// 2026 per-token rate card (custom Dedicated Endpoint pricing only), so it is
// not publicly verifiable; Mistral defaults are moving "latest" aliases with
// conflicting public prices for the current version; Azure, RunPod, Infinity,
// Featherless, and Synthetic are BYO deployment/subscription or volatile model
// ID surfaces without stable per-token list pricing.
var DefaultPricing = map[string]Pricing{
	// OpenAI (cached input billed at ~0.5×; automatic caching has no write cost).
	"openai:gpt-4o":                 {PromptPerMillion: 2.50, CompletionPerMillion: 10.00, CacheReadPerMillion: 1.25},
	"openai:gpt-4o-2024-11-20":      {PromptPerMillion: 2.50, CompletionPerMillion: 10.00, CacheReadPerMillion: 1.25},
	"openai:gpt-4o-2024-08-06":      {PromptPerMillion: 2.50, CompletionPerMillion: 10.00, CacheReadPerMillion: 1.25},
	"openai:gpt-4o-mini":            {PromptPerMillion: 0.15, CompletionPerMillion: 0.60, CacheReadPerMillion: 0.075},
	"openai:gpt-4o-mini-2024-07-18": {PromptPerMillion: 0.15, CompletionPerMillion: 0.60, CacheReadPerMillion: 0.075},
	"openai:gpt-4-turbo":            {PromptPerMillion: 10.00, CompletionPerMillion: 30.00},
	"openai:gpt-4-turbo-preview":    {PromptPerMillion: 10.00, CompletionPerMillion: 30.00},
	"openai:gpt-4":                  {PromptPerMillion: 30.00, CompletionPerMillion: 60.00},
	"openai:gpt-4-32k":              {PromptPerMillion: 60.00, CompletionPerMillion: 120.00},
	"openai:gpt-3.5-turbo":          {PromptPerMillion: 0.50, CompletionPerMillion: 1.50},
	"openai:gpt-3.5-turbo-16k":      {PromptPerMillion: 0.50, CompletionPerMillion: 1.50},
	"openai:o1":                     {PromptPerMillion: 15.00, CompletionPerMillion: 60.00, CacheReadPerMillion: 7.50},
	"openai:o1-preview":             {PromptPerMillion: 15.00, CompletionPerMillion: 60.00, CacheReadPerMillion: 7.50},
	"openai:o1-mini":                {PromptPerMillion: 3.00, CompletionPerMillion: 12.00, CacheReadPerMillion: 1.50},
	"openai:o3":                     {PromptPerMillion: 2.00, CompletionPerMillion: 8.00, CacheReadPerMillion: 0.50},
	"openai:o3-mini":                {PromptPerMillion: 1.10, CompletionPerMillion: 4.40, CacheReadPerMillion: 0.275},
	"openai:o4-mini":                {PromptPerMillion: 1.10, CompletionPerMillion: 4.40, CacheReadPerMillion: 0.275},
	"openai:gpt-4.1":                {PromptPerMillion: 2.00, CompletionPerMillion: 8.00, CacheReadPerMillion: 0.50},
	"openai:gpt-4.1-mini":           {PromptPerMillion: 0.40, CompletionPerMillion: 1.60, CacheReadPerMillion: 0.10},
	"openai:gpt-4.1-nano":           {PromptPerMillion: 0.10, CompletionPerMillion: 0.40, CacheReadPerMillion: 0.025},
	// GPT-5 family (cached input ≈0.1× prompt). 5.4/5.5 are the current flagships
	// (developers.openai.com pricing, June 2026); pro variants priced per the page.
	"openai:gpt-5":        {PromptPerMillion: 1.25, CompletionPerMillion: 10.00, CacheReadPerMillion: 0.125},
	"openai:gpt-5-mini":   {PromptPerMillion: 0.25, CompletionPerMillion: 2.00, CacheReadPerMillion: 0.025},
	"openai:gpt-5-nano":   {PromptPerMillion: 0.05, CompletionPerMillion: 0.40, CacheReadPerMillion: 0.005},
	"openai:gpt-5.4":      {PromptPerMillion: 2.50, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.25},
	"openai:gpt-5.4-mini": {PromptPerMillion: 0.75, CompletionPerMillion: 4.50, CacheReadPerMillion: 0.075},
	"openai:gpt-5.4-nano": {PromptPerMillion: 0.20, CompletionPerMillion: 1.25, CacheReadPerMillion: 0.02},
	"openai:gpt-5.4-pro":  {PromptPerMillion: 30.00, CompletionPerMillion: 180.00},
	"openai:gpt-5.5":      {PromptPerMillion: 5.00, CompletionPerMillion: 30.00, CacheReadPerMillion: 0.50},
	"openai:gpt-5.5-pro":  {PromptPerMillion: 30.00, CompletionPerMillion: 180.00},

	// OpenAI Embeddings
	"openai:text-embedding-3-small": {PromptPerMillion: 0.02},
	"openai:text-embedding-3-large": {PromptPerMillion: 0.13},
	"openai:text-embedding-ada-002": {PromptPerMillion: 0.10},

	// Anthropic (cache read ≈0.1× prompt, cache write ≈1.25× prompt). The Sonnet
	// tier has held at $3/$15 across generations; Opus 4/4.1 at $15/$75.
	"anthropic:claude-sonnet-4-20250514":   {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-sonnet-4":            {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-3-7-sonnet-20250219": {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-opus-4-20250514":     {PromptPerMillion: 15.00, CompletionPerMillion: 75.00, CacheReadPerMillion: 1.50, CacheWritePerMillion: 18.75},
	"anthropic:claude-opus-4":              {PromptPerMillion: 15.00, CompletionPerMillion: 75.00, CacheReadPerMillion: 1.50, CacheWritePerMillion: 18.75},
	"anthropic:claude-3-5-sonnet-20241022": {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-3-5-sonnet-20240620": {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-3-5-haiku-20241022":  {PromptPerMillion: 0.80, CompletionPerMillion: 4.00, CacheReadPerMillion: 0.08, CacheWritePerMillion: 1.00},
	"anthropic:claude-3-opus-20240229":     {PromptPerMillion: 15.00, CompletionPerMillion: 75.00, CacheReadPerMillion: 1.50, CacheWritePerMillion: 18.75},
	"anthropic:claude-3-sonnet-20240229":   {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-3-haiku-20240307":    {PromptPerMillion: 0.25, CompletionPerMillion: 1.25, CacheReadPerMillion: 0.03, CacheWritePerMillion: 0.30},
	// Current Claude lineup (claude.com pricing, June 2026): Opus 4.5+ at $5/$25,
	// Sonnet 4.x at $3/$15, Haiku 4.5 at $1/$5, Fable 5 at $10/$50; cache read ≈0.1×,
	// 5m cache write ≈1.25×. Opus 4.1 retains the older $15/$75. Both alias and dated
	// model ids are covered.
	"anthropic:claude-fable-5":             {PromptPerMillion: 10.00, CompletionPerMillion: 50.00, CacheReadPerMillion: 1.00, CacheWritePerMillion: 12.50},
	"anthropic:claude-opus-4-8":            {PromptPerMillion: 5.00, CompletionPerMillion: 25.00, CacheReadPerMillion: 0.50, CacheWritePerMillion: 6.25},
	"anthropic:claude-opus-4-7":            {PromptPerMillion: 5.00, CompletionPerMillion: 25.00, CacheReadPerMillion: 0.50, CacheWritePerMillion: 6.25},
	"anthropic:claude-opus-4-6":            {PromptPerMillion: 5.00, CompletionPerMillion: 25.00, CacheReadPerMillion: 0.50, CacheWritePerMillion: 6.25},
	"anthropic:claude-opus-4-5":            {PromptPerMillion: 5.00, CompletionPerMillion: 25.00, CacheReadPerMillion: 0.50, CacheWritePerMillion: 6.25},
	"anthropic:claude-opus-4-5-20251101":   {PromptPerMillion: 5.00, CompletionPerMillion: 25.00, CacheReadPerMillion: 0.50, CacheWritePerMillion: 6.25},
	"anthropic:claude-opus-4-1":            {PromptPerMillion: 15.00, CompletionPerMillion: 75.00, CacheReadPerMillion: 1.50, CacheWritePerMillion: 18.75},
	"anthropic:claude-opus-4-1-20250805":   {PromptPerMillion: 15.00, CompletionPerMillion: 75.00, CacheReadPerMillion: 1.50, CacheWritePerMillion: 18.75},
	"anthropic:claude-sonnet-4-6":          {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-sonnet-4-5":          {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-sonnet-4-5-20250929": {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-haiku-4-5":           {PromptPerMillion: 1.00, CompletionPerMillion: 5.00, CacheReadPerMillion: 0.10, CacheWritePerMillion: 1.25},
	"anthropic:claude-haiku-4-5-20251001":  {PromptPerMillion: 1.00, CompletionPerMillion: 5.00, CacheReadPerMillion: 0.10, CacheWritePerMillion: 1.25},
	"anthropic:claude-3-5-sonnet-latest":   {PromptPerMillion: 3.00, CompletionPerMillion: 15.00, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75},
	"anthropic:claude-3-5-haiku-latest":    {PromptPerMillion: 0.80, CompletionPerMillion: 4.00, CacheReadPerMillion: 0.08, CacheWritePerMillion: 1.00},
	"anthropic:claude-3-opus-latest":       {PromptPerMillion: 15.00, CompletionPerMillion: 75.00, CacheReadPerMillion: 1.50, CacheWritePerMillion: 18.75},
	"anthropic:claude-2.1":                 {PromptPerMillion: 8.00, CompletionPerMillion: 24.00},
	"anthropic:claude-2.0":                 {PromptPerMillion: 8.00, CompletionPerMillion: 24.00},
	"anthropic:claude-instant-1.2":         {PromptPerMillion: 0.80, CompletionPerMillion: 2.40},

	// Google Gemini (cached content billed at ~0.25× prompt). 2.5 Pro uses tiered
	// pricing (≤200K context shown here).
	// Gemini 3.x — current family (ai.google.dev pricing, June 2026; cache ≈0.25×).
	// 3.1 Pro is tiered (≤200K shown). 2.0 models were shut down June 2026.
	"gemini:gemini-3.5-flash":        {PromptPerMillion: 1.50, CompletionPerMillion: 9.00, CacheReadPerMillion: 0.375},
	"gemini:gemini-3.1-pro-preview":  {PromptPerMillion: 2.00, CompletionPerMillion: 12.00, CacheReadPerMillion: 0.50},
	"gemini:gemini-3.1-flash-lite":   {PromptPerMillion: 0.25, CompletionPerMillion: 1.50},
	"gemini:gemini-3-flash-preview":  {PromptPerMillion: 0.50, CompletionPerMillion: 3.00},
	"gemini:gemini-2.5-pro":          {PromptPerMillion: 1.25, CompletionPerMillion: 10.00, CacheReadPerMillion: 0.3125},
	"gemini:gemini-2.5-flash":        {PromptPerMillion: 0.30, CompletionPerMillion: 2.50, CacheReadPerMillion: 0.075},
	"gemini:gemini-2.5-flash-lite":   {PromptPerMillion: 0.10, CompletionPerMillion: 0.40},
	"gemini:gemini-2.0-flash-exp":    {PromptPerMillion: 0.10, CompletionPerMillion: 0.40},
	"gemini:gemini-2.0-flash":        {PromptPerMillion: 0.10, CompletionPerMillion: 0.40, CacheReadPerMillion: 0.025},
	"gemini:gemini-2.0-flash-lite":   {PromptPerMillion: 0.075, CompletionPerMillion: 0.30, CacheReadPerMillion: 0.01875},
	"gemini:gemini-1.5-flash":        {PromptPerMillion: 0.075, CompletionPerMillion: 0.30, CacheReadPerMillion: 0.01875},
	"gemini:gemini-1.5-flash-8b":     {PromptPerMillion: 0.0375, CompletionPerMillion: 0.15},
	"gemini:gemini-1.5-pro":          {PromptPerMillion: 1.25, CompletionPerMillion: 5.00, CacheReadPerMillion: 0.3125},
	"gemini:gemini-1.5-pro-latest":   {PromptPerMillion: 1.25, CompletionPerMillion: 5.00, CacheReadPerMillion: 0.3125},
	"gemini:gemini-1.5-pro-001":      {PromptPerMillion: 1.25, CompletionPerMillion: 5.00, CacheReadPerMillion: 0.3125},
	"gemini:gemini-1.5-pro-002":      {PromptPerMillion: 1.25, CompletionPerMillion: 5.00, CacheReadPerMillion: 0.3125},
	"gemini:gemini-1.5-flash-latest": {PromptPerMillion: 0.075, CompletionPerMillion: 0.30, CacheReadPerMillion: 0.01875},
	"gemini:gemini-1.5-flash-001":    {PromptPerMillion: 0.075, CompletionPerMillion: 0.30, CacheReadPerMillion: 0.01875},
	"gemini:gemini-1.5-flash-002":    {PromptPerMillion: 0.075, CompletionPerMillion: 0.30, CacheReadPerMillion: 0.01875},
	"gemini:gemini-1.0-pro":          {PromptPerMillion: 0.50, CompletionPerMillion: 1.50},
	"gemini:gemini-1.0-pro-latest":   {PromptPerMillion: 0.50, CompletionPerMillion: 1.50},
	"gemini:gemini-pro":              {PromptPerMillion: 0.50, CompletionPerMillion: 1.50},
	"gemini:gemini-1.0-pro-vision":   {PromptPerMillion: 0.50, CompletionPerMillion: 1.50},
	"gemini:gemini-pro-vision":       {PromptPerMillion: 0.50, CompletionPerMillion: 1.50},
	"gemini:gemini-embedding-001":    {PromptPerMillion: 0.15},
	"gemini:text-embedding-004":      {}, // Free
	"gemini:embedding-001":           {}, // Free

	// DeepSeek (cache hits billed at a steep discount).
	"deepseek:deepseek-chat":     {PromptPerMillion: 0.27, CompletionPerMillion: 1.10, CacheReadPerMillion: 0.07},
	"deepseek:deepseek-reasoner": {PromptPerMillion: 0.55, CompletionPerMillion: 2.19, CacheReadPerMillion: 0.14},

	// Cloud provider serverless/list pricing.
	"groq:llama-3.3-70b-versatile":                                {PromptPerMillion: 0.59, CompletionPerMillion: 0.79}, // Groq list price (cloudzero.com/blog/groq-pricing, helicone.ai), verified 2026-06
	"fireworks:accounts/fireworks/models/llama-v3p1-70b-instruct": {PromptPerMillion: 0.90, CompletionPerMillion: 0.90}, // Fireworks serverless (fireworks.ai/models/fireworks/llama-v3p1-70b-instruct), verified 2026-06
	"perplexity:sonar":                                            {PromptPerMillion: 1.00, CompletionPerMillion: 1.00}, // Perplexity Sonar token rates (pricepertoken.com/perplexity-sonar) — excludes per-request search fee; verified 2026-06
	"zai:glm-5.2":                                                 {PromptPerMillion: 1.40, CompletionPerMillion: 4.40}, // Z.AI GLM-5 series (docs.z.ai pricing), verified 2026-06
	"zai:glm-5.1":                                                 {PromptPerMillion: 1.40, CompletionPerMillion: 4.40}, // docs.z.ai, verified 2026-06
	"zai:glm-5":                                                   {PromptPerMillion: 1.00, CompletionPerMillion: 3.20}, // docs.z.ai, verified 2026-06
	"zai:glm-5-turbo":                                             {PromptPerMillion: 1.20, CompletionPerMillion: 4.00}, // docs.z.ai, verified 2026-06
	"zai:glm-4.7":                                                 {PromptPerMillion: 0.60, CompletionPerMillion: 2.20}, // Z.AI GLM-4.7 direct (docs.z.ai; was 0.40/1.75 via openrouter), verified 2026-06
	"zai:glm-4.6":                                                 {PromptPerMillion: 0.60, CompletionPerMillion: 2.20}, // docs.z.ai, verified 2026-06
	"zai:glm-4.5":                                                 {PromptPerMillion: 0.60, CompletionPerMillion: 2.20}, // docs.z.ai, verified 2026-06
	"zai:glm-4.5-air":                                             {PromptPerMillion: 0.20, CompletionPerMillion: 1.10}, // docs.z.ai, verified 2026-06
	"zai:glm-4.7-Flash":                                           {PromptPerMillion: 0.06, CompletionPerMillion: 0.40}, // Z.AI GLM-4.7-Flash (pricepertoken.com/z-ai/glm-4.7-flash), verified 2026-06

	// TogetherAI (varies by model, these are examples)
	"togetherai:meta-llama/Llama-3.3-70B-Instruct-Turbo":       {PromptPerMillion: 0.88, CompletionPerMillion: 0.88},
	"togetherai:meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo": {PromptPerMillion: 3.50, CompletionPerMillion: 3.50},
	"togetherai:meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo":  {PromptPerMillion: 0.88, CompletionPerMillion: 0.88},
	"togetherai:meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo":   {PromptPerMillion: 0.18, CompletionPerMillion: 0.18},
	"togetherai:mistralai/Mixtral-8x7B-Instruct-v0.1":          {PromptPerMillion: 0.60, CompletionPerMillion: 0.60},
}

// ModelTokenPricing returns display token pricing for a provider/model from
// DefaultPricing. PromptPerMillion maps to ModelPricing.Input and
// CompletionPerMillion maps to ModelPricing.Output. The returned boolean is
// false when DefaultPricing has no entry for the model; callers must not treat
// that as a fabricated zero price.
func ModelTokenPricing(provider Provider, model string) (*ModelPricing, bool) {
	pricing, ok := DefaultPricing[fmt.Sprintf("%s:%s", provider, model)]
	if !ok {
		return nil, false
	}
	return &ModelPricing{
		Input:  pricing.PromptPerMillion,
		Output: pricing.CompletionPerMillion,
	}, true
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

// Record records token usage for a request and returns the computed cost.
// The boolean return value is false when no pricing is known for the
// provider/model, allowing callers to distinguish unknown pricing from a real
// zero-cost model. Custom pricing can be supplied with NewCostTracker or
// SetPricing.
// If token counts approach overflow (extremely unlikely), the record is skipped
// to prevent data corruption.
//
// Lock contention optimization: Cost calculation is performed before acquiring
// the write lock to minimize lock hold time under high concurrency.
func (t *CostTracker) Record(provider Provider, model string, usage Usage) (float64, bool) {
	key := t.makeKey(provider, model)
	now := time.Now()

	// Pre-calculate cost OUTSIDE the lock to minimize lock hold time.
	// Pricing data is read-only after initialization, so this is safe.
	var cost float64
	var known bool
	t.mu.RLock()
	if pricing, ok := t.pricing[key]; ok {
		cost = pricing.cost(usage)
		known = true
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
		return cost, known
	}

	u.PromptTokens += int64(usage.PromptTokens)
	u.CompletionTokens += int64(usage.CompletionTokens)
	u.CacheReadTokens += int64(usage.CacheReadTokens)
	u.CacheCreationTokens += int64(usage.CacheCreationTokens)
	u.Requests++
	u.LastUsed = now
	u.EstimatedCost += cost
	return cost, known
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

// EstimateCostKnown calculates the estimated cost for a given usage using
// default pricing. The boolean return value is false when no pricing is known
// for the provider/model, which distinguishes unknown pricing from a real
// zero-cost model. Register custom pricing on a CostTracker when defaults do not
// include a provider/model.
func EstimateCostKnown(provider Provider, model string, usage Usage) (float64, bool) {
	key := fmt.Sprintf("%s:%s", provider, model)
	pricing, ok := DefaultPricing[key]
	if !ok {
		return 0, false
	}
	return pricing.cost(usage), true
}

// EstimateCost calculates the estimated cost for a given usage using default
// pricing. It returns 0 when pricing is unknown for backward compatibility; use
// EstimateCostKnown when callers must distinguish unknown pricing from a real
// zero-cost model. Register custom pricing on a CostTracker when defaults do not
// include a provider/model.
func EstimateCost(provider Provider, model string, usage Usage) float64 {
	cost, _ := EstimateCostKnown(provider, model, usage)
	return cost
}

// FormatCost formats a cost value as a USD string.
func FormatCost(cost float64) string {
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
