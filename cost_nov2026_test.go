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
