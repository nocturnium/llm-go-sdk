package observability

import (
	"context"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// stampedLLM streams chunks stamped the way a provider's StreamSender stamps
// them.
type stampedLLM struct{ mockLangfuseLLM }

func (l *stampedLLM) Stream(ctx context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	out := make(chan llms.StreamChunk, 2)
	s := llms.NewStreamSender(ctx, out, 0)
	s.SetIdentity(llms.ProviderAnthropic, "claude")
	s.Send(llms.StreamChunk{Reasoning: &llms.ReasoningContent{Content: "r", Signature: "sig"}})
	s.SendFinal(llms.StreamChunk{
		ToolCalls:   []llms.ToolCall{{ID: "c", Signature: "cs"}},
		Adjustments: []string{"anthropic.thinking_suspended"},
	})
	close(out)
	return out, nil
}

// TestStreamWrappers_PassProvenanceThrough checks that every observability
// wrapper forwards the answering client's stamps unchanged. A wrapper that set
// its own identity, or rebuilt chunks, would relabel reasoning and send it back
// to a provider that rejects it.
func TestStreamWrappers_PassProvenanceThrough(t *testing.T) {
	base := &stampedLLM{}
	otelMW, err := NewOTelMiddleware(base)
	if err != nil {
		t.Fatal(err)
	}
	metricsMW, err := NewMetricsMiddleware(base)
	if err != nil {
		t.Fatal(err)
	}
	langfuseMW, err := NewLangfuseOTelMiddleware(base)
	if err != nil {
		t.Fatal(err)
	}
	wrappers := map[string]llms.LLM{
		"otel":     otelMW,
		"metrics":  metricsMW,
		"langfuse": langfuseMW,
		"logging":  NewLoggingMiddleware(base, NewJSONLogger(func([]byte) error { return nil })),
	}
	for name, wrapper := range wrappers {
		stream, err := wrapper.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		res, err := llms.CollectStream(stream)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Provider != llms.ProviderAnthropic || res.Model != "claude" ||
			len(res.Adjustments) != 1 || res.Reasoning == nil || res.Reasoning.Provider != llms.ProviderAnthropic ||
			len(res.ToolCalls) != 1 || res.ToolCalls[0].SignatureProvider != llms.ProviderAnthropic {
			t.Errorf("%s changed the stamps: %+v reasoning %+v", name, res, res.Reasoning)
		}
	}
}
