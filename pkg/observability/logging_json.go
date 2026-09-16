package observability

import (
	"context"
	"encoding/json"
)

// JSONLogger logs entries as JSON lines
type JSONLogger struct {
	encode    func(any) ([]byte, error)
	write     func([]byte) error
	onError   func(error)
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
		// Privacy-safe by default: messages, content, structured input/output and
		// tool calls are redacted unless the caller
		// opts out via WithJSONRedaction(false).
		redact:    true,
		maxLength: 1000,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// WithJSONWriteError installs a callback for a failing sink. A logger has
// nowhere to log its own failure, so without one a closed file or a full disk
// drops every line in silence; the callback is how an operator hears about it.
func WithJSONWriteError(fn func(error)) JSONLoggerOption {
	return func(l *JSONLogger) {
		l.onError = fn
	}
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
	// Sanitize CR/LF: an upstream/provider error can echo user-controlled input,
	// so escape it like other user-influenced values to prevent forged log lines
	// (CWE-117).
	e.Error = sanitizeLogValue(err.Error())
	l.writeEntry("error", e)
}

func (l *JSONLogger) prepareEntry(entry *LogEntry, _ bool) *LogEntry {
	e := *entry

	if l.redact {
		// Every field that can carry conversation content goes, not just the two
		// obvious ones: InputJSON and OutputJSON are the serialized messages, and
		// tool-call arguments are model output built from the prompt.
		e.Messages = nil
		e.Content = ""
		e.InputJSON = ""
		e.OutputJSON = ""
		e.ToolCalls = nil
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

	encoded, err := l.encode(data)
	if err != nil {
		l.reportError(err)
		return
	}

	encoded = append(encoded, '\n')
	if err := l.write(encoded); err != nil {
		l.reportError(err)
	}
}

// reportError hands a sink or encoding failure to the caller's callback, if one
// was installed.
func (l *JSONLogger) reportError(err error) {
	if l.onError != nil {
		l.onError(err)
	}
}
