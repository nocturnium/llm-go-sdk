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
// hashes the provider, effective model, messages, and all output-affecting
// options (sampling parameters, tools, tool choice, response format, reasoning,
// message-merging, ExtraBody, and WebSearch) — see cacheKeyShape.
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
		c.cache = NewBoundedMemoryResponseCache(5*time.Minute, defaultCacheMaxEntries)
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
		return cloneResponse(resp), nil
	}

	resp, err := c.llm.GenerateContent(ctx, messages, options...)
	if err == nil && resp != nil {
		c.cache.Set(ctx, key, cloneResponse(resp))
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

// cacheKeyShape enumerates every request field that the default cache key
// hashes. Each field here MUST change the model's output; fields that only
// affect cost, latency, transport, or observability are intentionally excluded
// (see defaultCacheKey). TestDefaultCacheKey_CoversAllOutputAffectingOptions
// reflects over this type and CallOptions to fail if a new output-affecting
// option is added without being hashed here.
type cacheKeyShape struct {
	Provider              Provider
	Model                 string
	Messages              []Message
	Temperature           *float64
	MaxTokens             *int
	TopP                  *float64
	FrequencyPenalty      *float64
	PresencePenalty       *float64
	StopWords             []string
	Tools                 []Tool
	ToolChoice            *ToolChoice
	ResponseFormat        *ResponseFormat
	Reasoning             *ReasoningConfig
	DisableMessageMerging bool
	ExtraBody             map[string]any
	WebSearch             *WebSearchConfig
}

// defaultCacheKey hashes the output-affecting parts of a request, including
// provider-specific ExtraBody (e.g. a LoRAX adapter_id) and WebSearch grounding,
// both of which change the model output. Fields that do NOT change the output —
// prompt-cache directives (cost/latency), token estimation, stream buffer sizing,
// and trace context — are intentionally excluded. A request that fails to marshal
// (e.g. an ExtraBody holding an unmarshalable value) falls back to a sentinel and
// simply never caches, which is the correct fail-safe.
func defaultCacheKey(provider Provider, model string, messages []Message, opts *CallOptions) string {
	k := cacheKeyShape{
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
		k.DisableMessageMerging = opts.DisableMessageMerging
		k.ExtraBody = opts.ExtraBody
		k.WebSearch = opts.WebSearch
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

// cloneResponse deep-copies resp so the returned value shares no mutable
// reference state (slices, pointers, maps) with the original. The cache stores a
// clone on Set and returns a clone on Get, so mutating a returned Response's
// ToolCalls, SearchResults, or Reasoning can neither poison the cached entry nor
// race another concurrent cache hit. Returns nil for a nil input.
//
// Reasoning.Metadata is copied one level deep — sufficient because providers
// populate it with scalar values; nested reference values inside Metadata are
// not independently cloned.
func cloneResponse(resp *Response) *Response {
	if resp == nil {
		return nil
	}
	cp := *resp // value fields (ID, Content, FinishReason, Usage) copy directly

	if resp.ToolCalls != nil {
		cp.ToolCalls = make([]ToolCall, len(resp.ToolCalls))
		copy(cp.ToolCalls, resp.ToolCalls)
		for i := range cp.ToolCalls {
			if fn := cp.ToolCalls[i].Function; fn != nil {
				fnCopy := *fn
				cp.ToolCalls[i].Function = &fnCopy
			}
		}
	}

	if resp.SearchResults != nil {
		cp.SearchResults = make([]SearchResult, len(resp.SearchResults))
		copy(cp.SearchResults, resp.SearchResults)
		for i := range cp.SearchResults {
			if d := cp.SearchResults[i].Date; d != nil {
				dCopy := *d
				cp.SearchResults[i].Date = &dCopy
			}
		}
	}

	if resp.Reasoning != nil {
		rc := *resp.Reasoning
		if resp.Reasoning.Metadata != nil {
			rc.Metadata = make(map[string]any, len(resp.Reasoning.Metadata))
			for mk, mv := range resp.Reasoning.Metadata {
				rc.Metadata[mk] = mv
			}
		}
		cp.Reasoning = &rc
	}

	return &cp
}

// memoryCacheEntry is a cached response with its expiry.
type memoryCacheEntry struct {
	resp    *Response
	expires time.Time
}

// defaultCacheMaxEntries bounds the default in-memory cache so a high-cardinality,
// low-repeat workload cannot grow it without limit (OOM). Generous enough that
// realistic repeat-heavy workloads never evict.
const defaultCacheMaxEntries = 10000

// MemoryResponseCache is an in-memory ResponseCache with per-entry TTL expiry and
// an optional maximum size. The zero value is not usable; construct with
// NewMemoryResponseCache or NewBoundedMemoryResponseCache.
type MemoryResponseCache struct {
	ttl        time.Duration
	maxEntries int // 0 = unbounded
	mu         sync.Mutex
	entries    map[string]memoryCacheEntry
	now        func() time.Time // injectable clock for tests
}

// NewMemoryResponseCache returns an in-memory cache whose entries expire after
// ttl. A ttl of 0 means entries never expire. The cache is UNBOUNDED; for
// high-cardinality workloads use NewBoundedMemoryResponseCache to cap memory.
func NewMemoryResponseCache(ttl time.Duration) *MemoryResponseCache {
	return NewBoundedMemoryResponseCache(ttl, 0)
}

// NewBoundedMemoryResponseCache returns an in-memory cache that, in addition to
// ttl expiry, evicts entries once it would exceed maxEntries — first dropping
// expired entries, then the earliest-expiring one — so memory stays bounded
// under high-cardinality traffic. maxEntries <= 0 means unbounded.
func NewBoundedMemoryResponseCache(ttl time.Duration, maxEntries int) *MemoryResponseCache {
	return &MemoryResponseCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		entries:    make(map[string]memoryCacheEntry),
		now:        time.Now,
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
	if m.maxEntries > 0 {
		if _, replacing := m.entries[key]; !replacing && len(m.entries) >= m.maxEntries {
			m.evictLocked()
		}
	}
	m.entries[key] = memoryCacheEntry{resp: resp, expires: expires}
}

// evictLocked frees room in a bounded cache that is at capacity: it first drops
// all expired entries, then, if still at capacity, evicts the earliest-expiring
// entry (never-expire entries are evicted last). Caller must hold m.mu.
//
// Selecting the victim is an O(maxEntries) scan, so at capacity every
// non-replacing Set pays one linear pass. This is an accepted tradeoff for a
// simple map-based cache (bounded, microsecond-scale at the default 10k cap); a
// heap or LRU list would be the follow-up if a much larger cap is ever needed.
func (m *MemoryResponseCache) evictLocked() {
	now := m.now()
	for k, e := range m.entries {
		if !e.expires.IsZero() && now.After(e.expires) {
			delete(m.entries, k)
		}
	}
	for len(m.entries) >= m.maxEntries {
		var evictKey string
		var evictExp time.Time
		found := false // NOT `evictKey == ""` — "" is a legitimate map key
		for k, e := range m.entries {
			switch {
			case !found:
				evictKey, evictExp, found = k, e.expires, true
			case e.expires.IsZero():
				// never-expire: don't prefer over an expiring candidate
			case evictExp.IsZero() || e.expires.Before(evictExp):
				evictKey, evictExp = k, e.expires
			}
		}
		if !found {
			break
		}
		delete(m.entries, evictKey)
	}
}

// Len returns the number of entries currently held (including any not yet
// evicted expired entries). Primarily for tests and introspection.
func (m *MemoryResponseCache) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}
