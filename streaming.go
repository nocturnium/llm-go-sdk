package llms

import (
	"context"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultStreamSendTimeout is the minimum timeout applied to stream sends
// to prevent goroutine leaks when consumers stop reading.
const DefaultStreamSendTimeout = 30 * time.Second

// SendResult describes the outcome of a StreamSender.Send call so callers can
// react to *why* a send stopped (and forward the correct terminal chunk).
type SendResult int

const (
	// SendOK indicates the chunk was delivered to the consumer.
	SendOK SendResult = iota
	// SendContextCanceled indicates the context was canceled/expired before the
	// chunk could be delivered.
	SendContextCanceled
	// SendTimeout indicates the consumer stopped reading and the send timed out.
	SendTimeout
)

// StreamSender provides a helper for sending chunks with timeout to prevent goroutine leaks.
// If the consumer stops reading, the send times out so the producing goroutine can exit.
//
// Terminal delivery: regardless of how a stream ends (normal EOF, error, context
// cancellation, or send timeout), exactly one terminal chunk (one with Done=true
// or a non-nil Error) is delivered to the consumer. Use SendFinal for a normal
// terminal chunk and DeliverTerminal for terminal chunks produced on early-exit
// paths. The terminalSent guard ensures a single terminal chunk is emitted.
type StreamSender struct {
	chunks       chan<- StreamChunk
	timeout      time.Duration
	ctx          context.Context
	timedOut     atomic.Bool // tracks if we've already timed out
	terminalSent atomic.Bool // tracks if a terminal (Done/Error) chunk was delivered
}

// NewStreamSender creates a new StreamSender with the configured timeout.
// If sendTimeout is 0 or negative, the default timeout (DefaultStreamSendTimeout,
// 30s) is applied to prevent goroutine leaks.
func NewStreamSender(ctx context.Context, chunks chan<- StreamChunk, sendTimeout time.Duration) *StreamSender {
	timeout := DefaultStreamSendTimeout
	if sendTimeout > 0 {
		timeout = sendTimeout
	}
	return &StreamSender{
		chunks:  chunks,
		timeout: timeout,
		ctx:     ctx,
	}
}

// Send attempts to send a chunk to the channel.
// It returns a SendResult describing the outcome:
//   - SendOK: the chunk was delivered.
//   - SendContextCanceled: the context was canceled/expired before delivery.
//   - SendTimeout: the consumer stopped reading and the send timed out.
//
// Send does NOT itself emit a terminal chunk on the non-OK paths; callers are
// expected to forward the appropriate terminal chunk via DeliverTerminal (or
// SendFinal) so that exactly one terminal chunk is delivered on every exit path.
func (s *StreamSender) Send(chunk StreamChunk) SendResult {
	// If we've already timed out, don't try to send again.
	if s.timedOut.Load() {
		return SendTimeout
	}

	// Fast path: buffered stream channels almost always have room, so avoid
	// allocating a timer unless the send would block.
	select {
	case s.chunks <- chunk:
		// If a terminal chunk (Done or Error) passes through Send, record it so
		// EnsureTerminal/DeliverTerminal do not emit a duplicate terminal chunk.
		if chunk.Done || chunk.Error != nil {
			s.terminalSent.Store(true)
		}
		return SendOK
	default:
	}

	// Slow path: the channel is full, so wait for room, context cancellation, or
	// the bounded timeout. Use time.NewTimer instead of time.After to prevent
	// timer leaks in high-throughput streaming scenarios.
	timer := time.NewTimer(s.timeout)
	defer timer.Stop()

	select {
	case <-s.ctx.Done():
		return SendContextCanceled
	case s.chunks <- chunk:
		// If a terminal chunk (Done or Error) passes through Send, record it so
		// EnsureTerminal/DeliverTerminal do not emit a duplicate terminal chunk.
		if chunk.Done || chunk.Error != nil {
			s.terminalSent.Store(true)
		}
		return SendOK
	case <-timer.C:
		// Mark as timed out so subsequent sends short-circuit.
		s.timedOut.Store(true)
		return SendTimeout
	}
}

// SendOK reports whether the result indicates the chunk was delivered.
func (r SendResult) SendOK() bool { return r == SendOK }

// EnsureTerminal guarantees a terminal chunk has been delivered to the consumer.
// If a terminal chunk (Done or Error) was already sent — whether via SendFinal,
// DeliverTerminal, or a terminal chunk that passed through Send — this is a no-op.
// Otherwise it delivers a terminal chunk derived from the context: the context
// error if the context was canceled/expired, or a Done chunk for a clean finish.
//
// Stream wrappers call this after their source channel drains so that a source
// which closes without ever emitting a terminal chunk (e.g. an upstream producer
// that reacts to context cancellation by simply closing) still results in the
// consumer observing a terminal chunk rather than a silent close.
func (s *StreamSender) EnsureTerminal() {
	if s.terminalSent.Load() {
		return
	}
	if err := s.ctx.Err(); err != nil {
		s.DeliverTerminal(StreamChunk{Error: err})
		return
	}
	s.DeliverTerminal(StreamChunk{Done: true})
}

// ForwardTerminalOnEarlyExit inspects a SendResult from Send and, if the send did
// not succeed, delivers the appropriate terminal chunk to the consumer:
//   - SendContextCanceled: a chunk carrying the context error (so a truncated
//     stream is never mistaken for a successful completion).
//   - SendTimeout: a chunk carrying ErrStreamTimeout.
//
// It returns true when the caller should stop streaming (i.e. the result was not
// SendOK). This centralizes the "emit exactly one terminal chunk on every exit
// path" guarantee for provider stream goroutines.
func (s *StreamSender) ForwardTerminalOnEarlyExit(result SendResult) (stop bool) {
	switch result {
	case SendOK:
		return false
	case SendContextCanceled:
		deliverTerminalForContext(s.ctx, s)
		return true
	case SendTimeout:
		s.DeliverTerminal(StreamChunk{Error: ErrStreamTimeout})
		return true
	default:
		s.DeliverTerminal(StreamChunk{Done: true})
		return true
	}
}

// SendFinal sends a final chunk (with Done=true) as the stream's terminal chunk.
// It guarantees the terminal chunk is delivered to the consumer even if the
// consumer has stopped reading or the context was canceled, and ensures only a
// single terminal chunk is emitted across SendFinal/DeliverTerminal calls.
func (s *StreamSender) SendFinal(chunk StreamChunk) {
	chunk.Done = true
	s.DeliverTerminal(chunk)
}

// DeliverTerminal reliably delivers a single terminal chunk (Done=true or a
// non-nil Error) to the consumer. It is safe to call from any early-exit path
// (context cancellation, send timeout, error). Only the first terminal chunk is
// delivered; subsequent calls are no-ops.
//
// Delivery does not depend on the (possibly canceled) stream context: the
// terminal chunk is pushed into the buffered output channel, falling back to a
// bounded wait so a slow consumer still observes it. If the consumer has fully
// stopped reading and the buffer is saturated for the full timeout, delivery is
// abandoned to avoid a goroutine leak — but in that case the consumer has
// already abandoned the stream.
func (s *StreamSender) DeliverTerminal(chunk StreamChunk) {
	// Ensure the chunk is unambiguously terminal.
	if chunk.Error == nil && !chunk.Done {
		chunk.Done = true
	}

	// Only the first terminal chunk is delivered.
	if !s.terminalSent.CompareAndSwap(false, true) {
		return
	}

	// Fast path: buffered channel almost always accepts a single terminal chunk.
	select {
	case s.chunks <- chunk:
		return
	default:
	}

	// Slow path: wait (bounded) for the consumer to make room. Deliberately do
	// not select on s.ctx.Done(): the context is frequently already canceled on
	// the very paths that need to report a terminal chunk.
	timer := time.NewTimer(s.timeout)
	defer timer.Stop()

	select {
	case s.chunks <- chunk:
	case <-timer.C:
		// Consumer has stopped reading entirely; abandon to avoid a leak.
	}
}

// TimedOut returns true if the sender has experienced a timeout.
func (s *StreamSender) TimedOut() bool {
	return s.timedOut.Load()
}

// GetBufferSize returns the appropriate buffer size based on options.
// Returns the default (100) if the configured value is 0 or negative.
func GetBufferSize(opts *CallOptions) int {
	if opts.StreamBufferSize > 0 {
		return opts.StreamBufferSize
	}
	return 100 // default
}

// GetSendTimeout returns the send timeout from options.
// Returns the default (DefaultStreamSendTimeout, 30s) if the configured value is
// 0 or negative. This aligns with NewStreamSender's behavior which always uses a
// minimum timeout.
func GetSendTimeout(opts *CallOptions) time.Duration {
	if opts.StreamSendTimeout > 0 {
		return opts.StreamSendTimeout
	}
	// Return default timeout to match NewStreamSender behavior
	return DefaultStreamSendTimeout
}

// StreamProcessor is a function that processes each chunk in a stream.
// It can modify the chunk, perform side effects (like tracking usage), or both.
// Return the (possibly modified) chunk to forward it to the consumer.
type StreamProcessor func(chunk StreamChunk) StreamChunk

// StreamResult is the terminal outcome of a fully drained stream.
type StreamResult struct {
	Content      string
	Reasoning    *ReasoningContent
	ToolCalls    []ToolCall
	FinishReason FinishReason
	Usage        *Usage
}

// CollectStream drains stream into a StreamResult.
// It accumulates text content and reasoning content from non-error chunks,
// captures terminal metadata from the Done chunk, and returns immediately with
// the partial result when a terminal error chunk is observed.
func CollectStream(stream <-chan StreamChunk) (StreamResult, error) {
	var result StreamResult
	var content strings.Builder
	var reasoning strings.Builder

	for chunk := range stream {
		if chunk.Error != nil {
			result.Content = content.String()
			if reasoning.Len() > 0 {
				result.Reasoning = &ReasoningContent{Content: reasoning.String()}
			}
			return result, chunk.Error
		}

		content.WriteString(chunk.Content)
		if chunk.Reasoning != nil {
			reasoning.WriteString(chunk.Reasoning.Content)
		}
		if chunk.Done {
			result.FinishReason = chunk.FinishReason
			result.Usage = chunk.Usage
			result.ToolCalls = chunk.ToolCalls
		}
	}

	result.Content = content.String()
	if reasoning.Len() > 0 {
		result.Reasoning = &ReasoningContent{Content: reasoning.String()}
	}
	return result, nil
}

// StreamText drains stream and returns its accumulated text content.
// If the stream ends with a terminal error chunk, the partial text and that
// error are returned.
func StreamText(stream <-chan StreamChunk) (string, error) {
	result, err := CollectStream(stream)
	return result.Content, err
}

// WrapStream wraps a source stream with common middleware setup:
// - Creates a buffered output channel based on options
// - Sets up a StreamSender with timeout to prevent goroutine leaks
// - Runs the processor function on each chunk in a goroutine
// - Properly closes the output channel when done
//
// This eliminates boilerplate code across middleware implementations.
//
// Example usage:
//
//	func (m *MyMiddleware) Stream(ctx context.Context, messages []Message, options ...CallOption) (<-chan StreamChunk, error) {
//	    source, err := m.llm.Stream(ctx, messages, options...)
//	    if err != nil {
//	        return nil, err
//	    }
//	    opts := ApplyOptions(options...)
//	    return WrapStream(ctx, source, opts, func(chunk StreamChunk) StreamChunk {
//	        // Process chunk (e.g., track usage)
//	        if chunk.Usage != nil {
//	            m.recordUsage(chunk.Usage)
//	        }
//	        return chunk
//	    }), nil
//	}
func WrapStream(ctx context.Context, source <-chan StreamChunk, opts *CallOptions, processor StreamProcessor) <-chan StreamChunk {
	bufferSize := GetBufferSize(opts)
	wrapped := make(chan StreamChunk, bufferSize)
	sender := NewStreamSender(ctx, wrapped, opts.StreamSendTimeout)

	go func() {
		defer close(wrapped)
		// After the source drains (or this goroutine returns early), guarantee the
		// consumer observed a terminal chunk rather than a silent close.
		defer sender.EnsureTerminal()
		for chunk := range source {
			// Apply processor if provided
			if processor != nil {
				chunk = processor(chunk)
			}
			// Use StreamSender to prevent goroutine leak if consumer stops reading.
			// On early exit, forward a terminal chunk so the consumer never sees a
			// silent close that looks like a successful completion.
			if sender.ForwardTerminalOnEarlyExit(sender.Send(chunk)) {
				return
			}
		}
	}()

	return wrapped
}

// deliverTerminalForContext forwards a terminal chunk derived from the context's
// cancellation cause. The cause (context.Canceled or context.DeadlineExceeded) is
// reported via the Error field so that truncation is never mistaken for a
// successful completion. If the context has no error (defensive), a Done chunk is
// delivered instead.
func deliverTerminalForContext(ctx context.Context, sender *StreamSender) {
	if err := ctx.Err(); err != nil {
		sender.DeliverTerminal(StreamChunk{Error: err})
		return
	}
	sender.DeliverTerminal(StreamChunk{Done: true})
}

// WrapStreamWithFinalizer is like WrapStream but also calls a finalizer function
// after all chunks have been processed or when the goroutine exits early.
// The finalizer receives the last usage seen (may be nil) and any accumulated state.
//
// Example usage for tracking total usage:
//
//	var totalUsage *Usage
//	return WrapStreamWithFinalizer(ctx, source, opts,
//	    func(chunk StreamChunk) StreamChunk {
//	        if chunk.Usage != nil {
//	            totalUsage = chunk.Usage
//	        }
//	        return chunk
//	    },
//	    func() {
//	        if totalUsage != nil {
//	            m.recordFinalUsage(totalUsage)
//	        }
//	    },
//	), nil
func WrapStreamWithFinalizer(ctx context.Context, source <-chan StreamChunk, opts *CallOptions, processor StreamProcessor, finalizer func()) <-chan StreamChunk {
	bufferSize := GetBufferSize(opts)
	wrapped := make(chan StreamChunk, bufferSize)
	sender := NewStreamSender(ctx, wrapped, opts.StreamSendTimeout)

	go func() {
		defer close(wrapped)
		defer func() {
			if finalizer != nil {
				finalizer()
			}
		}()
		// Registered last so it runs first (before the finalizer and close):
		// guarantee the consumer observed a terminal chunk rather than a silent close.
		defer sender.EnsureTerminal()

		for chunk := range source {
			// Apply processor if provided
			if processor != nil {
				chunk = processor(chunk)
			}
			// Use StreamSender to prevent goroutine leak if consumer stops reading.
			// On early exit, forward a terminal chunk so the consumer never sees a
			// silent close that looks like a successful completion.
			if sender.ForwardTerminalOnEarlyExit(sender.Send(chunk)) {
				return
			}
		}
	}()

	return wrapped
}
