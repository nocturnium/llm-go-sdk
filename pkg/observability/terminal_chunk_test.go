package observability

import (
	"context"
	"sync"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// terminalTestLLM streams content and then closes without a terminal chunk,
// which is what a dropped upstream connection looks like to a middleware.
type terminalTestLLM struct{ mockLangfuseLLM }

func (l *terminalTestLLM) Stream(ctx context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	out := make(chan llms.StreamChunk, 3)
	go func() {
		defer close(out)
		out <- llms.StreamChunk{Content: "half "}
		out <- llms.StreamChunk{Content: "an answer"}
	}()
	return out, nil
}

// errorThenTrailingLLM streams an error chunk and then one more chunk whose send
// blocks until the consumer reads it. A wrapper that returns on the error chunk
// without draining leaves this producer goroutine parked on its send; drained
// is closed once the trailing chunk has been consumed.
type errorThenTrailingLLM struct {
	mockLangfuseLLM
	drained chan struct{}
}

func (l *errorThenTrailingLLM) Stream(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	out := make(chan llms.StreamChunk)
	l.drained = make(chan struct{})
	drained := l.drained
	go func() {
		defer close(out)
		defer close(drained)
		out <- llms.StreamChunk{Error: context.DeadlineExceeded}
		// Blocks until the consumer reads (or drains) it. Only a wrapper that
		// drains its source lets this send complete.
		out <- llms.StreamChunk{Content: "trailing"}
	}()
	return out, nil
}

// Every stream wrapper must drain its provider after an error chunk, not only
// after a send failure. Otherwise a provider that keeps producing (or parks on
// its send) leaks its goroutine and HTTP body.
func TestStreamWrappers_DrainAfterErrorChunk(t *testing.T) {
	base := &errorThenTrailingLLM{}

	otelMW, err := NewOTelMiddleware(base)
	if err != nil {
		t.Fatalf("NewOTelMiddleware: %v", err)
	}
	metricsMW, err := NewMetricsMiddleware(base)
	if err != nil {
		t.Fatalf("NewMetricsMiddleware: %v", err)
	}
	langfuseMW, err := NewLangfuseOTelMiddleware(base)
	if err != nil {
		t.Fatalf("NewLangfuseOTelMiddleware: %v", err)
	}

	wrappers := map[string]llms.LLM{
		"otel":     otelMW,
		"metrics":  metricsMW,
		"langfuse": langfuseMW,
		"logging":  NewLoggingMiddleware(base, NewJSONLogger(func([]byte) error { return nil })),
	}

	for name, wrapper := range wrappers {
		stream, err := wrapper.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
		if err != nil {
			t.Fatalf("%s: Stream: %v", name, err)
		}
		// Read the error chunk; the wrapper then returns and must drain.
		if chunk := <-stream; chunk.Error == nil {
			t.Fatalf("%s: first chunk error = nil, want a stream error", name)
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range stream {
				_ = chunk
				// drain the stream so the producer's trailing send can complete
			}
		}()
		// The wrapper closing its output only proves the consumer finished; the
		// provider goroutine is the thing a missed drain leaks, so wait on the
		// channel it closes after its blocked trailing send completes.
		select {
		case <-base.drained:
		case <-time.After(2 * time.Second):
			t.Errorf("%s: provider stayed blocked after the error chunk; the source was not drained", name)
		}
		wg.Wait()
	}
}

// TestStreamWrappers_DeliverTerminalChunk pins the exactly-one-terminal-chunk
// contract on every middleware that wraps a stream. Each wrapper used to close
// its channel bare, so a source ending without a terminal chunk reached the
// consumer as a clean short read.
func TestStreamWrappers_DeliverTerminalChunk(t *testing.T) {
	base := &terminalTestLLM{}

	otelMW, err := NewOTelMiddleware(base)
	if err != nil {
		t.Fatalf("NewOTelMiddleware: %v", err)
	}
	metricsMW, err := NewMetricsMiddleware(base)
	if err != nil {
		t.Fatalf("NewMetricsMiddleware: %v", err)
	}
	langfuseMW, err := NewLangfuseOTelMiddleware(base)
	if err != nil {
		t.Fatalf("NewLangfuseOTelMiddleware: %v", err)
	}

	wrappers := map[string]llms.LLM{
		"otel":     otelMW,
		"metrics":  metricsMW,
		"langfuse": langfuseMW,
		"logging":  NewLoggingMiddleware(base, NewJSONLogger(func([]byte) error { return nil })),
	}

	for name, wrapper := range wrappers {
		stream, err := wrapper.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
		if err != nil {
			t.Fatalf("%s: Stream: %v", name, err)
		}
		var terminals int
		for chunk := range stream {
			if chunk.Done || chunk.Error != nil {
				terminals++
			}
		}
		if terminals != 1 {
			t.Errorf("%s delivered %d terminal chunks, want 1", name, terminals)
		}
	}
}
