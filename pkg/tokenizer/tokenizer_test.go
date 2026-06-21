package tokenizer_test

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/tokenizer"
)

func TestForModel_KnownCounts(t *testing.T) {
	cases := []struct {
		model string
		text  string
		want  int
	}{
		{"gpt-4o", "hello world", 2},
		{"gpt-4o", "Hello, world!", 4},
		{"gpt-4", "hello world", 2},
		{"gpt-4", "Hello, world!", 4},
		{"gpt-3.5-turbo", "hello world", 2},
	}
	for _, tc := range cases {
		tk, err := tokenizer.ForModel(tc.model)
		if err != nil {
			t.Fatalf("ForModel(%q): %v", tc.model, err)
		}
		if got := tk.CountTokens(tc.text); got != tc.want {
			t.Errorf("ForModel(%q).CountTokens(%q) = %d, want %d", tc.model, tc.text, got, tc.want)
		}
	}
}

func TestForEncoding(t *testing.T) {
	for _, enc := range []string{"o200k_base", "cl100k_base"} {
		tk, err := tokenizer.ForEncoding(enc)
		if err != nil {
			t.Fatalf("ForEncoding(%q): %v", enc, err)
		}
		if got := tk.CountTokens("hello world"); got != 2 {
			t.Errorf("ForEncoding(%q).CountTokens(\"hello world\") = %d, want 2", enc, got)
		}
	}
}

func TestForModel_UnknownModel(t *testing.T) {
	if _, err := tokenizer.ForModel("definitely-not-a-real-model"); err == nil {
		t.Fatal("expected error for unknown model, got nil")
	}
}

func TestCountTokens_Empty(t *testing.T) {
	tk, err := tokenizer.ForEncoding("cl100k_base")
	if err != nil {
		t.Fatal(err)
	}
	if got := tk.CountTokens(""); got != 0 {
		t.Errorf("CountTokens(\"\") = %d, want 0", got)
	}
}

// Special-token text must be counted as ordinary text, not as a control token,
// so untrusted input can't smuggle in encoder special tokens.
func TestCountTokens_SpecialTokenIsOrdinary(t *testing.T) {
	tk, err := tokenizer.ForEncoding("cl100k_base")
	if err != nil {
		t.Fatal(err)
	}
	if got := tk.CountTokens("<|endoftext|>"); got <= 1 {
		t.Errorf("CountTokens(\"<|endoftext|>\") = %d, want >1 (treated as ordinary text)", got)
	}
}

// The tokenizer must satisfy llms.Tokenizer and override the heuristic when set
// on a TokenEstimator.
func TestEstimatorIntegration(t *testing.T) {
	tk, err := tokenizer.ForModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}

	est := llms.DefaultTokenEstimator()
	est.Tokenizer = tk
	if got := est.EstimateTokens("Hello, world!"); got != 4 {
		t.Errorf("estimator with tokenizer: EstimateTokens = %d, want 4 (exact)", got)
	}

	// Without a tokenizer it falls back to the heuristic (non-zero, not exact).
	heuristic := llms.DefaultTokenEstimator()
	if heuristic.EstimateTokens("Hello, world!") == 0 {
		t.Error("heuristic estimator returned 0 for non-empty text")
	}
}
