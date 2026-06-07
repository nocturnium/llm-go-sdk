# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Documentation improvements: comprehensive README sections for all features
- Provider package documentation (doc.go files for all 6 providers)
- Shared test utilities (`testutil_test.go`): MockLLM, MockServerConfig, response fixtures, assertion helpers
- `CapableProvider` interface for capability introspection
- `WrapStream` helper for middleware stream handling

### Changed
- **Provider Consolidation**: OpenAI-compatible providers (OpenAI, TogetherAI, Featherless, Synthetic) now use `openaicompat.BaseProvider` for ~75% code reduction
- Extracted Anthropic converters to `providers/anthropic/converters.go`
- Extracted Gemini converters to `providers/gemini/converters.go`
- Split `metrics.go` into focused files: `metrics.go`, `metrics_otel.go`, `metrics_sliding_window.go`
- Split `logging.go` into focused files: `logging.go`, `logging_slog.go`, `logging_json.go`
- Split `resilience.go` into focused files: `resilience.go`, `resilience_circuit_breaker.go`, `resilience_retry.go`

### Improved
- Reduced middleware file sizes from ~500-700 lines to ~150-200 lines each
- Better separation of concerns in metrics, logging, and resilience modules
- More testable code through modular file organization

## [0.1.0] - 2024-12-26

### Added

#### Core Features
- Unified `LLM` interface for all providers
- Native HTTP implementation (no external LLM SDK dependencies)
- Streaming responses via Go channels with backpressure handling
- Function/tool calling support across all providers
- Functional options pattern for clean configuration

#### Providers
- **OpenAI** - GPT-4o, GPT-4, GPT-3.5-turbo with embeddings support
- **Anthropic** - Claude 3.5 Sonnet, Claude 3 Opus/Sonnet/Haiku
- **Google Gemini** - Gemini 2.0, 1.5 Flash/Pro with embeddings support
- **TogetherAI** - Llama, Mixtral, Qwen (OpenAI-compatible)
- **Featherless** - Thousands of open-source models (OpenAI-compatible)
- **Synthetic** - Privacy-focused coding LLMs (OpenAI-compatible)

#### Vision / Multi-Modal
- Image support for PNG, JPEG, GIF, WebP (max 20MB)
- Helper functions: `NewImageMessage`, `NewImageFileMessage`, `NewMultiPartMessage`
- URL and base64 image sources

#### Embeddings
- `Embedder` interface for text embeddings
- Support in OpenAI and Gemini providers
- Configurable dimensions and task types

#### Resilience
- Circuit breaker pattern with configurable thresholds
- Exponential backoff retry with jitter
- Automatic retry on 429, 500, 502, 503, 504 errors
- State change callbacks for monitoring

#### Rate Limiting
- Client-side rate limiting with token bucket algorithm
- Request and token rate limits
- Provider default limits included
- Shared rate limiter for multiple clients

#### Fallback Chains
- Automatic failover between multiple providers
- Configurable fallback selectors (Default, Always, Never)
- Weighted fallback chains for prioritization
- Health tracking for clients

#### Cost Tracking
- Token usage tracking per model
- Cost estimation with built-in pricing for 25+ models
- Cost middleware for automatic tracking
- Reporting and analytics functions

#### Observability
- OpenTelemetry integration (tracing + metrics)
- Structured logging middleware
- Request/response logging with truncation
- Metrics: request count, tokens, duration, errors

#### Error Handling
- Rich error types: `APIError`, `ValidationError`, `StreamError`
- Specific errors: rate limit, quota exceeded, auth failed
- `errors.Is` and `errors.As` compatible

#### CLI Tool
- `chat` - Interactive chat with any provider
- `complete` - Simple text completion
- `tool-demo` - Function calling demonstration
- `providers` - List available providers

### Security
- API key validation
- 20MB image size limit to prevent DoS
- Content truncation in logs (100KB limit)
- Base64 validation without full decode (memory protection)
