package llms

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestWrapStreamWithFinalizer_ProcessorSeesSynthesizedTerminal pins that a
// terminal chunk the wrapper synthesizes reaches the processor. The resilience
// breaker reads the terminal chunk through its processor, so an abandoned or
// timed-out stream used to be recorded as a healthy probe.
func TestWrapStreamWithFinalizer_ProcessorSeesSynthesizedTerminal(t *testing.T) {
	source := make(chan StreamChunk)
	go func() {
		defer close(source)
		source <- StreamChunk{Content: "partial"}
	}()

	seen := make(chan StreamChunk, 4)
	opts := ApplyOptions(WithStreamBufferSize(1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := WrapStreamWithFinalizer(ctx, source, opts,
		func(chunk StreamChunk) StreamChunk {
			seen <- chunk
			return chunk
		},
		nil,
	)
	for range out { //nolint:revive // draining is the point
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case chunk := <-seen:
			if chunk.Error != nil || chunk.Done {
				if chunk.Error != nil && !errors.Is(chunk.Error, context.Canceled) {
					t.Errorf("terminal error = %v, want context.Canceled", chunk.Error)
				}
				return
			}
		case <-deadline:
			t.Fatal("the processor never saw a terminal chunk")
		}
	}
}

// TestWrapStream_DrainsAbandonedSource pins that a producer is released when
// the consumer stops reading. The wrapper used to return without draining, so
// the producer stayed blocked on its send for a full send timeout while
// holding whatever permit it had.
func TestWrapStream_DrainsAbandonedSource(t *testing.T) {
	source := make(chan StreamChunk)
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		defer close(source)
		for i := range 50 {
			source <- StreamChunk{Content: string(rune('a' + i%26))}
		}
	}()

	opts := ApplyOptions(WithStreamBufferSize(1), WithStreamSendTimeout(50*time.Millisecond))
	out := WrapStream(context.Background(), source, opts, nil)

	// Read one chunk, then abandon the stream.
	<-out

	select {
	case <-producerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("the producer was never released: the abandoned source was not drained")
	}
}
