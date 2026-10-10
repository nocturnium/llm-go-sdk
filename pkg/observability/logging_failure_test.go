package observability

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// countingLogger records which of the two terminal paths a stream took.
// The middleware logs from its stream goroutine, so the counters and the last
// error are only touched atomically to keep the test race-free.
type countingLogger struct {
	responses atomic.Int32
	errors    atomic.Int32
	lastErr   atomic.Pointer[error]
}

func (l *countingLogger) LogRequest(context.Context, *LogEntry)  {}
func (l *countingLogger) LogResponse(context.Context, *LogEntry) { l.responses.Add(1) }
func (l *countingLogger) LogError(_ context.Context, _ *LogEntry, err error) {
	l.errors.Add(1)
	l.lastErr.Store(&err)
}

// TestLoggingMiddleware_CanceledStreamLogsError pins that an abandoned stream
// is logged as a failure. It used to reach LogResponse with no error set, so a
// canceled stream was indistinguishable from a clean one once the default
// redaction stripped the marker appended to the content.
func TestLoggingMiddleware_CanceledStreamLogsError(t *testing.T) {
	logger := &countingLogger{}
	base := &terminalTestLLM{}
	mw := NewLoggingMiddleware(base, logger)

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := mw.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	cancel()
	for range stream { //nolint:revive // draining the stream is the point
	}

	deadline := time.After(2 * time.Second)
	for logger.responses.Load()+logger.errors.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("logger saw neither a response nor an error")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if logger.errors.Load() != 1 || logger.responses.Load() != 0 {
		t.Errorf("LogError=%d LogResponse=%d, want 1 and 0", logger.errors.Load(), logger.responses.Load())
	}
	loggedErr := logger.lastErr.Load()
	if loggedErr == nil || !errors.Is(*loggedErr, context.Canceled) {
		t.Errorf("logged error = %v, want context.Canceled", loggedErr)
	}
}

// TestJSONLogger_NilWriteReportsInsteadOfPanicking pins that a logger built
// with no sink reports the mistake rather than taking the process down.
func TestJSONLogger_NilWriteReportsInsteadOfPanicking(t *testing.T) {
	var reported error
	logger := NewJSONLogger(nil, WithJSONWriteError(func(err error) { reported = err }))

	logger.LogResponse(context.Background(), &LogEntry{Provider: "test"})

	if reported == nil {
		t.Error("nil write function was not reported")
	}
}
