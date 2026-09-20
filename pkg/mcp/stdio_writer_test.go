package mcp

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestStdio_WriterGoroutineExitsWithTransport pins that the writer goroutine
// stops when the transport finishes. It used to park on its receive forever,
// so every session that wrote anything leaked one goroutine.
func TestStdio_WriterGoroutineExitsWithTransport(t *testing.T) {
	before := runtime.NumGoroutine()

	for range 20 {
		tr := &stdioTransport{stdin: nopWriteCloser{}, done: make(chan struct{})}
		if err := tr.write(context.Background(), []byte(`{"jsonrpc":"2.0"}`)); err != nil {
			t.Fatalf("write: %v", err)
		}
		close(tr.done)
	}

	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Errorf("goroutines went from %d to %d: the writers outlived their transports", before, after)
	}
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }
