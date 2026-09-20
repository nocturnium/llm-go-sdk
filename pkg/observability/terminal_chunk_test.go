package observability

import (
	"context"
	"testing"

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
