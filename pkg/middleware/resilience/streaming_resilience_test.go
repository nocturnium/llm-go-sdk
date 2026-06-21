package resilience

import (
	"context"
	"sync/atomic"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

// streamMock is an LLM whose Stream emits a fixed set of chunks (used to simulate
// streams that fail via a terminal error chunk rather than a synchronous error).
type streamMock struct {
	provider     llms.Provider
	model        string
	streamChunks []llms.StreamChunk
	streamErr    error
	calls        int32
}

func (m *streamMock) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func (m *streamMock) GenerateContent(context.Context, []llms.Message, ...llms.CallOption) (*llms.Response, error) {
	return &llms.Response{}, nil
}

func (m *streamMock) Stream(context.Context, []llms.Message, ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	atomic.AddInt32(&m.calls, 1)
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan llms.StreamChunk, len(m.streamChunks))
	for _, c := range m.streamChunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *streamMock) Provider() llms.Provider { return m.provider }
func (m *streamMock) Model() string           { return m.model }

// TestResilientClient_StreamRecordsFailure verifies a stream that fails via a
// terminal error chunk trips the circuit breaker (previously it was recorded as a
// false success).
func TestResilientClient_StreamRecordsFailure(t *testing.T) {
	cb := NewCircuitBreaker(WithMaxFailures(1))
	mock := &streamMock{streamChunks: []llms.StreamChunk{{Error: &llms.APIError{StatusCode: 503}}}}
	rc := NewResilientClient(mock, WithCircuitBreaker(cb), WithMaxRetries(0))

	ch, err := rc.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch { //nolint:revive // drain to completion so the finalizer runs
	}

	if cb.State() != CircuitOpen {
		t.Errorf("expected breaker OPEN after a failed stream, got %v", cb.State())
	}
}

// TestResilientClient_StreamRecordsSuccess verifies a clean stream does not trip
// the breaker.
func TestResilientClient_StreamRecordsSuccess(t *testing.T) {
	cb := NewCircuitBreaker(WithMaxFailures(1))
	mock := &streamMock{streamChunks: []llms.StreamChunk{{Content: "ok"}, {Done: true}}}
	rc := NewResilientClient(mock, WithCircuitBreaker(cb), WithMaxRetries(0))

	ch, err := rc.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch { //nolint:revive // drain
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected breaker CLOSED after a clean stream, got %v", cb.State())
	}
}

// TestFallbackChain_StreamFailsOverOnError verifies the chain advances to the next
// client when a stream errors before emitting any content (previously the failing
// client was marked healthy and no failover occurred).
func TestFallbackChain_StreamFailsOverOnError(t *testing.T) {
	bad := &streamMock{provider: "p0", streamChunks: []llms.StreamChunk{{Error: &llms.APIError{StatusCode: 503}}}}
	good := &streamMock{provider: "p1", streamChunks: []llms.StreamChunk{{Content: "hello"}, {Done: true}}}
	fc := NewFallbackChain([]llms.LLM{bad, good})

	ch, err := fc.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var content string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error chunk after failover: %v", chunk.Error)
		}
		content += chunk.Content
	}

	if content != "hello" {
		t.Errorf("expected failover to the healthy client (content=hello), got %q", content)
	}
	if atomic.LoadInt32(&good.calls) != 1 {
		t.Errorf("expected the healthy client to be invoked once, got %d", good.calls)
	}
}
