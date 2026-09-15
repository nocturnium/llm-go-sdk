//go:build integration

package openrouter

import (
	"context"
	"os"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// TestLiveOpenRouter_ServiceTier asks for flex capacity and reports the tier
// that served the request. A model with no flex endpoint routes normally at
// standard rates, so an empty or "default" tier is a pass, not a failure.
func TestLiveOpenRouter_ServiceTier(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	resp, err := c.GenerateContent(ctx, []llms.Message{{Role: llms.RoleUser, Content: "Reply with hello."}},
		llms.WithMaxTokens(32), WithServiceTier(TierFlex), llms.WithPricingMode(llms.PricingModeFlex))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content == "" {
		t.Fatal("empty completion")
	}
	t.Logf("served tier: %q", resp.ServiceTier)
}

// TestLiveOpenRouter_Batch submits a one-request batch and waits for it. The
// completion window is 24 hours, so the wait is bounded by LLM_BATCH_TIMEOUT
// (default 15 minutes) and a batch still running at the deadline is skipped
// rather than failed; the batch id is logged so it can be read or deleted later.
func TestLiveOpenRouter_Batch(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveBatchTimeout(t))
	defer cancel()

	batcher := NewNativeBatcher(c, WithBatchPollInterval(10*time.Second))
	resp, err := batcher.ProcessBatch(ctx, []llms.BatchRequest{
		llms.NewBatchRequest("live-1", []llms.Message{{Role: llms.RoleUser, Content: "Reply with the word hello."}}, llms.WithMaxTokens(32)),
	})
	if err != nil {
		if ctx.Err() != nil {
			t.Skipf("batch still running at the deadline: %v", err)
		}
		t.Fatal(err)
	}
	if resp.SuccessCount != 1 {
		t.Fatalf("results: %+v", resp.Results["live-1"])
	}
	if resp.Results["live-1"].Response.Content == "" {
		t.Fatal("empty batch completion")
	}
	t.Logf("batch usage: %+v in %s", resp.TotalUsage, resp.Duration)
}

// TestLiveOpenRouter_BatchLifecycle exercises submit, list, poll and delete
// without waiting for completion, then removes the batch it created.
func TestLiveOpenRouter_BatchLifecycle(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	created, err := c.SubmitBatch(ctx, BatchSubmission{
		Endpoint: BatchEndpointChatCompletions,
		Model:    DefaultModel,
		Requests: []BatchItem{{CustomID: "lifecycle-1", Body: map[string]any{
			"messages":   []map[string]string{{"role": "user", "content": "Reply with hello."}},
			"max_tokens": 32,
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("submitted %s (%s)", created.ID, created.Status)

	// A 202 is not yet a queryable batch: reads and listings 404 for a few
	// seconds after submission.
	var batch *Batch
	for deadline := time.Now().Add(90 * time.Second); ; {
		got, err := c.GetBatch(ctx, created.ID)
		if err == nil {
			batch = got
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
	}
	t.Logf("polled %s: %s %+v", batch.ID, batch.Status, batch.RequestCounts)

	list, err := c.ListBatches(ctx, ListBatchesParams{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, listed := range list.Data {
		if listed.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("submitted batch %s missing from the newest %d", created.ID, len(list.Data))
	}

	// Only a terminal batch can be deleted; an in-flight one returns 409.
	if !batch.Status.IsTerminal() {
		t.Logf("leaving %s in flight; delete it later", batch.ID)
		return
	}
	deletion, err := c.DeleteBatch(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deletion.Deletion.Openrouter != "deleted" {
		t.Fatalf("deletion: %+v", deletion)
	}
}

// liveBatchTimeout bounds the batch wait; override with LLM_BATCH_TIMEOUT.
func liveBatchTimeout(t *testing.T) time.Duration {
	t.Helper()
	value := os.Getenv("LLM_BATCH_TIMEOUT")
	if value == "" {
		return 15 * time.Minute
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		t.Fatalf("LLM_BATCH_TIMEOUT: %v", err)
	}
	return d
}
