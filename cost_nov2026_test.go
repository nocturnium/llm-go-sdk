package llms

import "testing"

// TestDefaultPricing_Nov2026Models pins the rate cards added from first-party
// pricing pages on 2026-11-11 so a later refresh cannot silently drop them.
func TestDefaultPricing_Nov2026Models(t *testing.T) {
	cases := []struct {
		key           string
		input, output float64
	}{
		{"openai:gpt-6.1-terra", 1.00, 5.00},
		{"anthropic:claude-fable-5-2", 8.00, 40.00},
		{"anthropic:claude-mythos-5-2", 8.00, 40.00},
		{"gemini:gemini-4-argon", 4.00, 20.00},
		{"gemini:gemini-4-carbon", 4.00, 20.00},
		{"zai:glm-5.3-flashx-legal", 0.37, 1.25},
		{"deepseek:deepseek-flash", 0.30, 1.20},
		{"mistral:mistral-large-4", 0.70, 2.10},
		{"cerebras:gpt-oss-120b", 0.35, 0.75},
		{"cerebras:qwen-3.8-27b", 0.99, 1.49},
		{"perplexity:sonar", 0.25, 2.50},
		{"togetherai:moonshotai/Kimi-K3", 3.00, 15.00},
		{"togetherai:moonshotai/Kimi-K3.1-Pro", 2.00, 10.00},
		{"togetherai:MiniMaxAI/MiniMax-M3.1-Flash", 0.15, 0.60},
		{"togetherai:meta-models/Muse-Spark-Open-816B-A42B", 1.25, 4.25},
		{"togetherai:reflection/Beam-1.0-501B-A23B", 0.40, 1.20},
		{"togetherai:zai-org/GLM-5.2", 1.40, 4.40},
		{"togetherai:nvidia/NVIDIA-Nemotron-3.5-Super-VL", 0.15, 0.50},
		{"togetherai:ricursive-ai/Ripple-65B-Preview", 0.20, 0.60},
		{"fireworks:accounts/fireworks/models/kimi-k3p1-pro", 2.00, 10.00},
		{"fireworks:accounts/fireworks/models/muse-spark-open", 1.25, 4.25},
	}
	for _, c := range cases {
		p, ok := DefaultPricing[c.key]
		if !ok {
			t.Errorf("%s: missing from DefaultPricing", c.key)
			continue
		}
		if p.Input != c.input || p.Output != c.output {
			t.Errorf("%s: got %v/%v, want %v/%v", c.key, p.Input, p.Output, c.input, c.output)
		}
	}
}
