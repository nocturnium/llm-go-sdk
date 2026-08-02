package resilience

import (
	"context"
	"sync"
	"syscall"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// gatedLLM blocks inside GenerateContent until released, letting a test
// interleave a chain mutation at a precise point while a call is in flight.
type gatedLLM struct {
	started   chan struct{}
	release   chan struct{}
	err       error
	closeOnce sync.Once
}

func (g *gatedLLM) signalAndWait() {
	if g.started != nil {
		g.closeOnce.Do(func() { close(g.started) })
	}
	if g.release != nil {
		<-g.release
	}
}

func (g *gatedLLM) Call(ctx context.Context, _ string, _ ...llms.CallOption) (string, error) {
	_, err := g.GenerateContent(ctx, nil)
	return "", err
}

func (g *gatedLLM) GenerateContent(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (*llms.Response, error) {
	g.signalAndWait()
	if g.err != nil {
		return nil, g.err
	}
	return &llms.Response{Content: "gated-ok"}, nil
}

func (g *gatedLLM) Stream(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	g.signalAndWait()
	if g.err != nil {
		return nil, g.err
	}
	ch := make(chan llms.StreamChunk, 1)
	ch <- llms.StreamChunk{Content: "gated-ok"}
	close(ch)
	return ch, nil
}

func (g *gatedLLM) Provider() llms.Provider { return "gated" }
func (g *gatedLLM) Model() string           { return "gated" }

// TestFallbackChain_RemoveDuringCallDoesNotMisattributeHealth is the core
// stable-id guarantee: if a client is removed from the chain while a call is
// mid-flight through it, that client's subsequent failure must be recorded
// against the removed client (a no-op) — never against whichever survivor now
// occupies its old slice position.
//
// Under the prior positional design, marking used the loop index into a slice
// that RemoveClient had shifted, so the removed primary's failure landed on the
// survivor and left it wrongly unhealthy. Health is now keyed by stable id, so
// the survivor is untouched.
func TestFallbackChain_RemoveDuringCallDoesNotMisattributeHealth(t *testing.T) {
	// primary blocks mid-call then fails transiently; survivor succeeds.
	primary := &gatedLLM{
		started: make(chan struct{}),
		release: make(chan struct{}),
		err:     syscall.ECONNREFUSED, // both provider-unhealthy AND fall-through
	}
	survivor := &mockFallbackLLM{provider: "survivor", callResp: "ok"}

	fc := NewFallbackChain([]llms.LLM{primary, survivor})

	var (
		resp    *llms.Response
		callErr error
		done    = make(chan struct{})
	)
	go func() {
		defer close(done)
		resp, callErr = fc.GenerateContent(context.Background(), nil)
	}()

	// Wait until the call is inside primary.GenerateContent — the per-call
	// snapshot has been taken and released — then remove the primary (index 0).
	<-primary.started
	if !fc.RemoveClient(0) {
		t.Fatal("RemoveClient(0) should succeed")
	}
	// Release the primary so it fails; the chain then records health.
	close(primary.release)
	<-done

	if callErr != nil {
		t.Fatalf("call should have failed over to the survivor and succeeded, got err %v", callErr)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("expected survivor's response, got %+v", resp)
	}

	// The survivor is now the only client (index 0). It must be healthy: the
	// removed primary's transient failure must not have been charged to it.
	if !fc.IsClientHealthy(0) {
		t.Fatal("survivor was wrongly marked unhealthy by the removed primary's failure (positional misattribution)")
	}
}

// TestFallbackChain_HealthFollowsClientAcrossRemoval verifies (non-concurrently)
// that a client's health travels with the client, not its index: removing an
// earlier client leaves a later client's unhealthy state intact.
func TestFallbackChain_HealthFollowsClientAcrossRemoval(t *testing.T) {
	a := &mockFallbackLLM{provider: "a"}
	b := &mockFallbackLLM{provider: "b"}
	c := &mockFallbackLLM{provider: "c"}
	fc := NewFallbackChain([]llms.LLM{a, b, c})

	// Mark c (index 2) unhealthy, then remove a (index 0).
	fc.SetClientHealthy(2, false)
	if !fc.RemoveClient(0) {
		t.Fatal("RemoveClient(0) should succeed")
	}

	// Chain is now [b, c]; c shifted to index 1 but keeps its unhealthy state.
	if !fc.IsClientHealthy(0) {
		t.Error("b (index 0) should be healthy")
	}
	if fc.IsClientHealthy(1) {
		t.Error("c (index 1) should remain unhealthy after the earlier client was removed")
	}
}
