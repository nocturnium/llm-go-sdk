package resilience

import (
	"context"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// usagelessLLM answers without reporting token usage, as several
// OpenAI-compatible servers do.
type usagelessLLM struct{ mockRateLimitLLM }

func (l *usagelessLLM) GenerateContent(context.Context, []llms.Message, ...llms.CallOption) (*llms.Response, error) {
	return &llms.Response{Content: "ok"}, nil
}

// TestRateLimitedClient_NoUsageKeepsTheReservation pins that a response
// carrying no usage does not refund the estimate. Recording zero read as a
// gross overestimate, so a provider that never reports usage paced at zero
// tokens per request.
func TestRateLimitedClient_NoUsageKeepsTheReservation(t *testing.T) {
	limiter := NewRateLimiter(
		WithRequestsPerMinute(1000),
		WithRequestBurst(1000),
		WithTokensPerMinute(10000),
		WithTokenBurst(10000),
		WithTokenEstimate(500),
		WithBlocking(false),
	)
	client := NewRateLimitedClientWithLimiter(&usagelessLLM{}, limiter)

	before := limiter.TokensRemaining()
	if _, err := client.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}}); err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	after := limiter.TokensRemaining()

	if after >= before {
		t.Errorf("tokens went from %d to %d: the estimate was refunded", before, after)
	}
}
