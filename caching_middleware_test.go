package llms

import (
	"context"
	"testing"
	"time"
)

func TestCachedClient_HitAndMiss(t *testing.T) {
	mock := NewMockLLM(WithMockGenerateResponse(&Response{Content: "cached"}))
	client := NewCachedClient(mock)

	msgs := []Message{{Role: RoleUser, Content: "hello"}}

	// First call: miss → wrapped LLM invoked.
	r1, err := client.GenerateContent(context.Background(), msgs)
	if err != nil || r1.Content != "cached" {
		t.Fatalf("first call: resp=%v err=%v", r1, err)
	}
	// Second identical call: hit → wrapped LLM NOT invoked again.
	r2, err := client.GenerateContent(context.Background(), msgs)
	if err != nil || r2.Content != "cached" {
		t.Fatalf("second call: resp=%v err=%v", r2, err)
	}
	if mock.callCount != 1 {
		t.Errorf("wrapped callCount = %d, want 1 (second call should hit cache)", mock.callCount)
	}

	// Different message content → different key → miss.
	_, err = client.GenerateContent(context.Background(), []Message{{Role: RoleUser, Content: "different"}})
	if err != nil {
		t.Fatal(err)
	}
	if mock.callCount != 2 {
		t.Errorf("wrapped callCount = %d, want 2 (different request misses)", mock.callCount)
	}
}

func TestCachedClient_OptionsAffectKey(t *testing.T) {
	mock := NewMockLLM(WithMockGenerateResponse(&Response{Content: "x"}))
	client := NewCachedClient(mock)
	msgs := []Message{{Role: RoleUser, Content: "hi"}}

	_, _ = client.GenerateContent(context.Background(), msgs, WithTemperature(0))
	_, _ = client.GenerateContent(context.Background(), msgs, WithTemperature(0.9))
	if mock.callCount != 2 {
		t.Errorf("callCount = %d, want 2 (different temperature must miss)", mock.callCount)
	}
	// Same temperature again hits.
	_, _ = client.GenerateContent(context.Background(), msgs, WithTemperature(0))
	if mock.callCount != 2 {
		t.Errorf("callCount = %d, want 2 (repeat of temp=0 should hit)", mock.callCount)
	}
}

func TestCachedClient_ErrorsNotCached(t *testing.T) {
	mock := NewMockLLM(WithMockGenerateError(context.DeadlineExceeded))
	client := NewCachedClient(mock)
	msgs := []Message{{Role: RoleUser, Content: "hi"}}

	if _, err := client.GenerateContent(context.Background(), msgs); err == nil {
		t.Fatal("expected error")
	}
	if _, err := client.GenerateContent(context.Background(), msgs); err == nil {
		t.Fatal("expected error")
	}
	if mock.callCount != 2 {
		t.Errorf("callCount = %d, want 2 (errors must not be cached)", mock.callCount)
	}
}

func TestCachedClient_ReturnsCopy(t *testing.T) {
	mock := NewMockLLM(WithMockGenerateResponse(&Response{Content: "orig"}))
	client := NewCachedClient(mock)
	msgs := []Message{{Role: RoleUser, Content: "hi"}}

	r1, _ := client.GenerateContent(context.Background(), msgs)
	r1.Content = "mutated" // caller mutation must not poison the cache
	r2, _ := client.GenerateContent(context.Background(), msgs)
	if r2.Content != "orig" {
		t.Errorf("cached response mutated via caller: got %q, want %q", r2.Content, "orig")
	}
}

func TestCachedClient_UnwrapAndIdentity(t *testing.T) {
	mock := NewMockLLM()
	client := NewCachedClient(mock)
	if client.Unwrap() != mock {
		t.Error("Unwrap did not return the wrapped LLM")
	}
	if UnwrapAll(client) != mock {
		t.Error("UnwrapAll did not reach the base LLM")
	}
	if client.Provider() != mock.Provider() || client.Model() != mock.Model() {
		t.Error("Provider/Model not forwarded")
	}
}

func TestMemoryResponseCache_TTLExpiry(t *testing.T) {
	cache := NewMemoryResponseCache(time.Minute)
	now := time.Unix(0, 0)
	cache.now = func() time.Time { return now }

	ctx := context.Background()
	cache.Set(ctx, "k", &Response{Content: "v"})
	if _, ok := cache.Get(ctx, "k"); !ok {
		t.Fatal("expected hit immediately after Set")
	}

	now = now.Add(2 * time.Minute) // advance past TTL
	if _, ok := cache.Get(ctx, "k"); ok {
		t.Error("expected miss after TTL expiry")
	}
	if cache.Len() != 0 {
		t.Errorf("expired entry not evicted on Get: Len=%d", cache.Len())
	}
}

func TestMemoryResponseCache_NoExpiryWhenTTLZero(t *testing.T) {
	cache := NewMemoryResponseCache(0)
	now := time.Unix(0, 0)
	cache.now = func() time.Time { return now }
	ctx := context.Background()

	cache.Set(ctx, "k", &Response{Content: "v"})
	now = now.Add(1000 * time.Hour)
	if _, ok := cache.Get(ctx, "k"); !ok {
		t.Error("entry expired despite TTL=0 (never expire)")
	}
}
