package llms

import "testing"

func TestReasoningBudgetForEffort(t *testing.T) {
	cases := map[ReasoningEffort]int{
		ReasoningEffortMinimal:   1024,
		ReasoningEffortLow:       4096,
		ReasoningEffortMedium:    12288,
		ReasoningEffortHigh:      24576,
		ReasoningEffort(""):      0,
		ReasoningEffort("bogus"): 0,
	}
	for effort, want := range cases {
		if got := ReasoningBudgetForEffort(effort); got != want {
			t.Errorf("ReasoningBudgetForEffort(%q) = %d, want %d", effort, got, want)
		}
	}
}

func TestReasoningConfigIsEnabled(t *testing.T) {
	tru, fls := true, false
	cases := []struct {
		name string
		cfg  *ReasoningConfig
		want bool
	}{
		{"nil", nil, false},
		{"empty", &ReasoningConfig{}, false},
		{"enabled true", &ReasoningConfig{Enabled: &tru}, true},
		{"enabled false beats hints", &ReasoningConfig{Enabled: &fls, Effort: ReasoningEffortHigh}, false},
		{"effort implies enabled", &ReasoningConfig{Effort: ReasoningEffortLow}, true},
		{"budget implies enabled", &ReasoningConfig{BudgetTokens: 2048}, true},
	}
	for _, tc := range cases {
		if got := tc.cfg.IsEnabled(); got != tc.want {
			t.Errorf("%s: IsEnabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWithReasoningCopiesEnabledPointer(t *testing.T) {
	enabled := true
	opts := ApplyOptions(WithReasoning(ReasoningConfig{Enabled: &enabled}))
	// Mutating the caller's bool must not change the stored option.
	enabled = false
	if opts.Reasoning == nil || opts.Reasoning.Enabled == nil || !*opts.Reasoning.Enabled {
		t.Fatalf("expected stored Enabled to remain true, got %+v", opts.Reasoning)
	}
}

func TestResponseReasoningText(t *testing.T) {
	var nilResp *Response
	if got := nilResp.ReasoningText(); got != "" {
		t.Errorf("nil response: expected empty, got %q", got)
	}
	if got := (&Response{}).ReasoningText(); got != "" {
		t.Errorf("no reasoning: expected empty, got %q", got)
	}
	resp := &Response{}
	resp.SetReasoning(&ReasoningContent{Content: "because"})
	if got := resp.ReasoningText(); got != "because" {
		t.Errorf("expected %q, got %q", "because", got)
	}
	// SetReasoning populates both the canonical field and the deprecated alias.
	if resp.Reasoning != resp.Thinking {
		t.Error("expected Reasoning and Thinking to point at the same value")
	}
}

func TestThinkingContentAliasIdentity(t *testing.T) {
	// ThinkingContent must remain a true alias of ReasoningContent: a
	// *ThinkingContent must be assignable wherever *ReasoningContent is expected.
	tc := &ThinkingContent{Content: "ok"}
	resp := &Response{Reasoning: tc} // compiles only if the types are identical
	if resp.ReasoningText() != "ok" {
		t.Error("ThinkingContent is not an alias of ReasoningContent")
	}
}
