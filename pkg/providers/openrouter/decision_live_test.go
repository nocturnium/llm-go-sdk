//go:build integration

package openrouter

import (
	"context"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// TestLiveOpenRouter_Decide makes one typesafe/jev-1.14 call (about $0.00002).
func TestLiveOpenRouter_Decide(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	resp, err := c.Decide(ctx, llms.DecisionRequest{
		State: "Help! My payouts have been failing for 3 days.",
		Questions: map[string]llms.DecisionQuestion{
			"urgent": {Kind: llms.DecisionKindNoul, Instructions: "Does this convey urgency?"},
			"dept":   {Kind: llms.DecisionKindChoice, Choices: []llms.DecisionChoice{{Label: "billing", Guidance: "payments"}, {Label: "tech", Guidance: "bugs"}}},
			"anger":  {Kind: llms.DecisionKindScore, Levels: []string{"calm", "annoyed", "furious"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p := resp.Answers["urgent"].Probability; p == nil || *p < 0 || *p > 1 {
		t.Fatalf("noul probability %v", p)
	}
	if resp.Answers["dept"].Choice == "" || resp.Answers["anger"].Score == nil {
		t.Fatalf("answers %+v", resp.Answers)
	}
	if resp.Usage.Cost == nil || resp.Usage.PromptTokens == 0 {
		t.Fatalf("usage %+v", resp.Usage)
	}
	for name, a := range resp.Answers {
		if a.Kind == "" {
			t.Errorf("%s: unmodelled answer type, Raw=%s", name, a.Raw)
		}
	}
	t.Logf("model=%s cost=$%.8f", resp.Model, *resp.Usage.Cost)
}
