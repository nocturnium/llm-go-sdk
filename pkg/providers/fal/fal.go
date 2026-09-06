package fal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// Client is a native media-only fal.ai queue client, safe for concurrent use.
type Client struct {
	// transport carries the API key to the queue host.
	transport *httpclient.Client
	// assets downloads generated media without credentials.
	assets  *httpclient.Client
	options *options
	base    *url.URL
	headers map[string]string
}

// New constructs a client. Missing credentials return ErrMissingAPIKey; invalid
// base URLs, model IDs or queue priorities return ErrInvalidParameters.
func New(opts ...Option) (*Client, error) {
	o := apply(opts...)
	if o.Timeout <= 0 {
		o.Timeout = defaultOptions().Timeout
	}
	key, err := llms.RequireAPIKey("fal", o.APIKey, llms.EnvFalAPIKey)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(o.BaseURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, invalid("invalid base URL")
	}
	for _, model := range []string{o.ImageModel, o.VideoModel, o.SpeechModel, o.TranscriptionModel} {
		if err = validateModel(model); err != nil {
			return nil, err
		}
	}
	if o.QueuePriority != "" && o.QueuePriority != "normal" && o.QueuePriority != "low" {
		return nil, invalid("queue priority must be normal or low")
	}
	transportOpts := []httpclient.ClientOption{}
	if o.HTTPClient != nil {
		transportOpts = append(transportOpts, httpclient.WithHTTPClient(o.HTTPClient))
	}
	transportOpts = append(transportOpts, httpclient.WithTimeout(o.Timeout), httpclient.WithAllowPrivateIPs(o.AllowPrivateIPs), httpclient.WithAllowHTTP(o.AllowHTTP))
	return &Client{
		options:   o,
		base:      base,
		headers:   map[string]string{"Authorization": "Key " + key},
		transport: httpclient.NewClient(transportOpts...),
		assets:    httpclient.NewClient(transportOpts...),
	}, nil
}

// Provider returns fal's identifier.
func (c *Client) Provider() llms.Provider { return llms.ProviderFal }

// Model returns the default image application, the primary fal use case.
func (c *Client) Model() string { return c.options.ImageModel }

// Capabilities reports provider-level media support; individual applications vary.
func (c *Client) Capabilities() llms.Capabilities {
	return llms.Capabilities{ImageGeneration: true, VideoGeneration: true, Speech: true, Transcription: true}
}

var (
	_ llms.ImageGenerator    = (*Client)(nil)
	_ llms.VideoGenerator    = (*Client)(nil)
	_ llms.SpeechSynthesizer = (*Client)(nil)
	_ llms.Transcriber       = (*Client)(nil)
)

func (c *Client) startOperation(ctx context.Context, operation, model string) (context.Context, func(error)) {
	ctx, span := otel.Tracer("llms").Start(ctx, "fal."+operation)
	span.SetAttributes(attribute.String("llm.provider", "fal"), attribute.String("llm.model", model), attribute.String("llm.operation", operation))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

func invalid(message string) error {
	return fmt.Errorf("fal: %s: %w", message, llms.ErrInvalidParameters)
}

// validateModel accepts fal application paths such as fal-ai/flux/schnell.
func validateModel(model string) error {
	if model == "" || len(model) > 256 || strings.HasPrefix(model, "/") || strings.HasSuffix(model, "/") || strings.Contains(model, "//") {
		return invalid("invalid model ID")
	}
	for _, segment := range strings.Split(model, "/") {
		if strings.Trim(segment, ".") == "" {
			return invalid("invalid model ID")
		}
	}
	for _, r := range model {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == '/'
		if !valid {
			return invalid("invalid model ID")
		}
	}
	return nil
}

// validateRequestID accepts queue request identifiers (UUID-like tokens).
func validateRequestID(id string) error {
	if id == "" || len(id) > 256 {
		return invalid("invalid request ID")
	}
	for _, r := range id {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if !valid {
			return invalid("invalid request ID")
		}
	}
	return nil
}

// mergeExtra copies extra into body, rejecting keys the typed options own.
func mergeExtra(body, extra map[string]any, reserved ...string) error {
	for k, v := range extra {
		for _, r := range reserved {
			if k == r {
				return invalid(fmt.Sprintf("Extra key %q is reserved for the typed option", k))
			}
		}
		body[k] = v
	}
	return nil
}
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case float32:
		return number(float64(n))
	case json.Number:
		f, e := n.Float64()
		return f, e == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	default:
		return 0, false
	}
}

// billableUnits parses X-Fal-Billable-Units into metadata; cost is never derived from it.
func billableUnits(header interface{ Get(string) string }, metadata map[string]any) {
	if header == nil {
		return
	}
	raw := header.Get("X-Fal-Billable-Units")
	if raw == "" {
		return
	}
	if units, err := strconv.ParseFloat(raw, 64); err == nil && units >= 0 && !math.IsInf(units, 0) {
		metadata["billable_units"] = units
	}
}

// falError is the parsed shape of a fal error body: either a model validation
// list ({"detail":[{"msg","type",...}]}) or an infrastructure envelope
// ({"detail":"...","error_type":"..."}).
type falError struct {
	message, errorType string
}

// trimBody trims surrounding whitespace from an error body.
func trimBody(body string) string { return strings.TrimSpace(body) }

// unescapeBody reverses the control-character escaping the transport's log
// sanitizer applies to unparsed error bodies, so pretty-printed JSON still parses.
var unescapeBody = strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\r`, "\r")

// unmarshalBody decodes a sanitized error body, retrying once after unescaping.
func unmarshalBody(body string, v any) error {
	if err := json.Unmarshal([]byte(body), v); err == nil {
		return nil
	}
	return json.Unmarshal([]byte(unescapeBody.Replace(body)), v)
}
func parseFalError(body string) falError {
	body = trimBody(body)
	var envelope struct {
		Detail    json.RawMessage `json:"detail"`
		ErrorType string          `json:"error_type"`
	}
	out := falError{message: body}
	if unmarshalBody(body, &envelope) != nil {
		return out
	}
	out.errorType = envelope.ErrorType
	// A parsed envelope with an empty detail keeps the raw body only when no
	// error_type describes it either.
	if out.errorType != "" {
		out.message = ""
	}
	var plain string
	if json.Unmarshal(envelope.Detail, &plain) == nil {
		if plain != "" {
			out.message = plain
		}
		return out
	}
	var items []struct {
		Msg  string `json:"msg"`
		Type string `json:"type"`
	}
	if json.Unmarshal(envelope.Detail, &items) == nil && len(items) > 0 {
		messages := make([]string, 0, len(items))
		for _, item := range items {
			messages = append(messages, item.Msg)
		}
		out.message = strings.Join(messages, "; ")
		if out.errorType == "" {
			out.errorType = items[0].Type
		}
	}
	return out
}

// moderation builds the input-stage ModerationError fal reports for rejected prompts.
func moderation(stage llms.ModerationStage, reasons ...string) *llms.ModerationError {
	return &llms.ModerationError{Provider: "fal", Stage: stage, Reasons: reasons, Charged: false}
}

// classifyErrorType maps a fal error_type (from status bodies or error envelopes)
// to an SDK sentinel. Unknown types return nil.
func classifyErrorType(errorType, message string) error {
	switch errorType {
	case "content_policy_violation":
		return moderation(llms.ModerationInput, message)
	case "no_media_generated":
		return llms.ErrIncompleteResponse
	case "request_timeout", "startup_timeout", "generation_timeout":
		return llms.ErrTimeout
	case "internal_server_error", "downstream_service_error", "downstream_service_unavailable":
		return llms.ErrServiceUnavailable
	case "bad_request":
		return llms.ErrInvalidParameters
	default:
		return nil
	}
}

// wrapError adds operation context and maps native HTTP errors to SDK sentinels,
// preserving *httpclient.APIError for errors.As. Nil stays nil.
func wrapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var api *httpclient.APIError
	if !errors.As(err, &api) {
		return fmt.Errorf("fal: %s: %w", operation, err)
	}
	parsed := parseFalError(api.Message)
	sentinel := classifyErrorType(parsed.errorType, parsed.message)
	if sentinel == nil {
		switch api.StatusCode {
		case 401, 403:
			sentinel = llms.ErrAuthenticationFailed
		case 402:
			sentinel = llms.ErrQuotaExceeded
		case 404:
			sentinel = llms.ErrModelNotFound
		case 400, 422:
			sentinel = llms.ErrInvalidParameters
		case 429:
			sentinel = llms.ErrRateLimited
		case 504:
			sentinel = llms.ErrTimeout
		default:
			switch {
			case api.StatusCode >= 500:
				sentinel = llms.ErrServiceUnavailable
			case api.StatusCode >= 400:
				sentinel = llms.ErrInvalidParameters
			}
		}
	}
	message := parsed.message
	if parsed.errorType != "" && message != "" {
		message = parsed.errorType + ": " + message
	} else if parsed.errorType != "" {
		message = parsed.errorType
	}
	if message != "" {
		message += ": "
	}
	if sentinel == nil {
		return fmt.Errorf("fal: %s: %s%w", operation, message, err)
	}
	return fmt.Errorf("fal: %s: %s%w", operation, message, errors.Join(sentinel, err))
}
