package resilience

import (
	"context"
	"errors"
	"io"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker()

	if cb.State() != CircuitClosed {
		t.Errorf("initial state = %v, want Closed", cb.State())
	}
	if !cb.Allow() {
		t.Error("should allow requests in closed state")
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := NewCircuitBreaker(WithMaxFailures(3))

	// Record failures
	for i := 0; i < 3; i++ {
		cb.Allow()
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Errorf("state = %v, want Open", cb.State())
	}
	if cb.Allow() {
		t.Error("should not allow requests when open")
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cb := NewCircuitBreaker(WithMaxFailures(3))

	// Record some failures
	cb.Allow()
	cb.RecordFailure()
	cb.Allow()
	cb.RecordFailure()

	// Success should reset
	cb.Allow()
	cb.RecordSuccess()

	// More failures shouldn't open yet (count was reset)
	cb.Allow()
	cb.RecordFailure()
	cb.Allow()
	cb.RecordFailure()

	if cb.State() != CircuitClosed {
		t.Error("circuit should still be closed after reset")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(2),
		WithResetTimeout(50*time.Millisecond),
	)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()
	cb.Allow()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Fatal("circuit should be open")
	}

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Next Allow should transition to half-open
	if !cb.Allow() {
		t.Error("should allow after reset timeout")
	}
	if cb.State() != CircuitHalfOpen {
		t.Errorf("state = %v, want HalfOpen", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(10*time.Millisecond),
		WithHalfOpenMax(2),
	)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(15 * time.Millisecond)
	cb.Allow()

	if cb.State() != CircuitHalfOpen {
		t.Fatalf("state = %v, want HalfOpen", cb.State())
	}

	// Record successes to close
	cb.RecordSuccess()
	cb.Allow()
	cb.RecordSuccess()

	if cb.State() != CircuitClosed {
		t.Errorf("state = %v, want Closed", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(10*time.Millisecond),
	)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(15 * time.Millisecond)
	cb.Allow()

	if cb.State() != CircuitHalfOpen {
		t.Fatalf("state = %v, want HalfOpen", cb.State())
	}

	// Failure should go back to open
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("state = %v, want Open", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenLimitsRequests(t *testing.T) {
	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithResetTimeout(10*time.Millisecond),
		WithHalfOpenMax(2),
	)

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(15 * time.Millisecond)

	// Should allow up to halfOpenMax requests
	if !cb.Allow() {
		t.Error("should allow first half-open request")
	}
	if !cb.Allow() {
		t.Error("should allow second half-open request")
	}
	if cb.Allow() {
		t.Error("should not allow third half-open request")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(WithMaxFailures(1))

	// Open the circuit
	cb.Allow()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Fatal("circuit should be open")
	}

	// Reset
	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("state = %v, want Closed", cb.State())
	}
	if !cb.Allow() {
		t.Error("should allow after reset")
	}
}

func TestCircuitBreaker_StateChangeCallback(t *testing.T) {
	var stateChanges []struct{ from, to CircuitState }
	var mu sync.Mutex

	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithOnStateChange(func(from, to CircuitState) {
			mu.Lock()
			stateChanges = append(stateChanges, struct{ from, to CircuitState }{from, to})
			mu.Unlock()
		}),
	)

	cb.Allow()
	cb.RecordFailure()

	// Wait for goroutine callback
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(stateChanges) != 1 {
		t.Fatalf("expected 1 state change, got %d", len(stateChanges))
	}
	if stateChanges[0].from != CircuitClosed || stateChanges[0].to != CircuitOpen {
		t.Errorf("change = %v->%v, want Closed->Open", stateChanges[0].from, stateChanges[0].to)
	}
}

func TestCircuitBreaker_StateChangeCallbackPanicRecovered(t *testing.T) {
	callbackStarted := make(chan struct{})
	transitions := make(chan struct{ from, to CircuitState }, 1)

	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithOnStateChange(func(from, to CircuitState) {
			transitions <- struct{ from, to CircuitState }{from, to}
			close(callbackStarted)
			panic("callback panic")
		}),
	)

	cb.Allow()
	cb.RecordFailure()

	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for callback")
	}

	transition := <-transitions
	if transition.from != CircuitClosed || transition.to != CircuitOpen {
		t.Errorf("change = %v->%v, want Closed->Open", transition.from, transition.to)
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("state = %v, want Open", cb.State())
	}
	if cb.Allow() {
		t.Fatal("circuit should still block requests after callback panic")
	}
}

func TestCircuitBreaker_StateChangeCallbackUsesSingleGoroutine(t *testing.T) {
	before := waitForStableGoroutineCount(t)
	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	callbackDone := make(chan struct{})
	transitions := make(chan struct{ from, to CircuitState }, 1)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseCallback)
		})
	})

	cb := NewCircuitBreaker(
		WithMaxFailures(1),
		WithOnStateChange(func(from, to CircuitState) {
			transitions <- struct{ from, to CircuitState }{from, to}
			close(callbackEntered)
			<-releaseCallback
			close(callbackDone)
		}),
	)

	cb.Allow()
	cb.RecordFailure()

	select {
	case <-callbackEntered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for callback to enter")
	}

	transition := <-transitions
	if transition.from != CircuitClosed || transition.to != CircuitOpen {
		t.Errorf("change = %v->%v, want Closed->Open", transition.from, transition.to)
	}

	if got := waitForGoroutineDelta(t, before, 1); got != 1 {
		t.Fatalf("callback goroutine delta = %d, want 1", got)
	}

	releaseOnce.Do(func() {
		close(releaseCallback)
	})
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for callback to finish")
	}
}

func waitForStableGoroutineCount(t *testing.T) int {
	t.Helper()

	prev := runtime.NumGoroutine()
	stableSamples := 0
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	for {
		select {
		case <-ticker.C:
			next := runtime.NumGoroutine()
			if next == prev {
				stableSamples++
				if stableSamples == 5 {
					return next
				}
				continue
			}
			prev = next
			stableSamples = 0
		case <-deadline.C:
			return runtime.NumGoroutine()
		}
	}
}

func waitForGoroutineDelta(t *testing.T, baseline, want int) int {
	t.Helper()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(200 * time.Millisecond)
	defer deadline.Stop()

	last := runtime.NumGoroutine() - baseline
	for {
		if last == want {
			return last
		}

		select {
		case <-ticker.C:
			last = runtime.NumGoroutine() - baseline
		case <-deadline.C:
			return last
		}
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}

	for _, tc := range tests {
		if tc.state.String() != tc.expected {
			t.Errorf("%v.String() = %s, want %s", tc.state, tc.state.String(), tc.expected)
		}
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()

	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 1*time.Second {
		t.Errorf("InitialDelay = %v, want 1s", cfg.InitialDelay)
	}
	if cfg.BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor = %f, want 2.0", cfg.BackoffFactor)
	}
	if cfg.ShouldRetry == nil {
		t.Error("ShouldRetry should not be nil")
	}
}

func TestDefaultShouldRetry(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"rate limited", &llms.APIError{StatusCode: 429}, true},
		{"server error 500", &llms.APIError{StatusCode: 500}, true},
		{"server error 502", &llms.APIError{StatusCode: 502}, true},
		{"server error 503", &llms.APIError{StatusCode: 503}, true},
		{"server error 504", &llms.APIError{StatusCode: 504}, true},
		{"client error 400", &llms.APIError{StatusCode: 400}, false},
		{"auth error 401", &llms.APIError{StatusCode: 401}, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"circuit open", ErrCircuitOpen, false},
		{"random error", errors.New("random"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DefaultShouldRetry(tc.err)
			if result != tc.expected {
				t.Errorf("ShouldRetry(%v) = %v, want %v", tc.err, result, tc.expected)
			}
		})
	}
}

func TestIsProviderUnhealthy(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"rate limited", &llms.APIError{StatusCode: 429}, true},
		{"server error", &llms.APIError{StatusCode: 503}, true},
		{"client error", &llms.APIError{StatusCode: 400}, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"circuit open", ErrCircuitOpen, false},
		{"eof", io.EOF, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"connection refused", syscall.ECONNREFUSED, true},
		{"connection reset", syscall.ECONNRESET, true},
		{"url wrapped connection refused", &url.Error{Op: "Post", URL: "http://127.0.0.1", Err: syscall.ECONNREFUSED}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isProviderUnhealthy(tc.err)
			if result != tc.expected {
				t.Errorf("isProviderUnhealthy(%v) = %v, want %v", tc.err, result, tc.expected)
			}
		})
	}
}

// mockResilientLLM is a mock LLM for testing resilient client
type mockResilientLLM struct {
	provider    llms.Provider
	model       string
	callCount   int32
	responses   []mockResponse
	responseIdx int32
	mu          sync.Mutex
}

type mockResponse struct {
	content string
	err     error
}

func (m *mockResilientLLM) Call(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
	return m.getResponse()
}

func (m *mockResilientLLM) GenerateContent(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (*llms.Response, error) {
	content, err := m.getResponse()
	if err != nil {
		return nil, err
	}
	return &llms.Response{Content: content}, nil
}

func (m *mockResilientLLM) Stream(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	content, err := m.getResponse()
	if err != nil {
		return nil, err
	}
	ch := make(chan llms.StreamChunk, 1)
	ch <- llms.StreamChunk{Content: content}
	close(ch)
	return ch, nil
}

func (m *mockResilientLLM) getResponse() (string, error) {
	atomic.AddInt32(&m.callCount, 1)

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.responses) == 0 {
		return "default", nil
	}

	idx := m.responseIdx
	responsesLen := len(m.responses)
	if int(idx) >= responsesLen {
		if responsesLen > 0 {
			idx = int32(responsesLen - 1)
		} else {
			idx = 0
		}
	}
	m.responseIdx++

	resp := m.responses[idx]
	return resp.content, resp.err
}

func (m *mockResilientLLM) Provider() llms.Provider { return m.provider }
func (m *mockResilientLLM) Model() string           { return m.model }
func (m *mockResilientLLM) CallCount() int          { return int(atomic.LoadInt32(&m.callCount)) }

func TestResilientClient_SuccessNoRetry(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{{content: "success"}},
	}

	client := NewResilientClient(llm)

	result, err := client.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("result = %s, want 'success'", result)
	}
	if llm.CallCount() != 1 {
		t.Errorf("call count = %d, want 1", llm.CallCount())
	}
}

func TestResilientClient_RetriesOnRetryableError(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{
			{err: &llms.APIError{StatusCode: 429, Message: "rate limited"}},
			{err: &llms.APIError{StatusCode: 503, Message: "service unavailable"}},
			{content: "success"},
		},
	}

	client := NewResilientClient(llm,
		WithRetryConfig(&RetryConfig{
			MaxAttempts:   3,
			InitialDelay:  10 * time.Millisecond,
			BackoffFactor: 1.0,
			ShouldRetry:   DefaultShouldRetry,
		}),
	)

	result, err := client.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("result = %s, want 'success'", result)
	}
	if llm.CallCount() != 3 {
		t.Errorf("call count = %d, want 3", llm.CallCount())
	}
}

func TestResilientClient_NoRetryOnNonRetryableError(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{
			{err: &llms.APIError{StatusCode: 401, Message: "unauthorized"}},
		},
	}

	client := NewResilientClient(llm)

	_, err := client.Call(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if llm.CallCount() != 1 {
		t.Errorf("call count = %d, want 1 (no retries)", llm.CallCount())
	}
}

func TestResilientClient_MaxAttemptsExhausted(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{
			{err: &llms.APIError{StatusCode: 429}},
			{err: &llms.APIError{StatusCode: 429}},
			{err: &llms.APIError{StatusCode: 429}},
		},
	}

	client := NewResilientClient(llm,
		WithRetryConfig(&RetryConfig{
			MaxAttempts:   3,
			InitialDelay:  10 * time.Millisecond,
			BackoffFactor: 1.0,
			ShouldRetry:   DefaultShouldRetry,
		}),
	)

	_, err := client.Call(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error after max attempts")
	}
	if llm.CallCount() != 3 {
		t.Errorf("call count = %d, want 3", llm.CallCount())
	}
}

func TestResilientClient_CircuitBreakerOpens(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{
			{err: &llms.APIError{StatusCode: 500}},
			{err: &llms.APIError{StatusCode: 500}},
			{err: &llms.APIError{StatusCode: 500}},
		},
	}

	breaker := NewCircuitBreaker(WithMaxFailures(2))
	client := NewResilientClient(llm,
		WithCircuitBreaker(breaker),
		WithRetryConfig(&RetryConfig{
			MaxAttempts:  1, // No retries - just let circuit breaker handle it
			InitialDelay: 10 * time.Millisecond,
			ShouldRetry:  func(error) bool { return false },
		}),
	)

	// First two calls fail and open the circuit
	_, _ = client.Call(context.Background(), "test1")
	_, _ = client.Call(context.Background(), "test2")

	if breaker.State() != CircuitOpen {
		t.Fatalf("breaker state = %v, want Open", breaker.State())
	}

	// Third call should be blocked by circuit breaker
	_, err := client.Call(context.Background(), "test3")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("err = %v, want ErrCircuitOpen", err)
	}

	// LLM should only have been called twice
	if llm.CallCount() != 2 {
		t.Errorf("call count = %d, want 2", llm.CallCount())
	}
}

func TestResilientClient_CircuitBreakerOpensOnConnectionRefused(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{{err: &url.Error{Op: "Post", URL: "http://127.0.0.1", Err: syscall.ECONNREFUSED}}},
	}

	breaker := NewCircuitBreaker(WithMaxFailures(2))
	client := NewResilientClient(llm,
		WithCircuitBreaker(breaker),
		WithRetryConfig(&RetryConfig{
			MaxAttempts:  1,
			InitialDelay: time.Millisecond,
			ShouldRetry:  func(error) bool { return false },
		}),
	)

	for i := 0; i < 10; i++ {
		_, _ = client.Call(context.Background(), "test")
	}

	if breaker.State() != CircuitOpen {
		t.Fatalf("breaker state = %v, want Open", breaker.State())
	}
	if llm.CallCount() != 2 {
		t.Errorf("call count = %d, want 2", llm.CallCount())
	}
}

func TestResilientClient_PreservesLastErrorWhenRetriesExhausted(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{{err: &llms.APIError{StatusCode: 500}}},
	}
	breaker := NewCircuitBreaker(WithMaxFailures(1))
	client := NewResilientClient(llm,
		WithCircuitBreaker(breaker),
		WithRetryConfig(&RetryConfig{
			MaxAttempts:   3,
			InitialDelay:  time.Millisecond,
			BackoffFactor: 1,
			ShouldRetry:   DefaultShouldRetry,
		}),
	)

	_, err := client.Call(context.Background(), "test")
	if !errors.Is(err, llms.ErrServerError) {
		t.Fatalf("err = %v, want preserved server error", err)
	}
	if breaker.State() != CircuitOpen {
		t.Fatalf("breaker state = %v, want Open after terminal provider failure", breaker.State())
	}
}

func TestResilientClient_CircuitBreakerCountsFailingCallsNotAttempts(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{{err: &llms.APIError{StatusCode: 500}}},
	}
	breaker := NewCircuitBreaker(WithMaxFailures(2))
	client := NewResilientClient(llm,
		WithCircuitBreaker(breaker),
		WithRetryConfig(&RetryConfig{
			MaxAttempts:   3,
			InitialDelay:  time.Millisecond,
			BackoffFactor: 1,
			ShouldRetry:   DefaultShouldRetry,
		}),
	)

	_, err := client.Call(context.Background(), "test1")
	if err == nil {
		t.Fatal("expected first call to fail")
	}
	if breaker.State() != CircuitClosed {
		t.Fatalf("breaker state after one failed call = %v, want Closed", breaker.State())
	}
	if llm.CallCount() != 3 {
		t.Fatalf("call count after one logical call = %d, want 3 attempts", llm.CallCount())
	}

	_, err = client.Call(context.Background(), "test2")
	if err == nil {
		t.Fatal("expected second call to fail")
	}
	if breaker.State() != CircuitOpen {
		t.Fatalf("breaker state after two failed calls = %v, want Open", breaker.State())
	}
	if llm.CallCount() != 6 {
		t.Fatalf("call count after two logical calls = %d, want 6 attempts", llm.CallCount())
	}
}

func TestResilientClient_RetryConfigCopyOnWrite(t *testing.T) {
	cfg := &RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  time.Millisecond,
		BackoffFactor: 1,
		ShouldRetry:   DefaultShouldRetry,
	}

	client1 := NewResilientClient(&mockResilientLLM{}, WithRetryConfig(cfg), WithMaxRetries(1))
	client2 := NewResilientClient(&mockResilientLLM{}, WithRetryConfig(cfg), WithMaxRetries(4))

	if cfg.MaxAttempts != 3 {
		t.Fatalf("shared retry config MaxAttempts = %d, want unchanged 3", cfg.MaxAttempts)
	}
	if client1.retry.MaxAttempts != 2 {
		t.Fatalf("client1 MaxAttempts = %d, want 2", client1.retry.MaxAttempts)
	}
	if client2.retry.MaxAttempts != 5 {
		t.Fatalf("client2 MaxAttempts = %d, want 5", client2.retry.MaxAttempts)
	}
}

func TestCalculateDelay_ClampsJitter(t *testing.T) {
	for i := 0; i < 1000; i++ {
		if delay := calculateDelay(time.Millisecond, 2); delay < 0 {
			t.Fatalf("delay = %v, want non-negative", delay)
		}
	}
}

// TestResilientClient_BreakerIgnoresContextCanceled verifies that non-provider
// failures (context cancellation) never trip the circuit breaker, while
// retryable provider failures (503) do.
func TestResilientClient_BreakerIgnoresContextCanceled(t *testing.T) {
	noRetry := func() *RetryConfig {
		return &RetryConfig{
			MaxAttempts:  1, // no retries; isolate breaker accounting
			InitialDelay: time.Millisecond,
			ShouldRetry:  func(error) bool { return false },
		}
	}

	t.Run("context canceled does not open breaker", func(t *testing.T) {
		llm := &mockResilientLLM{
			responses: []mockResponse{{err: context.Canceled}},
		}
		breaker := NewCircuitBreaker(WithMaxFailures(5))
		client := NewResilientClient(llm,
			WithCircuitBreaker(breaker),
			WithRetryConfig(noRetry()),
		)

		for i := 0; i < 5; i++ {
			_, _ = client.Call(context.Background(), "x")
		}

		if breaker.State() != CircuitClosed {
			t.Fatalf("breaker state = %v, want Closed (context.Canceled must not trip the breaker)", breaker.State())
		}
	})

	t.Run("retryable 503 opens breaker", func(t *testing.T) {
		llm := &mockResilientLLM{
			responses: []mockResponse{{err: &llms.APIError{StatusCode: 503, Message: "service unavailable"}}},
		}
		breaker := NewCircuitBreaker(WithMaxFailures(5))
		client := NewResilientClient(llm,
			WithCircuitBreaker(breaker),
			WithRetryConfig(noRetry()),
		)

		for i := 0; i < 5; i++ {
			_, _ = client.Call(context.Background(), "x")
		}

		if breaker.State() != CircuitOpen {
			t.Fatalf("breaker state = %v, want Open (5 retryable 503s must open the breaker)", breaker.State())
		}
	})
}

func TestResilientClient_OnRetryCallback(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{
			{err: &llms.APIError{StatusCode: 429}},
			{content: "success"},
		},
	}

	var retryAttempts []int
	client := NewResilientClient(llm,
		WithRetryConfig(&RetryConfig{
			MaxAttempts:   3,
			InitialDelay:  10 * time.Millisecond,
			BackoffFactor: 1.0,
			ShouldRetry:   DefaultShouldRetry,
		}),
		WithOnRetry(func(attempt int, _ error, _ time.Duration) {
			retryAttempts = append(retryAttempts, attempt)
		}),
	)

	_, err := client.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(retryAttempts) != 1 {
		t.Errorf("retry attempts = %v, want [1]", retryAttempts)
	}
}

func TestResilientClient_HonorsRetryAfter(t *testing.T) {
	retryAfter := 30 * time.Millisecond
	llm := &mockResilientLLM{
		responses: []mockResponse{
			{err: &llms.APIError{StatusCode: 429, RetryAfter: retryAfter}},
			{content: "success"},
		},
	}

	var observedDelay time.Duration
	client := NewResilientClient(llm,
		WithRetryConfig(&RetryConfig{
			MaxAttempts:   2,
			InitialDelay:  time.Millisecond,
			MaxDelay:      time.Second,
			BackoffFactor: 1,
			Jitter:        0,
			ShouldRetry:   DefaultShouldRetry,
		}),
		WithOnRetry(func(_ int, _ error, delay time.Duration) {
			observedDelay = delay
		}),
	)

	start := time.Now()
	result, err := client.Call(context.Background(), "test")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("result = %s, want success", result)
	}
	if observedDelay < retryAfter {
		t.Errorf("retry delay = %v, want at least %v", observedDelay, retryAfter)
	}
	if elapsed < retryAfter {
		t.Errorf("elapsed = %v, want at least %v", elapsed, retryAfter)
	}
}

func TestResilientClient_MaxAttemptsZeroAttemptsOnce(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{{err: &llms.APIError{StatusCode: 429}}},
	}
	client := NewResilientClient(llm,
		WithRetryConfig(&RetryConfig{
			MaxAttempts:  0,
			InitialDelay: time.Millisecond,
			ShouldRetry:  DefaultShouldRetry,
		}),
	)

	_, err := client.Call(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if llm.CallCount() != 1 {
		t.Errorf("call count = %d, want 1", llm.CallCount())
	}
}

func TestResilientClient_ContextCancellation(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{
			{err: &llms.APIError{StatusCode: 429}},
			{err: &llms.APIError{StatusCode: 429}},
		},
	}

	client := NewResilientClient(llm,
		WithRetryConfig(&RetryConfig{
			MaxAttempts:   10,
			InitialDelay:  100 * time.Millisecond,
			BackoffFactor: 1.0,
			ShouldRetry:   DefaultShouldRetry,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Call(ctx, "test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestResilientClient_GenerateContent(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{{content: "generated"}},
	}

	client := NewResilientClient(llm)

	resp, err := client.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "generated" {
		t.Errorf("content = %s, want 'generated'", resp.Content)
	}
}

func TestResilientClient_Stream(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{{content: "streamed"}},
	}

	client := NewResilientClient(llm)

	stream, err := client.Stream(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content string
	for chunk := range stream {
		content += chunk.Content
	}
	if content != "streamed" {
		t.Errorf("content = %s, want 'streamed'", content)
	}
}

func TestResilientClient_GetProvider(t *testing.T) {
	llm := &mockResilientLLM{provider: llms.ProviderOpenAI}
	client := NewResilientClient(llm)

	if client.Provider() != llms.ProviderOpenAI {
		t.Errorf("provider = %v, want OpenAI", client.Provider())
	}
}

func TestResilientClient_GetModel(t *testing.T) {
	llm := &mockResilientLLM{model: "gpt-4"}
	client := NewResilientClient(llm)

	if client.Model() != "gpt-4" {
		t.Errorf("model = %s, want 'gpt-4'", client.Model())
	}
}

func TestResilientClient_Unwrap(t *testing.T) {
	llm := &mockResilientLLM{}
	client := NewResilientClient(llm)

	if client.Unwrap() != llm {
		t.Error("Unwrap should return underlying LLM")
	}
}

func TestResilientClient_CircuitBreakerAccessor(t *testing.T) {
	breaker := NewCircuitBreaker()
	client := NewResilientClient(&mockResilientLLM{}, WithCircuitBreaker(breaker))

	if client.CircuitBreaker() != breaker {
		t.Error("CircuitBreaker() should return the breaker")
	}
}

func TestResilientClient_WithMaxRetries(t *testing.T) {
	llm := &mockResilientLLM{
		responses: []mockResponse{
			{err: &llms.APIError{StatusCode: 429}},
			{err: &llms.APIError{StatusCode: 429}},
			{content: "success"},
		},
	}

	client := NewResilientClient(llm,
		WithMaxRetries(2), // 2 retries = 3 total attempts
		WithRetryDelay(10*time.Millisecond),
	)

	_, err := client.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if llm.CallCount() != 3 {
		t.Errorf("call count = %d, want 3", llm.CallCount())
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(WithMaxFailures(100))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				cb.Allow()
				if j%2 == 0 {
					cb.RecordSuccess()
				} else {
					cb.RecordFailure()
				}
			}
		}()
	}
	wg.Wait()

	// Should not panic and state should be valid
	state := cb.State()
	if state != CircuitClosed && state != CircuitOpen && state != CircuitHalfOpen {
		t.Errorf("invalid state: %v", state)
	}
}
