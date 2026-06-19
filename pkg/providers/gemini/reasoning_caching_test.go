package gemini

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/internal/geminiapi"
)

// TestConvertUsageMetadata_TokenConsistency verifies reasoning tokens are counted
// exactly once across both Gemini accounting backends, that PromptTokens excludes
// cache, and that prompt+completion+cache reconciles with the reported total.
func TestConvertUsageMetadata_TokenConsistency(t *testing.T) {
	cases := []struct {
		name           string
		um             geminiapi.UsageMetadata
		wantPrompt     int
		wantCompletion int
		wantReasoning  int
	}{
		{
			// Separate accounting (Vertex / Gemini-API-in-practice): candidates
			// EXCLUDES thoughts; total = prompt + candidates + thoughts.
			name: "thoughts separate from candidates",
			um: geminiapi.UsageMetadata{
				PromptTokenCount: 100, CachedContentTokenCount: 40,
				CandidatesTokenCount: 50, ThoughtsTokenCount: 500, TotalTokenCount: 650,
			},
			wantPrompt: 60, wantCompletion: 550, wantReasoning: 500,
		},
		{
			// Folded accounting (Gemini API as documented): candidates INCLUDES
			// thoughts; total = prompt + candidates.
			name: "thoughts folded into candidates",
			um: geminiapi.UsageMetadata{
				PromptTokenCount: 100, CachedContentTokenCount: 40,
				CandidatesTokenCount: 550, ThoughtsTokenCount: 500, TotalTokenCount: 650,
			},
			wantPrompt: 60, wantCompletion: 550, wantReasoning: 500,
		},
		{
			name: "no thoughts",
			um: geminiapi.UsageMetadata{
				PromptTokenCount: 100, CandidatesTokenCount: 50, TotalTokenCount: 150,
			},
			wantPrompt: 100, wantCompletion: 50, wantReasoning: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := convertUsageMetadata(&tc.um)
			if u.PromptTokens != tc.wantPrompt {
				t.Errorf("PromptTokens = %d, want %d", u.PromptTokens, tc.wantPrompt)
			}
			if u.CompletionTokens != tc.wantCompletion {
				t.Errorf("CompletionTokens = %d, want %d", u.CompletionTokens, tc.wantCompletion)
			}
			if u.ReasoningTokens != tc.wantReasoning {
				t.Errorf("ReasoningTokens = %d, want %d", u.ReasoningTokens, tc.wantReasoning)
			}
			if u.PromptTokens+u.CompletionTokens+u.CacheReadTokens != u.TotalTokens {
				t.Errorf("prompt(%d)+completion(%d)+cache(%d) != total(%d)",
					u.PromptTokens, u.CompletionTokens, u.CacheReadTokens, u.TotalTokens)
			}
		})
	}
}

func TestConvertResponse_ToolCallFinishReason(t *testing.T) {
	// Gemini returns STOP even with function calls; convertResponse must normalize
	// to FinishReasonToolCalls so cross-provider callers keying on it work.
	resp := &geminiapi.GenerateContentResponse{
		Candidates: []geminiapi.Candidate{{
			FinishReason: "STOP",
			Content: &geminiapi.Content{Parts: []geminiapi.Part{{
				FunctionCall: &geminiapi.FunctionCall{Name: "get_weather", Args: map[string]any{"q": "NYC"}},
			}}},
		}},
	}
	got := convertResponse(resp)
	if got.FinishReason != llms.FinishReasonToolCalls {
		t.Errorf("FinishReason = %q, want %q", got.FinishReason, llms.FinishReasonToolCalls)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
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
