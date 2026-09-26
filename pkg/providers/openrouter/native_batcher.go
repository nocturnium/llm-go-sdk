package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// NativeBatcher adapts OpenRouter's asynchronous Batch API to
// [llms.BatchProcessor], so code written against the interface can run on the
// discounted batch lane instead of fanning out live calls.
//
// It is opt-in and separate from *Client on purpose: ProcessBatch here submits a
// batch and then blocks on it, and OpenRouter's completion window is 24 hours.
// Wrapping the client in [llms.NewConcurrentBatcher] instead keeps the old
// behavior of concurrent live calls at standard pricing.
//
//	batcher := openrouter.NewNativeBatcher(client)
//	ctx, cancel := context.WithTimeout(ctx, 6*time.Hour)
//	defer cancel()
//	resp, err := batcher.ProcessBatch(ctx, requests)
type NativeBatcher struct {
	client   *Client
	interval time.Duration
}

// NativeBatcherOption configures a [NativeBatcher].
type NativeBatcherOption func(*NativeBatcher)

// WithBatchPollInterval sets the initial polling cadence for ProcessBatch. It
// backs off from there, to at most five minutes. Default 15 seconds.
func WithBatchPollInterval(d time.Duration) NativeBatcherOption {
	return func(b *NativeBatcher) {
		if d > 0 {
			b.interval = d
		}
	}
}

// NewNativeBatcher wraps a client as an [llms.BatchProcessor] backed by the
// native Batch API.
func NewNativeBatcher(client *Client, opts ...NativeBatcherOption) *NativeBatcher {
	b := &NativeBatcher{client: client, interval: defaultBatchPollInterval}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

var _ llms.BatchProcessor = (*NativeBatcher)(nil)

// ProcessBatch submits every request as one batch and blocks until it reaches a
// terminal state, then maps the inline result lines back onto request IDs.
//
// Which [llms.BatchOption] values apply: WithMaxBatchSize is enforced locally
// before submitting, and WithProgressCallback is driven from the batch's
// request counts on each poll. MaxConcurrency and ContinueOnError have no
// meaning for a server-side batch and are ignored, as is RequestTimeout: the
// deadline that matters is the one on ctx, which must be generous enough for a
// batch that may take hours.
//
// Every request in the batch runs on one model, since the batch model is
// submitted once. Requests that resolve to different models are rejected with
// [llms.ErrInvalidParameters] rather than silently re-pointed. The batch API is
// text-only, so image parts are rejected here rather than at upstream validation.
//
// A batch that ends failed, expired or canceled returns an error wrapping
// [llms.ErrJobFailed]. A completed batch can still hold per-request failures;
// those land on the individual [llms.BatchResult] values, and the response
// carries the same counts as the local batcher.
func (b *NativeBatcher) ProcessBatch(ctx context.Context, requests []llms.BatchRequest, options ...llms.BatchOption) (*llms.BatchResponse, error) {
	if len(requests) == 0 {
		return nil, llms.ErrBatchEmpty
	}
	opts := llms.ApplyBatchOptions(options...)
	if opts.MaxBatchSize > 0 && len(requests) > opts.MaxBatchSize {
		return nil, fmt.Errorf("%w: %d requests exceeds limit of %d", llms.ErrBatchTooLarge, len(requests), opts.MaxBatchSize)
	}
	submission, err := b.buildSubmission(requests)
	if err != nil {
		return nil, openaicompat.WrapError(b.client.Provider(), "process batch", err)
	}

	start := time.Now()
	created, err := b.client.SubmitBatch(ctx, *submission)
	if err != nil {
		return nil, err
	}
	waitOpts := []WaitOption{WithPollInterval(b.interval)}
	if opts.ProgressCallback != nil {
		waitOpts = append(waitOpts, WithPollCallback(func(batch *Batch) {
			done := batch.RequestCounts.Completed + batch.RequestCounts.Failed
			opts.ProgressCallback(done, len(requests))
		}))
	}
	batch, err := b.client.WaitBatch(ctx, created.ID, waitOpts...)
	if err != nil {
		return nil, err
	}
	if batch.Status != BatchCompleted {
		return nil, openaicompat.WrapError(b.client.Provider(), "process batch", batchStatusError(batch))
	}
	return collectBatchResults(requests, batch, b.client.Model(), time.Since(start)), nil
}

// buildSubmission converts neutral batch requests into one chat-completions
// submission, resolving the single batch-wide model.
func (b *NativeBatcher) buildSubmission(requests []llms.BatchRequest) (*BatchSubmission, error) {
	model := ""
	items := make([]BatchItem, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		if request.ID == "" {
			return nil, fmt.Errorf("batch request ID is required: %w", llms.ErrInvalidParameters)
		}
		if _, ok := seen[request.ID]; ok {
			return nil, fmt.Errorf("duplicate batch request ID %q: %w", request.ID, llms.ErrInvalidParameters)
		}
		seen[request.ID] = struct{}{}

		callOpts := llms.ApplyOptions(request.Options...)
		if err := callOpts.Validate(); err != nil {
			return nil, err
		}
		prepared, err := llms.PrepareMessages(request.Messages, callOpts)
		if err != nil {
			return nil, err
		}
		if err := llms.ValidateInlineSystem(prepared); err != nil {
			return nil, err
		}
		if err := llms.ValidateToolCallIDs(prepared); err != nil {
			return nil, err
		}
		if err := requireTextOnly(request.ID, prepared); err != nil {
			return nil, err
		}

		requestModel := callOpts.Model
		if requestModel == "" {
			requestModel = b.client.Model()
		}
		if model == "" {
			model = requestModel
		}
		if requestModel != model {
			return nil, fmt.Errorf("a batch carries one model, got %q and %q: %w", model, requestModel, llms.ErrInvalidParameters)
		}
		items = append(items, BatchItem{CustomID: request.ID, Body: openaicompat.BuildChatRequest(requestModel, prepared, callOpts, false)})
	}
	return &BatchSubmission{Endpoint: BatchEndpointChatCompletions, Model: model, Requests: items}, nil
}

// requireTextOnly rejects the multimodal parts the batch API refuses upstream,
// so the caller sees which request is at fault instead of a validation error
// naming a custom_id.
func requireTextOnly(id string, messages []llms.Message) error {
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type != llms.PartTypeText {
				return fmt.Errorf("batch request %q carries a %s part and batches are text-only: %w", id, part.Type, llms.ErrInvalidParameters)
			}
		}
	}
	return nil
}

// batchStatusError describes a non-completed terminal batch.
func batchStatusError(batch *Batch) error {
	detail := string(batch.Status)
	if batch.Error != nil && batch.Error.Message != "" {
		detail += ": " + batch.Error.Message
	}
	return fmt.Errorf("batch %s ended %s: %w", batch.ID, detail, llms.ErrJobFailed)
}

// collectBatchResults maps result lines back onto the submitted request IDs. A
// request with no line is recorded as a failure so the counts stay honest.
//
// Each response is stamped with the model its request asked for, the request's
// WithModel override or else defaultModel, like a synchronous call's.
func collectBatchResults(requests []llms.BatchRequest, batch *Batch, defaultModel string, duration time.Duration) *llms.BatchResponse {
	out := &llms.BatchResponse{Results: make(map[string]*llms.BatchResult, len(requests)), Duration: duration}
	lines := make(map[string]BatchResultLine, len(batch.Results))
	for _, line := range batch.Results {
		lines[line.CustomID] = line
	}
	for _, request := range requests {
		line, ok := lines[request.ID]
		if !ok {
			out.Results[request.ID] = &llms.BatchResult{ID: request.ID, Error: fmt.Errorf("batch %s returned no result for %q: %w", batch.ID, request.ID, llms.ErrIncompleteResponse)}
			out.FailureCount++
			continue
		}
		response, err := convertBatchLine(line)
		if err != nil {
			out.Results[request.ID] = &llms.BatchResult{ID: request.ID, Error: err}
			out.FailureCount++
			continue
		}
		requestModel := llms.ApplyOptions(request.Options...).Model
		if requestModel == "" {
			requestModel = defaultModel
		}
		llms.StampResponse(response, llms.ProviderOpenRouter, requestModel)
		out.Results[request.ID] = &llms.BatchResult{ID: request.ID, Response: response}
		out.SuccessCount++
		out.TotalUsage.PromptTokens += response.Usage.PromptTokens
		out.TotalUsage.CompletionTokens += response.Usage.CompletionTokens
		out.TotalUsage.TotalTokens += response.Usage.TotalTokens
		out.TotalUsage.CacheReadTokens += response.Usage.CacheReadTokens
		out.TotalUsage.CacheCreationTokens += response.Usage.CacheCreationTokens
		out.TotalUsage.ReasoningTokens += response.Usage.ReasoningTokens
	}
	return out
}

// convertBatchLine turns one result line into a response, or into the error the
// line reports. Exactly one of the two fields is populated per the wire
// contract; a line with neither is treated as a failure rather than an empty
// success.
func convertBatchLine(line BatchResultLine) (*llms.Response, error) {
	if line.Error != nil {
		message := line.Error.Message
		if message == "" {
			message = line.Error.Code
		}
		return nil, fmt.Errorf("batch request %q failed (%s): %w", line.CustomID, message, llms.ErrJobFailed)
	}
	if line.Response == nil {
		return nil, fmt.Errorf("batch request %q returned neither a response nor an error: %w", line.CustomID, llms.ErrIncompleteResponse)
	}
	if line.Response.StatusCode >= 400 {
		return nil, fmt.Errorf("batch request %q failed with status %d: %w", line.CustomID, line.Response.StatusCode, llms.ErrJobFailed)
	}
	var completion openaicompat.ChatCompletionResponse
	if err := json.Unmarshal(line.Response.Body, &completion); err != nil {
		return nil, fmt.Errorf("batch request %q returned an undecodable body: %w", line.CustomID, err)
	}
	return openaicompat.ConvertResponse(&completion), nil
}
