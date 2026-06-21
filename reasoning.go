package llms

// This file defines the cross-provider reasoning ("thinking") surface: the
// effort/budget controls a caller sets on a request and the ReasoningContent a
// model returns. Providers map ReasoningConfig onto their own knobs — OpenAI's
// reasoning_effort, Anthropic/Gemini thinking-token budgets, or the Z.AI/Qwen
// "thinking" toggle — and normalize their output back into ReasoningContent.

// ReasoningEffort controls how much effort a reasoning-capable model spends on
// its internal chain-of-thought before answering. Providers translate it to
// their own parameter: OpenAI sends it verbatim as reasoning_effort; providers
// that only accept a token budget (Anthropic, Gemini) derive one via
// ReasoningBudgetForEffort.
type ReasoningEffort string

const (
	// ReasoningEffortMinimal requests the least reasoning (fastest, cheapest).
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	// ReasoningEffortLow requests light reasoning.
	ReasoningEffortLow ReasoningEffort = "low"
	// ReasoningEffortMedium requests moderate reasoning.
	ReasoningEffortMedium ReasoningEffort = "medium"
	// ReasoningEffortHigh requests the most thorough reasoning.
	ReasoningEffortHigh ReasoningEffort = "high"
)

// ReasoningConfig requests model reasoning ("thinking") for a call. It is a
// portable intent: a provider applies whichever fields it understands and
// ignores the rest. Leave it nil to use the model's default behavior.
type ReasoningConfig struct {
	// Effort is the qualitative reasoning level. Optional.
	Effort ReasoningEffort
	// BudgetTokens caps the tokens the model may spend reasoning. Optional; used
	// by providers with an explicit thinking budget (Anthropic, Gemini). When zero
	// and Effort is set, providers derive a budget via ReasoningBudgetForEffort.
	BudgetTokens int
	// Enabled explicitly toggles reasoning on or off for providers that expose a
	// boolean switch (Z.AI GLM, Qwen). Nil means "unspecified". A non-nil value is
	// honored verbatim, so &false can disable thinking on models that default it on.
	Enabled *bool
}

// IsEnabled reports whether the config asks for reasoning to be active. A nil
// config, or one whose Enabled is explicitly false, is not enabled. Otherwise it
// is enabled when Enabled is true or an Effort/BudgetTokens hint is present.
// Providers use this to decide whether to send a reasoning directive.
func (c *ReasoningConfig) IsEnabled() bool {
	if c == nil {
		return false
	}
	if c.Enabled != nil {
		return *c.Enabled
	}
	return c.Effort != "" || c.BudgetTokens > 0
}

// ReasoningContent represents the model's reasoning/chain-of-thought output.
// It is surfaced on Response.Reasoning and StreamChunk.Reasoning when the
// provider/model supports reasoning.
type ReasoningContent struct {
	// Content is the model's reasoning text.
	Content string `json:"content,omitempty"`
	// Tokens is the number of tokens spent on reasoning, when reported separately.
	// Zero if the provider does not report it.
	Tokens int `json:"tokens,omitempty"`
	// Signature is an opaque provider token that authenticates a reasoning block
	// (Anthropic returns one for extended-thinking blocks). It must be echoed back
	// on a follow-up turn to preserve the thinking context. Empty if unused.
	Signature string `json:"signature,omitempty"`
	// Metadata carries provider-specific reasoning details.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ThinkingContent is the former name of ReasoningContent.
//
// Deprecated: use ReasoningContent. This alias is retained for backward
// compatibility and will be removed in a future major version.
type ThinkingContent = ReasoningContent

// ReasoningBudgetForEffort maps a qualitative ReasoningEffort to a default
// thinking-token budget for providers that require an explicit budget. Returns 0
// for an empty/unknown effort so callers can fall back to the model default.
func ReasoningBudgetForEffort(e ReasoningEffort) int {
	switch e {
	case ReasoningEffortMinimal:
		return 1024
	case ReasoningEffortLow:
		return 4096
	case ReasoningEffortMedium:
		return 12288
	case ReasoningEffortHigh:
		return 24576
	default:
		return 0
	}
}

// WithReasoning merges the reasoning configuration for a call.
//
// Non-zero Effort and BudgetTokens values overwrite any existing reasoning
// settings, while zero values mean "unspecified" and preserve values already set
// by options such as WithReasoningEffort or WithReasoningBudget. Enabled is
// explicit because it is a pointer: nil preserves the existing setting, while
// non-nil true or false overwrites it. For same-field conflicts, the later
// option still wins.
func WithReasoning(cfg ReasoningConfig) CallOption {
	return func(o *CallOptions) {
		reasoning := ensureReasoningConfig(o)
		mergeReasoningConfig(reasoning, cfg)
	}
}

// WithReasoningEffort requests reasoning at the given qualitative effort level.
// It composes with WithReasoning, WithReasoningBudget, and WithThinkingMode:
// orthogonal fields are preserved regardless of option order, while later
// effort options overwrite earlier effort settings.
func WithReasoningEffort(effort ReasoningEffort) CallOption {
	return func(o *CallOptions) {
		ensureReasoningConfig(o).Effort = effort
	}
}

// WithReasoningBudget caps the tokens the model may spend reasoning. Honored by
// providers with an explicit thinking budget (Anthropic, Gemini). It composes
// with WithReasoning, WithReasoningEffort, and WithThinkingMode: orthogonal
// fields are preserved regardless of option order, while later budget options
// overwrite earlier budget settings.
func WithReasoningBudget(tokens int) CallOption {
	return func(o *CallOptions) {
		ensureReasoningConfig(o).BudgetTokens = tokens
	}
}

// WithThinkingMode enables or disables the model's reasoning ("thinking") mode.
// It composes with WithReasoning, WithReasoningEffort, and WithReasoningBudget:
// orthogonal fields are preserved regardless of option order, while later
// enabled/disabled settings overwrite earlier Enabled settings. Unlike the
// value fields on ReasoningConfig, false is explicit here because it is carried
// through a non-nil *bool.
//
// Deprecated: use WithReasoning, WithReasoningEffort, or WithReasoningBudget.
// This forwards to ReasoningConfig.Enabled and is retained for backward
// compatibility.
func WithThinkingMode(enabled bool) CallOption {
	return func(o *CallOptions) {
		b := enabled
		ensureReasoningConfig(o).Enabled = &b
	}
}

func ensureReasoningConfig(o *CallOptions) *ReasoningConfig {
	if o.Reasoning == nil {
		o.Reasoning = &ReasoningConfig{}
	}
	return o.Reasoning
}

func mergeReasoningConfig(dst *ReasoningConfig, src ReasoningConfig) {
	if src.Effort != "" {
		dst.Effort = src.Effort
	}
	if src.BudgetTokens != 0 {
		dst.BudgetTokens = src.BudgetTokens
	}
	if src.Enabled != nil {
		b := *src.Enabled
		dst.Enabled = &b
	}
}
