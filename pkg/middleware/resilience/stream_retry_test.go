package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// flakyStreamLLM fails the first failCount Stream calls with streamErr, then
// succeeds. It models a transient connect failure (429/5xx) that a retry should
// recover from before any chunk is delivered.
type flakyStreamLLM struct {
	failCount int32
	streamErr error
	calls     int32
}

func (m *flakyStreamLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func (m *flakyStreamLLM) GenerateContent(context.Context, []llms.Message, ...llms.CallOption) (*llms.Response, error) {
	return &llms.Response{}, nil
}

func (m *flakyStreamLLM) Stream(context.Context, []llms.Message, ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	if atomic.AddInt32(&m.calls, 1) <= m.failCount {
		return nil, m.streamErr
	}
	ch := make(chan llms.StreamChunk, 2)
	ch <- llms.StreamChunk{Content: "ok"}
	ch <- llms.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (m *flakyStreamLLM) Provider() llms.Provider { return "flaky" }
func (m *flakyStreamLLM) Model() string           { return "flaky-model" }

// A transient failure to open the stream (before any chunk is emitted) must be
// retried, matching GenerateContent; the caller should receive the successful
// stream rather than the first 503.
func TestResilientClient_StreamRetriesInitialTransientFailure(t *testing.T) {
	mock := &flakyStreamLLM{failCount: 1, streamErr: &llms.APIError{StatusCode: 503}}
	rc := NewResilientClient(mock,
		WithMaxRetries(2),
		WithRetryDelay(time.Millisecond),
	)

	ch, err := rc.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Stream returned the transient error instead of retrying: %v", err)
	}

	var content string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected error chunk: %v", chunk.Error)
		}
		content += chunk.Content
	}
	if content != "ok" {
		t.Errorf("got content %q, want ok", content)
	}
	if got := atomic.LoadInt32(&mock.calls); got != 2 {
		t.Errorf("got %d Stream calls, want 2 (one failure, one retry)", got)
	}
}

// A non-retryable error (e.g. a terminal 400) must not be retried; the caller
// sees it immediately.
func TestResilientClient_StreamDoesNotRetryTerminalError(t *testing.T) {
	mock := &flakyStreamLLM{failCount: 5, streamErr: &llms.APIError{StatusCode: 400}}
	rc := NewResilientClient(mock,
		WithMaxRetries(3),
		WithRetryDelay(time.Millisecond),
	)

	_, err := rc.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected the terminal error to be returned")
	}
	var apiErr *llms.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Fatalf("got %v, want the original 400 APIError", err)
	}
	if got := atomic.LoadInt32(&mock.calls); got != 1 {
		t.Errorf("got %d Stream calls, want 1 (no retry for a terminal error)", got)
	}
}

// A retryable error that never recovers is returned after the attempts are
// exhausted, and the count reflects the configured retry budget.
func TestResilientClient_StreamExhaustsRetries(t *testing.T) {
	mock := &flakyStreamLLM{failCount: 10, streamErr: &llms.APIError{StatusCode: 503}}
	rc := NewResilientClient(mock,
		WithMaxRetries(2),
		WithRetryDelay(time.Millisecond),
	)

	_, err := rc.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected the persistent error to be returned")
	}
	if got := atomic.LoadInt32(&mock.calls); got != 3 {
		t.Errorf("got %d Stream calls, want 3 (initial + 2 retries)", got)
	}
}
