package llms

import "testing"

// TestCapabilityRegistry_CurrentFlagships guards against the capability registry
// drifting behind the current flagship lineup. Before this was fixed, models like
// gpt-5, claude-opus-4-8, and gemini-3.1-pro-preview fell through to stale provider
// defaults (wrong limits, SupportsReasoning=false). Limits here mirror each
// provider's knownModels overlay; reasoning mirrors isOpenAIReasoningModel (OpenAI)
// and the existing reasoning treatment of the same model generations.
func TestCapabilityRegistry_CurrentFlagships(t *testing.T) {
	r := NewCapabilityRegistry()

	cases := []struct {
		provider  Provider
		model     string
		ctx       int
		out       int
		reasoning bool
		vision    bool
		caching   bool
	}{
		// OpenAI GPT-5 family — 400k/128k, all reason.
		{ProviderOpenAI, "gpt-5", 400000, 128000, true, true, true},
		{ProviderOpenAI, "gpt-5-mini", 400000, 128000, true, true, true},
		{ProviderOpenAI, "gpt-5.4", 400000, 128000, true, true, true},
		{ProviderOpenAI, "gpt-5.5-pro", 400000, 128000, true, true, true},
		// GPT-5.6 — 922k max *input* (of a 1.05M total window), 128k out.
		{ProviderOpenAI, "gpt-5.6-sol", 922000, 128000, true, true, true},
		{ProviderOpenAI, "gpt-5.6-terra", 922000, 128000, true, true, true},
		{ProviderOpenAI, "gpt-5.6-luna", 922000, 128000, true, true, true},
		// Anthropic current flagships — 1M/128k, reason, JSON via tools (not flagged here).
		{ProviderAnthropic, "claude-fable-5", 1000000, 128000, true, true, true},
		{ProviderAnthropic, "claude-opus-5", 1000000, 128000, true, true, true},
		{ProviderAnthropic, "claude-sonnet-5", 1000000, 128000, true, true, true},
		{ProviderAnthropic, "claude-opus-4-8", 1000000, 128000, true, true, true},
		{ProviderAnthropic, "claude-opus-4-5", 200000, 64000, true, true, true},
		// Gemini 3.x — 1,048,576/65,536; pro/flash reason.
		{ProviderGemini, "gemini-3.1-pro-preview", 1048576, 65536, true, true, true},
		{ProviderGemini, "gemini-3.6-flash", 1048576, 65536, true, true, true},
		{ProviderGemini, "gemini-3.5-flash", 1048576, 65536, true, true, true},
	}

	for _, c := range cases {
		t.Run(string(c.provider)+":"+c.model, func(t *testing.T) {
			caps := r.Get(c.provider, c.model)
			if caps.MaxContextTokens != c.ctx {
				t.Errorf("MaxContextTokens=%d, want %d", caps.MaxContextTokens, c.ctx)
			}
			if caps.MaxOutputTokens != c.out {
				t.Errorf("MaxOutputTokens=%d, want %d", caps.MaxOutputTokens, c.out)
			}
			if caps.SupportsReasoning != c.reasoning {
				t.Errorf("SupportsReasoning=%v, want %v", caps.SupportsReasoning, c.reasoning)
			}
			if caps.SupportsVision != c.vision {
				t.Errorf("SupportsVision=%v, want %v", caps.SupportsVision, c.vision)
			}
			if caps.SupportsPromptCaching != c.caching {
				t.Errorf("SupportsPromptCaching=%v, want %v", caps.SupportsPromptCaching, c.caching)
			}
		})
	}
}

// TestCapabilityRegistry_ReasoningClassificationUnchanged pins the reasoning flag
// for representative non-flagship models so the flagship additions above cannot
// silently mislabel an existing model.
func TestCapabilityRegistry_ReasoningClassificationUnchanged(t *testing.T) {
	r := NewCapabilityRegistry()

	reasoning := map[[2]string]bool{
		{string(ProviderOpenAI), "o3"}:                    true,  // already a reasoning model
		{string(ProviderAnthropic), "claude-sonnet-4"}:    true,  // already flagged
		{string(ProviderOpenAI), "gpt-4o"}:                false, // not a reasoning model
		{string(ProviderGemini), "gemini-3.1-flash-lite"}: false, // flash-lite intentionally unflagged
	}

	for key, want := range reasoning {
		caps := r.Get(Provider(key[0]), key[1])
		if caps.SupportsReasoning != want {
			t.Errorf("%s:%s SupportsReasoning=%v, want %v", key[0], key[1], caps.SupportsReasoning, want)
		}
	}
}

// TestGetModelCapabilities_FlagshipPublicPath exercises the exported global-registry
// path the way provider Capabilities() implementations do, confirming the flagship
// fix is visible through the public API and not just the test-local registry.
func TestGetModelCapabilities_FlagshipPublicPath(t *testing.T) {
	caps := GetModelCapabilities(ProviderOpenAI, "gpt-5")
	if caps.MaxContextTokens != 400000 || !caps.SupportsReasoning {
		t.Errorf("GetModelCapabilities(openai, gpt-5) = {ctx:%d reasoning:%v}, want {400000 true}",
			caps.MaxContextTokens, caps.SupportsReasoning)
	}
}
