package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// BatchEndpoint is the request shape every item in a batch uses. One batch
// carries one endpoint; mixing shapes requires separate batches.
type BatchEndpoint string

const (
	// BatchEndpointChatCompletions batches /v1/chat/completions bodies.
	BatchEndpointChatCompletions BatchEndpoint = "/v1/chat/completions"
	// BatchEndpointResponses batches /v1/responses bodies.
	BatchEndpointResponses BatchEndpoint = "/v1/responses"
	// BatchEndpointMessages batches Anthropic-style /v1/messages bodies.
	BatchEndpointMessages BatchEndpoint = "/v1/messages"
	// BatchEndpointEmbeddings batches /v1/embeddings bodies.
	BatchEndpointEmbeddings BatchEndpoint = "/v1/embeddings"
)

// BatchStatus is the lifecycle state of a batch. The normal progression is
// validating, in_progress, finalizing, completed.
type BatchStatus string

// Batch lifecycle states.
const (
	BatchValidating BatchStatus = "validating"
	BatchInProgress BatchStatus = "in_progress"
	BatchFinalizing BatchStatus = "finalizing"
	BatchCompleted  BatchStatus = "completed"
	BatchFailed     BatchStatus = "failed"
	BatchExpired    BatchStatus = "expired"
	BatchCancelling BatchStatus = "cancelling" //nolint:misspell // OpenRouter wire spelling.
	BatchCancelled  BatchStatus = "cancelled"  //nolint:misspell // OpenRouter wire spelling.
)

// IsTerminal reports whether the batch has stopped moving. A completed batch may
// still contain per-request failures; check the result lines.
func (s BatchStatus) IsTerminal() bool {
	switch s {
	case BatchCompleted, BatchFailed, BatchExpired, BatchCancelled:
		return true
	default:
		return false
	}
}

// BatchItem is one request in a submission. Body is the full request body for
// the submission's endpoint, for example an
// [openaicompat.ChatCompletionRequest]. It may omit model to inherit the batch
// model; when it carries one, it must equal the batch model.
type BatchItem struct {
	CustomID string `json:"custom_id"`
	Body     any    `json:"body"`
}

// BatchSubmission is the body of a batch creation request.
//
// Field order is load-bearing: OpenRouter stream-parses the body so it can
// accept very large arrays, and returns 400 when requests is serialized before
// endpoint and model. Keep Requests last.
type BatchSubmission struct {
	Endpoint BatchEndpoint `json:"endpoint"`
	Model    string        `json:"model"`
	Requests []BatchItem   `json:"requests"`
}

// BatchCounts reports per-request progress.
type BatchCounts struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// BatchUsage is the aggregate usage of a completed batch. Cost is the reported
// USD charge, nil when unreported. IsByok is true when the batch ran through
// your own provider key, in which case the provider billed inference directly
// and OpenRouter charged only its BYOK fee.
type BatchUsage struct {
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	TotalTokens      int      `json:"total_tokens"`
	Cost             *float64 `json:"cost"`
	IsByok           bool     `json:"is_byok"`
}

// BatchError is a best-effort view of an error payload. OpenRouter documents
// that the field exists but not its schema, so unrecognized fields are dropped;
// use [Batch.Raw] when the full payload matters.
type BatchError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

// BatchLineResponse is the upstream response captured for one request.
type BatchLineResponse struct {
	StatusCode int             `json:"status_code"`
	RequestID  string          `json:"request_id"`
	Body       json.RawMessage `json:"body"`
}

// BatchResultLine is one entry of a completed batch's results. Exactly one of
// Response and Error is populated, and CustomID keys it back to the submitted
// item. Response.Body.id doubles as the generation id.
type BatchResultLine struct {
	ID       string             `json:"id"`
	CustomID string             `json:"custom_id"`
	Response *BatchLineResponse `json:"response"`
	Error    *BatchError        `json:"error"`
}

// Batch is a batch object as returned by submit, get and list.
//
// Results are inline and only on a completed batch read individually: there is
// no download route, and list entries always carry a nil Results.
type Batch struct {
	ID       string        `json:"id"`
	Object   string        `json:"object"`
	Endpoint BatchEndpoint `json:"endpoint"`
	Model    string        `json:"model"`
	// CompletionWindow is always "24h"; it is the only supported window.
	CompletionWindow string      `json:"completion_window"`
	Status           BatchStatus `json:"status"`
	// CreatedAt and FinalizedAt are Unix seconds; FinalizedAt is nil until the
	// batch reaches a terminal state.
	CreatedAt     int64             `json:"created_at"`
	FinalizedAt   *int64            `json:"finalized_at"`
	RequestCounts BatchCounts       `json:"request_counts"`
	Usage         *BatchUsage       `json:"usage"`
	Results       []BatchResultLine `json:"results"`
	Error         *BatchError       `json:"error"`
	// Raw is the undecoded batch object, kept so fields this struct does not
	// model (the beta API may add some) stay reachable.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the batch and retains the original payload in Raw.
func (b *Batch) UnmarshalJSON(data []byte) error {
	type plain Batch
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*b = Batch(decoded)
	b.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// BatchList is one page of batches, newest first. Page with After, not offsets.
type BatchList struct {
	Object  string  `json:"object"`
	Data    []Batch `json:"data"`
	FirstID string  `json:"first_id"`
	LastID  string  `json:"last_id"`
	HasMore bool    `json:"has_more"`
}

// BatchDeletion reports what a delete removed. Openrouter is always "deleted" on
// success; Upstream is present only when a provider had been assigned, and its
// Status is "deleted", "unsupported" or "not_applicable".
type BatchDeletion struct {
	ID       string `json:"id"`
	Object   string `json:"object"`
	Deletion struct {
		Openrouter string `json:"openrouter"`
		Upstream   *struct {
			Provider string `json:"provider"`
			Status   string `json:"status"`
		} `json:"upstream"`
	} `json:"deletion"`
}

// ListBatchesParams filters and pages a batch listing. The zero value lists the
// newest 20 batches in the workspace.
type ListBatchesParams struct {
	// Limit is 1-100; 0 leaves the server default of 20.
	Limit int
	// After is a cursor: pass the previous page's LastID.
	After string
	// Status filters to the given states. Only the terminal states plus
	// validating and in_progress are accepted; the two transitional states are not.
	Status []BatchStatus
	// CreatedAfter must precede CreatedBefore when both are set. Zero values are
	// omitted. Timestamps filter at second granularity, so prefer After for paging.
	CreatedAfter  time.Time
	CreatedBefore time.Time
}

func (p ListBatchesParams) query() (url.Values, error) {
	q := url.Values{}
	if p.Limit < 0 || p.Limit > 100 {
		return nil, fmt.Errorf("openrouter: batch list limit must be 1-100: %w", llms.ErrInvalidParameters)
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.After != "" {
		q.Set("after", p.After)
	}
	for _, status := range p.Status {
		q.Add("status", string(status))
	}
	if !p.CreatedAfter.IsZero() && !p.CreatedBefore.IsZero() && !p.CreatedAfter.Before(p.CreatedBefore) {
		return nil, fmt.Errorf("openrouter: created_after must precede created_before: %w", llms.ErrInvalidParameters)
	}
	if !p.CreatedAfter.IsZero() {
		q.Set("created_after", strconv.FormatInt(p.CreatedAfter.Unix(), 10))
	}
	if !p.CreatedBefore.IsZero() {
		q.Set("created_before", strconv.FormatInt(p.CreatedBefore.Unix(), 10))
	}
	return q, nil
}

// SubmitBatch queues a batch for asynchronous processing and returns the created
// batch, whose status is "validating": the request was persisted and queued, not
// run. Poll with [Client.GetBatch] or [Client.WaitBatch] for results, which
// arrive inline within the 24-hour completion window.
//
// Batches are text-only, so image, audio, file and non-text output requests are
// rejected upstream at validation. Batch requests are typically billed at 50% of
// standard per-token pricing; set [llms.PricingModeBatch] when estimating cost.
//
// Submission goes through the shared transport, which retries a 429 or 5xx, so a
// response lost in transit can leave a batch queued that this call never names.
// After a submission error, [Client.ListBatches] is the way to find one.
func (c *Client) SubmitBatch(ctx context.Context, submission BatchSubmission) (*Batch, error) {
	if err := submission.validate(); err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "submit batch", err)
	}
	var out Batch
	if err := c.betaRequest(ctx, http.MethodPost, "batches", nil, submission, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s BatchSubmission) validate() error {
	if s.Endpoint == "" {
		return fmt.Errorf("batch endpoint is required: %w", llms.ErrInvalidParameters)
	}
	if strings.TrimSpace(s.Model) == "" {
		return fmt.Errorf("batch model is required: %w", llms.ErrInvalidParameters)
	}
	if len(s.Requests) == 0 {
		return llms.ErrBatchEmpty
	}
	seen := make(map[string]struct{}, len(s.Requests))
	for _, item := range s.Requests {
		if strings.TrimSpace(item.CustomID) == "" {
			return fmt.Errorf("batch custom_id is required: %w", llms.ErrInvalidParameters)
		}
		if _, ok := seen[item.CustomID]; ok {
			return fmt.Errorf("duplicate batch custom_id %q: %w", item.CustomID, llms.ErrInvalidParameters)
		}
		seen[item.CustomID] = struct{}{}
		if item.Body == nil {
			return fmt.Errorf("batch request %q has no body: %w", item.CustomID, llms.ErrInvalidParameters)
		}
	}
	return nil
}

// GetBatch reads one batch. A completed batch carries its result lines inline;
// running, failed, expired and canceled batches carry none.
func (c *Client) GetBatch(ctx context.Context, batchID string) (*Batch, error) {
	route, err := batchRoute(batchID)
	if err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "get batch", err)
	}
	var out Batch
	if err := c.betaRequest(ctx, http.MethodGet, route, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListBatches returns one page of the workspace's batches, newest first. Every
// API key in a workspace sees the same batches, and listed entries never carry
// results; read a batch individually for those.
func (c *Client) ListBatches(ctx context.Context, params ListBatchesParams) (*BatchList, error) {
	query, err := params.query()
	if err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "list batches", err)
	}
	var out BatchList
	if err := c.betaRequest(ctx, http.MethodGet, "batches", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteBatch purges a terminal batch's requests and result artifacts ahead of
// the 30-day retention window. An in-flight batch returns 409, as does a BYOK
// batch whose provider key has been disabled. Deletion is synchronous: a later
// read returns 404.
func (c *Client) DeleteBatch(ctx context.Context, batchID string) (*BatchDeletion, error) {
	route, err := batchRoute(batchID)
	if err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "delete batch", err)
	}
	var out BatchDeletion
	if err := c.betaRequest(ctx, http.MethodDelete, route, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// batchRoute builds the per-batch route, rejecting ids that would escape it.
func batchRoute(batchID string) (string, error) {
	id := strings.TrimSpace(batchID)
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\?#%") {
		return "", fmt.Errorf("invalid batch ID: %w", llms.ErrInvalidParameters)
	}
	return "batches/" + id, nil
}

// Default polling cadence for WaitBatch. Batches run against a 24-hour window,
// so a slow poll costs nothing and a fast one only burns rate limit.
const (
	defaultBatchPollInterval = 15 * time.Second
	maxBatchPollInterval     = 5 * time.Minute
	// batchVisibilityGrace is how long WaitBatch keeps retrying a 404. A batch
	// accepted with 202 is not immediately readable: submit returns before the
	// id is queryable (a few seconds, observed live on 2026-09-14), and both the
	// read and the listing 404 until it lands. Past this window a 404 is a
	// missing or deleted batch and is returned.
	batchVisibilityGrace = 90 * time.Second
)

// WaitOptions configures [Client.WaitBatch].
type WaitOptions struct {
	// PollInterval is the first delay between reads; it backs off to at most
	// five minutes. Zero uses 15 seconds.
	PollInterval time.Duration
	// OnPoll, when set, is called with each observed batch.
	OnPoll func(*Batch)
}

// WaitOption configures [Client.WaitBatch].
type WaitOption func(*WaitOptions)

// WithPollInterval sets the initial polling cadence. It backs off from there.
func WithPollInterval(d time.Duration) WaitOption {
	return func(o *WaitOptions) {
		if d > 0 {
			o.PollInterval = d
		}
	}
}

// WithPollCallback observes every polled batch, for progress reporting.
func WithPollCallback(fn func(*Batch)) WaitOption {
	return func(o *WaitOptions) { o.OnPoll = fn }
}

// WaitBatch polls until the batch reaches a terminal state and returns it.
//
// It returns the batch for every terminal state, including failed, expired and
// canceled, so the caller decides what a non-completed outcome means; only
// transport and context errors come back as errors. A 404 in the first 90
// seconds is treated as a batch that is not queryable yet rather than a missing
// one, since submission returns before the id lands. Always pass a bounded
// context: the completion window is 24 hours.
func (c *Client) WaitBatch(ctx context.Context, batchID string, opts ...WaitOption) (*Batch, error) {
	options := WaitOptions{PollInterval: defaultBatchPollInterval}
	for _, opt := range opts {
		opt(&options)
	}
	interval := options.PollInterval
	if interval <= 0 {
		interval = defaultBatchPollInterval
	}
	deadline := time.Now().Add(batchVisibilityGrace)
	for {
		batch, err := c.GetBatch(ctx, batchID)
		if err != nil {
			if !isBatchNotYetVisible(err, deadline) {
				return nil, err
			}
			if err := sleepCtx(ctx, jitter(interval)); err != nil {
				return nil, openaicompat.WrapError(c.Provider(), "wait batch", err)
			}
			continue
		}
		if options.OnPoll != nil {
			options.OnPoll(batch)
		}
		if batch.Status.IsTerminal() {
			return batch, nil
		}
		if err := sleepCtx(ctx, jitter(interval)); err != nil {
			return nil, openaicompat.WrapError(c.Provider(), "wait batch", err)
		}
		if interval < maxBatchPollInterval {
			interval = min(interval*2, maxBatchPollInterval)
		}
	}
}

// isBatchNotYetVisible reports whether a read failed with a 404 inside the
// window where a just-submitted batch is not queryable yet.
func isBatchNotYetVisible(err error, deadline time.Time) bool {
	var apiErr *llms.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound && time.Now().Before(deadline)
}

// jitter spreads concurrent waiters by up to 10% of the interval.
func jitter(d time.Duration) time.Duration {
	return d + time.Duration(rand.Int64N(int64(d)/10+1)) // #nosec G404 -- polling jitter for thundering-herd spread, not cryptography
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
