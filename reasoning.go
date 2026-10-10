package llms

import "reflect"

// This file defines the cross-provider reasoning ("thinking") surface: the
// effort/budget controls a caller sets on a request and the ReasoningContent a
// model returns. Providers map ReasoningConfig onto their own knobs, OpenAI's
// reasoning_effort, Anthropic/Gemini thinking-token budgets, or the Z.AI/Qwen
// "thinking" toggle, and normalize their output back into ReasoningContent.

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

	// Provider and Model record who produced this reasoning. A signature or
	// encrypted payload is only valid for the provider that issued it, so each
	// provider replays reasoning stamped with its own name and drops reasoning
	// stamped with another's (see [ReasoningContent.ReplayableBy]). Providers
	// stamp what they return; reasoning built by hand is unstamped and replays
	// everywhere, as it did before stamps existed.
	//
	// The stamp names the client, not the endpoint: an Anthropic client pointed
	// at a third-party Anthropic-compatible server with WithBaseURL stamps
	// "anthropic" all the same. Model is recorded but replay is not restricted
	// by it, since no provider here is known to reject reasoning from another of
	// its own models.
	Provider Provider `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
}

// ReplayableBy reports whether provider p may send r back in a request: true
// when r is unstamped or was produced by p. It is false for a nil r.
func (r *ReasoningContent) ReplayableBy(p Provider) bool {
	return r != nil && (r.Provider == "" || r.Provider == p)
}

// Clone returns a copy of r with its own Metadata map. Slice and map values
// inside Metadata (Anthropic's redacted_thinking blocks, the Responses
// reasoning items) are copied too, so a caller mutating them cannot corrupt r or
// another clone. It returns nil for a nil r.
func (r *ReasoningContent) Clone() *ReasoningContent {
	if r == nil {
		return nil
	}
	c := *r
	if r.Metadata != nil {
		c.Metadata = make(map[string]any, len(r.Metadata))
		for k, v := range r.Metadata {
			c.Metadata[k] = cloneMetadataValue(v)
		}
	}
	return &c
}

// cloneMetadataValue deep-copies the reference-typed values that Metadata
// carries: slices, arrays, maps, pointers, and structs reachable through them.
// Scalars and opaque types are returned as-is. Reference values already copied
// on the current path are reused rather than copied again, so a self-referential
// value (m["self"] = m) clones to a self-referential copy instead of recursing
// forever.
func cloneMetadataValue(v any) any {
	if v == nil {
		return nil
	}
	return deepCopyValue(reflect.ValueOf(v), make(map[cloneKey]reflect.Value)).Interface()
}

// cloneKey identifies a reference value for the deep-copy memo. The length is
// part of the key so two slices that share a starting address but differ in
// length (overlapping subslices) are treated as distinct values rather than one
// returning the other's clone.
type cloneKey struct {
	ptr   uintptr
	len   int
	kind  reflect.Kind
	type_ reflect.Type
}

func deepCopyValue(v reflect.Value, seen map[cloneKey]reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		return deepCopyValue(v.Elem(), seen)
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		key := cloneKey{ptr: v.Pointer(), kind: v.Kind(), type_: v.Type()}
		if cp, ok := seen[key]; ok {
			return cp
		}
		cp := reflect.New(v.Type().Elem())
		seen[key] = cp
		cp.Elem().Set(deepCopyValue(v.Elem(), seen))
		return cp
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		key := cloneKey{ptr: v.Pointer(), len: v.Len(), kind: v.Kind(), type_: v.Type()}
		if cp, ok := seen[key]; ok {
			return cp
		}
		cp := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		seen[key] = cp
		for i := 0; i < v.Len(); i++ {
			cp.Index(i).Set(deepCopyValue(v.Index(i), seen))
		}
		return cp
	case reflect.Array:
		cp := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			cp.Index(i).Set(deepCopyValue(v.Index(i), seen))
		}
		return cp
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		// The map type belongs in the key: two named map types can share the same
		// underlying map, and reusing a clone of the wrong static type would
		// either mis-assign or panic when the value is placed in a typed field.
		key := cloneKey{ptr: v.Pointer(), kind: v.Kind(), type_: v.Type()}
		if cp, ok := seen[key]; ok {
			return cp
		}
		cp := reflect.MakeMapWithSize(v.Type(), v.Len())
		seen[key] = cp
		iter := v.MapRange()
		for iter.Next() {
			cp.SetMapIndex(deepCopyValue(iter.Key(), seen), deepCopyValue(iter.Value(), seen))
		}
		return cp
	case reflect.Struct:
		cp := reflect.New(v.Type()).Elem()
		// Copy the whole struct first so unexported fields keep their values;
		// only the exported, settable reference fields can be deep-copied.
		cp.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if !cp.Field(i).CanSet() {
				continue
			}
			switch v.Field(i).Kind() {
			case reflect.Interface, reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
				cp.Field(i).Set(deepCopyValue(v.Field(i), seen))
			}
		}
		return cp
	default:
		return v
	}
}

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
// It composes with WithReasoning and WithReasoningBudget: orthogonal fields are
// preserved regardless of option order, while later effort options overwrite
// earlier effort settings.
func WithReasoningEffort(effort ReasoningEffort) CallOption {
	return func(o *CallOptions) {
		ensureReasoningConfig(o).Effort = effort
	}
}

// WithReasoningBudget caps the tokens the model may spend reasoning. Honored by
// providers with an explicit thinking budget (Anthropic, Gemini). It composes
// with WithReasoning and WithReasoningEffort: orthogonal fields are preserved
// regardless of option order, while later budget options overwrite earlier
// budget settings.
func WithReasoningBudget(tokens int) CallOption {
	return func(o *CallOptions) {
		ensureReasoningConfig(o).BudgetTokens = tokens
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
