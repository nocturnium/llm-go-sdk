package elevenlabs

import (
	"encoding/json"
	"errors"
	"fmt"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// WrapError adds operation/provider context and maps native HTTP errors to SDK
// sentinels, preserving the original error for errors.As/Unwrap. Nil stays nil.
// Both string and object detail envelopes are supported, including plan gates.
func WrapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var api *httpclient.APIError
	if !errors.As(err, &api) {
		return fmt.Errorf("elevenlabs: %s: %w", operation, err)
	}
	message := api.Message
	var envelope struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal([]byte(message), &envelope) == nil {
		var detail struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Code    string `json:"code"`
		}
		var plain string
		if json.Unmarshal(envelope.Detail, &detail) == nil && detail.Message != "" {
			message = detail.Message
		} else if json.Unmarshal(envelope.Detail, &plain) == nil && plain != "" {
			message = plain
		}
	}
	var sentinel error
	switch api.StatusCode {
	case 401:
		sentinel = llms.ErrAuthenticationFailed
	case 402:
		sentinel = llms.ErrPlanRequired
	case 422, 400:
		sentinel = llms.ErrInvalidParameters
	case 429:
		sentinel = llms.ErrRateLimited
	case 404:
		sentinel = llms.ErrModelNotFound
	case 500, 502, 503, 504:
		sentinel = llms.ErrServiceUnavailable
	}
	if sentinel == nil {
		return fmt.Errorf("elevenlabs: %s: %s: %w", operation, message, err)
	}
	return fmt.Errorf("elevenlabs: %s: %s: %w", operation, message, errors.Join(sentinel, err))
}
