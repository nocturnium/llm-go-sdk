package llms

import "time"

// This file defines the cross-provider prompt-caching surface. Providers fall
// into two camps: those with explicit cache breakpoints (Anthropic) honor
// CacheControl marks and the CacheConfig directive; those that cache
// automatically (OpenAI, DeepSeek, Gemini) ignore the directives but still
// report cache token usage via Usage.CacheReadTokens / CacheCreationTokens.

// CacheControl marks a prompt-caching breakpoint on a Message. The provider
// caches the prompt prefix up to and including that message so a later request
// sharing the prefix can reuse it at the discounted cache-read rate.
//
// The zero value requests the provider's default cache lifetime (Anthropic:
// 5-minute "ephemeral"). Only providers with explicit breakpoints (Anthropic)
// act on it; automatic-caching providers ignore it.
type CacheControl struct {
	// TTL is the desired cache lifetime. Zero uses the provider default
	// (~5 minutes). A value of one hour or more maps to Anthropic's 1-hour tier.
	TTL time.Duration
}

// CacheConfig controls the SDK's automatic prompt caching for a call. Set it via
// WithCache, WithCacheTTL, or WithoutCache.
type CacheConfig struct {
	// Disabled turns off the SDK's automatic cache breakpoints for this call
	// (e.g. Anthropic's system-prompt and tool caching). Explicit per-message
	// CacheControl marks are still applied.
	Disabled bool
	// TTL sets the lifetime for the SDK's automatic breakpoints. Zero uses the
	// provider default; one hour or more maps to Anthropic's 1-hour tier.
	TTL time.Duration
}

// AnthropicTTL renders a cache TTL as the Anthropic cache_control "ttl" value
// ("1h" for one hour or longer), or "" to use the default 5-minute ephemeral
// cache. Exposed for provider adapters; most callers do not need it.
func AnthropicTTL(ttl time.Duration) string {
	if ttl >= time.Hour {
		return "1h"
	}
	return ""
}

// WithCache enables the SDK's automatic prompt caching for a call. For providers
// with explicit breakpoints (Anthropic) it caches the stable prefix — the system
// prompt and tool definitions. For providers that cache automatically (OpenAI,
// DeepSeek, Gemini) it is a no-op; cache usage is still reported either way.
func WithCache() CallOption {
	return func(o *CallOptions) {
		o.Cache = &CacheConfig{}
	}
}

// WithCacheTTL is like WithCache but requests a specific cache lifetime. A TTL of
// one hour or more selects Anthropic's 1-hour cache tier where supported.
func WithCacheTTL(ttl time.Duration) CallOption {
	return func(o *CallOptions) {
		o.Cache = &CacheConfig{TTL: ttl}
	}
}

// WithoutCache disables the SDK's automatic prompt caching for a call (e.g.
// Anthropic's default system-prompt caching). Explicit per-message CacheControl
// marks are still honored.
func WithoutCache() CallOption {
	return func(o *CallOptions) {
		o.Cache = &CacheConfig{Disabled: true}
	}
}
