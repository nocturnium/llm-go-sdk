package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// stallingLLM hands back a channel immediately and then never sends, which is
// what a provider stalled on time to first byte looks like.
type stallingLLM struct{ mockFallbackLLM }

func (l *stallingLLM) Stream(context.Context, []llms.Message, ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	return make(chan llms.StreamChunk), nil
}

// TestFallbackChain_StreamHonorsContextOnFirstChunk pins that waiting for the
// first chunk is bounded by the caller's context. The receive used to be
// unconditional, so a stalled provider hung Stream despite cancellation.
func TestFallbackChain_StreamHonorsContextOnFirstChunk(t *testing.T) {
	chain := NewFallbackChain([]llms.LLM{&stallingLLM{}})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := chain.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stream ignored the context while waiting for the first chunk")
	}
}
