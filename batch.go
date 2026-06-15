package llms

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Common batch errors.
var (
	// ErrBatchNotSupported is a sentinel that a Batcher implementation may return
	// from ProcessBatch when it does not support batch processing. The built-in
	// ConcurrentBatcher always supports batching and never returns it.
	ErrBatchNotSupported = errors.New("batch requests not supported by this provider")
	ErrBatchEmpty        = errors.New("batch is empty")
	// ErrBatchTooLarge is returned by ProcessBatch when the number of requests
	// exceeds the limit configured via WithMaxBatchSize.
	ErrBatchTooLarge = errors.New("batch exceeds maximum size")
)

// BatchProcessor is an interface for providers that support batch processing.
type BatchProcessor interface {
	// ProcessBatch processes multiple requests in a batch
	ProcessBatch(ctx context.Context, requests []BatchRequest, options ...BatchOption) (*BatchResponse, error)
}

// BatchRequest represents a single request in a batch.
type BatchRequest struct {
	// ID is a unique identifier for this request within the batch
	ID string

	// Messages is the conversation to process
	Messages []Message

	// Options are the call options for this specific request
	Options []CallOption
}

// BatchResponse contains the results of a batch operation.
type BatchResponse struct {
	// Results contains the individual results keyed by request ID
	Results map[string]*BatchResult

	// TotalUsage is the aggregate token usage across all requests
	TotalUsage Usage

	// Duration is the total time to process the batch
	Duration time.Duration

	// SuccessCount is the number of successful requests
	SuccessCount int

	// FailureCount is the number of failed requests
	FailureCount int
}

// BatchResult represents the result of a single request in a batch.
type BatchResult struct {
	// ID is the request ID
	ID string

	// Response is the LLM response (nil if error occurred)
	Response *Response

	// Error is any error that occurred (nil if successful)
	Error error

	// Duration is the time to process this specific request
	Duration time.Duration
}

// BatchOptions configures batch processing.
type BatchOptions struct {
	// MaxConcurrency limits the number of concurrent requests
	// Default is 5
	MaxConcurrency int

	// Timeout per individual request
	// Default is 60 seconds
	RequestTimeout time.Duration

	// ContinueOnError determines if processing continues after an error
	// Default is true
	ContinueOnError bool

	// ProgressCallback is called after each request completes
	ProgressCallback func(completed, total int)

	// MaxBatchSize, when > 0, caps the number of requests accepted by a single
	// ProcessBatch call; exceeding it returns ErrBatchTooLarge. Default 0 (no limit).
	MaxBatchSize int
}

// BatchOption is a function that modifies BatchOptions.
type BatchOption func(*BatchOptions)

// ApplyBatchOptions applies options and returns the result.
func ApplyBatchOptions(opts ...BatchOption) *BatchOptions {
	options := &BatchOptions{
		MaxConcurrency:  5,
		RequestTimeout:  60 * time.Second,
		ContinueOnError: true,
	}
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// WithMaxConcurrency sets the maximum number of concurrent requests.
func WithMaxConcurrency(n int) BatchOption {
	return func(o *BatchOptions) {
		if n > 0 {
			o.MaxConcurrency = n
		}
	}
}

// WithMaxBatchSize caps how many requests a single ProcessBatch call accepts.
// When the limit is exceeded, ProcessBatch returns ErrBatchTooLarge. A value of
// 0 (the default) means no limit.
func WithMaxBatchSize(n int) BatchOption {
	return func(o *BatchOptions) {
		if n > 0 {
			o.MaxBatchSize = n
		}
	}
}

// WithRequestTimeout sets the timeout for individual requests.
func WithRequestTimeout(d time.Duration) BatchOption {
	return func(o *BatchOptions) {
		o.RequestTimeout = d
	}
}

// WithContinueOnError sets whether to continue processing after an error.
func WithContinueOnError(continueOnError bool) BatchOption {
	return func(o *BatchOptions) {
		o.ContinueOnError = continueOnError
	}
}

// WithProgressCallback sets a callback for progress updates.
func WithProgressCallback(cb func(completed, total int)) BatchOption {
	return func(o *BatchOptions) {
		o.ProgressCallback = cb
	}
}

// ConcurrentBatcher implements local concurrent batch processing
// for any LLM that doesn't support native batch APIs.
type ConcurrentBatcher struct {
	llm LLM
}

// NewConcurrentBatcher creates a new concurrent batcher for the given LLM.
func NewConcurrentBatcher(llm LLM) *ConcurrentBatcher {
	return &ConcurrentBatcher{llm: llm}
}

// ProcessBatch processes multiple requests concurrently.
func (b *ConcurrentBatcher) ProcessBatch(ctx context.Context, requests []BatchRequest, options ...BatchOption) (*BatchResponse, error) {
	if len(requests) == 0 {
		return nil, ErrBatchEmpty
	}
	if err := validateBatchRequestIDs(requests); err != nil {
		return nil, err
	}

	opts := ApplyBatchOptions(options...)
	if opts.MaxBatchSize > 0 && len(requests) > opts.MaxBatchSize {
		return nil, fmt.Errorf("%w: %d requests exceeds limit of %d", ErrBatchTooLarge, len(requests), opts.MaxBatchSize)
	}
	start := time.Now()

	results := make(map[string]*BatchResult)
	var resultsMu sync.Mutex
	var totalUsage Usage
	var successCount, failureCount int

	// Create semaphore for concurrency control
	sem := make(chan struct{}, opts.MaxConcurrency)
	var wg sync.WaitGroup

	completed := 0
	var completedMu sync.Mutex
	reportProgress := func() {
		if opts.ProgressCallback == nil {
			return
		}
		completedMu.Lock()
		completed++
		currentCompleted := completed
		completedMu.Unlock()
		// Call callback outside lock to prevent deadlock.
		opts.ProgressCallback(currentCompleted, len(requests))
	}
	recordFailure := func(id string, duration time.Duration, err error) {
		resultsMu.Lock()
		results[id] = &BatchResult{
			ID:       id,
			Error:    err,
			Duration: duration,
		}
		failureCount++
		resultsMu.Unlock()
	}
	recordSuccess := func(id string, duration time.Duration, resp *Response) {
		resultsMu.Lock()
		results[id] = &BatchResult{
			ID:       id,
			Response: resp,
			Duration: duration,
		}
		successCount++
		totalUsage.PromptTokens += resp.Usage.PromptTokens
		totalUsage.CompletionTokens += resp.Usage.CompletionTokens
		totalUsage.TotalTokens += resp.Usage.TotalTokens
		resultsMu.Unlock()
	}

	// Create a cancellable context for stopping all goroutines when ContinueOnError=false
	batchCtx, cancelBatch := context.WithCancel(ctx)
	defer cancelBatch()

	for _, req := range requests {
		wg.Add(1)
		go func(r BatchRequest) {
			reqStart := time.Now()
			progressReported := false
			resultRecorded := false
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					if !resultRecorded {
						recordFailure(r.ID, time.Since(reqStart), fmt.Errorf("panic: %v", recovered))
					}
					if !opts.ContinueOnError {
						cancelBatch()
					}
					if !progressReported {
						reportProgress()
					}
				}
			}()

			// Check if batch has been canceled (due to error with ContinueOnError=false)
			select {
			case <-batchCtx.Done():
				// Don't record canceled requests - they weren't started
				return
			default:
			}

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-batchCtx.Done():
				// Context canceled while waiting for semaphore
				return
			}

			// Double-check context after acquiring semaphore
			select {
			case <-batchCtx.Done():
				return
			default:
			}

			// Create request-specific context with timeout
			reqCtx := batchCtx
			if opts.RequestTimeout > 0 {
				var cancel context.CancelFunc
				reqCtx, cancel = context.WithTimeout(batchCtx, opts.RequestTimeout)
				defer cancel()
			}

			// Final context check before expensive LLM call to avoid wasted work
			select {
			case <-batchCtx.Done():
				return
			default:
			}

			resp, err := b.llm.GenerateContent(reqCtx, r.Messages, r.Options...)
			duration := time.Since(reqStart)

			if err != nil {
				recordFailure(r.ID, duration, err)
				resultRecorded = true

				if !opts.ContinueOnError {
					// Cancel all pending requests
					cancelBatch()
					return
				}
			} else {
				recordSuccess(r.ID, duration, resp)
				resultRecorded = true
			}

			progressReported = true
			reportProgress()
		}(req)
	}

	wg.Wait()

	return &BatchResponse{
		Results:      results,
		TotalUsage:   totalUsage,
		Duration:     time.Since(start),
		SuccessCount: successCount,
		FailureCount: failureCount,
	}, nil
}

func validateBatchRequestIDs(requests []BatchRequest) error {
	seen := make(map[string]struct{}, len(requests))
	for _, req := range requests {
		if _, ok := seen[req.ID]; ok {
			return fmt.Errorf("duplicate batch request ID: %s", req.ID)
		}
		seen[req.ID] = struct{}{}
	}
	return nil
}

// NewBatchRequest creates a new batch request with the given ID and messages.
func NewBatchRequest(id string, messages []Message, options ...CallOption) BatchRequest {
	return BatchRequest{
		ID:       id,
		Messages: messages,
		Options:  options,
	}
}

// NewBatchRequestFromPrompt creates a batch request from a simple prompt.
func NewBatchRequestFromPrompt(id, prompt string, options ...CallOption) BatchRequest {
	return BatchRequest{
		ID: id,
		Messages: []Message{
			{Role: RoleUser, Content: prompt},
		},
		Options: options,
	}
}

// BatchBuilder helps build batch requests fluently.
//
// Thread-safety: All methods are safe for concurrent use.
type BatchBuilder struct {
	mu       sync.Mutex
	requests []BatchRequest
	options  []BatchOption
}

// NewBatchBuilder creates a new batch builder.
func NewBatchBuilder() *BatchBuilder {
	return &BatchBuilder{}
}

// Add adds a request to the batch with validation.
// Returns an error if:
//   - id is empty
//   - messages is empty
//   - id is a duplicate of an existing request
func (b *BatchBuilder) Add(id string, messages []Message, options ...CallOption) (*BatchBuilder, error) {
	if id == "" {
		return b, fmt.Errorf("batch request ID cannot be empty")
	}
	if len(messages) == 0 {
		return b, fmt.Errorf("batch request messages cannot be empty")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Check for duplicate IDs
	for _, req := range b.requests {
		if req.ID == id {
			return b, fmt.Errorf("duplicate batch request ID: %s", id)
		}
	}

	b.requests = append(b.requests, BatchRequest{
		ID:       id,
		Messages: messages,
		Options:  options,
	})
	return b, nil
}

// MustAdd adds a request to the batch, panicking on validation errors.
// Use this for fluent chaining when you're certain inputs are valid.
func (b *BatchBuilder) MustAdd(id string, messages []Message, options ...CallOption) *BatchBuilder {
	_, err := b.Add(id, messages, options...)
	if err != nil {
		panic(fmt.Sprintf("MustAdd: %v", err))
	}
	return b
}

// AddPrompt adds a simple prompt request to the batch with validation.
func (b *BatchBuilder) AddPrompt(id, prompt string, options ...CallOption) (*BatchBuilder, error) {
	return b.Add(id, []Message{{Role: RoleUser, Content: prompt}}, options...)
}

// MustAddPrompt adds a simple prompt request, panicking on validation errors.
func (b *BatchBuilder) MustAddPrompt(id, prompt string, options ...CallOption) *BatchBuilder {
	return b.MustAdd(id, []Message{{Role: RoleUser, Content: prompt}}, options...)
}

// WithOptions sets batch-level options.
func (b *BatchBuilder) WithOptions(options ...BatchOption) *BatchBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.options = append(b.options, options...)
	return b
}

// Requests returns a copy of the built requests.
// A copy is returned to prevent external modification.
func (b *BatchBuilder) Requests() []BatchRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]BatchRequest, len(b.requests))
	copy(result, b.requests)
	return result
}

// Options returns a copy of the batch options.
// A copy is returned to prevent external modification.
func (b *BatchBuilder) Options() []BatchOption {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]BatchOption, len(b.options))
	copy(result, b.options)
	return result
}

// Execute processes the batch using the given batcher.
func (b *BatchBuilder) Execute(ctx context.Context, batcher BatchProcessor) (*BatchResponse, error) {
	b.mu.Lock()
	requests := make([]BatchRequest, len(b.requests))
	copy(requests, b.requests)
	options := make([]BatchOption, len(b.options))
	copy(options, b.options)
	b.mu.Unlock()
	return batcher.ProcessBatch(ctx, requests, options...)
}

// Ensure ConcurrentBatcher implements BatchProcessor.
var _ BatchProcessor = (*ConcurrentBatcher)(nil)
