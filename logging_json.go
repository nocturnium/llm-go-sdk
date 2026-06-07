package llms

import (
	"context"
	"encoding/json"
)

// JSONLogger logs entries as JSON lines
type JSONLogger struct {
	encode    func(any) ([]byte, error)
	write     func([]byte) error
	redact    bool
	maxLength int
}

// JSONLoggerOption configures the JSONLogger
type JSONLoggerOption func(*JSONLogger)

// NewJSONLogger creates a new JSON logger that writes to the provided function
func NewJSONLogger(writeFn func([]byte) error, opts ...JSONLoggerOption) *JSONLogger {
	l := &JSONLogger{
		encode: json.Marshal,
		write:  writeFn,
		// Privacy-safe by default: messages/content are redacted unless the caller
		// opts out via WithJSONRedaction(false).
		redact:    true,
		maxLength: 1000,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// WithJSONRedaction enables or disables redaction for the JSON logger.
// Redaction is ON by default; pass WithJSONRedaction(false) to opt into logging
// messages and content.
func WithJSONRedaction(redact bool) JSONLoggerOption {
	return func(l *JSONLogger) {
		l.redact = redact
	}
}

// WithJSONMaxLength sets the max content length for JSON logger
func WithJSONMaxLength(maxLength int) JSONLoggerOption {
	return func(l *JSONLogger) {
		l.maxLength = maxLength
	}
}

// LogRequest logs a request as JSON
func (l *JSONLogger) LogRequest(_ context.Context, entry *LogEntry) {
	l.writeEntry("request", l.prepareEntry(entry, false))
}

// LogResponse logs a response as JSON
func (l *JSONLogger) LogResponse(_ context.Context, entry *LogEntry) {
	l.writeEntry("response", l.prepareEntry(entry, true))
}

// LogError logs an error as JSON
func (l *JSONLogger) LogError(_ context.Context, entry *LogEntry, err error) {
	e := l.prepareEntry(entry, false)
	e.Error = err.Error()
	l.writeEntry("error", e)
}

func (l *JSONLogger) prepareEntry(entry *LogEntry, _ bool) *LogEntry {
	e := *entry

	if l.redact {
		e.Messages = nil
		e.Content = ""
	} else {
		e.Content = truncateString(e.Content, l.maxLength)
	}

	return &e
}

func (l *JSONLogger) writeEntry(entryType string, entry *LogEntry) {
	data := map[string]any{
		"type":  entryType,
		"entry": entry,
	}

	bytes, err := l.encode(data)
	if err != nil {
		return
	}

	bytes = append(bytes, '\n')
	_ = l.write(bytes)
}
