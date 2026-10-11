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

func TestWithReasoningComposesOrthogonalFieldsRegardlessOfOrder(t *testing.T) {
	enabled := true
	cfgWithoutEffortOrBudget := ReasoningConfig{Enabled: &enabled}

	cases := []struct {
		name string
		opts []CallOption
	}{
		{
			name: "effort before config",
			opts: []CallOption{
				WithReasoningEffort(ReasoningEffortHigh),
				WithReasoning(cfgWithoutEffortOrBudget),
				WithReasoningBudget(8192),
			},
		},
		{
			name: "config before effort",
			opts: []CallOption{
				WithReasoning(cfgWithoutEffortOrBudget),
				WithReasoningEffort(ReasoningEffortHigh),
				WithReasoningBudget(8192),
			},
		},
		{
			name: "budget before config",
			opts: []CallOption{
				WithReasoningBudget(8192),
				WithReasoning(cfgWithoutEffortOrBudget),
				WithReasoningEffort(ReasoningEffortHigh),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := ApplyOptions(tc.opts...)
			assertReasoningConfig(t, opts.Reasoning, ReasoningConfig{
				Effort:       ReasoningEffortHigh,
				BudgetTokens: 8192,
				Enabled:      boolPtr(true),
			})
		})
	}
}

func TestWithReasoningIncomingConfigOverridesSetFields(t *testing.T) {
	enabled := false
	opts := ApplyOptions(
		WithReasoningEffort(ReasoningEffortLow),
		WithReasoningBudget(1024),
		WithReasoning(ReasoningConfig{Enabled: boolPtr(true)}),
		WithReasoning(ReasoningConfig{
			Effort:       ReasoningEffortHigh,
			BudgetTokens: 4096,
			Enabled:      &enabled,
		}),
	)

	assertReasoningConfig(t, opts.Reasoning, ReasoningConfig{
		Effort:       ReasoningEffortHigh,
		BudgetTokens: 4096,
		Enabled:      boolPtr(false),
	})
}

func TestWithReasoningPreservesExplicitEnabledFalse(t *testing.T) {
	opts := ApplyOptions(
		WithReasoning(ReasoningConfig{Enabled: boolPtr(false)}),
		WithReasoning(ReasoningConfig{
			Effort:       ReasoningEffortHigh,
			BudgetTokens: 8192,
		}),
	)

	assertReasoningConfig(t, opts.Reasoning, ReasoningConfig{
		Effort:       ReasoningEffortHigh,
		BudgetTokens: 8192,
		Enabled:      boolPtr(false),
	})
	if opts.Reasoning.IsEnabled() {
		t.Error("expected explicit Enabled=false to keep reasoning disabled")
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
}

func assertReasoningConfig(t *testing.T, got *ReasoningConfig, want ReasoningConfig) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected Reasoning to be set")
	}
	if got.Effort != want.Effort {
		t.Errorf("Effort = %q, want %q", got.Effort, want.Effort)
	}
	if got.BudgetTokens != want.BudgetTokens {
		t.Errorf("BudgetTokens = %d, want %d", got.BudgetTokens, want.BudgetTokens)
	}
	switch {
	case got.Enabled == nil && want.Enabled == nil:
	case got.Enabled == nil || want.Enabled == nil:
		t.Errorf("Enabled = %v, want %v", got.Enabled, want.Enabled)
	case *got.Enabled != *want.Enabled:
		t.Errorf("Enabled = %v, want %v", *got.Enabled, *want.Enabled)
	}
}

func boolPtr(v bool) *bool {
	return &v
}

// TestReasoningContentClone_IsolatesNestedMetadata guards the cache boundary: a
// clone must not share the slice or map values carried in Metadata, or mutating
// a cached response's reasoning would corrupt the stored copy (and vice versa).
func TestReasoningContentClone_IsolatesNestedMetadata(t *testing.T) {
	type item struct {
		ID      string
		Summary []string
	}
	original := &ReasoningContent{
		Content: "thinking",
		Metadata: map[string]any{
			"items":   []item{{ID: "a", Summary: []string{"s1"}}},
			"strings": []string{"x", "y"},
			"byName":  map[string]string{"k": "v"},
			"scalar":  7,
		},
	}
	clone := original.Clone()

	cloneItems := clone.Metadata["items"].([]item)
	cloneItems[0].ID = "mutated"
	cloneItems[0].Summary[0] = "mutated"
	clone.Metadata["strings"].([]string)[0] = "mutated"
	clone.Metadata["byName"].(map[string]string)["k"] = "mutated"

	origItems := original.Metadata["items"].([]item)
	if origItems[0].ID != "a" || origItems[0].Summary[0] != "s1" {
		t.Errorf("clone mutation leaked into original items: %#v", origItems[0])
	}
	if original.Metadata["strings"].([]string)[0] != "x" {
		t.Errorf("clone mutation leaked into original strings: %#v", original.Metadata["strings"])
	}
	if original.Metadata["byName"].(map[string]string)["k"] != "v" {
		t.Errorf("clone mutation leaked into original map: %#v", original.Metadata["byName"])
	}
	if clone.Metadata["scalar"] != 7 {
		t.Errorf("scalar metadata changed: %#v", clone.Metadata["scalar"])
	}
}

// TestReasoningContentClone_DistinctEmptySliceTypes guards the clone memo key:
// two zero-length slices can share a backing address but have different element
// types, so keying only on pointer, length, and kind aliases one as the other.
func TestReasoningContentClone_DistinctEmptySliceTypes(t *testing.T) {
	original := &ReasoningContent{Metadata: map[string]any{
		"ints":    []int{},
		"strings": []string{},
	}}

	clone := original.Clone()
	if _, ok := clone.Metadata["ints"].([]int); !ok {
		t.Fatalf("ints cloned as %T, want []int", clone.Metadata["ints"])
	}
	if _, ok := clone.Metadata["strings"].([]string); !ok {
		t.Fatalf("strings cloned as %T, want []string", clone.Metadata["strings"])
	}
}

// TestReasoningContentClone_DistinctMapTypes pins the same memo-key rule for
// maps: two named map types can share one underlying map, and keying only on
// pointer and kind would clone one as the other's static type.
func TestReasoningContentClone_DistinctMapTypes(t *testing.T) {
	type intMap map[string]int
	type stringMap map[string]string
	sharedInt := intMap{"a": 1}
	original := &ReasoningContent{Metadata: map[string]any{
		"ints":    sharedInt,
		"strings": stringMap(nil),
	}}

	clone := original.Clone()
	if _, ok := clone.Metadata["ints"].(intMap); !ok {
		t.Fatalf("ints cloned as %T, want intMap", clone.Metadata["ints"])
	}
	if _, ok := clone.Metadata["strings"].(stringMap); !ok {
		t.Fatalf("strings cloned as %T, want stringMap", clone.Metadata["strings"])
	}
}

// TestReasoningContentClone_SelfReferentialMetadata pins that a self-referential
// metadata value clones to a self-referential copy rather than recursing until
// the stack overflows.
func TestReasoningContentClone_SelfReferentialMetadata(t *testing.T) {
	m := map[string]any{}
	m["self"] = m
	original := &ReasoningContent{Metadata: map[string]any{"cycle": m}}

	clone := original.Clone()
	cloneCycle, ok := clone.Metadata["cycle"].(map[string]any)
	if !ok {
		t.Fatalf("clone cycle is %T, want map[string]any", clone.Metadata["cycle"])
	}
	if _, ok := cloneCycle["self"].(map[string]any); !ok {
		t.Errorf("clone cycle self-reference is %T, want map[string]any", cloneCycle["self"])
	}
	// The clone's cycle must be a distinct map from the original's.
	if _, same := cloneCycle["self"].(map[string]any); same {
		if _, aliased := original.Metadata["cycle"].(map[string]any)["self"]; aliased {
			// Mutating the clone must not be visible in the original.
			cloneCycle["mutated"] = true
			if _, leaked := m["mutated"]; leaked {
				t.Error("clone mutation leaked into the original metadata map")
			}
		}
	}
}
