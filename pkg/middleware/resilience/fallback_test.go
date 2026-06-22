package resilience

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

const (
	testGenerated = "generated"
	testStreamed  = "streamed"
)

// mockFallbackLLM is a mock LLM for testing fallback chain
type mockFallbackLLM struct {
	provider  llms.Provider
	model     string
	callResp  string
	callErr   error
	genResp   *llms.Response
	genErr    error
	streamErr error
	callCount int32
}

func (m *mockFallbackLLM) Call(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.callErr != nil {
		return "", m.callErr
	}
	return m.callResp, nil
}

func (m *mockFallbackLLM) GenerateContent(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (*llms.Response, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.genErr != nil {
		return nil, m.genErr
	}
	if m.callErr != nil {
		return nil, m.callErr
	}
	if m.genResp != nil {
		return m.genResp, nil
	}
	return &llms.Response{Content: m.callResp}, nil
}

func (m *mockFallbackLLM) Stream(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan llms.StreamChunk, 1)
	ch <- llms.StreamChunk{Content: m.callResp}
	close(ch)
	return ch, nil
}

func (m *mockFallbackLLM) Provider() llms.Provider { return m.provider }
func (m *mockFallbackLLM) Model() string           { return m.model }
func (m *mockFallbackLLM) CallCount() int          { return int(atomic.LoadInt32(&m.callCount)) }

type fallbackTestClock struct {
	now time.Time
}

func (c *fallbackTestClock) Now() time.Time {
	return c.now
}

func (c *fallbackTestClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func TestDefaultFallbackSelector(t *testing.T) {
	selector := DefaultFallbackSelector{}

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
		{"overloaded 529", &llms.APIError{StatusCode: 529}, true},
		{"rate_limit_error type", &llms.APIError{Type: "rate_limit_error"}, true},
		{"overloaded_error type", &llms.APIError{Type: "overloaded_error"}, true},
		{"server_error type", &llms.APIError{Type: "server_error"}, true},
		{"circuit open", ErrCircuitOpen, true},
		{"connection refused", syscall.ECONNREFUSED, true},
		{"url wrapped connection refused", &url.Error{Op: "Post", URL: "http://127.0.0.1", Err: syscall.ECONNREFUSED}, true},
		{"client error 400", &llms.APIError{StatusCode: 400}, false},
		{"auth error 401", &llms.APIError{StatusCode: 401}, false},
		{"not found 404", &llms.APIError{StatusCode: 404}, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"random error", errors.New("random"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := selector.ShouldFallback(tc.err)
			if result != tc.expected {
				t.Errorf("ShouldFallback(%v) = %v, want %v", tc.err, result, tc.expected)
			}
		})
	}
}

func TestAlwaysFallbackSelector(t *testing.T) {
	selector := AlwaysFallbackSelector{}

	if selector.ShouldFallback(nil) {
		t.Error("should not fallback on nil error")
	}
	if !selector.ShouldFallback(errors.New("any error")) {
		t.Error("should fallback on any error")
	}
}

func TestNeverFallbackSelector(t *testing.T) {
	selector := NeverFallbackSelector{}

	if selector.ShouldFallback(nil) {
		t.Error("should not fallback on nil")
	}
	if selector.ShouldFallback(errors.New("error")) {
		t.Error("should never fallback")
	}
}

func TestFallbackChain_EmptyChain(t *testing.T) {
	chain := NewFallbackChain(nil)

	_, err := chain.Call(context.Background(), "test")
	if !errors.Is(err, ErrNoClientsAvailable) {
		t.Errorf("err = %v, want ErrNoClientsAvailable", err)
	}
}

func TestFallbackChain_SingleClient_Success(t *testing.T) {
	client := &mockFallbackLLM{
		provider: llms.ProviderOpenAI,
		model:    testGPT4,
		callResp: testSuccess,
	}

	chain := NewFallbackChain([]llms.LLM{client})

	result, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != testSuccess {
		t.Errorf("result = %s, want 'success'", result)
	}
	if client.CallCount() != 1 {
		t.Errorf("call count = %d, want 1", client.CallCount())
	}
}

func TestFallbackChain_SingleClient_Error(t *testing.T) {
	expectedErr := errors.New("test error")
	client := &mockFallbackLLM{
		callErr: expectedErr,
	}

	chain := NewFallbackChain([]llms.LLM{client})

	_, err := chain.Call(context.Background(), "test")
	if !errors.Is(err, expectedErr) {
		t.Errorf("err = %v, want %v", err, expectedErr)
	}
}

func TestFallbackChain_FallsBack(t *testing.T) {
	client1 := &mockFallbackLLM{
		provider: llms.ProviderOpenAI,
		model:    testGPT4,
		callErr:  &llms.APIError{StatusCode: 429, Message: "rate limited"},
	}
	client2 := &mockFallbackLLM{
		provider: llms.ProviderAnthropic,
		model:    "claude-3",
		callResp: "from anthropic",
	}

	chain := NewFallbackChain([]llms.LLM{client1, client2})

	result, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from anthropic" {
		t.Errorf("result = %s, want 'from anthropic'", result)
	}
	if client1.CallCount() != 1 {
		t.Errorf("client1 call count = %d, want 1", client1.CallCount())
	}
	if client2.CallCount() != 1 {
		t.Errorf("client2 call count = %d, want 1", client2.CallCount())
	}
}

func TestFallbackChain_FallsBackOnConnectionRefused(t *testing.T) {
	client1 := &mockFallbackLLM{
		provider: llms.ProviderOpenAI,
		model:    testGPT4,
		callErr:  &url.Error{Op: "Post", URL: "http://127.0.0.1", Err: syscall.ECONNREFUSED},
	}
	client2 := &mockFallbackLLM{
		provider: llms.ProviderAnthropic,
		model:    "claude-3",
		callResp: "from anthropic",
	}

	chain := NewFallbackChain([]llms.LLM{client1, client2})

	result, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from anthropic" {
		t.Errorf("result = %s, want 'from anthropic'", result)
	}
	if client1.CallCount() != 1 {
		t.Errorf("client1 call count = %d, want 1", client1.CallCount())
	}
	if client2.CallCount() != 1 {
		t.Errorf("client2 call count = %d, want 1", client2.CallCount())
	}
}

func TestFallbackChain_NoFallbackOnNonRetryable(t *testing.T) {
	client1 := &mockFallbackLLM{
		callErr: &llms.APIError{StatusCode: 401, Message: "unauthorized"},
	}
	client2 := &mockFallbackLLM{
		callResp: "should not reach",
	}

	chain := NewFallbackChain([]llms.LLM{client1, client2})

	_, err := chain.Call(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if client2.CallCount() != 0 {
		t.Errorf("client2 should not be called, got %d", client2.CallCount())
	}
}

func TestFallbackChain_ClientErrorDoesNotMarkUnhealthy(t *testing.T) {
	client1 := &mockFallbackLLM{
		callErr: &llms.APIError{StatusCode: 400, Message: "bad request"},
	}
	client2 := &mockFallbackLLM{
		callResp: testSuccess,
	}

	chain := NewFallbackChain([]llms.LLM{client1, client2},
		WithFallbackSelector(AlwaysFallbackSelector{}),
	)

	result, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != testSuccess {
		t.Errorf("result = %s, want success", result)
	}
	if !chain.IsClientHealthy(0) {
		t.Error("400 response should not mark primary unhealthy")
	}
}

func TestFallbackChain_AllFail(t *testing.T) {
	client1 := &mockFallbackLLM{
		callErr: &llms.APIError{StatusCode: 500},
	}
	client2 := &mockFallbackLLM{
		callErr: &llms.APIError{StatusCode: 500},
	}
	client3 := &mockFallbackLLM{
		callErr: &llms.APIError{StatusCode: 500},
	}

	chain := NewFallbackChain([]llms.LLM{client1, client2, client3})

	_, err := chain.Call(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error when all clients fail")
	}
	if !errors.Is(err, ErrAllClientsFailed) {
		t.Fatalf("err = %v, want ErrAllClientsFailed", err)
	}
	if !errors.Is(err, llms.ErrServerError) {
		t.Fatalf("err = %v, want preserved server error", err)
	}

	// All clients should have been tried
	if client1.CallCount() != 1 || client2.CallCount() != 1 || client3.CallCount() != 1 {
		t.Errorf("all clients should be called once, got %d, %d, %d",
			client1.CallCount(), client2.CallCount(), client3.CallCount())
	}
}

func TestFallbackChain_GenerateContentAllFailReturnsSentinel(t *testing.T) {
	client1 := &mockFallbackLLM{genErr: &llms.APIError{StatusCode: 503}}
	client2 := &mockFallbackLLM{genErr: &llms.APIError{StatusCode: 503}}
	chain := NewFallbackChain([]llms.LLM{client1, client2})

	_, err := chain.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "test"}})
	if !errors.Is(err, ErrAllClientsFailed) {
		t.Fatalf("err = %v, want ErrAllClientsFailed", err)
	}
	if !errors.Is(err, llms.ErrServiceUnavailable) {
		t.Fatalf("err = %v, want preserved service unavailable error", err)
	}
}

func TestFallbackChain_StreamAllFailReturnsSentinel(t *testing.T) {
	client1 := &mockFallbackLLM{streamErr: &llms.APIError{StatusCode: 503}}
	client2 := &mockFallbackLLM{streamErr: &llms.APIError{StatusCode: 503}}
	chain := NewFallbackChain([]llms.LLM{client1, client2})

	_, err := chain.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "test"}})
	if !errors.Is(err, ErrAllClientsFailed) {
		t.Fatalf("err = %v, want ErrAllClientsFailed", err)
	}
	if !errors.Is(err, llms.ErrServiceUnavailable) {
		t.Fatalf("err = %v, want preserved service unavailable error", err)
	}
}

func TestFallbackChain_OnFallbackCallback(t *testing.T) {
	client1 := &mockFallbackLLM{
		provider: llms.ProviderOpenAI,
		callErr:  &llms.APIError{StatusCode: 429},
	}
	client2 := &mockFallbackLLM{
		provider: llms.ProviderAnthropic,
		callResp: testSuccess,
	}

	var fallbackCalled bool
	var fromIdx, toIdx int
	chain := NewFallbackChain([]llms.LLM{client1, client2},
		WithOnFallback(func(fi, ti int, _, _ llms.LLM, _ error) {
			fallbackCalled = true
			fromIdx = fi
			toIdx = ti
		}),
	)

	_, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fallbackCalled {
		t.Error("onFallback should have been called")
	}
	if fromIdx != 0 || toIdx != 1 {
		t.Errorf("fallback indices = %d->%d, want 0->1", fromIdx, toIdx)
	}
}

func TestFallbackChain_OnSuccessCallback(t *testing.T) {
	client := &mockFallbackLLM{
		callResp: testSuccess,
	}

	var successCalled bool
	var successIdx int
	chain := NewFallbackChain([]llms.LLM{client},
		WithOnSuccess(func(idx int, _ llms.LLM) {
			successCalled = true
			successIdx = idx
		}),
	)

	_, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !successCalled {
		t.Error("onSuccess should have been called")
	}
	if successIdx != 0 {
		t.Errorf("success index = %d, want 0", successIdx)
	}
}

func TestFallbackChain_GenerateContent(t *testing.T) {
	client := &mockFallbackLLM{
		genResp: &llms.Response{Content: testGenerated},
	}

	chain := NewFallbackChain([]llms.LLM{client})

	resp, err := chain.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != testGenerated {
		t.Errorf("content = %s, want 'generated'", resp.Content)
	}
}

func TestFallbackChain_Stream(t *testing.T) {
	client := &mockFallbackLLM{
		callResp: testStreamed,
	}

	chain := NewFallbackChain([]llms.LLM{client})

	stream, err := chain.Stream(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content string
	for chunk := range stream {
		content += chunk.Content
	}
	if content != testStreamed {
		t.Errorf("content = %s, want 'streamed'", content)
	}
}

func TestFallbackChain_GetProvider(t *testing.T) {
	client := &mockFallbackLLM{provider: llms.ProviderOpenAI}
	chain := NewFallbackChain([]llms.LLM{client})

	if chain.Provider() != llms.ProviderOpenAI {
		t.Errorf("provider = %v, want OpenAI", chain.Provider())
	}
}

func TestFallbackChain_GetModel(t *testing.T) {
	client := &mockFallbackLLM{model: testGPT4}
	chain := NewFallbackChain([]llms.LLM{client})

	if chain.Model() != testGPT4 {
		t.Errorf("model = %s, want 'gpt-4'", chain.Model())
	}
}

func TestFallbackChain_Clients(t *testing.T) {
	client1 := &mockFallbackLLM{}
	client2 := &mockFallbackLLM{}
	chain := NewFallbackChain([]llms.LLM{client1, client2})

	clients := chain.Clients()
	if len(clients) != 2 {
		t.Errorf("clients count = %d, want 2", len(clients))
	}
}

func TestFallbackChain_AddClient(t *testing.T) {
	client1 := &mockFallbackLLM{}
	chain := NewFallbackChain([]llms.LLM{client1})

	client2 := &mockFallbackLLM{}
	chain.AddClient(client2)

	if len(chain.Clients()) != 2 {
		t.Error("should have 2 clients after add")
	}
}

func TestFallbackChain_RemoveClient(t *testing.T) {
	client1 := &mockFallbackLLM{}
	client2 := &mockFallbackLLM{}
	chain := NewFallbackChain([]llms.LLM{client1, client2})

	if !chain.RemoveClient(0) {
		t.Error("remove should return true")
	}
	if len(chain.Clients()) != 1 {
		t.Error("should have 1 client after remove")
	}

	// Invalid index
	if chain.RemoveClient(5) {
		t.Error("remove with invalid index should return false")
	}
}

func TestFallbackChain_ConcurrentCallWithClientMutation(t *testing.T) {
	const (
		initialClients = 8
		readerCount    = 8
		callIterations = 1000
		mutations      = 1000
	)

	allClients := make([]llms.LLM, initialClients+mutations)
	for i := range allClients {
		allClients[i] = &mockFallbackLLM{
			provider: llms.ProviderOpenAI,
			model:    testGPT4,
			callErr:  &llms.APIError{StatusCode: 503, Message: "service unavailable"},
			genErr:   &llms.APIError{StatusCode: 503, Message: "service unavailable"},
		}
	}

	chain := NewFallbackChain(allClients[:initialClients],
		WithFallbackSelector(AlwaysFallbackSelector{}),
		WithOnFallback(func(_, _ int, from, to llms.LLM, _ error) {
			_ = from.Provider()
			_ = to.Provider()
		}),
	)

	var wg sync.WaitGroup
	wg.Add(readerCount + 1)

	for i := 0; i < readerCount; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callIterations; j++ {
				_, err := chain.Call(context.Background(), "test")
				if err != nil && !errors.Is(err, ErrAllClientsFailed) {
					t.Errorf("Call() error = %v, want ErrAllClientsFailed", err)
					return
				}
			}
		}()
	}

	go func() {
		defer wg.Done()
		for i := 0; i < mutations; i++ {
			if !chain.RemoveClient(0) {
				t.Error("RemoveClient(0) returned false")
				return
			}
			chain.AddClient(allClients[initialClients+i])
		}
	}()

	wg.Wait()
}

func TestFallbackChain_HealthTracking(t *testing.T) {
	client1 := &mockFallbackLLM{
		callErr: &llms.APIError{StatusCode: 500},
	}
	client2 := &mockFallbackLLM{
		callResp: testSuccess,
	}

	chain := NewFallbackChain([]llms.LLM{client1, client2})

	// First call - client1 fails, client2 succeeds
	_, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// client1 should be marked unhealthy
	if chain.IsClientHealthy(0) {
		t.Error("client1 should be unhealthy")
	}
	if !chain.IsClientHealthy(1) {
		t.Error("client2 should be healthy")
	}

	// Reset call counts
	atomic.StoreInt32(&client1.callCount, 0)
	atomic.StoreInt32(&client2.callCount, 0)

	// Second call - should skip unhealthy client1
	_, err = chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client1.CallCount() != 0 {
		t.Errorf("unhealthy client1 should be skipped, got %d calls", client1.CallCount())
	}
}

func TestFallbackChain_RecoveryAfterRestoresPrimary(t *testing.T) {
	clock := &fallbackTestClock{now: time.Unix(100, 0)}
	client1 := &mockFallbackLLM{
		provider: llms.ProviderOpenAI,
		callErr:  &llms.APIError{StatusCode: 503},
		callResp: "from primary",
	}
	client2 := &mockFallbackLLM{
		provider: llms.ProviderAnthropic,
		callResp: "from fallback",
	}

	chain := NewFallbackChain([]llms.LLM{client1, client2},
		WithRecoveryAfter(10*time.Millisecond),
		withFallbackClock(clock.Now),
	)

	result, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from fallback" {
		t.Errorf("result = %s, want fallback response", result)
	}
	if chain.IsClientHealthy(0) {
		t.Fatal("primary should be unhealthy after transient failure")
	}

	atomic.StoreInt32(&client1.callCount, 0)
	atomic.StoreInt32(&client2.callCount, 0)

	result, err = chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from fallback" {
		t.Errorf("result = %s, want fallback response before recovery", result)
	}
	if client1.CallCount() != 0 {
		t.Errorf("primary should be skipped during cooldown, got %d calls", client1.CallCount())
	}
	if client2.CallCount() != 1 {
		t.Errorf("fallback call count = %d, want 1", client2.CallCount())
	}

	client1.callErr = nil
	atomic.StoreInt32(&client1.callCount, 0)
	atomic.StoreInt32(&client2.callCount, 0)
	clock.Advance(10 * time.Millisecond)

	result, err = chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from primary" {
		t.Errorf("result = %s, want primary response after recovery", result)
	}
	if client1.CallCount() != 1 {
		t.Errorf("primary call count = %d, want 1", client1.CallCount())
	}
	if client2.CallCount() != 0 {
		t.Errorf("fallback should not be called after primary recovers, got %d calls", client2.CallCount())
	}
	if !chain.IsClientHealthy(0) {
		t.Error("primary should be healthy after successful half-open probe")
	}
}

func TestFallbackChain_WithRecoveryAfterHonored(t *testing.T) {
	clock := &fallbackTestClock{now: time.Unix(200, 0)}
	client := &mockFallbackLLM{
		callErr: &llms.APIError{StatusCode: 503},
	}
	chain := NewFallbackChain([]llms.LLM{client},
		WithRecoveryAfter(25*time.Millisecond),
		withFallbackClock(clock.Now),
	)

	_, err := chain.Call(context.Background(), "test")
	if err == nil {
		t.Fatal("expected failure")
	}
	if chain.IsClientHealthy(0) {
		t.Fatal("client should be unhealthy immediately after failure")
	}

	clock.Advance(24 * time.Millisecond)
	if chain.IsClientHealthy(0) {
		t.Error("client should remain unhealthy before configured recovery window")
	}

	clock.Advance(time.Millisecond)
	if !chain.IsClientHealthy(0) {
		t.Error("client should be healthy once configured recovery window elapses")
	}
}

func TestFallbackChain_AllClientsInCooldownStillAttempts(t *testing.T) {
	clock := &fallbackTestClock{now: time.Unix(300, 0)}
	client1 := &mockFallbackLLM{callResp: "from primary"}
	client2 := &mockFallbackLLM{callResp: "from fallback"}
	chain := NewFallbackChain([]llms.LLM{client1, client2},
		WithRecoveryAfter(time.Minute),
		withFallbackClock(clock.Now),
	)

	chain.SetClientHealthy(0, false)
	chain.SetClientHealthy(1, false)

	result, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from primary" {
		t.Errorf("result = %s, want primary response", result)
	}
	if client1.CallCount() != 1 {
		t.Errorf("primary should be attempted when all clients are cooling down, got %d calls", client1.CallCount())
	}
	if client2.CallCount() != 0 {
		t.Errorf("fallback should not be needed after primary succeeds, got %d calls", client2.CallCount())
	}
}

func TestFallbackChain_SetClientHealthy(t *testing.T) {
	client := &mockFallbackLLM{}
	chain := NewFallbackChain([]llms.LLM{client})

	chain.SetClientHealthy(0, false)
	if chain.IsClientHealthy(0) {
		t.Error("client should be unhealthy")
	}

	chain.SetClientHealthy(0, true)
	if !chain.IsClientHealthy(0) {
		t.Error("client should be healthy")
	}
}

func TestFallbackChain_ResetHealth(t *testing.T) {
	client1 := &mockFallbackLLM{}
	client2 := &mockFallbackLLM{}
	chain := NewFallbackChain([]llms.LLM{client1, client2})

	chain.SetClientHealthy(0, false)
	chain.SetClientHealthy(1, false)

	chain.ResetHealth()

	if !chain.IsClientHealthy(0) || !chain.IsClientHealthy(1) {
		t.Error("all clients should be healthy after reset")
	}
}

func TestFallbackChain_CustomSelector(t *testing.T) {
	client1 := &mockFallbackLLM{
		callErr: errors.New("custom error"),
	}
	client2 := &mockFallbackLLM{
		callResp: testSuccess,
	}

	// Custom selector that falls back on any error
	chain := NewFallbackChain([]llms.LLM{client1, client2},
		WithFallbackSelector(AlwaysFallbackSelector{}),
	)

	result, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != testSuccess {
		t.Errorf("result = %s, want 'success'", result)
	}
}

func TestFallbackChain_CircuitOpenTriggersFallback(t *testing.T) {
	client1 := &mockFallbackLLM{
		callErr: ErrCircuitOpen,
	}
	client2 := &mockFallbackLLM{
		callResp: testSuccess,
	}

	chain := NewFallbackChain([]llms.LLM{client1, client2})

	result, err := chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != testSuccess {
		t.Errorf("result = %s, want 'success'", result)
	}
}

func TestWeightedFallbackChain(t *testing.T) {
	client1 := &mockFallbackLLM{provider: llms.ProviderOpenAI, model: "low-priority"}
	client2 := &mockFallbackLLM{provider: llms.ProviderAnthropic, model: "high-priority", callResp: testSuccess}
	client3 := &mockFallbackLLM{provider: llms.ProviderGemini, model: "medium-priority"}

	// client2 has highest weight, should be tried first
	chain, err := NewWeightedFallbackChain(
		[]llms.LLM{client1, client2, client3},
		[]int{1, 10, 5},
	)
	if err != nil {
		t.Fatalf("failed to create weighted chain: %v", err)
	}

	_, err = chain.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// client2 should be first due to highest weight
	if client2.CallCount() != 1 {
		t.Errorf("high-priority client should be called first")
	}
	if client1.CallCount() != 0 || client3.CallCount() != 0 {
		t.Error("other clients should not be called")
	}

	// Verify weights are sorted
	weights := chain.Weights()
	if weights[0] != 10 || weights[1] != 5 || weights[2] != 1 {
		t.Errorf("weights = %v, want [10, 5, 1]", weights)
	}
}

func TestFallbackChain_EmptyProviderAndModel(t *testing.T) {
	chain := NewFallbackChain(nil)

	if chain.Provider() != "" {
		t.Errorf("provider = %s, want empty", chain.Provider())
	}
	if chain.Model() != "" {
		t.Errorf("model = %s, want empty", chain.Model())
	}
}
