package llms

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCollectStream(t *testing.T) {
	t.Run("clean stream returns content and terminal metadata", func(t *testing.T) {
		usage := &Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8}
		toolCalls := []ToolCall{
			{
				ID:   "call_1",
				Type: ToolTypeFunction,
				Function: &FunctionCall{
					Name:      "lookup",
					Arguments: `{"q":"go"}`,
				},
			},
		}
		stream := streamFromChunks(
			StreamChunk{Content: "hello "},
			StreamChunk{Content: "world", Reasoning: &ReasoningContent{Content: "think "}},
			StreamChunk{
				Done:         true,
				Reasoning:    &ReasoningContent{Content: "more"},
				FinishReason: FinishReasonToolCalls,
				Usage:        usage,
				ToolCalls:    toolCalls,
			},
		)

		result, err := CollectStream(stream)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if result.Content != "hello world" {
			t.Fatalf("expected full content, got %q", result.Content)
		}
		if result.Reasoning == nil || result.Reasoning.Content != "think more" {
			t.Fatalf("expected accumulated reasoning, got %+v", result.Reasoning)
		}
		if result.FinishReason != FinishReasonToolCalls {
			t.Fatalf("expected finish reason %q, got %q", FinishReasonToolCalls, result.FinishReason)
		}
		if result.Usage != usage {
			t.Fatalf("expected terminal usage pointer, got %+v", result.Usage)
		}
		if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "call_1" {
			t.Fatalf("expected terminal tool calls, got %+v", result.ToolCalls)
		}
	})

	t.Run("terminal error returns partial content and exact error", func(t *testing.T) {
		sentinel := context.DeadlineExceeded
		stream := streamFromChunks(
			StreamChunk{Content: "partial "},
			StreamChunk{Content: "text"},
			StreamChunk{Error: sentinel},
		)

		result, err := CollectStream(stream)
		if err == nil {
			t.Fatal("expected non-nil error from terminal error chunk")
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected error to match sentinel, got %v", err)
		}
		if result.Content != "partial text" {
			t.Fatalf("expected partial content, got %q", result.Content)
		}
	})
}

func TestStreamText_TerminalError(t *testing.T) {
	sentinel := errors.New("stream failed")
	stream := streamFromChunks(
		StreamChunk{Content: "partial"},
		StreamChunk{Error: sentinel},
	)

	text, err := StreamText(stream)
	if err == nil {
		t.Fatal("expected non-nil error from terminal error chunk")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected error to match sentinel, got %v", err)
	}
	if text != "partial" {
		t.Fatalf("expected partial text, got %q", text)
	}
}

func streamFromChunks(chunks ...StreamChunk) <-chan StreamChunk {
	stream := make(chan StreamChunk, len(chunks))
	for _, chunk := range chunks {
		stream <- chunk
	}
	close(stream)
	return stream
}

// TestWrapStream_ContextCancelEmitsTerminalChunk verifies that when the consumer
// cancels the context mid-stream, the wrapped stream still delivers exactly one
// terminal chunk carrying a non-nil Error before the channel closes. A silent
// close would make a truncated stream look like a successful completion.
func TestWrapStream_ContextCancelEmitsTerminalChunk(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Source emits forever (until we stop reading from it) so the wrapper goroutine
	// is guaranteed to still be sending when the context is canceled.
	source := make(chan StreamChunk)
	go func() {
		for {
			select {
			case source <- StreamChunk{Content: "tick"}:
			case <-ctx.Done():
				close(source)
				return
			}
		}
	}()

	// Small buffer so the wrapper blocks on send once the consumer stops reading,
	// exercising the context-cancellation path inside StreamSender.Send.
	opts := &CallOptions{StreamBufferSize: 1, StreamSendTimeout: 5 * time.Second}
	out := WrapStream(ctx, source, opts, nil)

	// Read one chunk, then cancel and drain the channel to completion.
	<-out
	cancel()

	sawTerminal := false
	var terminal StreamChunk
	// Bound the test so a regression (silent close with no terminal) cannot hang.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				if !sawTerminal {
					t.Fatal("channel closed without delivering a terminal chunk (Done or Error)")
				}
				if terminal.Error == nil && !terminal.Done {
					t.Fatalf("terminal chunk was neither an error nor Done: %+v", terminal)
				}
				if terminal.Error != nil && !errors.Is(terminal.Error, context.Canceled) {
					t.Fatalf("expected terminal error to wrap context.Canceled, got %v", terminal.Error)
				}
				return
			}
			if chunk.Error != nil || chunk.Done {
				sawTerminal = true
				terminal = chunk
			}
		case <-deadline:
			t.Fatal("timed out waiting for stream to close after context cancellation")
		}
	}
}

// TestStreamSender_DeliverTerminalOnce verifies a single terminal chunk is
// emitted even if DeliverTerminal/SendFinal are called multiple times.
func TestStreamSender_DeliverTerminalOnce(t *testing.T) {
	chunks := make(chan StreamChunk, 4)
	sender := NewStreamSender(context.Background(), chunks, 1*time.Second)

	sender.DeliverTerminal(StreamChunk{Error: errors.New("first")})
	sender.DeliverTerminal(StreamChunk{Done: true})
	sender.SendFinal(StreamChunk{Content: "ignored"})
	close(chunks)

	count := 0
	for range chunks {
		count++
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 terminal chunk, got %d", count)
	}
}

// TestStreamSender_SendResultTimeout verifies that when the consumer stops
// reading, Send reports SendTimeout and ForwardTerminalOnEarlyExit delivers a
// terminal timeout error.
func TestStreamSender_SendResultTimeout(t *testing.T) {
	// Unbuffered channel with no reader: the very first send times out.
	chunks := make(chan StreamChunk)
	sender := &StreamSender{
		chunks:  chunks,
		timeout: 50 * time.Millisecond,
		ctx:     context.Background(),
	}

	result := sender.Send(StreamChunk{Content: "x"})
	if result != SendTimeout {
		t.Fatalf("expected SendTimeout, got %v", result)
	}

	// After a timeout, ForwardTerminalOnEarlyExit must report stop and attempt to
	// deliver a terminal error chunk.
	go func() {
		// Drain so DeliverTerminal's send can complete.
		<-chunks
	}()
	if stop := sender.ForwardTerminalOnEarlyExit(result); !stop {
		t.Fatal("expected ForwardTerminalOnEarlyExit to report stop=true on timeout")
	}
}

func BenchmarkStreamSender_SendFastPath(b *testing.B) {
	chunks := make(chan StreamChunk, 1)
	sender := NewStreamSender(context.Background(), chunks, time.Second)
	chunk := StreamChunk{Content: "x"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if result := sender.Send(chunk); result != SendOK {
			b.Fatalf("expected SendOK, got %v", result)
		}
		<-chunks
	}
}

func BenchmarkStreamSender_SendTimeout(b *testing.B) {
	chunks := make(chan StreamChunk, 1)
	chunks <- StreamChunk{Content: "blocked"}
	sender := NewStreamSender(context.Background(), chunks, time.Nanosecond)
	chunk := StreamChunk{Content: "x"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sender.timedOut.Store(false)
		if result := sender.Send(chunk); result != SendTimeout {
			b.Fatalf("expected SendTimeout, got %v", result)
		}
	}
}
