package openaicompat

// Streaming for the OpenAI Responses API. Unlike chat completions (which streams
// opaque `data:` chunks terminated by [DONE]), the Responses API emits typed SSE
// events (response.output_text.delta, response.completed, …). We stream text and
// reasoning deltas live, and take tool calls + usage + finish reason from the
// authoritative `response.completed` payload.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/httpclient"
)

// ResponsesStreamEvent is the JSON payload shared by Responses stream events.
// Only the fields we consume are modeled; the event kind is in Type.
type ResponsesStreamEvent struct {
	Type     string             `json:"type"`
	Delta    string             `json:"delta,omitempty"`
	Response *ResponsesResponse `json:"response,omitempty"`
	Message  string             `json:"message,omitempty"`
	Code     string             `json:"code,omitempty"`
}

// ResponsesStreamReader reads typed Responses-API SSE events.
type ResponsesStreamReader struct {
	sse *httpclient.SSEReader
}

// Read returns the next stream event, or io.EOF when the stream ends.
func (r *ResponsesStreamReader) Read() (*ResponsesStreamEvent, error) {
	for {
		event, err := r.sse.Read()
		if err != nil {
			return nil, err
		}
		if event.Data == "" {
			continue
		}
		if event.Data == "[DONE]" {
			return nil, io.EOF
		}
		var env ResponsesStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &env); err != nil {
			return nil, fmt.Errorf("failed to unmarshal responses stream event: %w", err)
		}
		// Prefer the SSE event-type line when the payload omits "type".
		if env.Type == "" {
			env.Type = event.Event
		}
		return &env, nil
	}
}

// Close closes the underlying stream.
func (r *ResponsesStreamReader) Close() error { return r.sse.Close() }

// CreateResponseStream sends a streaming Responses-API request to POST /responses.
func (c *Client) CreateResponseStream(ctx context.Context, req *ResponsesRequest) (*ResponsesStreamReader, error) {
	req.Stream = true
	headers := c.getHeaders()

	body, err := c.httpClient.DoStream(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.buildURL("/responses"),
		Headers: headers,
		Body:    req,
	})
	if err != nil {
		return nil, err
	}
	return &ResponsesStreamReader{sse: httpclient.NewSSEReader(body)}, nil
}

// ProcessResponsesStream consumes a Responses event stream and forwards neutral
// StreamChunks. Text and reasoning are streamed as deltas arrive; tool calls,
// usage, and finish reason are taken from the terminal response.completed payload.
func ProcessResponsesStream(
	ctx context.Context,
	stream *ResponsesStreamReader,
	chunks chan<- llms.StreamChunk,
	sender *llms.StreamSender,
	provider string,
	config *StreamConfig,
) {
	defer close(chunks)

	if stream == nil {
		if sender != nil {
			sender.SendFinal(llms.StreamChunk{Error: fmt.Errorf("%s: stream reader is nil", provider)})
		}
		return
	}
	if sender == nil {
		_ = stream.Close()
		return
	}
	defer func() { _ = stream.Close() }()

	readDone := make(chan struct{})
	defer close(readDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.Close()
		case <-readDone:
		}
	}()

	// A malformed/hostile stream must never crash the host process.
	defer func() {
		if r := recover(); r != nil {
			sender.DeliverTerminal(llms.StreamChunk{
				Error: fmt.Errorf("%s: panic during stream processing: %v", provider, r),
			})
		}
	}()

	var accumulatedContent string

	for {
		select {
		case <-ctx.Done():
			sender.DeliverTerminal(llms.StreamChunk{Error: ctx.Err(), Done: true})
			return
		default:
		}

		env, err := stream.Read()
		if errors.Is(err, io.EOF) {
			if ctx.Err() != nil {
				sender.DeliverTerminal(llms.StreamChunk{Error: ctx.Err(), Done: true})
				return
			}
			// Stream ended without a terminal response.completed event; close out
			// with whatever we have (optionally estimating usage).
			final := llms.StreamChunk{FinishReason: llms.FinishReasonStop}
			if config != nil && config.EstimateTokens {
				u := llms.EstimateUsageFromMessages(config.Messages, accumulatedContent)
				final.Usage = &u
			}
			sender.SendFinal(final)
			return
		}
		if err != nil {
			if ctx.Err() != nil {
				sender.DeliverTerminal(llms.StreamChunk{Error: ctx.Err(), Done: true})
				return
			}
			sender.SendFinal(llms.StreamChunk{Error: fmt.Errorf("%s: %w", provider, err)})
			return
		}

		switch env.Type {
		case "response.output_text.delta":
			if env.Delta != "" {
				accumulatedContent += env.Delta
				if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{Content: env.Delta})) {
					return
				}
			}

		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if env.Delta != "" {
				rc := &llms.ReasoningContent{Content: env.Delta}
				if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{Reasoning: rc})) {
					return
				}
			}

		case "response.completed", "response.incomplete":
			final := finalChunkFromResponse(env.Response, config, accumulatedContent)
			sender.SendFinal(final)
			return

		case "response.failed", "error":
			msg := env.Message
			if env.Response != nil && env.Response.Error != nil {
				msg = env.Response.Error.Message
			}
			if msg == "" {
				msg = "responses stream failed"
			}
			sender.DeliverTerminal(llms.StreamChunk{Error: fmt.Errorf("%s: %s", provider, msg)})
			return
		}
	}
}

// finalChunkFromResponse builds the terminal StreamChunk from a completed/incomplete
// response payload, deriving tool calls, usage, and finish reason authoritatively.
func finalChunkFromResponse(resp *ResponsesResponse, config *StreamConfig, accumulatedContent string) llms.StreamChunk {
	final := llms.StreamChunk{FinishReason: llms.FinishReasonStop}
	if resp == nil {
		return final
	}

	converted := ConvertResponsesResponse(resp)
	final.ToolCalls = converted.ToolCalls
	final.FinishReason = converted.FinishReason

	usage := converted.Usage
	if usage.TotalTokens == 0 && config != nil && config.EstimateTokens {
		usage = llms.EstimateUsageFromMessages(config.Messages, accumulatedContent)
	}
	final.Usage = &usage
	return final
}
