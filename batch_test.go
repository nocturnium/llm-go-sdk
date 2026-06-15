package llms

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testHelloWorld = "Hello, world!"
)

func TestApplyBatchOptions_Defaults(t *testing.T) {
	opts := ApplyBatchOptions()

	if opts.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency = %d, want 5", opts.MaxConcurrency)
	}
	if opts.RequestTimeout != 60*time.Second {
		t.Errorf("RequestTimeout = %v, want 60s", opts.RequestTimeout)
	}
	if !opts.ContinueOnError {
		t.Error("ContinueOnError should be true by default")
	}
}

func TestApplyBatchOptions_Custom(t *testing.T) {
	progressCalled := false
	opts := ApplyBatchOptions(
		WithMaxConcurrency(10),
		WithRequestTimeout(30*time.Second),
		WithContinueOnError(false),
		WithProgressCallback(func(_, _ int) {
			progressCalled = true
		}),
	)

	if opts.MaxConcurrency != 10 {
		t.Errorf("MaxConcurrency = %d, want 10", opts.MaxConcurrency)
	}
	if opts.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %v, want 30s", opts.RequestTimeout)
	}
	if opts.ContinueOnError {
		t.Error("ContinueOnError should be false")
	}
	if opts.ProgressCallback == nil {
		t.Fatal("ProgressCallback should not be nil")
	}
	opts.ProgressCallback(1, 1)
	if !progressCalled {
		t.Error("ProgressCallback was not called")
	}
}

func TestWithMaxConcurrency_ZeroIgnored(t *testing.T) {
	opts := ApplyBatchOptions(WithMaxConcurrency(0))
	if opts.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency = %d, want 5 (default)", opts.MaxConcurrency)
	}
}

func TestNewBatchRequest(t *testing.T) {
	messages := []Message{{Role: RoleUser, Content: "Hello"}}
	req := NewBatchRequest("test-1", messages, WithTemperature(0.5))

	if req.ID != "test-1" {
		t.Errorf("ID = %s, want test-1", req.ID)
	}
	if len(req.Messages) != 1 {
		t.Errorf("Messages length = %d, want 1", len(req.Messages))
	}
	if len(req.Options) != 1 {
		t.Errorf("Options length = %d, want 1", len(req.Options))
	}
}

func TestNewBatchRequestFromPrompt(t *testing.T) {
	req := NewBatchRequestFromPrompt("test-2", testHelloWorld)

	if req.ID != "test-2" {
		t.Errorf("ID = %s, want test-2", req.ID)
	}
	if len(req.Messages) != 1 {
		t.Errorf("Messages length = %d, want 1", len(req.Messages))
	}
	if req.Messages[0].Role != RoleUser {
		t.Errorf("Role = %s, want user", req.Messages[0].Role)
	}
	if req.Messages[0].Content != testHelloWorld {
		t.Errorf("Content = %s, want %q", req.Messages[0].Content, testHelloWorld)
	}
}

func TestBatchBuilder(t *testing.T) {
	builder := NewBatchBuilder().
		MustAddPrompt("req-1", "Question 1").
		MustAddPrompt("req-2", "Question 2", WithTemperature(0.8)).
		MustAdd("req-3", []Message{
			{Role: RoleSystem, Content: "You are helpful"},
			{Role: RoleUser, Content: "Question 3"},
		}).
		WithOptions(WithMaxConcurrency(3))

	requests := builder.Requests()
	if len(requests) != 3 {
		t.Errorf("Requests count = %d, want 3", len(requests))
	}

	opts := builder.Options()
	if len(opts) != 1 {
		t.Errorf("Options count = %d, want 1", len(opts))
	}

	if requests[0].ID != "req-1" {
		t.Errorf("First request ID = %s, want req-1", requests[0].ID)
	}
	if requests[1].ID != "req-2" {
		t.Errorf("Second request ID = %s, want req-2", requests[1].ID)
	}
	if len(requests[2].Messages) != 2 {
		t.Errorf("Third request messages = %d, want 2", len(requests[2].Messages))
	}
}

// mockBatchLLM is a mock LLM for testing batch operations
type mockBatchLLM struct {
	responses map[string]*Response
	errors    map[string]error
	delay     time.Duration
	callCount int32
	callFn    func(messages []Message) (*Response, error)
}

func (m *mockBatchLLM) Call(ctx context.Context, prompt string, options ...CallOption) (string, error) {
	resp, err := m.GenerateContent(ctx, []Message{{Role: RoleUser, Content: prompt}}, options...)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (m *mockBatchLLM) GenerateContent(ctx context.Context, messages []Message, _ ...CallOption) (*Response, error) {
	atomic.AddInt32(&m.callCount, 1)

	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}

	if m.callFn != nil {
		return m.callFn(messages)
	}

	// Check for predetermined errors
	if len(messages) > 0 && m.errors != nil {
		content := messages[len(messages)-1].Content
		if err, ok := m.errors[content]; ok {
			return nil, err
		}
	}

	// Return predetermined responses
	if len(messages) > 0 && m.responses != nil {
		content := messages[len(messages)-1].Content
		if resp, ok := m.responses[content]; ok {
			return resp, nil
		}
	}

	return &Response{
		Content:      "default response",
		FinishReason: "stop",
		Usage:        Usage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
	}, nil
}

func (m *mockBatchLLM) Stream(_ context.Context, _ []Message, _ ...CallOption) (<-chan StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (m *mockBatchLLM) Provider() Provider { return ProviderOpenAI }
func (m *mockBatchLLM) Model() string      { return "test-model" }

func TestConcurrentBatcher_EmptyBatch(t *testing.T) {
	llm := &mockBatchLLM{}
	batcher := NewConcurrentBatcher(llm)

	_, err := batcher.ProcessBatch(context.Background(), nil)
	if !errors.Is(err, ErrBatchEmpty) {
		t.Errorf("err = %v, want ErrBatchEmpty", err)
	}
}

func TestConcurrentBatcher_SingleRequest(t *testing.T) {
	llm := &mockBatchLLM{
		responses: map[string]*Response{
			"Hello": {Content: "Hi there!", Usage: Usage{TotalTokens: 10}},
		},
	}
	batcher := NewConcurrentBatcher(llm)

	requests := []BatchRequest{
		NewBatchRequestFromPrompt("req-1", "Hello"),
	}

	resp, err := batcher.ProcessBatch(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.SuccessCount != 1 {
		t.Errorf("SuccessCount = %d, want 1", resp.SuccessCount)
	}
	if resp.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", resp.FailureCount)
	}

	result, ok := resp.Results["req-1"]
	if !ok {
		t.Fatal("result for req-1 not found")
	}
	if result.Response.Content != "Hi there!" {
		t.Errorf("Content = %s, want 'Hi there!'", result.Response.Content)
	}
}

func TestConcurrentBatcher_MultipleRequests(t *testing.T) {
	llm := &mockBatchLLM{}
	batcher := NewConcurrentBatcher(llm)

	requests := []BatchRequest{
		NewBatchRequestFromPrompt("req-1", "Question 1"),
		NewBatchRequestFromPrompt("req-2", "Question 2"),
		NewBatchRequestFromPrompt("req-3", "Question 3"),
	}

	resp, err := batcher.ProcessBatch(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.SuccessCount != 3 {
		t.Errorf("SuccessCount = %d, want 3", resp.SuccessCount)
	}
	if len(resp.Results) != 3 {
		t.Errorf("Results count = %d, want 3", len(resp.Results))
	}

	// Verify all requests were processed
	for _, id := range []string{"req-1", "req-2", "req-3"} {
		if _, ok := resp.Results[id]; !ok {
			t.Errorf("missing result for %s", id)
		}
	}
}

func TestConcurrentBatcher_Concurrency(t *testing.T) {
	var maxConcurrent int32
	var currentConcurrent int32

	llm := &mockBatchLLM{
		delay: 50 * time.Millisecond,
		callFn: func(_ []Message) (*Response, error) {
			current := atomic.AddInt32(&currentConcurrent, 1)
			defer atomic.AddInt32(&currentConcurrent, -1)

			// Track maximum concurrency
			for {
				maxVal := atomic.LoadInt32(&maxConcurrent)
				if current <= maxVal || atomic.CompareAndSwapInt32(&maxConcurrent, maxVal, current) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)
			return &Response{Content: "ok"}, nil
		},
	}
	batcher := NewConcurrentBatcher(llm)

	requests := make([]BatchRequest, 10)
	for i := range requests {
		requests[i] = NewBatchRequestFromPrompt(string(rune('a'+i)), "test")
	}

	_, err := batcher.ProcessBatch(context.Background(), requests, WithMaxConcurrency(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if maxConcurrent > 3 {
		t.Errorf("max concurrent = %d, expected <= 3", maxConcurrent)
	}
}

func TestConcurrentBatcher_PartialFailure(t *testing.T) {
	testErr := errors.New("test error")
	llm := &mockBatchLLM{
		errors: map[string]error{
			"fail": testErr,
		},
	}
	batcher := NewConcurrentBatcher(llm)

	requests := []BatchRequest{
		NewBatchRequestFromPrompt("success-1", "ok"),
		NewBatchRequestFromPrompt("failure", "fail"),
		NewBatchRequestFromPrompt("success-2", "ok"),
	}

	resp, err := batcher.ProcessBatch(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", resp.SuccessCount)
	}
	if resp.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", resp.FailureCount)
	}

	if !errors.Is(resp.Results["failure"].Error, testErr) {
		t.Errorf("expected testErr for failure request")
	}
}

func TestConcurrentBatcher_PanicIsolation(t *testing.T) {
	llm := &mockBatchLLM{
		callFn: func(messages []Message) (*Response, error) {
			content := messages[len(messages)-1].Content
			if content == "panic" {
				panic("batch panic")
			}
			return &Response{
				Content: content + " response",
				Usage:   Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
			}, nil
		},
	}
	batcher := NewConcurrentBatcher(llm)

	requests := []BatchRequest{
		NewBatchRequestFromPrompt("success-1", "ok-1"),
		NewBatchRequestFromPrompt("panic", "panic"),
		NewBatchRequestFromPrompt("success-2", "ok-2"),
	}

	var progressCalls int32
	resp, err := batcher.ProcessBatch(context.Background(), requests,
		WithProgressCallback(func(_, _ int) {
			atomic.AddInt32(&progressCalls, 1)
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", resp.SuccessCount)
	}
	if resp.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", resp.FailureCount)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("Results count = %d, want 3", len(resp.Results))
	}

	panicResult := resp.Results["panic"]
	if panicResult == nil {
		t.Fatal("missing panic result")
	}
	if panicResult.Error == nil {
		t.Fatal("panic result Error is nil")
	}
	if !strings.Contains(panicResult.Error.Error(), "panic: batch panic") {
		t.Errorf("panic error = %q, want panic message", panicResult.Error.Error())
	}
	if panicResult.Response != nil {
		t.Error("panic result Response should be nil")
	}

	for _, id := range []string{"success-1", "success-2"} {
		result := resp.Results[id]
		if result == nil {
			t.Fatalf("missing result for %s", id)
		}
		if result.Error != nil {
			t.Errorf("%s Error = %v, want nil", id, result.Error)
		}
		if result.Response == nil {
			t.Errorf("%s Response is nil", id)
		}
	}
	if got := atomic.LoadInt32(&progressCalls); got != 3 {
		t.Errorf("progress calls = %d, want 3", got)
	}
}

func TestConcurrentBatcher_DuplicateRequestID(t *testing.T) {
	llm := &mockBatchLLM{}
	batcher := NewConcurrentBatcher(llm)

	requests := []BatchRequest{
		NewBatchRequestFromPrompt("duplicate", "first"),
		NewBatchRequestFromPrompt("duplicate", "second"),
	}

	resp, err := batcher.ProcessBatch(context.Background(), requests)
	if err == nil {
		t.Fatal("expected duplicate request ID error")
	}
	if resp != nil {
		t.Fatalf("response = %#v, want nil", resp)
	}
	if !strings.Contains(err.Error(), "duplicate batch request ID: duplicate") {
		t.Errorf("error = %q, want duplicate request ID message", err.Error())
	}
	if got := atomic.LoadInt32(&llm.callCount); got != 0 {
		t.Errorf("call count = %d, want 0", got)
	}
}

func TestConcurrentBatcher_ContextCancellation(t *testing.T) {
	llm := &mockBatchLLM{
		delay: 1 * time.Second, // Long delay
	}
	batcher := NewConcurrentBatcher(llm)

	requests := []BatchRequest{
		NewBatchRequestFromPrompt("req-1", "test"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	resp, err := batcher.ProcessBatch(ctx, requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", resp.FailureCount)
	}

	result := resp.Results["req-1"]
	if result.Error == nil {
		t.Error("expected timeout error")
	}
}

func TestConcurrentBatcher_ProgressCallback(t *testing.T) {
	llm := &mockBatchLLM{}
	batcher := NewConcurrentBatcher(llm)

	var progressCalls []int
	requests := []BatchRequest{
		NewBatchRequestFromPrompt("req-1", "test1"),
		NewBatchRequestFromPrompt("req-2", "test2"),
		NewBatchRequestFromPrompt("req-3", "test3"),
	}

	_, err := batcher.ProcessBatch(context.Background(), requests,
		WithMaxConcurrency(1), // Sequential to ensure order
		WithProgressCallback(func(completed, _ int) {
			progressCalls = append(progressCalls, completed)
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(progressCalls) != 3 {
		t.Errorf("progress calls = %d, want 3", len(progressCalls))
	}
}

func TestConcurrentBatcher_TokenUsageAggregation(t *testing.T) {
	llm := &mockBatchLLM{
		callFn: func(_ []Message) (*Response, error) {
			return &Response{
				Content: "ok",
				Usage: Usage{
					PromptTokens:     10,
					CompletionTokens: 20,
					TotalTokens:      30,
				},
			}, nil
		},
	}
	batcher := NewConcurrentBatcher(llm)

	requests := []BatchRequest{
		NewBatchRequestFromPrompt("req-1", "test1"),
		NewBatchRequestFromPrompt("req-2", "test2"),
		NewBatchRequestFromPrompt("req-3", "test3"),
	}

	resp, err := batcher.ProcessBatch(context.Background(), requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.TotalUsage.PromptTokens != 30 {
		t.Errorf("PromptTokens = %d, want 30", resp.TotalUsage.PromptTokens)
	}
	if resp.TotalUsage.CompletionTokens != 60 {
		t.Errorf("CompletionTokens = %d, want 60", resp.TotalUsage.CompletionTokens)
	}
	if resp.TotalUsage.TotalTokens != 90 {
		t.Errorf("TotalTokens = %d, want 90", resp.TotalUsage.TotalTokens)
	}
}

func TestBatchBuilder_Execute(t *testing.T) {
	llm := &mockBatchLLM{}
	batcher := NewConcurrentBatcher(llm)

	resp, err := NewBatchBuilder().
		MustAddPrompt("req-1", "Hello").
		MustAddPrompt("req-2", "World").
		WithOptions(WithMaxConcurrency(2)).
		Execute(context.Background(), batcher)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", resp.SuccessCount)
	}
}

func TestBatchErrors(t *testing.T) {
	if ErrBatchNotSupported == nil {
		t.Error("ErrBatchNotSupported should not be nil")
	}
	if ErrBatchEmpty == nil {
		t.Error("ErrBatchEmpty should not be nil")
	}
	if ErrBatchTooLarge == nil {
		t.Error("ErrBatchTooLarge should not be nil")
	}
}

// TestConcurrentBatcher_MaxBatchSize covers WithMaxBatchSize / ErrBatchTooLarge (#31).
func TestConcurrentBatcher_MaxBatchSize(t *testing.T) {
	batcher := NewConcurrentBatcher(&mockBatchLLM{})
	reqs := []BatchRequest{
		NewBatchRequestFromPrompt("a", "x"),
		NewBatchRequestFromPrompt("b", "y"),
		NewBatchRequestFromPrompt("c", "z"),
	}
	if _, err := batcher.ProcessBatch(context.Background(), reqs, WithMaxBatchSize(2)); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("expected ErrBatchTooLarge for 3 > 2, got %v", err)
	}
	resp, err := batcher.ProcessBatch(context.Background(), reqs[:2], WithMaxBatchSize(2))
	if err != nil {
		t.Fatalf("unexpected error within limit: %v", err)
	}
	if resp == nil || len(resp.Results) != 2 {
		t.Fatalf("expected 2 results within limit, got %#v", resp)
	}
}
