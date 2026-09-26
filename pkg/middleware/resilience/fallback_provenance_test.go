package resilience

import (
	"context"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// stampedStreamLLM streams one reasoning chunk and a final chunk, stamped the
// way a provider's StreamSender stamps them.
type stampedStreamLLM struct{ mockFallbackLLM }

func (m *stampedStreamLLM) Stream(ctx context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	ch := make(chan llms.StreamChunk, 2)
	s := llms.NewStreamSender(ctx, ch, 0)
	s.SetIdentity(m.provider, m.model)
	s.Send(llms.StreamChunk{Reasoning: &llms.ReasoningContent{Content: "r", Signature: "sig"}})
	s.SendFinal(llms.StreamChunk{ToolCalls: []llms.ToolCall{{ID: "c", Signature: "cs"}}})
	close(ch)
	return ch, nil
}

// TestFallbackChain_PreservesServingIdentity checks that a fallback chain reports
// the entry that answered, not entry 0, on both paths. The stamps are what let
// each provider replay only its own reasoning when a later turn is served by a
// different entry.
func TestFallbackChain_PreservesServingIdentity(t *testing.T) {
	primary := &mockFallbackLLM{provider: llms.ProviderOpenAI, model: "gpt", genErr: &llms.APIError{StatusCode: 503}, streamErr: &llms.APIError{StatusCode: 503}}

	t.Run("GenerateContent", func(t *testing.T) {
		served := &llms.Response{Content: "ok"}
		llms.StampResponse(served, llms.ProviderAnthropic, "claude")
		secondary := &mockFallbackLLM{provider: llms.ProviderAnthropic, model: "claude", genResp: served}
		fc := NewFallbackChain([]llms.LLM{primary, secondary})

		resp, err := fc.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Provider != llms.ProviderAnthropic || resp.Model != "claude" {
			t.Errorf("served identity = %q/%q, want anthropic/claude", resp.Provider, resp.Model)
		}
		if fc.Provider() != llms.ProviderOpenAI {
			t.Errorf("chain Provider() = %q; the chain still names entry 0", fc.Provider())
		}
	})

	t.Run("Stream", func(t *testing.T) {
		secondary := &stampedStreamLLM{mockFallbackLLM{provider: llms.ProviderGemini, model: "gemini"}}
		fc := NewFallbackChain([]llms.LLM{primary, secondary})

		stream, err := fc.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		res, err := llms.CollectStream(stream)
		if err != nil {
			t.Fatal(err)
		}
		if res.Provider != llms.ProviderGemini || res.Model != "gemini" {
			t.Errorf("served identity = %q/%q, want gemini/gemini", res.Provider, res.Model)
		}
		if res.Reasoning == nil || res.Reasoning.Provider != llms.ProviderGemini {
			t.Errorf("reasoning stamp = %+v", res.Reasoning)
		}
		if len(res.ToolCalls) != 1 || res.ToolCalls[0].SignatureProvider != llms.ProviderGemini {
			t.Errorf("tool call stamp = %+v", res.ToolCalls)
		}
	})
}
