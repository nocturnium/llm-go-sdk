package llms

import (
	"context"
	"fmt"
	"reflect"
	"sync"
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
	// A response with every mutable reference field populated. The mock returns
	// this same pointer on every call, so a shallow-copying cache would alias its
	// slices/pointers/map across the cache entry and every hit.
	base := &Response{
		Content:       "orig",
		ToolCalls:     []ToolCall{{ID: "c1", Type: ToolTypeFunction, Function: &FunctionCall{Name: "f", Arguments: `{"a":1}`}}},
		SearchResults: []SearchResult{{Title: "t", URL: "u", Snippet: "s"}},
		Reasoning:     &ReasoningContent{Content: "why", Metadata: map[string]any{"k": "v"}},
	}
	client := NewCachedClient(NewMockLLM(WithMockGenerateResponse(base)))
	msgs := []Message{{Role: RoleUser, Content: "hi"}}

	// Populate the cache (miss), then take a hit and mutate every reference field
	// on the returned value.
	_, _ = client.GenerateContent(context.Background(), msgs)
	r1, _ := client.GenerateContent(context.Background(), msgs)
	r1.Content = "mutated"
	r1.ToolCalls[0].Function.Arguments = "POISON"
	r1.SearchResults[0].Title = "POISON"
	r1.Reasoning.Content = "POISON"
	r1.Reasoning.Metadata["k"] = "POISON"

	// A fresh hit must be pristine — the mutation above must not have poisoned the
	// cached entry (which a shallow copy would allow via shared backing arrays).
	r2, _ := client.GenerateContent(context.Background(), msgs)
	if r2.Content != "orig" {
		t.Errorf("Content poisoned: got %q", r2.Content)
	}
	if r2.ToolCalls[0].Function.Arguments != `{"a":1}` {
		t.Errorf("ToolCalls.Function poisoned: got %q", r2.ToolCalls[0].Function.Arguments)
	}
	if r2.SearchResults[0].Title != "t" {
		t.Errorf("SearchResults poisoned: got %q", r2.SearchResults[0].Title)
	}
	if r2.Reasoning.Content != "why" {
		t.Errorf("Reasoning.Content poisoned: got %q", r2.Reasoning.Content)
	}
	if r2.Reasoning.Metadata["k"] != "v" {
		t.Errorf("Reasoning.Metadata poisoned: got %v", r2.Reasoning.Metadata["k"])
	}
}

// TestCachedClient_ConcurrentHitsIsolated exercises the -race detector: many
// goroutines take the same cache hit and mutate their returned copy's reference
// fields. If Get handed back aliased slices/maps, these mutations would race.
func TestCachedClient_ConcurrentHitsIsolated(t *testing.T) {
	base := &Response{
		Content:   "orig",
		ToolCalls: []ToolCall{{ID: "c1", Type: ToolTypeFunction, Function: &FunctionCall{Arguments: "orig"}}},
		Reasoning: &ReasoningContent{Content: "r", Metadata: map[string]any{"k": "v"}},
	}
	client := NewCachedClient(NewMockLLM(WithMockGenerateResponse(base)))
	msgs := []Message{{Role: RoleUser, Content: "hi"}}
	_, _ = client.GenerateContent(context.Background(), msgs) // populate the cache

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, _ := client.GenerateContent(context.Background(), msgs)
			r.ToolCalls[0].Function.Arguments = fmt.Sprintf("g%d", i)
			r.Reasoning.Metadata["k"] = i
			if r.Content != "orig" {
				t.Errorf("content = %q, want orig", r.Content)
			}
		}(i)
	}
	wg.Wait()
}

// TestDefaultCacheKey_CoversAllOutputAffectingOptions is the guardrail that keeps
// the cache key complete: every CallOptions field must either be hashed by
// defaultCacheKey (present in cacheKeyShape) or be explicitly listed as
// non-output-affecting. Adding a new output-affecting option without hashing it
// (the ExtraBody/WebSearch class of bug) fails this test.
func TestDefaultCacheKey_CoversAllOutputAffectingOptions(t *testing.T) {
	keyed := map[string]bool{}
	kt := reflect.TypeOf(cacheKeyShape{})
	for i := 0; i < kt.NumField(); i++ {
		keyed[kt.Field(i).Name] = true
	}
	// CallOptions fields intentionally excluded from the key: they change cost,
	// latency, transport, or observability — never the model's output.
	excluded := map[string]bool{
		"Cache":             true, // prompt-cache directive: same output, cheaper/faster
		"EstimateTokens":    true, // post-hoc token counting
		"StreamBufferSize":  true, // streaming transport (Stream is not cached)
		"StreamSendTimeout": true, // streaming transport
		"Trace":             true, // observability context
	}
	ot := reflect.TypeOf(CallOptions{})
	for i := 0; i < ot.NumField(); i++ {
		name := ot.Field(i).Name
		if !keyed[name] && !excluded[name] {
			t.Errorf("CallOptions field %q is neither hashed by defaultCacheKey (add it to cacheKeyShape) nor documented as non-output-affecting (add it to the excluded allowlist)", name)
		}
	}
}

func TestDefaultCacheKey_ExtraBodyAndWebSearchAffectKey(t *testing.T) {
	msgs := []Message{{Role: RoleUser, Content: "hi"}}
	base := defaultCacheKey(ProviderOpenAI, "m", msgs, &CallOptions{})
	adapterA := defaultCacheKey(ProviderOpenAI, "m", msgs, &CallOptions{ExtraBody: map[string]any{"adapter_id": "lora-A"}})
	adapterB := defaultCacheKey(ProviderOpenAI, "m", msgs, &CallOptions{ExtraBody: map[string]any{"adapter_id": "lora-B"}})
	search := defaultCacheKey(ProviderOpenAI, "m", msgs, &CallOptions{WebSearch: &WebSearchConfig{Enabled: true}})

	if base == adapterA {
		t.Error("ExtraBody must change the cache key (adapter_id changes the output)")
	}
	if adapterA == adapterB {
		t.Error("different ExtraBody values must produce different cache keys")
	}
	if base == search {
		t.Error("WebSearch must change the cache key")
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

func TestMemoryResponseCache_BoundedEviction(t *testing.T) {
	cache := NewBoundedMemoryResponseCache(time.Minute, 3)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		cache.Set(ctx, fmt.Sprintf("k%d", i), &Response{Content: "v"})
	}
	if cache.Len() > 3 {
		t.Errorf("bounded cache exceeded cap: Len=%d, want <=3", cache.Len())
	}
}

func TestMemoryResponseCache_UnboundedByDefault(t *testing.T) {
	cache := NewMemoryResponseCache(time.Minute) // unbounded
	ctx := context.Background()
	for i := 0; i < 200; i++ {
		cache.Set(ctx, fmt.Sprintf("k%d", i), &Response{Content: "v"})
	}
	if cache.Len() != 200 {
		t.Errorf("NewMemoryResponseCache should stay unbounded: Len=%d, want 200", cache.Len())
	}
}

func TestNewCachedClient_DefaultCacheIsBounded(t *testing.T) {
	c := NewCachedClient(NewMockLLM(WithMockGenerateResponse(&Response{Content: "x"})))
	mrc, ok := c.cache.(*MemoryResponseCache)
	if !ok {
		t.Fatalf("default cache type = %T, want *MemoryResponseCache", c.cache)
	}
	if mrc.maxEntries <= 0 {
		t.Errorf("default cache must be bounded to prevent OOM, maxEntries=%d", mrc.maxEntries)
	}
}

// TestMemoryResponseCache_EvictsEarliestExpiringFirst makes the eviction-ORDER
// policy load-bearing: at capacity the earliest-expiring entry is the victim and
// a never-expire entry survives.
func TestMemoryResponseCache_EvictsEarliestExpiringFirst(t *testing.T) {
	cache := NewBoundedMemoryResponseCache(time.Hour, 3)
	base := time.Unix(1000, 0)
	cache.now = func() time.Time { return base }

	cache.mu.Lock()
	cache.entries["old"] = memoryCacheEntry{resp: &Response{}, expires: base.Add(time.Minute)}
	cache.entries["mid"] = memoryCacheEntry{resp: &Response{}, expires: base.Add(time.Hour)}
	cache.entries["never"] = memoryCacheEntry{resp: &Response{}, expires: time.Time{}} // never expires
	cache.mu.Unlock()

	// At cap (3); a 4th distinct key evicts the earliest-expiring ("old").
	cache.Set(context.Background(), "new", &Response{})

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, ok := cache.entries["old"]; ok {
		t.Error("earliest-expiring 'old' should have been evicted")
	}
	if _, ok := cache.entries["never"]; !ok {
		t.Error("never-expire 'never' should have survived")
	}
	if _, ok := cache.entries["new"]; !ok {
		t.Error("'new' should have been inserted")
	}
}

// TestMemoryResponseCache_EmptyStringKeyDoesNotDefeatEviction guards the bug
// where "" (a valid map key) collided with the eviction sentinel and leaked the
// cap.
func TestMemoryResponseCache_EmptyStringKeyDoesNotDefeatEviction(t *testing.T) {
	cache := NewBoundedMemoryResponseCache(time.Hour, 1)
	ctx := context.Background()
	cache.Set(ctx, "", &Response{}) // empty-string key is legitimate
	for i := 0; i < 50; i++ {
		cache.Set(ctx, fmt.Sprintf("k%d", i), &Response{})
	}
	if cache.Len() > 1 {
		t.Errorf("cap=1 exceeded — empty-string key defeated eviction: Len=%d", cache.Len())
	}
}
