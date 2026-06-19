package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

const (
	testSuccess = "success"
	testGPT4    = "gpt-4"
)

func TestNewRateLimiter_Defaults(t *testing.T) {
	rl := NewRateLimiter()

	if rl.requestsPerMin != 60 {
		t.Errorf("requestsPerMin = %d, want 60", rl.requestsPerMin)
	}
	if rl.requestBurst != 1 {
		t.Errorf("requestBurst = %d, want 1", rl.requestBurst)
	}
	if rl.requestBurst == rl.requestsPerMin {
		t.Error("default request burst should not equal requestsPerMin")
	}
	if rl.tokensPerMin != 0 {
		t.Errorf("tokensPerMin = %d, want 0 (disabled)", rl.tokensPerMin)
	}
	if !rl.blocking {
		t.Error("blocking should be true by default")
	}
}

func TestNewRateLimiter_CustomOptions(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(120),
		WithRequestBurst(3),
		WithTokensPerMinute(100000),
		WithTokenEstimate(500),
		WithBlocking(false),
		WithWaitTimeout(10*time.Second),
	)

	if rl.requestsPerMin != 120 {
		t.Errorf("requestsPerMin = %d, want 120", rl.requestsPerMin)
	}
	if rl.requestBurst != 3 {
		t.Errorf("requestBurst = %d, want 3", rl.requestBurst)
	}
	if rl.tokensPerMin != 100000 {
		t.Errorf("tokensPerMin = %d, want 100000", rl.tokensPerMin)
	}
	if rl.tokenEstimate != 500 {
		t.Errorf("tokenEstimate = %d, want 500", rl.tokenEstimate)
	}
	if rl.blocking {
		t.Error("blocking should be false")
	}
}

func TestRateLimiter_Wait_AllowsRequests(t *testing.T) {
	rl := NewRateLimiter(WithRequestsPerMinute(60))

	ctx := context.Background()
	err := rl.Wait(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRateLimiter_Wait_BlocksWhenExceeded(t *testing.T) {
	// Very low rate - 1 request per minute
	rl := NewRateLimiter(
		WithRequestsPerMinute(1),
		WithWaitTimeout(100*time.Millisecond),
	)

	ctx := context.Background()

	// First request should succeed
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("first request should succeed: %v", err)
	}

	// Second request should timeout
	// Note: The rate limiter may return immediately if it knows the wait
	// would exceed the deadline - this is correct behavior
	err := rl.Wait(ctx)

	if !errors.Is(err, ErrRateLimitTimeout) {
		t.Errorf("expected ErrRateLimitTimeout, got %v", err)
	}
}

func TestRateLimiter_NonBlocking(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(1),
		WithBlocking(false),
	)

	ctx := context.Background()

	// First request should succeed
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("first request should succeed: %v", err)
	}

	// Second request should fail immediately
	start := time.Now()
	err := rl.Wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Errorf("expected ErrRateLimitExceeded, got %v", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("should not wait in non-blocking mode, elapsed = %v", elapsed)
	}
}

func TestRateLimiter_TokenLimit(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(100),
		WithRequestBurst(100),
		WithTokensPerMinute(1000),
		WithTokenEstimate(500),
		WithBlocking(false),
	)

	ctx := context.Background()

	// First request (500 tokens) should succeed
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("first request should succeed: %v", err)
	}

	// Second request (500 tokens) should succeed
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("second request should succeed: %v", err)
	}

	// Third request should fail (over token limit)
	err := rl.Wait(ctx)
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Errorf("expected ErrRateLimitExceeded, got %v", err)
	}
}

func TestRateLimiter_RecordTokens(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(100),
		WithRequestBurst(100),
		WithTokensPerMinute(1000),
		WithTokenEstimate(100), // Estimate low
		WithBlocking(false),
	)

	ctx := context.Background()

	// Make request with low estimate
	if err := rl.Wait(ctx); err != nil {
		t.Fatal(err)
	}

	// Record higher actual usage
	rl.RecordTokens(500)

	// Tokens remaining should be lower now
	remaining := rl.TokensRemaining()
	// Should have used ~500 tokens (100 estimated + 400 extra)
	if remaining > 600 {
		t.Errorf("remaining = %d, expected less after recording extra tokens", remaining)
	}
}

func TestRateLimiter_RecordTokensRefundsOverEstimate(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(100),
		WithRequestBurst(100),
		WithTokensPerMinute(1000),
		WithTokenEstimate(800),
		WithBlocking(false),
	)

	if err := rl.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	beforeRefund := rl.TokensRemaining()

	rl.RecordTokens(300)

	afterRefund := rl.TokensRemaining()
	if afterRefund <= beforeRefund {
		t.Fatalf("tokens remaining = %d, want greater than before refund %d", afterRefund, beforeRefund)
	}
	if afterRefund < 650 {
		t.Fatalf("tokens remaining = %d, want refund close to 700", afterRefund)
	}
}

func TestRateLimiter_RequestsRemaining(t *testing.T) {
	rl := NewRateLimiter(WithRequestsPerMinute(10), WithRequestBurst(10))

	initial := rl.RequestsRemaining()
	if initial != 10 {
		t.Errorf("initial remaining = %d, want 10", initial)
	}

	_ = rl.Wait(context.Background())

	after := rl.RequestsRemaining()
	if after != 9 {
		t.Errorf("after one request remaining = %d, want 9", after)
	}
}

func TestRateLimiter_TokensRemaining_Disabled(t *testing.T) {
	rl := NewRateLimiter() // No token limit

	remaining := rl.TokensRemaining()
	if remaining != -1 {
		t.Errorf("tokens remaining = %d, want -1 (disabled)", remaining)
	}
}

func TestRateLimiter_Reset(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(2),
		WithRequestBurst(2),
		WithBlocking(false),
	)

	ctx := context.Background()

	// Use up all tokens
	_ = rl.Wait(ctx)
	_ = rl.Wait(ctx)

	// Should be rate limited
	if err := rl.Wait(ctx); !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatal("should be rate limited")
	}

	// Reset
	rl.Reset()

	// Should work again
	if err := rl.Wait(ctx); err != nil {
		t.Errorf("should work after reset: %v", err)
	}
}

func TestRateLimiter_ContextCancellation(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(1),
		WithWaitTimeout(1*time.Second),
	)

	ctx := context.Background()

	// Use up the token
	_ = rl.Wait(ctx)

	// Create canceled context
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	// Should return immediately with context error
	err := rl.Wait(cancelCtx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// mockRateLimitLLM is a mock LLM for testing rate limited client
type mockRateLimitLLM struct {
	provider  llms.Provider
	model     string
	callResp  string
	genResp   *llms.Response
	callCount int32
}

func (m *mockRateLimitLLM) Call(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
	atomic.AddInt32(&m.callCount, 1)
	return m.callResp, nil
}

func (m *mockRateLimitLLM) GenerateContent(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (*llms.Response, error) {
	atomic.AddInt32(&m.callCount, 1)
	if m.genResp != nil {
		return m.genResp, nil
	}
	return &llms.Response{Content: m.callResp, Usage: llms.Usage{TotalTokens: 100}}, nil
}

func (m *mockRateLimitLLM) Stream(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	atomic.AddInt32(&m.callCount, 1)
	ch := make(chan llms.StreamChunk, 2)
	ch <- llms.StreamChunk{Content: m.callResp}
	ch <- llms.StreamChunk{Usage: &llms.Usage{TotalTokens: 50}}
	close(ch)
	return ch, nil
}

func (m *mockRateLimitLLM) Provider() llms.Provider { return m.provider }
func (m *mockRateLimitLLM) Model() string           { return m.model }
func (m *mockRateLimitLLM) CallCount() int          { return int(atomic.LoadInt32(&m.callCount)) }

func TestRateLimitedClient_Call(t *testing.T) {
	llm := &mockRateLimitLLM{callResp: testSuccess}
	client := NewRateLimitedClient(llm, WithRequestsPerMinute(60))

	result, err := client.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != testSuccess {
		t.Errorf("result = %s, want %q", result, testSuccess)
	}
}

func TestRateLimitedClient_GenerateContent(t *testing.T) {
	llm := &mockRateLimitLLM{
		genResp: &llms.Response{Content: "generated", Usage: llms.Usage{TotalTokens: 150}},
	}
	client := NewRateLimitedClient(llm, WithRequestsPerMinute(60))

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

func TestRateLimitedClient_Stream(t *testing.T) {
	llm := &mockRateLimitLLM{callResp: "streamed"}
	client := NewRateLimitedClient(llm, WithRequestsPerMinute(60))

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

func TestRateLimitedClient_RateLimited(t *testing.T) {
	llm := &mockRateLimitLLM{callResp: testSuccess}
	client := NewRateLimitedClient(llm,
		WithRequestsPerMinute(1),
		WithBlocking(false),
	)

	// First call should succeed
	_, err := client.Call(context.Background(), "test")
	if err != nil {
		t.Fatalf("first call should succeed: %v", err)
	}

	// Second call should be rate limited
	_, err = client.Call(context.Background(), "test")
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Errorf("expected ErrRateLimitExceeded, got %v", err)
	}

	// LLM should only have been called once
	if llm.CallCount() != 1 {
		t.Errorf("call count = %d, want 1", llm.CallCount())
	}
}

func TestRateLimitedClient_GetProvider(t *testing.T) {
	llm := &mockRateLimitLLM{provider: llms.ProviderOpenAI}
	client := NewRateLimitedClient(llm)

	if client.Provider() != llms.ProviderOpenAI {
		t.Errorf("provider = %v, want OpenAI", client.Provider())
	}
}

func TestRateLimitedClient_GetModel(t *testing.T) {
	llm := &mockRateLimitLLM{model: testGPT4}
	client := NewRateLimitedClient(llm)

	if client.Model() != testGPT4 {
		t.Errorf("model = %s, want %q", client.Model(), testGPT4)
	}
}

func TestRateLimitedClient_Limiter(t *testing.T) {
	client := NewRateLimitedClient(&mockRateLimitLLM{})

	if client.Limiter() == nil {
		t.Error("Limiter() should not return nil")
	}
}

func TestRateLimitedClient_Unwrap(t *testing.T) {
	llm := &mockRateLimitLLM{}
	client := NewRateLimitedClient(llm)

	if client.Unwrap() != llm {
		t.Error("Unwrap should return underlying LLM")
	}
}

func TestRateLimitedClientWithLimiter(t *testing.T) {
	llm := &mockRateLimitLLM{}
	limiter := NewRateLimiter(WithRequestsPerMinute(100))

	client := NewRateLimitedClientWithLimiter(llm, limiter)

	if client.Limiter() != limiter {
		t.Error("should use provided limiter")
	}
}

func TestProviderRateLimits(t *testing.T) {
	// Check that known providers have limits defined
	providers := []llms.Provider{llms.ProviderOpenAI, llms.ProviderAnthropic, llms.ProviderGemini, llms.ProviderTogetherAI}

	for _, p := range providers {
		limits, ok := ProviderRateLimits[p]
		if !ok {
			t.Errorf("missing rate limits for %s", p)
			continue
		}
		if limits.RequestsPerMinute <= 0 {
			t.Errorf("%s has invalid RequestsPerMinute: %d", p, limits.RequestsPerMinute)
		}
	}
}

func TestNewProviderRateLimitedClient(t *testing.T) {
	llm := &mockRateLimitLLM{provider: llms.ProviderOpenAI}
	client := NewProviderRateLimitedClient(llm)

	limiter := client.Limiter()
	if limiter.requestsPerMin != ProviderRateLimits[llms.ProviderOpenAI].RequestsPerMinute {
		t.Errorf("requests/min = %d, want %d",
			limiter.requestsPerMin, ProviderRateLimits[llms.ProviderOpenAI].RequestsPerMinute)
	}
}

func TestNewProviderRateLimitedClient_Override(t *testing.T) {
	llm := &mockRateLimitLLM{provider: llms.ProviderOpenAI}
	client := NewProviderRateLimitedClient(llm, WithRequestsPerMinute(999))

	limiter := client.Limiter()
	if limiter.requestsPerMin != 999 {
		t.Errorf("requests/min = %d, want 999 (overridden)", limiter.requestsPerMin)
	}
}

func TestSharedRateLimiter(t *testing.T) {
	shared := NewSharedRateLimiter()

	// Get limiter for a key
	limiter1 := shared.GetLimiter("test-key", WithRequestsPerMinute(100))
	limiter2 := shared.GetLimiter("test-key")

	if limiter1 != limiter2 {
		t.Error("should return same limiter for same key")
	}

	limiter3 := shared.GetLimiter("other-key")
	if limiter1 == limiter3 {
		t.Error("should return different limiter for different key")
	}
}

func TestSharedRateLimiter_GetProviderLimiter(t *testing.T) {
	shared := NewSharedRateLimiter()

	limiter := shared.GetProviderLimiter(llms.ProviderOpenAI)
	if limiter.requestsPerMin != ProviderRateLimits[llms.ProviderOpenAI].RequestsPerMinute {
		t.Error("should use provider default limits")
	}

	// Should return same limiter
	limiter2 := shared.GetProviderLimiter(llms.ProviderOpenAI)
	if limiter != limiter2 {
		t.Error("should return same limiter for same provider")
	}
}

func TestSharedRateLimiter_RemoveLimiter(t *testing.T) {
	shared := NewSharedRateLimiter()

	limiter1 := shared.GetLimiter("test-key")
	shared.RemoveLimiter("test-key")
	limiter2 := shared.GetLimiter("test-key")

	if limiter1 == limiter2 {
		t.Error("should create new limiter after removal")
	}
}

func TestSharedRateLimiter_ResetAll(t *testing.T) {
	shared := NewSharedRateLimiter()

	// Create and exhaust a limiter
	limiter := shared.GetLimiter("test", WithRequestsPerMinute(1), WithBlocking(false))
	_ = limiter.Wait(context.Background())

	// Should be rate limited
	if err := limiter.Wait(context.Background()); !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatal("should be rate limited")
	}

	// Reset all
	shared.ResetAll()

	// Should work again
	if err := limiter.Wait(context.Background()); err != nil {
		t.Errorf("should work after reset: %v", err)
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(WithRequestsPerMinute(1000), WithRequestBurst(1000))

	var wg sync.WaitGroup
	var successCount int32
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := rl.Wait(ctx); err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	// All should succeed with high enough limit
	if successCount != 100 {
		t.Errorf("success count = %d, want 100", successCount)
	}
}

// TestRateLimiter_ResetConcurrentWithReads exercises the data race between
// Reset (which swaps the internal limiter pointers) and the lock-free readers
// (Wait, RequestsRemaining, TokensRemaining, RecordTokens). It must be clean
// under `go test -race`.
func TestRateLimiter_ResetConcurrentWithReads(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(100000),
		WithTokensPerMinute(100000),
		WithBlocking(false),
	)

	ctx := context.Background()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Resetters: continuously swap the limiter pointers.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					rl.Reset()
				}
			}
		}()
	}

	// Readers: concurrently call the pointer-reading methods.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = rl.Wait(ctx)
					_ = rl.RequestsRemaining()
					_ = rl.TokensRemaining()
					rl.RecordTokens(2000)
				}
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestRateLimiter_TokenBurst covers WithTokenBurst (#25): the token bucket
// defaults to a full minute's budget but is configurable for tighter pacing.
func TestRateLimiter_TokenBurst(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(1000))
	if got := rl.tokenBucketBurst(); got != 1000 {
		t.Errorf("default tokenBucketBurst() = %d, want 1000 (full minute)", got)
	}

	rl2 := NewRateLimiter(WithTokensPerMinute(1000), WithTokenBurst(100))
	if got := rl2.tokenBucketBurst(); got != 100 {
		t.Errorf("tokenBucketBurst() = %d, want 100", got)
	}
	if rl2.tokenLimiter == nil || rl2.tokenLimiter.Burst() != 100 {
		t.Errorf("token limiter burst not applied: %#v", rl2.tokenLimiter)
	}
}
