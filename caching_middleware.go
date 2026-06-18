package llms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// ResponseCache is a pluggable backend for the response-cache middleware
// (CachedClient). Implementations may be in-memory, Redis-backed, etc. Get
// returns the cached response and true on a hit. Implementations must be safe
// for concurrent use.
type ResponseCache interface {
	Get(ctx context.Context, key string) (*Response, bool)
	Set(ctx context.Context, key string, resp *Response)
}

// CachedClient is an LLM middleware that caches GenerateContent responses keyed
// by the request (provider, model, messages, and output-affecting options). It
// turns repeated identical requests into cache hits — useful for deterministic
// (temperature 0) calls, idempotent retries, test suites, and fan-out workloads
// that repeat prompts.
//
// Stream is passed through uncached: streaming responses are not buffered.
// Because cache keys include sampling parameters, requests with temperature > 0
// that you expect to vary should either skip the cache or use a short TTL — a hit
// returns the previously sampled response verbatim.
//
// CachedClient implements LLM and Wrapper, so it composes with other middleware.
type CachedClient struct {
	llm   LLM
	cache ResponseCache
	keyFn func(provider Provider, model string, messages []Message, opts *CallOptions) string
}

// CacheClientOption configures a CachedClient.
type CacheClientOption func(*CachedClient)

// WithResponseCache sets the cache backend. Defaults to an in-memory cache with
// a 5-minute TTL when not provided.
func WithResponseCache(cache ResponseCache) CacheClientOption {
	return func(c *CachedClient) { c.cache = cache }
}

// WithCacheKeyFunc overrides how a request is reduced to a cache key. The default
// hashes the provider, effective model, messages, and output-affecting options
// (sampling parameters, tools, tool choice, response format, reasoning).
func WithCacheKeyFunc(fn func(provider Provider, model string, messages []Message, opts *CallOptions) string) CacheClientOption {
	return func(c *CachedClient) {
		if fn != nil {
			c.keyFn = fn
		}
	}
}

// NewCachedClient wraps llm with a response cache. With no options it uses an
// in-memory cache with a 5-minute TTL.
func NewCachedClient(llm LLM, opts ...CacheClientOption) *CachedClient {
	c := &CachedClient{llm: llm}
	for _, opt := range opts {
		opt(c)
	}
	if c.cache == nil {
		c.cache = NewMemoryResponseCache(5 * time.Minute)
	}
	if c.keyFn == nil {
		c.keyFn = defaultCacheKey
	}
	return c
}

// GenerateContent returns a cached response when one exists for the request,
// otherwise it calls the wrapped LLM and caches a successful, non-nil response.
func (c *CachedClient) GenerateContent(ctx context.Context, messages []Message, options ...CallOption) (*Response, error) {
	applied := ApplyOptions(options...)
	model := applied.Model
	if model == "" {
		model = c.llm.Model()
	}
	key := c.keyFn(c.llm.Provider(), model, messages, applied)

	if resp, ok := c.cache.Get(ctx, key); ok {
		cp := *resp
		return &cp, nil
	}

	resp, err := c.llm.GenerateContent(ctx, messages, options...)
	if err == nil && resp != nil {
		stored := *resp
		c.cache.Set(ctx, key, &stored)
	}
	return resp, err
}

// Stream passes through to the wrapped LLM without caching.
func (c *CachedClient) Stream(ctx context.Context, messages []Message, options ...CallOption) (<-chan StreamChunk, error) {
	return c.llm.Stream(ctx, messages, options...)
}

// Provider returns the wrapped provider type.
func (c *CachedClient) Provider() Provider { return c.llm.Provider() }

// Model returns the wrapped model name.
func (c *CachedClient) Model() string { return c.llm.Model() }

// Unwrap returns the wrapped LLM.
func (c *CachedClient) Unwrap() LLM { return c.llm }

var _ LLM = (*CachedClient)(nil)
var _ Wrapper = (*CachedClient)(nil)

// defaultCacheKey hashes the output-affecting parts of a request. Fields that do
// not change the model output (streaming buffer sizes, trace context, token
// estimation, prompt-cache directives) are intentionally excluded.
func defaultCacheKey(provider Provider, model string, messages []Message, opts *CallOptions) string {
	type keyShape struct {
		Provider         Provider
		Model            string
		Messages         []Message
		Temperature      *float64
		MaxTokens        *int
		TopP             *float64
		FrequencyPenalty *float64
		PresencePenalty  *float64
		StopWords        []string
		Tools            []Tool
		ToolChoice       *ToolChoice
		ResponseFormat   *ResponseFormat
		Reasoning        *ReasoningConfig
	}
	k := keyShape{
		Provider: provider,
		Model:    model,
		Messages: messages,
	}
	if opts != nil {
		k.Temperature = opts.Temperature
		k.MaxTokens = opts.MaxTokens
		k.TopP = opts.TopP
		k.FrequencyPenalty = opts.FrequencyPenalty
		k.PresencePenalty = opts.PresencePenalty
		k.StopWords = opts.StopWords
		k.Tools = opts.Tools
		k.ToolChoice = opts.ToolChoice
		k.ResponseFormat = opts.ResponseFormat
		k.Reasoning = opts.Reasoning
	}
	data, err := json.Marshal(k)
	if err != nil {
		// Fall back to a non-colliding-with-real-keys sentinel; an unhashable
		// request simply never caches.
		return "llms:uncacheable"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// memoryCacheEntry is a cached response with its expiry.
type memoryCacheEntry struct {
	resp    *Response
	expires time.Time
}

// MemoryResponseCache is an in-memory ResponseCache with per-entry TTL expiry.
// The zero value is not usable; construct with NewMemoryResponseCache.
type MemoryResponseCache struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]memoryCacheEntry
	now     func() time.Time // injectable clock for tests
}

// NewMemoryResponseCache returns an in-memory cache whose entries expire after
// ttl. A ttl of 0 means entries never expire.
func NewMemoryResponseCache(ttl time.Duration) *MemoryResponseCache {
	return &MemoryResponseCache{
		ttl:     ttl,
		entries: make(map[string]memoryCacheEntry),
		now:     time.Now,
	}
}

// Get returns the cached response for key if present and unexpired.
func (m *MemoryResponseCache) Get(_ context.Context, key string) (*Response, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok {
		return nil, false
	}
	if !entry.expires.IsZero() && m.now().After(entry.expires) {
		delete(m.entries, key)
		return nil, false
	}
	return entry.resp, true
}

// Set stores resp under key with the cache's TTL.
func (m *MemoryResponseCache) Set(_ context.Context, key string, resp *Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var expires time.Time
	if m.ttl > 0 {
		expires = m.now().Add(m.ttl)
	}
	m.entries[key] = memoryCacheEntry{resp: resp, expires: expires}
}

// Len returns the number of entries currently held (including any not yet
// evicted expired entries). Primarily for tests and introspection.
func (m *MemoryResponseCache) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}
