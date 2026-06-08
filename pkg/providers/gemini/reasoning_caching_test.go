package gemini

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/internal/geminiapi"
)

// TestConvertUsageMetadata_TokenConsistency guards against double-counting thought
// tokens: on the Gemini API candidatesTokenCount already includes thoughts, so
// CompletionTokens must equal candidatesTokenCount (not candidates+thoughts), and
// PromptTokens must exclude cached tokens.
func TestConvertUsageMetadata_TokenConsistency(t *testing.T) {
	um := &geminiapi.UsageMetadata{
		PromptTokenCount:        100,
		CandidatesTokenCount:    550, // includes 500 thought tokens
		ThoughtsTokenCount:      500,
		CachedContentTokenCount: 40,
		TotalTokenCount:         650,
	}
	u := convertUsageMetadata(um)

	if u.CompletionTokens != 550 {
		t.Errorf("CompletionTokens = %d, want 550 (no double-count)", u.CompletionTokens)
	}
	if u.ReasoningTokens != 500 {
		t.Errorf("ReasoningTokens = %d, want 500", u.ReasoningTokens)
	}
	if u.PromptTokens != 60 {
		t.Errorf("PromptTokens = %d, want 60 (100 - 40 cached)", u.PromptTokens)
	}
	if u.CacheReadTokens != 40 {
		t.Errorf("CacheReadTokens = %d, want 40", u.CacheReadTokens)
	}
	// Prompt + completion must reconcile with the reported total.
	if u.PromptTokens+u.CompletionTokens+u.CacheReadTokens != u.TotalTokens {
		t.Errorf("prompt(%d)+completion(%d)+cache(%d) != total(%d)",
			u.PromptTokens, u.CompletionTokens, u.CacheReadTokens, u.TotalTokens)
	}
}

func TestGeminiBuildRequest_ThinkingConfig(t *testing.T) {
	client, err := New(WithAPIKey("test-key"), WithModel("gemini-2.5-pro"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	messages := []llms.Message{{Role: llms.RoleUser, Content: "hi"}}

	// A budget enables thinking with thought summaries.
	req := client.buildRequest(messages, llms.ApplyOptions(llms.WithReasoningBudget(2048)))
	if req.GenerationConfig.ThinkingConfig == nil {
		t.Fatal("expected ThinkingConfig to be set")
	}
	if !req.GenerationConfig.ThinkingConfig.IncludeThoughts {
		t.Error("expected IncludeThoughts=true")
	}
	if req.GenerationConfig.ThinkingConfig.ThinkingBudget == nil || *req.GenerationConfig.ThinkingConfig.ThinkingBudget != 2048 {
		t.Errorf("expected ThinkingBudget=2048, got %+v", req.GenerationConfig.ThinkingConfig.ThinkingBudget)
	}

	// Explicitly disabling reasoning sets a zero budget.
	disabled := false
	req = client.buildRequest(messages, llms.ApplyOptions(llms.WithReasoning(llms.ReasoningConfig{Enabled: &disabled})))
	if req.GenerationConfig.ThinkingConfig == nil || req.GenerationConfig.ThinkingConfig.ThinkingBudget == nil ||
		*req.GenerationConfig.ThinkingConfig.ThinkingBudget != 0 {
		t.Errorf("expected disabled thinking (budget 0), got %+v", req.GenerationConfig.ThinkingConfig)
	}
}
