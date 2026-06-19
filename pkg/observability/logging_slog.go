package observability

import (
	"context"
	"errors"
	"log/slog"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

// SlogLogger is a Logger implementation using log/slog
type SlogLogger struct {
	logger    *slog.Logger
	level     slog.Level
	redact    bool
	maxLength int
}

// SlogLoggerOption configures the SlogLogger
type SlogLoggerOption func(*SlogLogger)

// NewSlogLogger creates a new SlogLogger
func NewSlogLogger(logger *slog.Logger, opts ...SlogLoggerOption) *SlogLogger {
	if logger == nil {
		logger = slog.Default()
	}

	l := &SlogLogger{
		logger: logger,
		level:  slog.LevelInfo,
		// Privacy-safe by default: prompts/responses are redacted unless the caller
		// opts out via WithRedaction(false).
		redact:    true,
		maxLength: 500,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// WithLogLevel sets the log level
func WithLogLevel(level slog.Level) SlogLoggerOption {
	return func(l *SlogLogger) {
		l.level = level
	}
}

// WithRedaction enables or disables redaction of sensitive content.
// Redaction is ON by default; pass WithRedaction(false) to opt into logging
// prompts and responses.
func WithRedaction(redact bool) SlogLoggerOption {
	return func(l *SlogLogger) {
		l.redact = redact
	}
}

// WithMaxLength sets the maximum length of logged content
func WithMaxLength(maxLength int) SlogLoggerOption {
	return func(l *SlogLogger) {
		l.maxLength = maxLength
	}
}

// LogRequest logs an LLM request
func (l *SlogLogger) LogRequest(ctx context.Context, entry *LogEntry) {
	attrs := l.buildRequestAttrs(entry)
	l.logger.LogAttrs(ctx, l.level, "llm_request", attrs...)
}

// LogResponse logs an LLM response
func (l *SlogLogger) LogResponse(ctx context.Context, entry *LogEntry) {
	attrs := l.buildResponseAttrs(entry)
	l.logger.LogAttrs(ctx, l.level, "llm_response", attrs...)
}

// LogError logs an error
func (l *SlogLogger) LogError(ctx context.Context, entry *LogEntry, err error) {
	attrs := l.buildErrorAttrs(entry, err)
	l.logger.LogAttrs(ctx, slog.LevelError, "llm_error", attrs...)
}

func (l *SlogLogger) buildRequestAttrs(entry *LogEntry) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("request_id", entry.RequestID),
		slog.String("provider", string(entry.Provider)),
		slog.String("model", entry.Model),
		slog.String("operation", entry.Operation),
		slog.Time("timestamp", entry.Timestamp),
		slog.Bool("streaming", entry.Streaming),
	}

	if !l.redact && len(entry.Messages) > 0 {
		attrs = append(attrs, slog.Int("message_count", len(entry.Messages)))
		if len(entry.Messages) > 0 {
			lastMsg := entry.Messages[len(entry.Messages)-1]
			content := sanitizeLogValue(l.truncate(lastMsg.Content))
			attrs = append(attrs, slog.String("last_message", content))
		}
	}

	if entry.Metadata != nil {
		for k, v := range entry.Metadata {
			attrs = append(attrs, slog.Any(k, v))
		}
	}

	return attrs
}

func (l *SlogLogger) buildResponseAttrs(entry *LogEntry) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("request_id", entry.RequestID),
		slog.String("provider", string(entry.Provider)),
		slog.String("model", entry.Model),
		slog.String("operation", entry.Operation),
		slog.Duration("duration", entry.Duration),
		slog.String("finish_reason", entry.FinishReason),
	}

	if entry.Usage != nil {
		attrs = append(attrs,
			slog.Int("prompt_tokens", entry.Usage.PromptTokens),
			slog.Int("completion_tokens", entry.Usage.CompletionTokens),
			slog.Int("total_tokens", entry.Usage.TotalTokens),
		)
	}

	if !l.redact {
		content := sanitizeLogValue(l.truncate(entry.Content))
		attrs = append(attrs, slog.String("content", content))
	}

	if len(entry.ToolCalls) > 0 {
		attrs = append(attrs, slog.Int("tool_calls", len(entry.ToolCalls)))
	}

	return attrs
}

func (l *SlogLogger) buildErrorAttrs(entry *LogEntry, err error) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("request_id", entry.RequestID),
		slog.String("provider", string(entry.Provider)),
		slog.String("model", entry.Model),
		slog.String("operation", entry.Operation),
		slog.Duration("duration", entry.Duration),
		slog.String("error", err.Error()),
	}

	// Add structured error information
	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		attrs = append(attrs,
			slog.Int("status_code", apiErr.StatusCode),
			slog.String("error_type", apiErr.Type),
			slog.String("error_code", apiErr.Code),
		)
	}

	return attrs
}

func (l *SlogLogger) truncate(s string) string {
	return truncateString(s, l.maxLength)
}
