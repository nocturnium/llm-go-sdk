package llms

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Pricing describes a model's pricing. Token rates are in USD per 1M tokens.
// It is the single canonical pricing type, used by the cost tracker,
// DefaultPricing, and ModelInfo.Pricing. Input/Output/CacheRead/CacheWrite drive
// cost computation; Hourly/Finetune/Base are advertised-pricing metadata not used
// in per-token cost.
type Pricing struct {
	// Input is the cost per 1M input (prompt) tokens in USD.
	Input float64 `json:"input,omitempty"`
	// Output is the cost per 1M output (completion) tokens in USD.
	Output float64 `json:"output,omitempty"`
	// CacheRead is the cost per 1M cache-read (cache-hit) tokens in USD. Zero
	// falls back to the Input rate (a conservative estimate).
	CacheRead float64 `json:"cache_read,omitempty"`
	// CacheWrite is the cost per 1M cache-write (cache-creation) tokens in USD.
	// Zero falls back to the Input rate.
	CacheWrite float64 `json:"cache_write,omitempty"`
	// Hourly is the hourly rate for dedicated instances in USD (metadata; not
	// used in per-token cost computation).
	Hourly float64 `json:"hourly,omitempty"`
	// Finetune is the fine-tuning cost per 1M tokens in USD (metadata).
	Finetune float64 `json:"finetune,omitempty"`
	// Base is a provider-specific base cost in USD (metadata).
	Base float64 `json:"base,omitempty"`

	// Tiers optionally reprices the entire request once its total input token
	// count reaches a threshold. Several providers bill long-context requests at a
	// higher rate for the whole request rather than only for the tokens past the
	// threshold — OpenAI's gpt-5 family above 272K input tokens (2x input,
	// 1.5x output) and Gemini Pro above 200K are the current examples.
	//
	// A nil or empty Tiers means flat pricing, so this field is inert for every
	// model that does not tier. See PricingTier for selection semantics.
	Tiers []PricingTier `json:"tiers,omitempty"`
}

// PricingTier is a set of rates that replaces a Pricing's base rates once a
// request's total input token count reaches MinInputTokens. It is deliberately
// not an embedded Pricing: tiers do not nest, and the flat fields make that
// explicit at the type level.
//
// Rates are per 1M tokens in USD and follow the same zero-value fallback as
// Pricing — an unset CacheRead or CacheWrite bills at the tier's own Input rate,
// not at the base Pricing's rate.
type PricingTier struct {
	// MinInputTokens is the inclusive total-input-token threshold at which this
	// tier takes effect. Total input is PromptTokens + CacheReadTokens +
	// CacheCreationTokens, since Usage.PromptTokens excludes cache tokens by
	// contract and providers threshold on the full input.
	MinInputTokens int `json:"min_input_tokens"`

	// Input is the cost per 1M input (prompt) tokens in USD at this tier.
	Input float64 `json:"input,omitempty"`
	// Output is the cost per 1M output (completion) tokens in USD at this tier.
	Output float64 `json:"output,omitempty"`
	// CacheRead is the cost per 1M cache-read tokens in USD at this tier.
	CacheRead float64 `json:"cache_read,omitempty"`
	// CacheWrite is the cost per 1M cache-write tokens in USD at this tier.
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// totalInputTokens returns the token count a pricing tier thresholds against.
// Usage.PromptTokens excludes cache tokens by contract, so cache reads and cache
// creations must be added back to recover the request's true input size.
func totalInputTokens(usage Usage) int {
	return usage.PromptTokens + usage.CacheReadTokens + usage.CacheCreationTokens
}

// effective resolves the rates that apply to the given usage, substituting the
// highest matching tier's rates for the base rates. It returns the receiver
// unchanged when no tier is configured or none matches, so untiered pricing is
// unaffected. Tiers need not be sorted; the highest matching threshold wins.
func (p Pricing) effective(usage Usage) Pricing {
	if len(p.Tiers) == 0 {
		return p
	}
	total := totalInputTokens(usage)
	best := -1
	for i, t := range p.Tiers {
		if total < t.MinInputTokens {
			continue
		}
		if best == -1 || t.MinInputTokens > p.Tiers[best].MinInputTokens {
			best = i
		}
	}
	if best == -1 {
		return p
	}
	t := p.Tiers[best]
	out := p
	out.Tiers = nil // a resolved tier must not re-resolve
	out.Input, out.Output = t.Input, t.Output
	out.CacheRead, out.CacheWrite = t.CacheRead, t.CacheWrite
	return out
}

// cacheReadRate returns the effective cache-read rate, defaulting to the Input
// rate when no cache-specific rate is configured.
func (p Pricing) cacheReadRate() float64 {
	if p.CacheRead > 0 {
		return p.CacheRead
	}
	return p.Input
}

// cacheWriteRate returns the effective cache-write rate, defaulting to the Input
// rate when no cache-specific rate is configured.
func (p Pricing) cacheWriteRate() float64 {
	if p.CacheWrite > 0 {
		return p.CacheWrite
	}
	return p.Input
}

// cost computes the total USD cost for the given usage under this pricing,
// accounting for discounted cache-read and cache-write tokens. PromptTokens is
// assumed to exclude cache tokens (see the Usage contract). When Tiers are
// configured, the whole request is priced at the highest matching tier.
func (p Pricing) cost(usage Usage) float64 {
	const perMillion = 1_000_000.0
	e := p.effective(usage)
	return float64(usage.PromptTokens)/perMillion*e.Input +
		float64(usage.CompletionTokens)/perMillion*e.Output +
		float64(usage.CacheReadTokens)/perMillion*e.cacheReadRate() +
		float64(usage.CacheCreationTokens)/perMillion*e.cacheWriteRate()
}

// costForMode computes cost under a billing mode. A mode's rate card replaces
// the standard card wholesale, and tiers then resolve against whichever card
// applies — so a long-context batch request bills at the batch card's
// long-context tier if it declares one, not at the standard card's tier scaled
// by a discount.
//
// When no card is published for the mode, pricing falls back to standard rates
// and known is false, so a caller can tell "priced at standard because the mode
// is unknown" from "priced at the mode's published rate".
func costForMode(provider Provider, model string, usage Usage, mode PricingMode, standard Pricing, standardKnown bool) (cost float64, known bool) {
	card, ok := resolveModePricing(provider, model, mode, standard, standardKnown)
	if !ok {
		return standard.cost(usage), false
	}
	return card.cost(usage), true
}

// Long-context repricing thresholds. Both providers reprice the *entire* request
// once total input crosses the threshold, not just the tokens beyond it.
const (
	// openAILongContextThreshold is where the gpt-5 family switches to long-context
	// rates (2x input and cached input, 1.5x output).
	openAILongContextThreshold = 272_000
	// geminiLongContextThreshold is where Gemini Pro tiers switch to their >200K rates.
	geminiLongContextThreshold = 200_000
)

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
	"openai:gpt-4o":                 {Input: 2.50, Output: 10.00, CacheRead: 1.25},
	"openai:gpt-4o-2024-11-20":      {Input: 2.50, Output: 10.00, CacheRead: 1.25},
	"openai:gpt-4o-2024-08-06":      {Input: 2.50, Output: 10.00, CacheRead: 1.25},
	"openai:gpt-4o-mini":            {Input: 0.15, Output: 0.60, CacheRead: 0.075},
	"openai:gpt-4o-mini-2024-07-18": {Input: 0.15, Output: 0.60, CacheRead: 0.075},
	"openai:gpt-4-turbo":            {Input: 10.00, Output: 30.00},
	"openai:gpt-4-turbo-preview":    {Input: 10.00, Output: 30.00},
	"openai:gpt-4":                  {Input: 30.00, Output: 60.00},
	"openai:gpt-4-32k":              {Input: 60.00, Output: 120.00},
	"openai:gpt-3.5-turbo":          {Input: 0.50, Output: 1.50},
	"openai:gpt-3.5-turbo-16k":      {Input: 0.50, Output: 1.50},
	"openai:o1":                     {Input: 15.00, Output: 60.00, CacheRead: 7.50},
	"openai:o1-preview":             {Input: 15.00, Output: 60.00, CacheRead: 7.50},
	"openai:o1-mini":                {Input: 3.00, Output: 12.00, CacheRead: 1.50},
	"openai:o3":                     {Input: 2.00, Output: 8.00, CacheRead: 0.50},
	"openai:o3-mini":                {Input: 1.10, Output: 4.40, CacheRead: 0.275},
	"openai:o4-mini":                {Input: 1.10, Output: 4.40, CacheRead: 0.275},
	"openai:gpt-4.1":                {Input: 2.00, Output: 8.00, CacheRead: 0.50},
	"openai:gpt-4.1-mini":           {Input: 0.40, Output: 1.60, CacheRead: 0.10},
	"openai:gpt-4.1-nano":           {Input: 0.10, Output: 0.40, CacheRead: 0.025},
	// GPT-5 family (cached input ≈0.1× prompt). 5.6 (sol/terra/luna) is the current
	// flagship line (developers.openai.com pricing, verified 2026-08-02); 5.4/5.5
	// remain available. Base rates are the short-context tier; models that reprice
	// above 272K input tokens carry that as a Tier (2× input and cached, 1.5×
	// output, for the entire request). The mini/nano variants publish no
	// long-context row, so they are flat. Pro variants have no cached-input rate.
	"openai:gpt-5.6-sol": {
		Input: 5.00, Output: 30.00, CacheRead: 0.50, CacheWrite: 6.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 10.00, Output: 45.00, CacheRead: 1.00, CacheWrite: 12.50}},
	},
	"openai:gpt-5.6-terra": {
		Input: 2.00, Output: 12.00, CacheRead: 0.20, CacheWrite: 2.50,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 4.00, Output: 18.00, CacheRead: 0.40, CacheWrite: 5.00}},
	},
	"openai:gpt-5.6-luna": {
		Input: 0.20, Output: 1.20, CacheRead: 0.02, CacheWrite: 0.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 0.40, Output: 1.80, CacheRead: 0.04, CacheWrite: 0.50}},
	},
	"openai:gpt-5":        {Input: 1.25, Output: 10.00, CacheRead: 0.125},
	"openai:gpt-5-mini":   {Input: 0.25, Output: 2.00, CacheRead: 0.025},
	"openai:gpt-5-nano":   {Input: 0.05, Output: 0.40, CacheRead: 0.005},
	"openai:gpt-5.4-mini": {Input: 0.75, Output: 4.50, CacheRead: 0.075},
	"openai:gpt-5.4-nano": {Input: 0.20, Output: 1.25, CacheRead: 0.02},
	"openai:gpt-5.4": {
		Input: 2.50, Output: 15.00, CacheRead: 0.25,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 5.00, Output: 22.50, CacheRead: 0.50}},
	},
	"openai:gpt-5.4-pro": {
		Input: 30.00, Output: 180.00,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 60.00, Output: 270.00}},
	},
	"openai:gpt-5.5": {
		Input: 5.00, Output: 30.00, CacheRead: 0.50,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 10.00, Output: 45.00, CacheRead: 1.00}},
	},
	"openai:gpt-5.5-pro": {
		Input: 30.00, Output: 180.00,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 60.00, Output: 270.00}},
	},
	"openai:gpt-5.3-codex": {Input: 1.75, Output: 14.00, CacheRead: 0.175},
	"openai:gpt-5.2":       {Input: 1.75, Output: 14.00, CacheRead: 0.175},
	"openai:gpt-5.1":       {Input: 1.25, Output: 10.00, CacheRead: 0.125},

	// OpenAI Embeddings
	"openai:text-embedding-3-small": {Input: 0.02},
	"openai:text-embedding-3-large": {Input: 0.13},
	"openai:text-embedding-ada-002": {Input: 0.10},

	// Anthropic (cache read ≈0.1× prompt, cache write ≈1.25× prompt). The Sonnet
	// tier has held at $3/$15 across generations; Opus 4/4.1 at $15/$75.
	"anthropic:claude-sonnet-4-20250514":   {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-sonnet-4":            {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-3-7-sonnet-20250219": {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-opus-4-20250514":     {Input: 15.00, Output: 75.00, CacheRead: 1.50, CacheWrite: 18.75},
	"anthropic:claude-opus-4":              {Input: 15.00, Output: 75.00, CacheRead: 1.50, CacheWrite: 18.75},
	"anthropic:claude-3-5-sonnet-20241022": {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-3-5-sonnet-20240620": {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-3-5-haiku-20241022":  {Input: 0.80, Output: 4.00, CacheRead: 0.08, CacheWrite: 1.00},
	"anthropic:claude-3-opus-20240229":     {Input: 15.00, Output: 75.00, CacheRead: 1.50, CacheWrite: 18.75},
	"anthropic:claude-3-sonnet-20240229":   {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-3-haiku-20240307":    {Input: 0.25, Output: 1.25, CacheRead: 0.03, CacheWrite: 0.30},
	// Current Claude lineup (claude.com models overview, verified 2026-08-02):
	// Fable 5 at $10/$50, Opus 5 and Opus 4.5+ at $5/$25, Sonnet 5 and Sonnet 4.x at
	// $3/$15, Haiku 4.5 at $1/$5; cache read ≈0.1×, 5m cache write ≈1.25×. Opus 4.1
	// retains the older $15/$75. Both alias and dated model ids are covered.
	//
	// Sonnet 5 carries introductory pricing of $2/$10 per MTok through 2026-08-31.
	// The table records the standard $3/$15 rate: a static map cannot express a
	// time-boxed promotion, and estimates that silently expire are worse than
	// estimates that are consistently conservative.
	"anthropic:claude-fable-5":             {Input: 10.00, Output: 50.00, CacheRead: 1.00, CacheWrite: 12.50},
	"anthropic:claude-mythos-5":            {Input: 10.00, Output: 50.00, CacheRead: 1.00, CacheWrite: 12.50},
	"anthropic:claude-opus-5":              {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25},
	"anthropic:claude-sonnet-5":            {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-opus-4-8":            {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25},
	"anthropic:claude-opus-4-7":            {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25},
	"anthropic:claude-opus-4-6":            {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25},
	"anthropic:claude-opus-4-5":            {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25},
	"anthropic:claude-opus-4-5-20251101":   {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25},
	"anthropic:claude-opus-4-1":            {Input: 15.00, Output: 75.00, CacheRead: 1.50, CacheWrite: 18.75},
	"anthropic:claude-opus-4-1-20250805":   {Input: 15.00, Output: 75.00, CacheRead: 1.50, CacheWrite: 18.75},
	"anthropic:claude-sonnet-4-6":          {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-sonnet-4-5":          {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-sonnet-4-5-20250929": {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-haiku-4-5":           {Input: 1.00, Output: 5.00, CacheRead: 0.10, CacheWrite: 1.25},
	"anthropic:claude-haiku-4-5-20251001":  {Input: 1.00, Output: 5.00, CacheRead: 0.10, CacheWrite: 1.25},
	"anthropic:claude-3-5-sonnet-latest":   {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75},
	"anthropic:claude-3-5-haiku-latest":    {Input: 0.80, Output: 4.00, CacheRead: 0.08, CacheWrite: 1.00},
	"anthropic:claude-3-opus-latest":       {Input: 15.00, Output: 75.00, CacheRead: 1.50, CacheWrite: 18.75},
	"anthropic:claude-2.1":                 {Input: 8.00, Output: 24.00},
	"anthropic:claude-2.0":                 {Input: 8.00, Output: 24.00},
	"anthropic:claude-instant-1.2":         {Input: 0.80, Output: 2.40},

	// Google Gemini (ai.google.dev pricing, verified 2026-08-02). Cache-read rates
	// are the published per-model figures — Gemini bills cached input at ≈0.1× the
	// prompt rate, not the 0.25× this table previously assumed. CacheRead here is the
	// per-token read charge only; Gemini also bills context-cache *storage* per hour
	// ($1.00/1M/hr on Flash tiers, $4.50/1M/hr on Pro), which is time-based and so
	// cannot be modeled by this per-token table.
	//
	// Pro tiers reprice above 200K input tokens, carried as a Tier below; base rates
	// are the ≤200K tier. Text rates are used where a model prices audio separately.
	// 2.0 models shut down 2026-06-01.
	"gemini:gemini-3.6-flash":      {Input: 1.50, Output: 7.50, CacheRead: 0.15},
	"gemini:gemini-3.5-flash":      {Input: 1.50, Output: 9.00, CacheRead: 0.15},
	"gemini:gemini-3.5-flash-lite": {Input: 0.30, Output: 2.50, CacheRead: 0.03},
	"gemini:gemini-3.1-pro-preview": {
		Input: 2.00, Output: 12.00, CacheRead: 0.20,
		Tiers: []PricingTier{{MinInputTokens: geminiLongContextThreshold,
			Input: 4.00, Output: 18.00, CacheRead: 0.40}},
	},
	"gemini:gemini-3.1-flash-lite":  {Input: 0.25, Output: 1.50, CacheRead: 0.025},
	"gemini:gemini-3-flash-preview": {Input: 0.50, Output: 3.00, CacheRead: 0.05},
	"gemini:gemini-2.5-pro": {
		Input: 1.25, Output: 10.00, CacheRead: 0.125,
		Tiers: []PricingTier{{MinInputTokens: geminiLongContextThreshold,
			Input: 2.50, Output: 15.00, CacheRead: 0.25}},
	},
	"gemini:gemini-2.5-flash":        {Input: 0.30, Output: 2.50, CacheRead: 0.03},
	"gemini:gemini-2.5-flash-lite":   {Input: 0.10, Output: 0.40, CacheRead: 0.01},
	"gemini:gemini-2.0-flash-exp":    {Input: 0.10, Output: 0.40},
	"gemini:gemini-1.5-flash":        {Input: 0.075, Output: 0.30, CacheRead: 0.01875},
	"gemini:gemini-1.5-flash-8b":     {Input: 0.0375, Output: 0.15},
	"gemini:gemini-1.5-pro":          {Input: 1.25, Output: 5.00, CacheRead: 0.3125},
	"gemini:gemini-1.5-pro-latest":   {Input: 1.25, Output: 5.00, CacheRead: 0.3125},
	"gemini:gemini-1.5-pro-001":      {Input: 1.25, Output: 5.00, CacheRead: 0.3125},
	"gemini:gemini-1.5-pro-002":      {Input: 1.25, Output: 5.00, CacheRead: 0.3125},
	"gemini:gemini-1.5-flash-latest": {Input: 0.075, Output: 0.30, CacheRead: 0.01875},
	"gemini:gemini-1.5-flash-001":    {Input: 0.075, Output: 0.30, CacheRead: 0.01875},
	"gemini:gemini-1.5-flash-002":    {Input: 0.075, Output: 0.30, CacheRead: 0.01875},
	"gemini:gemini-1.0-pro":          {Input: 0.50, Output: 1.50},
	"gemini:gemini-1.0-pro-latest":   {Input: 0.50, Output: 1.50},
	"gemini:gemini-pro":              {Input: 0.50, Output: 1.50},
	"gemini:gemini-1.0-pro-vision":   {Input: 0.50, Output: 1.50},
	"gemini:gemini-pro-vision":       {Input: 0.50, Output: 1.50},
	"gemini:gemini-embedding-001":    {Input: 0.15},
	"gemini:text-embedding-004":      {}, // Free
	"gemini:embedding-001":           {}, // Free

	// DeepSeek (cache hits billed at a steep discount).
	"deepseek:deepseek-chat":     {Input: 0.27, Output: 1.10, CacheRead: 0.07},
	"deepseek:deepseek-reasoner": {Input: 0.55, Output: 2.19, CacheRead: 0.14},

	// Cloud provider serverless/list pricing.
	"groq:llama-3.3-70b-versatile":                                {Input: 0.59, Output: 0.79}, // Groq list price (cloudzero.com/blog/groq-pricing, helicone.ai), verified 2026-06
	"fireworks:accounts/fireworks/models/llama-v3p1-70b-instruct": {Input: 0.90, Output: 0.90}, // Fireworks serverless (fireworks.ai/models/fireworks/llama-v3p1-70b-instruct), verified 2026-06
	"perplexity:sonar":                                            {Input: 1.00, Output: 1.00}, // Perplexity Sonar token rates (pricepertoken.com/perplexity-sonar) — excludes per-request search fee; verified 2026-06
	"zai:glm-5.2":                                                 {Input: 1.40, Output: 4.40}, // Z.AI GLM-5 series (docs.z.ai pricing), verified 2026-06
	"zai:glm-5.1":                                                 {Input: 1.40, Output: 4.40}, // docs.z.ai, verified 2026-06
	"zai:glm-5":                                                   {Input: 1.00, Output: 3.20}, // docs.z.ai, verified 2026-06
	"zai:glm-5-turbo":                                             {Input: 1.20, Output: 4.00}, // docs.z.ai, verified 2026-06
	"zai:glm-4.7":                                                 {Input: 0.60, Output: 2.20}, // Z.AI GLM-4.7 direct (docs.z.ai; was 0.40/1.75 via openrouter), verified 2026-06
	"zai:glm-4.6":                                                 {Input: 0.60, Output: 2.20}, // docs.z.ai, verified 2026-06
	"zai:glm-4.5":                                                 {Input: 0.60, Output: 2.20}, // docs.z.ai, verified 2026-06
	"zai:glm-4.5-air":                                             {Input: 0.20, Output: 1.10}, // docs.z.ai, verified 2026-06
	"zai:glm-4.7-Flash":                                           {Input: 0.06, Output: 0.40}, // Z.AI GLM-4.7-Flash (pricepertoken.com/z-ai/glm-4.7-flash), verified 2026-06

	// TogetherAI (varies by model, these are examples)
	"togetherai:meta-llama/Llama-3.3-70B-Instruct-Turbo":       {Input: 0.88, Output: 0.88},
	"togetherai:meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo": {Input: 3.50, Output: 3.50},
	"togetherai:meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo":  {Input: 0.88, Output: 0.88},
	"togetherai:meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo":   {Input: 0.18, Output: 0.18},
	"togetherai:mistralai/Mixtral-8x7B-Instruct-v0.1":          {Input: 0.60, Output: 0.60},
}

// ModelTokenPricing returns the built-in per-token pricing (Input and Output, in
// USD per 1M tokens) for a provider/model from DefaultPricing as a *Pricing
// suitable for ModelInfo.Pricing. The returned boolean is false when
// DefaultPricing has no entry for the model; callers must not treat that as a
// fabricated zero price.
func ModelTokenPricing(provider Provider, model string) (*Pricing, bool) {
	pricing, ok := DefaultPricing[fmt.Sprintf("%s:%s", provider, model)]
	if !ok {
		return nil, false
	}
	return &Pricing{
		Input:  pricing.Input,
		Output: pricing.Output,
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
	media map[string]MediaTotal

	mu      sync.RWMutex
	usage   map[string]*ModelUsage
	pricing map[string]Pricing
	// modePricing holds caller-registered rate cards for non-standard billing
	// lanes, keyed "provider:model:mode". It overlays the built-in table.
	modePricing map[string]Pricing
	// modeCost accumulates spend per billing mode, keyed by mode. It lives here
	// rather than on ModelUsage because ModelUsage is a comparable struct and a
	// map field would silently make it non-comparable — the same class of change
	// that forced the v6 major.
	modeCost map[PricingMode]float64
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
		usage:       make(map[string]*ModelUsage),
		pricing:     pricing,
		modePricing: make(map[string]Pricing),
		modeCost:    make(map[PricingMode]float64),
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
	return t.RecordMode(provider, model, usage, PricingModeStandard)
}

// costFor resolves the cost of usage for a provider/model under a billing mode.
// The caller must hold at least a read lock.
//
// Resolution order for a non-standard mode: a rate card registered on this
// tracker, then the built-in published cards (including provider-wide rules),
// then standard rates with known=false. An unpriced model is (0, false)
// whatever the mode — a mode discount on an unknown base is still unknown.
func (t *CostTracker) costFor(provider Provider, model, key string, usage Usage, mode PricingMode) (float64, bool) {
	standard, standardKnown := t.pricing[key]
	if !standardKnown && mode == PricingModeStandard {
		return 0, false
	}
	if mode != PricingModeStandard {
		if override, ok := t.modePricing[modePricingKey(provider, model, mode)]; ok {
			return override.cost(usage), true
		}
	}
	cost, known := costForMode(provider, model, usage, mode, standard, standardKnown)
	if !standardKnown && !known {
		return 0, false
	}
	return cost, known
}

// RecordMode records usage priced under a billing mode, returning the cost and
// whether a rate card was known for that provider/model/mode.
//
// A mode with no published card prices at standard rates and reports known
// false, so an unknown mode is never silently discounted. Cost still accumulates
// into the single per-model [ModelUsage]; use [CostTracker.GetModeCosts] for the
// per-mode split.
func (t *CostTracker) RecordMode(provider Provider, model string, usage Usage, mode PricingMode) (float64, bool) {
	key := t.makeKey(provider, model)
	now := time.Now()

	// Pre-calculate cost OUTSIDE the write lock to minimize lock hold time.
	// Mode resolution reads the tracker's overlay, so it shares this read lock.
	t.mu.RLock()
	cost, known := t.costFor(provider, model, key, usage, mode)
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
	t.modeCost[mode] += cost
	return cost, known
}

// SetModePricing registers a rate card for a provider/model under a billing
// mode, overriding any built-in card. Use it for models the SDK does not cover,
// or for negotiated rates.
//
// Setting [PricingModeStandard] is a no-op guard against confusion: use
// [CostTracker.SetPricing] for standard rates.
func (t *CostTracker) SetModePricing(provider Provider, model string, mode PricingMode, pricing Pricing) {
	if mode == PricingModeStandard {
		t.SetPricing(provider, model, pricing)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.modePricing[modePricingKey(provider, model, mode)] = pricing
}

// GetModeCosts returns accumulated cost broken down by billing mode. The
// returned map is a copy and is safe to retain.
//
// This lives on the tracker rather than on [ModelUsage] because ModelUsage is a
// comparable struct, and adding a map field to it would make it non-comparable —
// a breaking change for any caller comparing two values with ==.
func (t *CostTracker) GetModeCosts() map[PricingMode]float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[PricingMode]float64, len(t.modeCost))
	for mode, cost := range t.modeCost {
		out[mode] = cost
	}
	return out
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
	for _, u := range t.media {
		total += u.Cost
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
	t.media = nil
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
	return modelPricingKey(provider, model)
}

// modelPricingKey builds the "provider:model" key used by DefaultPricing and by
// a tracker's pricing map. Both the tracker and the package-level estimators use
// it so the two can never drift.
func modelPricingKey(provider Provider, model string) string {
	return string(provider) + ":" + model
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

	// Options are applied only after the call succeeds so the error path does not
	// pay for a CallOptions allocation it will not use.
	m.tracker.RecordMode(m.llm.Provider(), m.llm.Model(), resp.Usage, ApplyOptions(options...).PricingMode)
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
				m.tracker.RecordMode(m.llm.Provider(), m.llm.Model(), *lastUsage, opts.PricingMode)
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

// EstimateCost calculates the estimated cost for a given usage using the
// built-in DefaultPricing table. The boolean is false when no built-in pricing
// exists for the provider/model — callers must not treat that as a real
// zero-cost model. Register custom pricing on a CostTracker when the defaults do
// not include a provider/model.
func EstimateCost(provider Provider, model string, usage Usage) (float64, bool) {
	return EstimateCostMode(provider, model, usage, PricingModeStandard)
}

// EstimateCostMode calculates the estimated cost for a given usage under a
// billing mode, using the built-in pricing tables.
//
// The boolean reports whether a rate card was known for that provider/model/mode
// specifically. A model priced at standard rates but with no published card for
// the requested mode returns its standard cost with false, so a caller can tell
// "this is the batch price" from "this is the standard price because no batch
// price is published". Not every model has a card in every lane —
// gpt-5.4-nano, for example, has no Fast mode rate.
func EstimateCostMode(provider Provider, model string, usage Usage, mode PricingMode) (float64, bool) {
	standard, standardKnown := DefaultPricing[modelPricingKey(provider, model)]
	if !standardKnown && mode == PricingModeStandard {
		return 0, false
	}
	cost, known := costForMode(provider, model, usage, mode, standard, standardKnown)
	if !standardKnown && !known {
		return 0, false
	}
	return cost, known
}

// FormatCost formats a cost value as a USD string.
func FormatCost(cost float64) string {
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
