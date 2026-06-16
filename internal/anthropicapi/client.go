package anthropicapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/internal/httpclient"
)

const (
	defaultBaseURL   = "https://api.anthropic.com/v1"
	anthropicVersion = "2023-06-01"
	eventMessageStop = "message_stop"
)

// Client is an Anthropic API client.
type Client struct {
	httpClient *httpclient.Client
	baseURL    string
	apiKey     string
}

// ClientConfig configures the Anthropic client.
type ClientConfig struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Timeout    time.Duration // Timeout for the underlying HTTP client; 0 uses the default.

	// AllowPrivateIPs allows requests to private/loopback IPs (SSRF opt-out).
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// NewClient creates a new Anthropic client.
func NewClient(config ClientConfig) *Client {
	var hcOpts []httpclient.ClientOption
	if config.HTTPClient != nil {
		hcOpts = append(hcOpts, httpclient.WithHTTPClient(config.HTTPClient))
	}
	if config.Timeout > 0 {
		hcOpts = append(hcOpts, httpclient.WithTimeout(config.Timeout))
	}
	hcOpts = append(hcOpts,
		httpclient.WithAllowPrivateIPs(config.AllowPrivateIPs),
		httpclient.WithAllowHTTP(config.AllowHTTP),
	)
	httpClient := httpclient.NewClient(hcOpts...)

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		apiKey:     config.APIKey,
	}
}

// CreateMessage sends a message request to Anthropic.
func (c *Client) CreateMessage(ctx context.Context, req *MessagesRequest) (*MessagesResponse, error) {
	headers := c.getHeaders()
	var response MessagesResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.baseURL + "/messages",
		Headers: headers,
		Body:    req,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// CreateMessageStream sends a streaming message request.
func (c *Client) CreateMessageStream(ctx context.Context, req *MessagesRequest) (*StreamReader, error) {
	req.Stream = true
	headers := c.getHeaders()

	body, err := c.httpClient.DoStream(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.baseURL + "/messages",
		Headers: headers,
		Body:    req,
	})

	if err != nil {
		return nil, err
	}

	return &StreamReader{
		sseReader: httpclient.NewSSEReader(body),
	}, nil
}

func (c *Client) getHeaders() map[string]string {
	return map[string]string{
		"x-api-key":         c.apiKey,
		"anthropic-version": anthropicVersion,
	}
}

// StreamReader reads streaming responses from Anthropic.
type StreamReader struct {
	sseReader *httpclient.SSEReader
}

// Read reads the next event from the stream.
func (r *StreamReader) Read() (*StreamEvent, error) {
	for {
		sseEvent, err := r.sseReader.Read()
		if err != nil {
			return nil, err
		}

		// Skip ping events
		if sseEvent.Event == "ping" {
			continue
		}

		// Parse the event data
		var event StreamEvent

		// First, determine the event type from the SSE event name
		eventType := sseEvent.Event
		if eventType == "" {
			// Try to parse from data
			var typeOnly struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(sseEvent.Data), &typeOnly); err == nil {
				eventType = typeOnly.Type
			}
		}

		// Handle message_stop specially - it signals end of stream
		if eventType == eventMessageStop {
			return &StreamEvent{Type: eventMessageStop}, nil
		}

		// Parse based on event type
		switch eventType {
		case "error":
			var raw struct {
				Type  string `json:"type"`
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(sseEvent.Data), &raw); err != nil {
				return nil, fmt.Errorf("failed to unmarshal error: %w", err)
			}
			return nil, &llms.APIError{
				Type:     raw.Error.Type,
				Message:  raw.Error.Message,
				Provider: llms.ProviderAnthropic,
			}
		case "message_start":
			if err := json.Unmarshal([]byte(sseEvent.Data), &event); err != nil {
				return nil, fmt.Errorf("failed to unmarshal message_start: %w", err)
			}
		case "content_block_start":
			if err := json.Unmarshal([]byte(sseEvent.Data), &event); err != nil {
				return nil, fmt.Errorf("failed to unmarshal content_block_start: %w", err)
			}
		case "content_block_delta":
			// Need special handling for delta parsing
			var raw struct {
				Type  string          `json:"type"`
				Index int             `json:"index"`
				Delta json.RawMessage `json:"delta"`
			}
			if err := json.Unmarshal([]byte(sseEvent.Data), &raw); err != nil {
				return nil, fmt.Errorf("failed to unmarshal content_block_delta: %w", err)
			}

			var delta StreamDelta
			if err := json.Unmarshal(raw.Delta, &delta); err != nil {
				return nil, fmt.Errorf("failed to unmarshal delta: %w", err)
			}

			event.Type = raw.Type
			event.Index = raw.Index
			event.Delta = &delta
		case "content_block_stop":
			event.Type = eventType
			// Parse index if present
			var raw struct {
				Index int `json:"index"`
			}
			_ = json.Unmarshal([]byte(sseEvent.Data), &raw)
			event.Index = raw.Index
		case "message_delta":
			var raw struct {
				Type  string          `json:"type"`
				Delta json.RawMessage `json:"delta"`
				Usage *Usage          `json:"usage"`
			}
			if err := json.Unmarshal([]byte(sseEvent.Data), &raw); err != nil {
				return nil, fmt.Errorf("failed to unmarshal message_delta: %w", err)
			}

			event.Type = raw.Type
			event.Usage = raw.Usage

			if len(raw.Delta) > 0 {
				var msgDelta MessageDelta
				if err := json.Unmarshal(raw.Delta, &msgDelta); err == nil {
					event.MessageDelta = &msgDelta
				}
			}
		default:
			// Skip unknown event types
			continue
		}

		if event.Type == "" {
			event.Type = eventType
		}

		return &event, nil
	}
}

// Close closes the stream.
func (r *StreamReader) Close() error {
	return r.sseReader.Close()
}

// BuildTextMessage creates a simple text message.
func BuildTextMessage(role, text string) Message {
	return Message{
		Role: role,
		Content: []ContentPart{
			{Type: "text", Text: text},
		},
	}
}

// BuildToolResultMessage creates a tool result message.
func BuildToolResultMessage(toolUseID, content string, isError bool) Message {
	return Message{
		Role: "user",
		Content: []ContentPart{
			{
				Type:      "tool_result",
				ToolUseID: toolUseID,
				Content:   content,
				IsError:   isError,
			},
		},
	}
}

// ExtractTextContent extracts all text from content parts.
func ExtractTextContent(content []ContentPart) string {
	var texts []string
	for _, part := range content {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "")
}

// IsEndOfStream checks if an event signals end of stream.
func IsEndOfStream(event *StreamEvent) bool {
	return event.Type == eventMessageStop
}

// IsStreamError checks if there was a streaming error.
func IsStreamError(err error) bool {
	return err != nil && !errors.Is(err, io.EOF)
}

// ListModels retrieves available models from the Anthropic API.
func (c *Client) ListModels(ctx context.Context, params *ModelsListParams) (*ModelsListResponse, error) {
	headers := c.getHeaders()
	var response ModelsListResponse

	// Build URL with query parameters
	url := c.baseURL + "/models"
	if params != nil {
		queryParts := []string{}
		if params.Limit > 0 {
			queryParts = append(queryParts, fmt.Sprintf("limit=%d", params.Limit))
		}
		if params.AfterID != "" {
			queryParts = append(queryParts, "after_id="+params.AfterID)
		}
		if params.BeforeID != "" {
			queryParts = append(queryParts, "before_id="+params.BeforeID)
		}
		if len(queryParts) > 0 {
			url += "?" + strings.Join(queryParts, "&")
		}
	}

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodGet,
		URL:     url,
		Headers: headers,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// GetModel retrieves a specific model by ID.
func (c *Client) GetModel(ctx context.Context, modelID string) (*ModelInfo, error) {
	headers := c.getHeaders()
	var response ModelInfo

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodGet,
		URL:     c.baseURL + "/models/" + modelID,
		Headers: headers,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// WrapError converts an httpclient.APIError to an llms.APIError with Anthropic provider context.
// For other errors, it wraps them in an llms.ProviderError.
// Returns nil if err is nil.
func WrapError(operation string, err error) error {
	if err == nil {
		return nil
	}

	// Check if it's an httpclient.APIError and convert to llms.APIError
	var httpErr *httpclient.APIError
	if errors.As(err, &httpErr) {
		return &llms.APIError{
			StatusCode:    httpErr.StatusCode,
			Message:       httpErr.Message,
			Type:          httpErr.Type,
			Code:          httpErr.Code,
			Param:         httpErr.Param,
			RequestID:     httpErr.RequestID,
			RetryAfter:    httpErr.RetryAfter,
			Provider:      llms.ProviderAnthropic,
			RequestURL:    httpErr.RequestURL,
			RequestMethod: httpErr.RequestMethod,
		}
	}

	// Check if it's already an llms.APIError
	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Provider == "" {
			apiErr.Provider = llms.ProviderAnthropic
		}
		return apiErr
	}

	// For other errors, wrap with provider context
	return llms.WrapProviderError(llms.ProviderAnthropic, operation, err)
}
