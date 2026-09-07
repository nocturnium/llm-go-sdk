package fal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// Queue status values reported by GET .../requests/{id}/status.
const (
	statusQueued     = "IN_QUEUE"
	statusInProgress = "IN_PROGRESS"
	statusCompleted  = "COMPLETED"
)

// queueRequest identifies a submitted request and the URLs used to track it.
type queueRequest struct {
	Model, RequestID, StatusURL, ResponseURL, CancelURL string
}

type submitResponse struct {
	RequestID     string `json:"request_id"`
	ResponseURL   string `json:"response_url"`
	StatusURL     string `json:"status_url"`
	CancelURL     string `json:"cancel_url"`
	QueuePosition int    `json:"queue_position"`
}

type statusResponse struct {
	Status        string          `json:"status"`
	RequestID     string          `json:"request_id"`
	ResponseURL   string          `json:"response_url"`
	QueuePosition *int            `json:"queue_position"`
	Logs          json.RawMessage `json:"logs"`
	Metrics       struct {
		InferenceTime float64 `json:"inference_time"`
	} `json:"metrics"`
	Error     string `json:"error"`
	ErrorType string `json:"error_type"`
}

// endpoint joins the queue base with a model path and request-relative segments.
func (c *Client) endpoint(model string, segments ...string) string {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + "/" + model
	if len(segments) > 0 {
		u.Path += "/" + strings.Join(segments, "/")
	}
	return u.String()
}

// namespace returns the application namespace fal uses for request routes: the
// first two path segments of the model ID (fal-ai/flux for fal-ai/flux/schnell).
// Request routes under the full model path return 405.
func namespace(model string) string {
	parts := strings.SplitN(model, "/", 3)
	if len(parts) < 2 {
		return model
	}
	return parts[0] + "/" + parts[1]
}

// sameOrigin reports whether a provider-supplied URL shares the queue base's
// scheme and host, so tracking URLs never redirect credentials elsewhere.
func (c *Client) sameOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == c.base.Scheme && strings.EqualFold(u.Host, c.base.Host) && u.User == nil
}

// trackingURL prefers the provider-returned URL when it is same-origin and
// otherwise derives {base}/{namespace}/requests/{id}/... from the model ID.
func (c *Client) trackingURL(returned, model, id string, segments ...string) string {
	if returned != "" && c.sameOrigin(returned) {
		return returned
	}
	return c.endpoint(namespace(model), append([]string{"requests", id}, segments...)...)
}

// do performs an authenticated JSON exchange and decodes into out when non-nil.
func (c *Client) do(ctx context.Context, method, endpoint string, headers map[string]string, body, out any) (http.Header, error) {
	merged := make(map[string]string, len(c.headers)+len(headers))
	for k, v := range c.headers {
		merged[k] = v
	}
	for k, v := range headers {
		merged[k] = v
	}
	data, header, err := c.transport.DoRawWithHeaders(ctx, httpclient.Request{Method: method, URL: endpoint, Headers: merged, Body: body})
	if err != nil {
		return nil, err
	}
	if out != nil {
		if err = json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
	}
	return header, nil
}

// submit enqueues body on model and returns the request handle.
func (c *Client) submit(ctx context.Context, model string, body map[string]any) (*queueRequest, error) {
	if err := validateModel(model); err != nil {
		return nil, err
	}
	var headers map[string]string
	if c.options.QueuePriority != "" {
		headers = map[string]string{"X-Fal-Queue-Priority": c.options.QueuePriority}
	}
	var out submitResponse
	if _, err := c.do(ctx, http.MethodPost, c.endpoint(model), headers, body, &out); err != nil {
		return nil, wrapError("submit", err)
	}
	if err := validateRequestID(out.RequestID); err != nil {
		return nil, err
	}
	return &queueRequest{
		Model:       model,
		RequestID:   out.RequestID,
		StatusURL:   c.trackingURL(out.StatusURL, model, out.RequestID, "status"),
		ResponseURL: c.trackingURL(out.ResponseURL, model, out.RequestID),
		CancelURL:   c.trackingURL(out.CancelURL, model, out.RequestID, "cancel"),
	}, nil
}

// status fetches the current queue status.
func (c *Client) status(ctx context.Context, q *queueRequest) (*statusResponse, error) {
	var out statusResponse
	if _, err := c.do(ctx, http.MethodGet, q.StatusURL, nil, nil, &out); err != nil {
		return nil, wrapError("status", err)
	}
	return &out, nil
}

// result fetches the model-specific result body and returns response headers.
func (c *Client) result(ctx context.Context, q *queueRequest, out any) (http.Header, error) {
	header, err := c.do(ctx, http.MethodGet, q.ResponseURL, nil, nil, out)
	if err != nil {
		return nil, wrapError("result", err)
	}
	return header, nil
}

// cancel requests cancellation. Completed requests return ErrInvalidParameters
// ("already completed"); unknown requests return ErrModelNotFound.
func (c *Client) cancel(ctx context.Context, q *queueRequest) error {
	var out struct {
		Status string `json:"status"`
	}
	_, err := c.do(ctx, http.MethodPut, q.CancelURL, nil, nil, &out)
	if err == nil {
		return nil
	}
	var api *httpclient.APIError
	if errors.As(err, &api) {
		var body struct {
			Status string `json:"status"`
		}
		_ = unmarshalBody(trimBody(api.Message), &body)
		switch {
		case api.StatusCode == 400 && body.Status == "ALREADY_COMPLETED":
			return fmt.Errorf("fal: cancel: request already completed: %w", errors.Join(llms.ErrInvalidParameters, err))
		case api.StatusCode == 404:
			return fmt.Errorf("fal: cancel: request not found: %w", errors.Join(llms.ErrModelNotFound, err))
		}
	}
	return wrapError("cancel", err)
}

// providerError marks errors that already carry the "fal:" prefix and SDK
// mapping, so polling does not wrap them a second time.
type providerError struct{ err error }

func (e *providerError) Error() string { return e.err.Error() }
func (e *providerError) Unwrap() error { return e.err }

// statusError converts a failed COMPLETED status into an SDK error, or nil.
func statusError(s *statusResponse) error {
	if s.Status != statusCompleted || (s.Error == "" && s.ErrorType == "") {
		return nil
	}
	if sentinel := classifyErrorType(s.ErrorType, s.Error); sentinel != nil {
		var moderated *llms.ModerationError
		if errors.As(sentinel, &moderated) {
			return moderated
		}
		return &providerError{fmt.Errorf("fal: %s: %s: %w", s.ErrorType, s.Error, errors.Join(sentinel, llms.ErrJobFailed))}
	}
	return &providerError{fmt.Errorf("fal: %s: %s: %w", s.ErrorType, s.Error, llms.ErrJobFailed)}
}

// jobStatus maps a queue status onto the SDK job model.
func jobStatus(s *statusResponse) *llms.JobStatus {
	out := &llms.JobStatus{}
	switch s.Status {
	case statusQueued:
		out.State = llms.JobQueued
		progress := 0.0
		out.Progress = &progress
	case statusInProgress:
		out.State = llms.JobRunning
	case statusCompleted:
		out.State = llms.JobSucceeded
		if err := statusError(s); err != nil {
			out.State = llms.JobFailed
			out.Err = err
			if errors.Is(err, llms.ErrContentFiltered) {
				out.State = llms.JobModerated
			}
		}
	default:
		out.State = llms.JobFailed
		out.Err = &providerError{fmt.Errorf("fal: unknown queue status %q: %w", s.Status, llms.ErrJobFailed)}
	}
	return out
}

// await polls until the request completes. Failed completions surface their
// mapped error; context and poll-timeout errors are wrapped as "poll".
func (c *Client) await(ctx context.Context, q *queueRequest) error {
	err := httpclient.Poll(ctx, httpclient.PollPolicy(c.options.PollPolicy), func(ctx context.Context) (bool, error) {
		s, e := c.status(ctx, q)
		if e != nil {
			return false, &providerError{e}
		}
		status := jobStatus(s)
		return status.State.Terminal(), status.Err
	})
	if err == nil {
		return nil
	}
	var moderated *llms.ModerationError
	if errors.As(err, &moderated) {
		return moderated
	}
	var mapped *providerError
	if errors.As(err, &mapped) {
		return mapped.err
	}
	return wrapError("poll", err)
}

// fetchAsset downloads a generated file through the SSRF-validated transport
// without credentials, retaining URL, bytes and MIME type.
func (c *Client) fetchAsset(ctx context.Context, assetURL, contentType string) (llms.MediaAsset, error) {
	if assetURL == "" {
		return llms.MediaAsset{}, fmt.Errorf("fal: result has no asset URL: %w", llms.ErrIncompleteResponse)
	}
	data, err := c.assets.DoBinary(ctx, http.MethodGet, assetURL, nil, map[string]string{"User-Agent": "llm-go-sdk/6"})
	if err != nil {
		return llms.MediaAsset{}, wrapError("asset download", err)
	}
	mime := contentType
	if mime == "" {
		mime = data.ContentType
	}
	return llms.MediaAsset{URL: assetURL, Data: data.Data, MIMEType: mime}, nil
}
