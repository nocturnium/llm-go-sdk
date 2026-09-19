package llms

import (
	"context"
	"errors"
	"testing"
	"time"
)

// nilResponseLLM returns (nil, nil), which the LLM contract forbids but a
// buggy provider or mock can still do.
type nilResponseLLM struct{ mockBatchLLM }

func (l *nilResponseLLM) GenerateContent(context.Context, []Message, ...CallOption) (*Response, error) {
	return nil, nil
}

// TestConcurrentBatcher_NilResponseDoesNotDeadlock pins that a provider
// returning no response and no error is recorded as a failure. It used to
// panic inside recordSuccess while holding resultsMu, and the recover handler
// then blocked on the same mutex, so ProcessBatch never returned.
func TestConcurrentBatcher_NilResponseDoesNotDeadlock(t *testing.T) {
	batcher := NewConcurrentBatcher(&nilResponseLLM{})
	requests := []BatchRequest{
		NewBatchRequestFromPrompt("req-1", "one"),
		NewBatchRequestFromPrompt("req-2", "two"),
	}

	done := make(chan *BatchResponse, 1)
	go func() {
		resp, err := batcher.ProcessBatch(context.Background(), requests)
		if err != nil {
			t.Errorf("ProcessBatch returned %v", err)
		}
		done <- resp
	}()

	select {
	case resp := <-done:
		if resp.FailureCount != 2 {
			t.Errorf("FailureCount = %d, want 2", resp.FailureCount)
		}
		for _, id := range []string{"req-1", "req-2"} {
			result := resp.Results[id]
			if result == nil || !errors.Is(result.Error, ErrIncompleteResponse) {
				t.Errorf("%s error = %v, want ErrIncompleteResponse", id, result)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ProcessBatch did not return: the nil response deadlocked the batch")
	}
}
