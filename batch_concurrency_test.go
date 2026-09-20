package llms

import (
	"context"
	"testing"
	"time"
)

// TestConcurrentBatcher_NonPositiveConcurrencyDoesNotHang pins that a
// non-positive MaxConcurrency falls back to the default. An unbuffered
// semaphore channel could never be acquired, so the batch hung forever.
func TestConcurrentBatcher_NonPositiveConcurrencyDoesNotHang(t *testing.T) {
	batcher := NewConcurrentBatcher(&mockBatchLLM{})
	requests := []BatchRequest{NewBatchRequestFromPrompt("req-1", "one")}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := batcher.ProcessBatch(context.Background(), requests, func(o *BatchOptions) {
			o.MaxConcurrency = 0
		}); err != nil {
			t.Errorf("ProcessBatch: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ProcessBatch hung with MaxConcurrency 0")
	}
}
