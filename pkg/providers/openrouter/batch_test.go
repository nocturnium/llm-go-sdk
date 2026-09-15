package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestBetaBaseURL(t *testing.T) {
	for _, tc := range []struct{ base, want string }{
		{"https://openrouter.ai/api/v1", "https://openrouter.ai/api/beta"},
		{"https://openrouter.ai/api/v1/", "https://openrouter.ai/api/beta"},
		{"https://proxy.example.com/openrouter", "https://proxy.example.com/openrouter/beta"},
		{"https://proxy.example.com", "https://proxy.example.com/beta"},
	} {
		u, err := url.Parse(tc.base)
		if err != nil {
			t.Fatal(err)
		}
		if got := betaBaseURL(u).String(); got != tc.want {
			t.Errorf("betaBaseURL(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}

// OpenRouter stream-parses the submission and rejects a body whose requests
// array precedes endpoint and model, so the field order is part of the contract.
func TestBatchSubmissionFieldOrder(t *testing.T) {
	data, err := json.Marshal(BatchSubmission{Endpoint: BatchEndpointChatCompletions, Model: "openai/gpt-4o", Requests: []BatchItem{{CustomID: "a", Body: map[string]any{}}}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, model, requests := strings.Index(string(data), `"endpoint"`), strings.Index(string(data), `"model"`), strings.Index(string(data), `"requests"`)
	if endpoint < 0 || model < 0 || requests < 0 || endpoint > requests || model > requests {
		t.Fatalf("requests must serialize last: %s", data)
	}
}

func TestBatchStatusIsTerminal(t *testing.T) {
	for status, want := range map[BatchStatus]bool{
		BatchValidating: false, BatchInProgress: false, BatchFinalizing: false, BatchCancelling: false,
		BatchCompleted: true, BatchFailed: true, BatchExpired: true, BatchCancelled: true,
	} {
		if got := status.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %v", status, got)
		}
	}
}

func TestSubmitBatchValidation(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("no request expected") })
	item := BatchItem{CustomID: "a", Body: map[string]any{}}
	for _, tc := range []struct {
		name       string
		submission BatchSubmission
		wantErr    error
	}{
		{"no endpoint", BatchSubmission{Model: "m", Requests: []BatchItem{item}}, llms.ErrInvalidParameters},
		{"no model", BatchSubmission{Endpoint: BatchEndpointChatCompletions, Requests: []BatchItem{item}}, llms.ErrInvalidParameters},
		{"no requests", BatchSubmission{Endpoint: BatchEndpointChatCompletions, Model: "m"}, llms.ErrBatchEmpty},
		{"blank custom_id", BatchSubmission{Endpoint: BatchEndpointChatCompletions, Model: "m", Requests: []BatchItem{{Body: map[string]any{}}}}, llms.ErrInvalidParameters},
		{"duplicate custom_id", BatchSubmission{Endpoint: BatchEndpointChatCompletions, Model: "m", Requests: []BatchItem{item, item}}, llms.ErrInvalidParameters},
		{"no body", BatchSubmission{Endpoint: BatchEndpointChatCompletions, Model: "m", Requests: []BatchItem{{CustomID: "a"}}}, llms.ErrInvalidParameters},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.SubmitBatch(context.Background(), tc.submission); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSubmitBatch(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/beta/batches" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("route %s %s", r.Method, r.URL.Path)
		}
		var body BatchSubmission
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Endpoint != BatchEndpointChatCompletions || body.Model != "openai/gpt-4o" || len(body.Requests) != 1 {
			t.Errorf("body %+v", body)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"id":"batch_1","object":"batch","status":"validating","completion_window":"24h","created_at":1782097200,"request_counts":{"total":1,"completed":0,"failed":0},"extra":"kept"}`)
	})
	batch, err := c.SubmitBatch(context.Background(), BatchSubmission{
		Endpoint: BatchEndpointChatCompletions,
		Model:    "openai/gpt-4o",
		Requests: []BatchItem{{CustomID: "req-1", Body: map[string]any{"messages": []any{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if batch.ID != "batch_1" || batch.Status != BatchValidating || batch.RequestCounts.Total != 1 || batch.CreatedAt != 1782097200 {
		t.Fatalf("batch %+v", batch)
	}
	if !strings.Contains(string(batch.Raw), `"extra":"kept"`) {
		t.Fatalf("raw payload dropped: %s", batch.Raw)
	}
}

func TestGetBatchAndIDValidation(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/beta/batches/batch_1" {
			t.Errorf("route %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"id":"batch_1","status":"completed","finalized_at":1782097300,"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"cost":0.5,"is_byok":true},"results":[{"id":"batch_req_1","custom_id":"req-1","response":{"status_code":200,"body":{"choices":[]}}}]}`)
	})
	batch, err := c.GetBatch(context.Background(), "batch_1")
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != BatchCompleted || batch.Usage == nil || batch.Usage.Cost == nil || *batch.Usage.Cost != 0.5 || !batch.Usage.IsByok {
		t.Fatalf("batch %+v", batch)
	}
	if batch.FinalizedAt == nil || *batch.FinalizedAt != 1782097300 || len(batch.Results) != 1 || batch.Results[0].CustomID != "req-1" {
		t.Fatalf("batch %+v", batch)
	}
	for _, id := range []string{"", " ", ".", "..", "a/b", "a?b", "a%2Fb"} {
		if _, err := c.GetBatch(context.Background(), id); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("GetBatch(%q) err = %v", id, err)
		}
		if _, err := c.DeleteBatch(context.Background(), id); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("DeleteBatch(%q) err = %v", id, err)
		}
	}
}

func TestListBatches(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/beta/batches" || q.Get("limit") != "50" || q.Get("after") != "batch_9" || len(q["status"]) != 2 || q.Get("created_after") != "1782097200" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"object":"list","data":[{"id":"batch_1","status":"completed"}],"first_id":"batch_1","last_id":"batch_1","has_more":true}`)
	})
	list, err := c.ListBatches(context.Background(), ListBatchesParams{
		Limit:        50,
		After:        "batch_9",
		Status:       []BatchStatus{BatchCompleted, BatchFailed},
		CreatedAfter: time.Unix(1782097200, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.LastID != "batch_1" || !list.HasMore {
		t.Fatalf("list %+v", list)
	}
}

func TestListBatchesValidation(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("no request expected") })
	for _, params := range []ListBatchesParams{
		{Limit: 101},
		{Limit: -1},
		{CreatedAfter: time.Unix(200, 0), CreatedBefore: time.Unix(100, 0)},
	} {
		if _, err := c.ListBatches(context.Background(), params); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("ListBatches(%+v) err = %v", params, err)
		}
	}
}

func TestDeleteBatch(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/beta/batches/batch_1" {
			t.Errorf("route %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"id":"batch_1","object":"batch","deletion":{"openrouter":"deleted","upstream":{"provider":"Anthropic","status":"deleted"}}}`)
	})
	deletion, err := c.DeleteBatch(context.Background(), "batch_1")
	if err != nil {
		t.Fatal(err)
	}
	if deletion.Deletion.Openrouter != "deleted" || deletion.Deletion.Upstream == nil || deletion.Deletion.Upstream.Provider != "Anthropic" {
		t.Fatalf("deletion %+v", deletion)
	}
}

func TestWaitBatch(t *testing.T) {
	var polls atomic.Int32
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		status := BatchInProgress
		if polls.Add(1) >= 3 {
			status = BatchCompleted
		}
		fmt.Fprintf(w, `{"id":"batch_1","status":%q,"request_counts":{"total":2,"completed":1,"failed":0}}`, status)
	})
	var observed int
	batch, err := c.WaitBatch(context.Background(), "batch_1", WithPollInterval(time.Millisecond), WithPollCallback(func(*Batch) { observed++ }))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != BatchCompleted || polls.Load() != 3 || observed != 3 {
		t.Fatalf("status %s polls %d observed %d", batch.Status, polls.Load(), observed)
	}
}

func TestWaitBatchContextCancelled(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"id":"batch_1","status":"in_progress"}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.WaitBatch(ctx, "batch_1", WithPollInterval(10*time.Millisecond)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

// A failed, expired or canceled batch is returned rather than raised: only
// transport and context failures are errors.
func TestWaitBatchTerminalFailure(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"id":"batch_1","status":"expired","error":{"code":"expired","message":"window elapsed"}}`)
	})
	batch, err := c.WaitBatch(context.Background(), "batch_1", WithPollInterval(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != BatchExpired || batch.Error == nil || batch.Error.Message != "window elapsed" {
		t.Fatalf("batch %+v", batch)
	}
}

// A batch is not readable the instant submission returns, so WaitBatch keeps
// polling through an early 404 instead of reporting a missing batch.
func TestWaitBatchToleratesEarlyNotFound(t *testing.T) {
	var reads atomic.Int32
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if reads.Add(1) <= 2 {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"message":"Batch job not found.","code":404}}`)
			return
		}
		fmt.Fprint(w, `{"id":"batch_1","status":"completed"}`)
	})
	batch, err := c.WaitBatch(context.Background(), "batch_1", WithPollInterval(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != BatchCompleted || reads.Load() != 3 {
		t.Fatalf("status %s after %d reads", batch.Status, reads.Load())
	}
}

// A 404 that never resolves ends the wait rather than looping forever; here the
// context deadline is what bounds it, since it is shorter than the grace window.
func TestWaitBatchPersistentNotFound(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"message":"Batch job not found.","code":404}}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.WaitBatch(ctx, "batch_missing", WithPollInterval(time.Millisecond)); err == nil {
		t.Fatal("expected an error")
	}
}
