# Streaming

Streaming lets you consume a model's response incrementally as tokens arrive, rather than waiting for the full completion. This is the right choice for interactive UIs (chat, REPLs), for long generations where time-to-first-token matters, and for surfacing partial output to a user as soon as it is available.

Every chat client in the SDK implements `Stream` as part of the `LLM` interface:

```go
Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error)
```

`Stream` accepts the same messages and [`CallOption`s](../configuration.md) as `GenerateContent`. It returns a **receive-only channel** of `llms.StreamChunk` values. The error in the return tuple is non-nil only for *setup* failures (invalid options, a failed initial connection); errors that occur mid-stream are delivered as a chunk on the channel (see [Terminal-chunk guarantee](#terminal-chunk-guarantee)).

!!! note
    Streaming is opt-in per call — it is a different method, not a flag on `GenerateContent`. There is no separate "enable streaming" option; calling `Stream` is what enables it.

## The StreamChunk type

Each value the channel yields is a `llms.StreamChunk`:

```go
type StreamChunk struct {
	// Content is the text content in this chunk.
	Content string

	// Reasoning contains reasoning content in this chunk (nil if none).
	Reasoning *ReasoningContent

	// ToolCalls contains any tool calls in this chunk (may be partial).
	ToolCalls []ToolCall

	// FinishReason is set on the final chunk.
	FinishReason FinishReason

	// Usage is only populated on the final chunk (if available).
	Usage *Usage

	// Error is set if an error occurred during streaming.
	Error error

	// Done indicates this is the final chunk.
	Done bool
}
```

Most chunks carry only an incremental `Content` fragment. The **terminal chunk** is distinguished by `Done == true` (a clean finish) or `Error != nil` (a failure). The terminal chunk is where you find the `FinishReason` and the final `Usage` (when the provider reports it).

| Field | When populated |
|-------|----------------|
| `Content` | Most non-terminal chunks; the incremental text fragment to append |
| `Reasoning` | Chunks from reasoning-capable providers (o1, DeepSeek, GLM); nil otherwise |
| `ToolCalls` | When the model is emitting a tool call; may be partial across chunks |
| `FinishReason` | The terminal chunk |
| `Usage` | The terminal chunk, when the provider reports token counts |
| `Error` | A terminal error chunk only |
| `Done` | The terminal chunk on a clean finish |

## The canonical consume loop

The idiomatic way to consume a stream is to `range` over the channel, check each chunk for an error, accumulate the content, and handle the terminal chunk. Because the SDK guarantees the channel is always closed, a plain `range` will terminate.

```go
package main

import (
	"context"
	"fmt"
	"os"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/openai"
)

func main() {
	client, err := openai.New(openai.WithModel("gpt-4o"))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Write a haiku about streaming data."},
	}

	stream, err := client.Stream(ctx, messages)
	if err != nil {
		panic(err) // setup error (bad options, failed connection)
	}

	var full string
	for chunk := range stream {
		// 1. Always check for a mid-stream error first.
		if chunk.Error != nil {
			fmt.Fprintf(os.Stderr, "\nstream error: %v\n", chunk.Error)
			break
		}

		// 2. Accumulate / render incremental content.
		if chunk.Content != "" {
			full += chunk.Content
			fmt.Print(chunk.Content) // render token-by-token
		}

		// 3. Handle the terminal chunk.
		if chunk.Done {
			fmt.Printf("\n\n[finish: %s]\n", chunk.FinishReason)
			if chunk.Usage != nil {
				fmt.Printf("[tokens: %d prompt + %d completion = %d total]\n",
					chunk.Usage.PromptTokens,
					chunk.Usage.CompletionTokens,
					chunk.Usage.TotalTokens)
			}
		}
	}

	_ = full // the fully accumulated response
}
```

The three steps inside the loop — **check `Error`**, **accumulate `Content`**, **handle the terminal chunk** — are the pattern to remember.

!!! tip
    Check `chunk.Error` before reading any other field. On an error chunk, `Content` and `Usage` may be empty or partial; treating the error path first keeps your accumulation logic clean.

## Terminal-chunk guarantee

The SDK provides a strong delivery contract:

> **Exactly one terminal chunk is delivered on every exit path**, and the channel is always closed afterward.

A terminal chunk is one with `Done == true` **or** a non-nil `Error`. This holds regardless of *how* the stream ends:

- **Normal completion** — a final chunk with `Done == true`, carrying `FinishReason` and (when available) `Usage`.
- **Mid-stream provider error** — a final chunk with `Error != nil`.
- **Context cancellation or deadline** — a final chunk with `Error` set to the context error (`context.Canceled` or `context.DeadlineExceeded`).
- **Consumer stops reading (send timeout)** — a terminal chunk carrying `llms.ErrStreamTimeout` is delivered if there is room; the producing goroutine then exits.

This means two things for your code:

1. You never have to distinguish "the channel closed" from "the stream finished" — a silent close that looks like success cannot happen. If the loop ends, you will have seen a terminal chunk (unless you `break`ed early yourself).
2. You should not assume `Done == true` implies success. A truncated stream caused by cancellation surfaces as an `Error` chunk, never as a clean `Done`. Always inspect `chunk.Error`.

!!! warning
    Because a canceled stream reports `chunk.Error` (a context error) rather than `Done`, never treat reaching the end of the `range` loop as proof the generation completed. The `Error`-then-`break` branch in the canonical loop is what distinguishes a truncated stream from a finished one.

## Cancellation

Cancelling the `context.Context` you passed to `Stream` stops generation. The stream responds by delivering a single terminal **error** chunk carrying the context's error, then closing the channel.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

stream, err := client.Stream(ctx, messages)
if err != nil {
	return err
}

for chunk := range stream {
	if chunk.Error != nil {
		// On cancellation/deadline this is context.Canceled or
		// context.DeadlineExceeded.
		if errors.Is(chunk.Error, context.DeadlineExceeded) {
			fmt.Println("\n[stream timed out]")
		}
		break
	}
	fmt.Print(chunk.Content)
}
```

To stop early from inside the loop (for example, once you have enough output), call `cancel()` and continue draining — or simply `break`. Either way the SDK's send-timeout machinery prevents the producer goroutine from leaking:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

stream, _ := client.Stream(ctx, messages)
for chunk := range stream {
	if chunk.Error != nil {
		break
	}
	fmt.Print(chunk.Content)
	if enoughOutput(chunk) {
		cancel() // signal the producer to stop
		break    // stop consuming
	}
}
```

!!! note
    `break`ing out of the loop without cancelling is safe: the SDK bounds how long the producer will wait to deliver a chunk to a stalled consumer (see [`WithStreamSendTimeout`](#tuning-the-send-timeout) below). Cancelling the context is still the cleanest way to tell the provider to stop generating immediately.

## Tuning the send timeout

When you stop reading from the channel but the producer is still generating, the producer's send would block forever. To prevent that goroutine leak, each send is bounded by a timeout. If the consumer does not read for that long, the producer abandons delivery and exits, and a terminal chunk carrying `llms.ErrStreamTimeout` is delivered when possible.

The default is `llms.DefaultStreamSendTimeout` (30 seconds). Override it per call with `WithStreamSendTimeout`:

```go
stream, err := client.Stream(ctx, messages,
	llms.WithStreamSendTimeout(5*time.Second),
)
```

A value of `0` (or negative) leaves the default in effect rather than disabling the timeout — the SDK always applies a minimum to avoid leaks.

You can also size the channel buffer with `WithStreamBufferSize` (default 100). A larger buffer reduces backpressure on the producer at the cost of memory:

```go
stream, err := client.Stream(ctx, messages,
	llms.WithStreamBufferSize(256),
	llms.WithStreamSendTimeout(10*time.Second),
)
```

## Collecting a full response

If you do not need to render chunks incrementally, use `llms.CollectStream` to drain the stream into a single result. It accumulates content and reasoning, captures terminal `FinishReason`, `Usage`, and `ToolCalls`, and returns any terminal error chunk as the function error so it cannot be missed.

```go
stream, err := client.Stream(ctx, messages)
if err != nil {
	return err
}

result, err := llms.CollectStream(stream)
if err != nil {
	return err // provider error, context cancellation, or llms.ErrStreamTimeout
}

fmt.Println(result.Content)
if result.Usage != nil {
	fmt.Printf("total tokens: %d\n", result.Usage.TotalTokens)
}
```

For the most common text-only case, `llms.StreamText` returns the accumulated content plus any terminal error:

```go
stream, err := client.Stream(ctx, messages)
if err != nil {
	return err
}

text, err := llms.StreamText(stream)
if err != nil {
	return err
}
fmt.Println(text)
```

!!! tip
    If you do not actually need incremental delivery, prefer `client.GenerateContent(ctx, messages)` — it returns a fully populated `*llms.Response` and avoids the bookkeeping entirely. Use `Stream` only when partial output, lower time-to-first-token, or early cancellation matter.

## Streaming reasoning and tool calls

Beyond plain text, chunks can carry reasoning content and tool calls.

**Reasoning** (`Reasoning`) arrives on providers that expose chain-of-thought
(OpenAI o1, DeepSeek, Z.AI GLM). It is delivered as
`*llms.ReasoningContent`; nil on providers that do not support it.

```go
for chunk := range stream {
	if chunk.Error != nil {
		break
	}
	if chunk.Reasoning != nil && chunk.Reasoning.Content != "" {
		fmt.Print("[thinking] ", chunk.Reasoning.Content)
	}
	if chunk.Content != "" {
		fmt.Print(chunk.Content)
	}
}
```

**Tool calls** (`ToolCalls`) may be emitted incrementally — a single logical tool call can be split across several chunks, so treat them as partial until the terminal chunk reports `FinishReason == llms.FinishReasonToolCalls`. For a full agentic loop that drives tools to completion, use [`llms.RunTools`](tools.md) rather than assembling tool-call deltas by hand.

## Streaming through middleware

Resilience and observability wrappers implement `Stream` too, so they compose transparently. The wrapped client returns the same `<-chan llms.StreamChunk` and honors the same terminal-chunk and send-timeout guarantees:

```go
base, _ := openai.New(openai.WithModel("gpt-4o"))
// observability lives in pkg/observability
client, err := observability.NewOTelMiddleware(base) // tracing wrapper
if err != nil {
	return err
}

stream, err := client.Stream(ctx, messages)
// consume exactly as before
```

!!! note
    Retries have limited reach for streaming: a wrapper such as `ResilientClient` can retry only the *initial connection*, not a failure that occurs partway through an in-progress stream. Once chunks have begun flowing, a mid-stream error is surfaced on the channel as a terminal error chunk.
