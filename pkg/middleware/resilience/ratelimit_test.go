package resilience

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
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

func TestRateLimiter_RefundIsConsumedByNextRequest(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(100),
		WithRequestBurst(100),
		WithTokensPerMinute(300),
		WithTokenEstimate(200),
		WithBlocking(false),
	)

	if err := rl.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	rl.RecordTokens(100)
	if got := rl.TokensRemaining(); got < 200 {
		t.Fatalf("tokens remaining after refund = %d, want at least one request estimate", got)
	}

	// The first request was refunded 100 tokens even though the bucket is not
	// rebuilt; a 200-token second request should fit because the refund offsets
	// half of it. The third then has no credit and must wait.
	if err := rl.Wait(context.Background()); err != nil {
		t.Fatalf("second request should consume the refund: %v", err)
	}
	if err := rl.Wait(context.Background()); !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("third request = %v, want ErrRateLimitExceeded", err)
	}
}

func TestRateLimiter_RecordTokensIgnoresNegativeUsage(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(600), WithTokenEstimate(200))
	before := rl.TokensRemaining()
	rl.RecordTokens(-5)
	if got := rl.TokensRemaining(); got != before {
		t.Fatalf("TokensRemaining = %d after negative usage, want %d", got, before)
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

// A request count above the limiter's burst is a permanent refusal. Clamping it
// to one would admit a call that asked for two or more while charging one,
// bypassing the configured request rate.
func TestRateLimiter_WaitN_RejectsRequestsAboveBurst(t *testing.T) {
	rl := NewRateLimiter(WithRequestsPerMinute(6000), WithRequestBurst(2), WithBlocking(true), WithWaitTimeout(time.Second))
	if err := rl.WaitN(context.Background(), 5, 0); !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("WaitN with requests above the burst = %v, want ErrRateLimitExceeded", err)
	}
}

// The non-blocking path must not clamp an oversized request count either: doing
// so would admit the call while charging less than the caller asked for.
func TestRateLimiter_TryAcquire_RejectsRequestsAboveBurst(t *testing.T) {
	rl := NewRateLimiter(WithRequestsPerMinute(6000), WithRequestBurst(2), WithBlocking(false))
	if err := rl.WaitN(context.Background(), 5, 0); !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("tryAcquire with requests above the burst = %v, want ErrRateLimitExceeded", err)
	}
}

// An oversized token charge on the non-blocking path must be financed like the
// blocking path: reserve the burst-sized part and carry the remainder as debt.
// Rejecting outright made a limiter whose burst sits below the estimate refuse
// every request forever, and clamping silently charged less than the caller
// asked for. The debt must show up as a later rejection once the burst refills.
func TestRateLimiter_TryAcquire_FinancesTokensAboveBurst(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(6000),
		WithTokensPerMinute(600),
		WithTokenBurst(10),
		WithBlocking(false),
	)
	if err := rl.WaitN(context.Background(), 1, 1000); err != nil {
		t.Fatalf("tryAcquire with tokens above the burst = %v, want success with debt", err)
	}
	rl.mu.RLock()
	debt := rl.tokenDebt
	rl.mu.RUnlock()
	if debt != 990 {
		t.Fatalf("tokenDebt after an oversized charge = %d, want 990", debt)
	}
	// The debt is settled before admission, so the very next call fails even
	// though the burst was only just drained.
	if err := rl.WaitN(context.Background(), 1, 1); !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("tryAcquire with outstanding debt = %v, want ErrRateLimitExceeded", err)
	}
}

// TestRateLimiter_TokenBurstCoversEstimate pins that a token budget smaller
// than the per-request estimate is still charged in full. Clamping the charge
// to a burst below the estimate let a 500/min limiter bill only 500 tokens per
// 1000-token request, doubling the effective rate.
func TestRateLimiter_TokenBurstCoversEstimate(t *testing.T) {
	rl := NewRateLimiter(
		WithTokensPerMinute(500),
		WithTokenEstimate(1000),
	)
	if got := rl.tokenBucketBurst(); got < 1000 {
		t.Fatalf("tokenBucketBurst() = %d, want at least the 1000-token estimate", got)
	}
}

// A request whose token wait fails must not consume request capacity. The
// blocking path used to spend a request token before discovering the token
// bucket could not be met, so a call that never started pushed the next request
// over the request limit.
func TestRateLimiter_TokenWaitFailureRefundsRequestCapacity(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(6000),
		WithRequestBurst(2),
		WithTokensPerMinute(600),
		WithTokenEstimate(1000),
		WithBlocking(true),
		WithWaitTimeout(20*time.Millisecond),
	)

	// Exhaust the token bucket so the next token wait must block past the wait
	// timeout. The request bucket still has capacity.
	if err := rl.WaitN(context.Background(), 1, 600); err != nil {
		t.Fatalf("initial WaitN failed: %v", err)
	}
	if err := rl.WaitN(context.Background(), 1, 600); !errors.Is(err, ErrRateLimitTimeout) {
		t.Fatalf("second WaitN = %v, want ErrRateLimitTimeout", err)
	}

	// The failed call must not have spent request capacity: one request should
	// still be immediately available.
	if err := rl.WaitN(context.Background(), 1, 0); err != nil {
		t.Fatalf("request capacity was consumed by the failed token wait: %v", err)
	}
}

// The non-blocking path must not spend request capacity when the token bucket
// is exhausted.
func TestRateLimiter_TryAcquire_TokenFailureRefundsRequestCapacity(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(6000),
		WithRequestBurst(2),
		WithTokensPerMinute(600),
		WithTokenEstimate(1000),
		WithBlocking(false),
	)
	if err := rl.WaitN(context.Background(), 1, 600); err != nil {
		t.Fatalf("initial WaitN failed: %v", err)
	}
	if err := rl.WaitN(context.Background(), 1, 600); !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("second WaitN = %v, want ErrRateLimitExceeded", err)
	}
	if err := rl.WaitN(context.Background(), 1, 0); err != nil {
		t.Fatalf("request capacity was consumed by the failed token acquire: %v", err)
	}
}

// A negative token count must be rejected, not treated as "no token charge"
// that silently bypasses the configured token rate.
func TestRateLimiter_WaitN_RejectsNegativeTokens(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(600))
	if err := rl.WaitN(context.Background(), 1, -1); err == nil {
		t.Fatal("WaitN with negative tokens succeeded, want error")
	}
}

func TestRateLimiter_WaitN_NonBlockingRejectsCanceledContext(t *testing.T) {
	rl := NewRateLimiter(WithRequestsPerMinute(6000), WithBlocking(false))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rl.WaitN(ctx, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitN = %v, want context.Canceled", err)
	}
}

// The non-blocking path must apply token credit before the burst check: 1000
// credits make a 1500-token request effectively 500, which fits a 1000 burst.
func TestRateLimiter_TryAcquire_AppliesCreditBeforeBurstCheck(t *testing.T) {
	rl := NewRateLimiter(
		WithRequestsPerMinute(6000),
		WithRequestBurst(2),
		WithTokensPerMinute(60000),
		WithTokenBurst(1000),
		WithBlocking(false),
	)
	rl.mu.Lock()
	rl.tokenCredit = 1000
	rl.mu.Unlock()

	if err := rl.WaitN(context.Background(), 1, 1500); err != nil {
		t.Fatalf("WaitN with credit applied = %v, want success", err)
	}
}

// A reported usage near math.MaxInt must not overflow the overflow calculation
// and skip accounting entirely.
func TestRateLimiter_RecordTokens_LargeUsageDoesNotWrap(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(60000), WithTokenBurst(1000), WithTokenEstimate(1))
	rl.RecordTokens(math.MaxInt)
	rl.mu.RLock()
	debt := rl.tokenDebt
	rl.mu.RUnlock()
	if debt <= 0 {
		t.Fatalf("tokenDebt after a MaxInt usage report = %d, want a positive debt", debt)
	}
}

// Repeated large usage reports must saturate debt instead of wrapping negative,
// which would make later requests look settled and bypass the limiter.
func TestRateLimiter_RecordTokens_DebtSaturates(t *testing.T) {
	rl := NewRateLimiter(WithTokensPerMinute(60000), WithTokenBurst(1000), WithTokenEstimate(0))
	rl.tokenDebt = maxInt() - 1
	rl.RecordTokens(math.MaxInt)
	rl.mu.RLock()
	debt := rl.tokenDebt
	rl.mu.RUnlock()
	if debt < 0 {
		t.Fatalf("tokenDebt overflowed to %d, want a non-negative saturated debt", debt)
	}
	if debt != maxInt() {
		t.Fatalf("tokenDebt = %d, want saturation at %d", debt, maxInt())
	}
}

// A usage report whose read of the limiter overlapped a Reset must not settle
// against the freshly reset state: its debt or credit belongs to the replaced
// bucket, and Reset is the operator's way to start clean.
func TestRateLimiter_RecordTokensDiscardsStaleGeneration(t *testing.T) {
	rl := NewRateLimiter(
		WithTokensPerMinute(600),
		WithTokenBurst(100),
		WithTokenEstimate(100),
	)
	_, staleLimiter, staleGen := rl.snapshotLimiters()

	rl.Reset()

	// An over-estimate overflow from before the Reset.
	rl.recordTokens(1_000_000, staleLimiter, staleGen)
	// A refund from before the Reset.
	rl.recordTokens(10, staleLimiter, staleGen)

	rl.mu.RLock()
	debt := rl.tokenDebt
	credit := rl.tokenCredit
	rl.mu.RUnlock()
	if debt != 0 || credit != 0 {
		t.Fatalf("stale usage adjustments leaked past Reset: debt=%d credit=%d, want 0 and 0", debt, credit)
	}
}
