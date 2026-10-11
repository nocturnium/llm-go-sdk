package llms

import (
	"context"
	"fmt"
	"math"
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
	// threshold, OpenAI's gpt-5 family above 272K input tokens (2x input,
	// 1.5x output) and Gemini Pro above 200K are the current examples.
	//
	// A nil or empty Tiers means flat pricing, so this field is inert for every
	// model that does not tier. See PricingTier for selection semantics.
	Tiers []PricingTier `json:"tiers,omitempty"`
}

// clonePricing returns an ownership-safe copy of a pricing card. Pricing.Tiers
// is mutable caller-owned data, so copying Pricing by value is insufficient.
func clonePricing(p Pricing) Pricing {
	p.Tiers = append([]PricingTier(nil), p.Tiers...)
	return p
}

// PricingTier is a set of rates that replaces a Pricing's base rates once a
// request's total input token count reaches MinInputTokens. It is deliberately
// not an embedded Pricing: tiers do not nest, and the flat fields make that
// explicit at the type level.
//
// Rates are per 1M tokens in USD and follow the same zero-value fallback as
// Pricing, an unset CacheRead or CacheWrite bills at the tier's own Input rate,
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
	max := int(^uint(0) >> 1)
	total := usage.PromptTokens
	for _, n := range []int{usage.CacheReadTokens, usage.CacheCreationTokens} {
		if n > 0 && total > max-n {
			return max
		}
		total += n
	}
	return total
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
	// A malformed or adversarially reported negative count must not reduce the
	// bill (or create a refund against the tracker), so clamp before pricing.
	usage = usage.nonNegative()
	e := p.effective(usage)
	return float64(usage.PromptTokens)/perMillion*e.Input +
		float64(usage.CompletionTokens)/perMillion*e.Output +
		float64(usage.CacheReadTokens)/perMillion*e.cacheReadRate() +
		float64(usage.CacheCreationTokens)/perMillion*e.cacheWriteRate()
}

// nonNegative returns a copy of usage with negative token counts clamped to
// zero. Cost and recorded totals must never move backward because a report was
// malformed.
func (u Usage) nonNegative() Usage {
	if u.PromptTokens < 0 {
		u.PromptTokens = 0
	}
	if u.CompletionTokens < 0 {
		u.CompletionTokens = 0
	}
	if u.TotalTokens < 0 {
		u.TotalTokens = 0
	}
	if u.CacheReadTokens < 0 {
		u.CacheReadTokens = 0
	}
	if u.CacheCreationTokens < 0 {
		u.CacheCreationTokens = 0
	}
	if u.ReasoningTokens < 0 {
		u.ReasoningTokens = 0
	}
	if u.Cost != nil {
		// A negative charge would refund tracked spend, and a non-finite one
		// (NaN, ±Inf) would poison every total it touches; neither is a real
		// invoice, so bank zero instead.
		if *u.Cost < 0 || math.IsNaN(*u.Cost) || math.IsInf(*u.Cost, 0) {
			zero := 0.0
			u.Cost = &zero
		}
	}
	return u
}

// costValue returns the reported charge, already clamped by nonNegative, or
// zero when the usage carries none.
func (u Usage) costValue() float64 {
	if u.Cost == nil {
		return 0
	}
	return *u.Cost
}

// costForMode computes cost under a billing mode. A mode's rate card replaces
// the standard card wholesale, and tiers then resolve against whichever card
// applies, so a long-context batch request bills at the batch card's
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
//
// Configure this table before concurrent use; concurrent mutation while a
// lookup runs is a data race, the same rule [MediaPricing] documents. A tracker
// snapshots it at construction, so pass custom rates to [NewCostTracker] or
// [CostTracker.SetPricing] rather than editing the global map at runtime.
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
		Input: 4.00, Output: 20.00, CacheRead: 0.40, CacheWrite: 5.00,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 8.00, Output: 30.00, CacheRead: 0.80, CacheWrite: 10.00}},
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
	"openai:gpt-6.1-sol": {
		Input: 2.00, Output: 10.00, CacheRead: 0.10, CacheWrite: 2.50,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 4.00, Output: 15.00, CacheRead: 0.20, CacheWrite: 5.00}},
	},
	"openai:gpt-6-sol": {
		Input: 2.00, Output: 10.00, CacheRead: 0.20, CacheWrite: 2.50,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 4.00, Output: 15.00, CacheRead: 0.40, CacheWrite: 5.00}},
	},
	"openai:gpt-6-astra": {
		Input: 10.00, Output: 50.00, CacheRead: 1.00, CacheWrite: 12.50,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 20.00, Output: 75.00, CacheRead: 2.00, CacheWrite: 25.00}},
	},
	"openai:gpt-6-luna": {
		Input: 0.10, Output: 0.50, CacheRead: 0.01, CacheWrite: 0.125,
		Tiers: []PricingTier{{MinInputTokens: openAILongContextThreshold,
			Input: 0.20, Output: 0.75, CacheRead: 0.02, CacheWrite: 0.25}},
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
	"openai:gpt-5.3-codex":          {Input: 1.75, Output: 14.00, CacheRead: 0.175},
	"openai:gpt-rosalind-research":  {Input: 5.00, Output: 25.00, CacheRead: 0.50},
	"openai:gpt-rosalind-discovery": {Input: 5.00, Output: 25.00, CacheRead: 0.50},
	"openai:gpt-5.6-cyber":          {Input: 12.50, Output: 75.00, CacheRead: 1.25, CacheWrite: 15.625},
	"openai:gpt-5.2":                {Input: 1.75, Output: 14.00, CacheRead: 0.175},
	"openai:gpt-5.2-pro":            {Input: 21.00, Output: 168.00},
	"openai:gpt-5.1":                {Input: 1.25, Output: 10.00, CacheRead: 0.125},
	"openai:gpt-5-pro":              {Input: 15.00, Output: 120.00},
	"openai:o1-pro":                 {Input: 150.00, Output: 600.00},
	"openai:o3-pro":                 {Input: 20.00, Output: 80.00},
	"openai:chat-latest":            {Input: 5.00, Output: 30.00, CacheRead: 0.50},
	"openai:gpt-4o-2024-05-13":      {Input: 5.00, Output: 15.00},
	"openai:gpt-4-turbo-2024-04-09": {Input: 10.00, Output: 30.00},
	"openai:gpt-4-0613":             {Input: 30.00, Output: 60.00},
	"openai:gpt-3.5-turbo-0125":     {Input: 0.50, Output: 1.50},
	"openai:gpt-3.5-turbo-1106":     {Input: 1.00, Output: 2.00},
	"openai:gpt-3.5-turbo-instruct": {Input: 1.50, Output: 2.00},
	"openai:davinci-002":            {Input: 2.00, Output: 2.00},
	"openai:babbage-002":            {Input: 0.40, Output: 0.40},

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
	"anthropic:claude-fable-5":    {Input: 10.00, Output: 50.00, CacheRead: 1.00, CacheWrite: 12.50},
	"anthropic:claude-mythos-5":   {Input: 10.00, Output: 50.00, CacheRead: 1.00, CacheWrite: 12.50},
	"anthropic:claude-opus-5":     {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25},
	"anthropic:claude-sonnet-5":   {Input: 2.00, Output: 10.00, CacheRead: 0.20, CacheWrite: 2.50},
	"anthropic:claude-fable-5-1":  {Input: 10.00, Output: 50.00, CacheRead: 0.25, CacheWrite: 12.50},
	"anthropic:claude-opus-5-5":   {Input: 4.00, Output: 20.00, CacheRead: 0.20, CacheWrite: 5.00},
	"anthropic:claude-sonnet-5-5": {Input: 2.00, Output: 10.00, CacheRead: 0.10, CacheWrite: 2.50},
	"anthropic:claude-haiku-5-5": {
		Input: 0.10, Output: 0.50, CacheRead: 0.01, CacheWrite: 0.125,
		Tiers: []PricingTier{{MinInputTokens: 100001,
			Input: 0.50, Output: 2.50, CacheRead: 0.05, CacheWrite: 0.625}},
	},
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
	// are the published per-model figures: Gemini bills cached input at ~0.1x the
	// prompt rate. CacheRead here is the
	// per-token read charge only; Gemini also bills context-cache *storage* per hour
	// ($1.00/1M/hr on Flash tiers, $4.50/1M/hr on Pro), which is time-based and so
	// cannot be modeled by this per-token table.
	//
	// Pro tiers reprice above 200K input tokens, carried as a Tier below; base rates
	// are the ≤200K tier. Text rates are used where a model prices audio separately.
	// 2.0 models shut down 2026-06-01.
	// Gemini 3.8 Flash bills these rates through 2026-12-31 and doubles them from
	// 2027-01-01 (Input 1.50, Output 7.50, CacheRead 0.15), per Google's pricing
	// page; TestGemini38FlashRateChange fails from that date as the reminder.
	"gemini:gemini-3.8-flash":      {Input: 0.75, Output: 3.75, CacheRead: 0.075},
	"gemini:gemini-3.6-flash":      {Input: 0.75, Output: 3.75, CacheRead: 0.075},
	"gemini:gemini-3.7-flash":      {Input: 0.75, Output: 3.75, CacheRead: 0.075},
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
	// Current DeepSeek API aliases, read 2026-10-09. Recorded at peak rates;
	// off-peak is 0.50x and is a time-window card a static map cannot express.
	"deepseek:deepseek-v4-flash":            {Input: 0.30, Output: 1.20, CacheRead: 0.006},
	"deepseek:deepseek-v4-flash-vision-exp": {Input: 0.30, Output: 1.20, CacheRead: 0.006},
	"deepseek:deepseek-v4-pro":              {Input: 1.32, Output: 3.96, CacheRead: 0.044},

	// Cloud provider serverless/list pricing.
	"groq:llama-3.3-70b-versatile": {Input: 0.59, Output: 0.79}, // Groq list price (cloudzero.com/blog/groq-pricing, helicone.ai), verified 2026-06
	// Current Groq console catalog, read 2026-10-09. The 3.1 8B, 3.3 70B, and
	// MiniMax M2.7 rows are enterprise-only now, so their old public rates are
	// left off rather than kept as if current.
	"groq:qwen/qwen3.8-27b":                    {Input: 0.80, Output: 4.00},
	"groq:openai/gpt-oss-120b":                 {Input: 0.15, Output: 0.60},
	"groq:openai/gpt-oss-20b":                  {Input: 0.075, Output: 0.30},
	"groq:openai/gpt-oss-safeguard-20b":        {Input: 0.075, Output: 0.30},
	"groq:meta-llama/llama-prompt-guard-2-22m": {Input: 0.03, Output: 0.03},
	"groq:meta-llama/llama-prompt-guard-2-86m": {Input: 0.04, Output: 0.04},

	// Mistral docs pricing, read 2026-10-09. The Large 4 promotional rate is a
	// time-boxed launch card; the standard rate is recorded for the same reason
	// Anthropic's Sonnet 5 promotion records its standard rate.
	"mistral:mistral-large-4":    {Input: 1.36, Output: 4.18, CacheRead: 0.14},
	"mistral:mistral-large-3":    {Input: 0.50, Output: 1.50, CacheRead: 0.05},
	"mistral:mistral-medium-3.5": {Input: 1.50, Output: 7.50, CacheRead: 0.15},
	"mistral:mistral-small-4":    {Input: 0.15, Output: 0.60, CacheRead: 0.015},
	"mistral:ministral-3-14b":    {Input: 0.20, Output: 0.20, CacheRead: 0.02},
	"mistral:ministral-3-8b":     {Input: 0.15, Output: 0.15, CacheRead: 0.015},
	"mistral:ministral-3-3b":     {Input: 0.10, Output: 0.10, CacheRead: 0.01},
	"mistral:codestral":          {Input: 0.30, Output: 0.90, CacheRead: 0.03},
	"mistral:codestral-embed":    {Input: 0.15, CacheRead: 0.015},
	"fireworks:accounts/fireworks/models/llama-v3p1-70b-instruct": {Input: 0.90, Output: 0.90}, // Fireworks serverless (fireworks.ai/models/fireworks/llama-v3p1-70b-instruct), verified 2026-06
	// Exact Fireworks serverless IDs, verified 2026-10-09. Priority/Fast/US
	// residency have separate cards with no universal multiplier, so only the
	// standard short-context rates are recorded here.
	"fireworks:accounts/fireworks/models/ember-1":                        {Input: 3.00, Output: 15.00, CacheRead: 0.30},
	"fireworks:accounts/fireworks/models/kimi-k3":                        {Input: 3.00, Output: 15.00, CacheRead: 0.30},
	"fireworks:accounts/fireworks/models/deepseek-v4p1-flash":            {Input: 0.30, Output: 1.20, CacheRead: 0.006},
	"fireworks:accounts/fireworks/models/glm-5p3":                        {Input: 1.40, Output: 4.40, CacheRead: 0.26},
	"fireworks:accounts/fireworks/models/glm-5p3-flash":                  {Input: 0.15, Output: 0.50, CacheRead: 0.03},
	"fireworks:accounts/fireworks/models/qwen3p8-max":                    {Input: 2.00, Output: 6.00, CacheRead: 0.25},
	"fireworks:accounts/fireworks/models/minimax-m3":                     {Input: 0.30, Output: 1.20, CacheRead: 0.06},
	"fireworks:accounts/fireworks/models/nemotron-3-ultra-nvfp4":         {Input: 0.60, Output: 2.40, CacheRead: 0.12},
	"fireworks:accounts/fireworks/models/nemotron-lightning-3p5-30b-a3b": {Input: 0.05, Output: 0.20, CacheRead: 0.01},
	"fireworks:accounts/fireworks/models/gpt-oss-120b":                   {Input: 0.15, Output: 0.60, CacheRead: 0.015},
	"perplexity:sonar":   {Input: 1.00, Output: 1.00},                  // Perplexity Sonar token rates (pricepertoken.com/perplexity-sonar), excludes per-request search fee; verified 2026-06
	"zai:glm-5.2":        {Input: 1.40, Output: 4.40, CacheRead: 0.26}, // Z.AI GLM-5 series (docs.z.ai pricing), verified 2026-10-09
	"zai:glm-5.1":        {Input: 1.40, Output: 4.40, CacheRead: 0.26}, // docs.z.ai, verified 2026-10-09
	"zai:glm-5":          {Input: 1.00, Output: 3.20, CacheRead: 0.20}, // docs.z.ai, verified 2026-10-09
	"zai:glm-5-turbo":    {Input: 1.20, Output: 4.00},                  // docs.z.ai, verified 2026-06
	"zai:glm-4.7":        {Input: 0.60, Output: 2.20, CacheRead: 0.11}, // Z.AI GLM-4.7 direct (docs.z.ai; was 0.40/1.75 via openrouter), verified 2026-10-09
	"zai:glm-4.6":        {Input: 0.60, Output: 2.20, CacheRead: 0.11}, // docs.z.ai, verified 2026-10-09
	"zai:glm-4.5":        {Input: 0.60, Output: 2.20, CacheRead: 0.11}, // docs.z.ai, verified 2026-10-09
	"zai:glm-4.5-air":    {Input: 0.20, Output: 1.10, CacheRead: 0.03}, // docs.z.ai, verified 2026-10-09
	"zai:glm-5.3":        {Input: 1.40, Output: 4.40, CacheRead: 0.26},
	"zai:glm-5.3-flash":  {Input: 0.15, Output: 0.50, CacheRead: 0.03},
	"zai:glm-5.3-flashx": {Input: 0.37, Output: 1.25, CacheRead: 0.075},
	"zai:glm-4.7-flashx": {Input: 0.07, Output: 0.40, CacheRead: 0.01},
	"zai:glm-4.6v":       {Input: 0.30, Output: 0.90, CacheRead: 0.05},
	"zai:glm-4.7-Flash":  {}, // Free (Z.AI direct pricing, verified 2026-10)

	// TogetherAI (varies by model, these are examples)
	"togetherai:meta-llama/Llama-3.3-70B-Instruct-Turbo":       {Input: 1.04, Output: 1.04},
	"togetherai:MiniMaxAI/MiniMax-M3":                          {Input: 0.30, Output: 1.20, CacheRead: 0.06},
	"togetherai:zai-org/GLM-5.3":                               {Input: 1.40, Output: 4.40, CacheRead: 0.26},
	"togetherai:zai-org/GLM-5.3-Flash":                         {Input: 0.15, Output: 0.50, CacheRead: 0.03},
	"togetherai:deepseek-ai/DeepSeek-V4.1-Flash":               {Input: 0.30, Output: 1.20, CacheRead: 0.006},
	"togetherai:deepseek-ai/DeepSeek-V4-Pro-0813":              {Input: 1.32, Output: 3.96, CacheRead: 0.13},
	"togetherai:meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo": {Input: 3.50, Output: 3.50},
	"togetherai:meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo":  {Input: 0.88, Output: 0.88},
	"togetherai:meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo":   {Input: 0.18, Output: 0.18},
	"togetherai:mistralai/Mixtral-8x7B-Instruct-v0.1":          {Input: 0.60, Output: 0.60},
	// Exact serverless IDs from docs.together.ai/docs/serverless/models, read
	// 2026-10-09. Disputed cache rates (Qwen3.8-2.4T, Kimi-K3) are omitted rather
	// than guessed; the standard input/output rates are not in dispute.
	"togetherai:thinkingmachines/Inkling":      {Input: 1.00, Output: 4.05, CacheRead: 0.17},
	"togetherai:Qwen/Qwen3.8-2.4T-A95B":        {Input: 2.00, Output: 6.00},
	"togetherai:Qwen/Qwen3.7-Max":              {Input: 2.50, Output: 7.50, CacheRead: 0.50},
	"togetherai:Qwen/Qwen3.6-Plus":             {Input: 0.50, Output: 3.00},
	"togetherai:Qwen/Qwen3.7-Plus":             {Input: 0.32, Output: 1.28},
	"togetherai:Qwen/Qwen3.8-Flash":            {Input: 0.15, Output: 0.47},
	"togetherai:moonshotai/Kimi-K3":            {Input: 2.70, Output: 13.50},
	"togetherai:meta-models/Muse-Glimmer-30B":  {Input: 0.35, Output: 1.50, CacheRead: 0.04},
	"togetherai:Prism-ML/Ternary-Bonsai-27B":   {}, // Free
	"togetherai:together/Tev1-4B-experimental": {Input: 0.042},
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
	// Copy the whole card, not just Input/Output: cache rates and long-context
	// tiers are part of the published price and dropping them would make this
	// helper's value disagree with EstimateCost. Copy the tier slice so the
	// returned card cannot alias the global table.
	out := pricing
	if len(pricing.Tiers) > 0 {
		out.Tiers = append([]PricingTier(nil), pricing.Tiers...)
	}
	return &out, true
}

// ModelUsage tracks usage for a specific model.
type ModelUsage struct {
	Provider            Provider `json:"provider"`
	Model               string   `json:"model"`
	PromptTokens        int64    `json:"prompt_tokens"`
	CompletionTokens    int64    `json:"completion_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	Requests            int64    `json:"requests"`
	// UnpricedRequests counts requests for which no rate card or provider-reported
	// cost was available. Their tokens are included above, but EstimatedCost
	// excludes them, so a non-zero value makes a partial report explicit.
	UnpricedRequests int64     `json:"unpriced_requests,omitempty"`
	EstimatedCost    float64   `json:"estimated_cost_usd"`
	FirstUsed        time.Time `json:"first_used"`
	LastUsed         time.Time `json:"last_used"`
}

// CostTracker tracks token usage and estimated costs across providers/models.
//
// Create one with [NewCostTracker]; the zero value has no pricing or usage maps
// and is not usable.
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
	// map field would silently make it non-comparable, the same class of change
	// that forced the v6 major.
	modeCost map[PricingMode]float64
}

// NewCostTracker creates a new cost tracker with optional custom pricing.
func NewCostTracker(customPricing ...map[string]Pricing) *CostTracker {
	// Merge default and custom pricing
	pricing := make(map[string]Pricing)
	for k, v := range DefaultPricing {
		pricing[k] = clonePricing(v)
	}
	for _, cp := range customPricing {
		for k, v := range cp {
			pricing[k] = clonePricing(v)
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
func (t *CostTracker) Record(provider Provider, model string, usage Usage) (float64, bool) {
	return t.RecordMode(provider, model, usage, PricingModeStandard)
}

// costFor resolves the cost of usage for a provider/model under a billing mode.
// The caller must hold at least a read lock.
//
// Resolution order for a non-standard mode: a rate card registered on this
// tracker, then the built-in published cards (including provider-wide rules),
// then standard rates with known=false. An unpriced model is (0, false)
// whatever the mode, a mode discount on an unknown base is still unknown.
func (t *CostTracker) costFor(provider Provider, model, key string, usage Usage, mode PricingMode) (float64, bool) {
	// A provider-reported charge is what was billed, so it outranks every rate
	// card, including one registered on this tracker: an estimate cannot be more
	// accurate than the invoice. RecordMode already clamps non-finite and negative
	// charges through Usage.nonNegative; clamp again here for direct callers.
	if usage.Cost != nil {
		return usage.nonNegative().costValue(), true
	}
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

	// Price and update under one write lock. Cost resolution reads the tracker's
	// pricing overlay, and a concurrent Reset clears the usage map and mode costs;
	// computing the cost under a read lock and then reacquiring the write lock let
	// a Reset land between the two, after which this call repopulated the tracker
	// the operator had just cleared. Holding the write lock for the whole update
	// makes Reset and RecordMode atomic with respect to each other.
	t.mu.Lock()
	defer t.mu.Unlock()
	usage = usage.nonNegative()
	cost, known := t.costFor(provider, model, key, usage, mode)

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
	if wouldOverflow(u.PromptTokens, int64(usage.PromptTokens)) ||
		wouldOverflow(u.CompletionTokens, int64(usage.CompletionTokens)) ||
		wouldOverflow(u.CacheReadTokens, int64(usage.CacheReadTokens)) ||
		wouldOverflow(u.CacheCreationTokens, int64(usage.CacheCreationTokens)) {
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
	if !known {
		u.UnpricedRequests++
	}
	u.LastUsed = now
	u.EstimatedCost += cost
	t.modeCost[mode] += cost
	return cost, known
}

func wouldOverflow(total, add int64) bool {
	return add < 0 || total > maxSafeTokenCount || add > maxSafeTokenCount-total
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
	t.modePricing[modePricingKey(provider, model, mode)] = clonePricing(pricing)
}

// GetModeCosts returns accumulated cost broken down by billing mode. The
// returned map is a copy and is safe to retain.
//
// This lives on the tracker rather than on [ModelUsage] because ModelUsage is a
// comparable struct, and adding a map field to it would make it non-comparable,
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
	t.modeCost = make(map[PricingMode]float64)
}

// SetPricing sets or updates pricing for a model.
func (t *CostTracker) SetPricing(provider Provider, model string, pricing Pricing) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pricing[t.makeKey(provider, model)] = clonePricing(pricing)
}

// GetPricing returns pricing for a model.
func (t *CostTracker) GetPricing(provider Provider, model string) (Pricing, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.pricing[t.makeKey(provider, model)]
	return clonePricing(p), ok
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
	opts := ApplyOptions(options...)
	provider, model := m.servedIdentity(resp.Provider, resp.Model, opts)
	m.tracker.RecordMode(provider, model, resp.Usage, opts.PricingMode)
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
	var streamProvider Provider
	var streamModel string

	return WrapStreamWithFinalizer(ctx, stream, opts,
		func(chunk StreamChunk) StreamChunk {
			if chunk.Usage != nil {
				lastUsage = chunk.Usage
				if chunk.Provider != "" || chunk.Model != "" {
					streamProvider, streamModel = chunk.Provider, chunk.Model
				}
			}
			return chunk
		},
		func() {
			if lastUsage != nil {
				provider, model := m.servedIdentity(streamProvider, streamModel, opts)
				m.tracker.RecordMode(provider, model, *lastUsage, opts.PricingMode)
			}
		},
	), nil
}

// servedIdentity uses the provider's response provenance when available. An
// explicit WithModel override remains the best fallback when a provider does
// not populate response metadata.
func (m *CostMiddleware) servedIdentity(provider Provider, model string, opts *CallOptions) (Provider, string) {
	if provider == "" {
		provider = m.llm.Provider()
	}
	if model == "" {
		model = opts.Model
	}
	if model == "" {
		model = m.llm.Model()
	}
	return provider, model
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
// exists for the provider/model, callers must not treat that as a real
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
// price is published". Not every model has a card in every lane,
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
