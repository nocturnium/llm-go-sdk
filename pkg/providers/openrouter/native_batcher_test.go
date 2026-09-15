package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func userRequest(id, text string, opts ...llms.CallOption) llms.BatchRequest {
	return llms.BatchRequest{ID: id, Messages: []llms.Message{{Role: llms.RoleUser, Content: text}}, Options: opts}
}

// batchServer answers a submit with batch_1 and every read with the given
// completed-batch results payload.
func batchServer(t *testing.T, results string) *Client {
	t.Helper()
	return mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body BatchSubmission
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Endpoint != BatchEndpointChatCompletions || body.Model != DefaultModel {
				t.Errorf("submission %+v", body)
			}
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"id":"batch_1","status":"validating","request_counts":{"total":2,"completed":0,"failed":0}}`)
			return
		}
		fmt.Fprintf(w, `{"id":"batch_1","status":"completed","request_counts":{"total":2,"completed":1,"failed":1},"results":[%s]}`, results)
	})
}

func TestNativeBatcherProcessBatch(t *testing.T) {
	c := batchServer(t, `{"id":"line_1","custom_id":"a","response":{"status_code":200,"body":{"id":"gen-1","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}}},
		{"id":"line_2","custom_id":"b","error":{"code":"rate_limited","message":"upstream refused"}}`)

	var progress [][2]int
	resp, err := NewNativeBatcher(c, WithBatchPollInterval(time.Millisecond)).ProcessBatch(
		context.Background(),
		[]llms.BatchRequest{userRequest("a", "hi"), userRequest("b", "yo")},
		llms.WithProgressCallback(func(completed, total int) { progress = append(progress, [2]int{completed, total}) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.SuccessCount != 1 || resp.FailureCount != 1 {
		t.Fatalf("counts %d/%d", resp.SuccessCount, resp.FailureCount)
	}
	if got := resp.Results["a"]; got.Error != nil || got.Response.Content != "hello" || got.Response.ID != "gen-1" {
		t.Fatalf("result a: %+v", got)
	}
	if got := resp.Results["b"]; got.Response != nil || !errors.Is(got.Error, llms.ErrJobFailed) {
		t.Fatalf("result b: %+v", got)
	}
	if resp.TotalUsage.PromptTokens != 10 || resp.TotalUsage.CompletionTokens != 4 || resp.TotalUsage.TotalTokens != 14 {
		t.Fatalf("usage %+v", resp.TotalUsage)
	}
	if len(progress) == 0 || progress[len(progress)-1] != [2]int{2, 2} {
		t.Fatalf("progress %v", progress)
	}
}

// A request the batch never answered is a failure, not a silent hole.
func TestNativeBatcherMissingResultLine(t *testing.T) {
	c := batchServer(t, `{"id":"line_1","custom_id":"a","response":{"status_code":200,"body":{"choices":[{"message":{"content":"hi"}}]}}}`)
	resp, err := NewNativeBatcher(c, WithBatchPollInterval(time.Millisecond)).ProcessBatch(context.Background(), []llms.BatchRequest{userRequest("a", "hi"), userRequest("b", "yo")})
	if err != nil {
		t.Fatal(err)
	}
	if resp.FailureCount != 1 || !errors.Is(resp.Results["b"].Error, llms.ErrIncompleteResponse) {
		t.Fatalf("results %+v", resp.Results)
	}
}

func TestNativeBatcherLineStatusFailure(t *testing.T) {
	c := batchServer(t, `{"id":"line_1","custom_id":"a","response":{"status_code":500,"body":{}}},{"id":"line_2","custom_id":"b"}`)
	resp, err := NewNativeBatcher(c, WithBatchPollInterval(time.Millisecond)).ProcessBatch(context.Background(), []llms.BatchRequest{userRequest("a", "hi"), userRequest("b", "yo")})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(resp.Results["a"].Error, llms.ErrJobFailed) || !errors.Is(resp.Results["b"].Error, llms.ErrIncompleteResponse) {
		t.Fatalf("results %+v %+v", resp.Results["a"], resp.Results["b"])
	}
}

func TestNativeBatcherTerminalFailure(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"id":"batch_1","status":"validating"}`)
			return
		}
		fmt.Fprint(w, `{"id":"batch_1","status":"failed","error":{"message":"validation failed"}}`)
	})
	_, err := NewNativeBatcher(c, WithBatchPollInterval(time.Millisecond)).ProcessBatch(context.Background(), []llms.BatchRequest{userRequest("a", "hi")})
	if !errors.Is(err, llms.ErrJobFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestNativeBatcherRejectsBeforeSubmitting(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("no request expected") })
	batcher := NewNativeBatcher(c)
	image := llms.BatchRequest{ID: "a", Messages: []llms.Message{{Role: llms.RoleUser, Parts: []llms.ContentPart{{Type: llms.PartTypeImage, Image: &llms.ImageContent{Source: llms.ImageSourceURL, MediaType: llms.MediaTypePNG, Data: "https://example.com/a.png"}}}}}}
	for _, tc := range []struct {
		name     string
		requests []llms.BatchRequest
		options  []llms.BatchOption
		wantErr  error
	}{
		{"empty", nil, nil, llms.ErrBatchEmpty},
		{"over max size", []llms.BatchRequest{userRequest("a", "hi"), userRequest("b", "yo")}, []llms.BatchOption{llms.WithMaxBatchSize(1)}, llms.ErrBatchTooLarge},
		{"duplicate id", []llms.BatchRequest{userRequest("a", "hi"), userRequest("a", "yo")}, nil, llms.ErrInvalidParameters},
		{"blank id", []llms.BatchRequest{userRequest("", "hi")}, nil, llms.ErrInvalidParameters},
		{"mixed models", []llms.BatchRequest{userRequest("a", "hi", llms.WithModel("x/one")), userRequest("b", "yo", llms.WithModel("x/two"))}, nil, llms.ErrInvalidParameters},
		{"image part", []llms.BatchRequest{image}, nil, llms.ErrInvalidParameters},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := batcher.ProcessBatch(context.Background(), tc.requests, tc.options...); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// Per-request call options reach the submitted body, and an explicit model on
// every request replaces the client default as the batch model.
func TestNativeBatcherRequestBodies(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Model    string `json:"model"`
				Requests []struct {
					CustomID string         `json:"custom_id"`
					Body     map[string]any `json:"body"`
				} `json:"requests"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Model != "x/one" || len(body.Requests) != 1 {
				t.Errorf("submission %+v", body)
			}
			item := body.Requests[0].Body
			if item["model"] != "x/one" || item["temperature"] != 0.25 || item["stream"] == true {
				t.Errorf("request body %v", item)
			}
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"id":"batch_1","status":"validating"}`)
			return
		}
		fmt.Fprint(w, `{"id":"batch_1","status":"completed","results":[{"custom_id":"a","response":{"status_code":200,"body":{"choices":[{"message":{"content":"ok"}}]}}}]}`)
	})
	resp, err := NewNativeBatcher(c, WithBatchPollInterval(time.Millisecond)).ProcessBatch(context.Background(), []llms.BatchRequest{
		userRequest("a", "hi", llms.WithModel("x/one"), llms.WithTemperature(0.25)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SuccessCount != 1 || resp.Results["a"].Response.Content != "ok" {
		t.Fatalf("results %+v", resp.Results)
	}
}
