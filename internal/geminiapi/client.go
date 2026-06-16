package geminiapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/internal/httpclient"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// Client is a Gemini API client.
type Client struct {
	httpClient *httpclient.Client
	baseURL    string
	apiKey     string
	model      string
}

// ClientConfig configures the Gemini client.
type ClientConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
	Timeout    time.Duration // Timeout for the underlying HTTP client; 0 uses the default.

	// AllowPrivateIPs allows requests to private/loopback IPs (SSRF opt-out).
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// NewClient creates a new Gemini client.
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
		model:      config.Model,
	}
}

// GenerateContent sends a content generation request.
func (c *Client) GenerateContent(ctx context.Context, model string, req *GenerateContentRequest) (*GenerateContentResponse, error) {
	if model == "" {
		model = c.model
	}
	model = sanitizeModelName(model)

	url := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, model)
	headers := c.getHeaders()

	var response GenerateContentResponse
	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     url,
		Headers: headers,
		Body:    req,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// GenerateContentStream sends a streaming content generation request.
func (c *Client) GenerateContentStream(ctx context.Context, model string, req *GenerateContentRequest) (*StreamReader, error) {
	if model == "" {
		model = c.model
	}
	model = sanitizeModelName(model)

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", c.baseURL, model)
	headers := c.getHeaders()

	body, err := c.httpClient.DoStream(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     url,
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
		"x-goog-api-key": c.apiKey,
	}
}

// StreamReader reads streaming responses from Gemini.
type StreamReader struct {
	sseReader *httpclient.SSEReader
}

// Read reads the next chunk from the stream.
func (r *StreamReader) Read() (*StreamChunk, error) {
	for {
		event, err := r.sseReader.Read()
		if err != nil {
			return nil, err
		}

		// Skip empty data
		if event.Data == "" {
			continue
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return nil, fmt.Errorf("failed to unmarshal chunk: %w", err)
		}

		return &chunk, nil
	}
}

// Close closes the stream.
func (r *StreamReader) Close() error {
	return r.sseReader.Close()
}

// BuildTextContent creates a content with text parts.
func BuildTextContent(role, text string) Content {
	return Content{
		Role: role,
		Parts: []Part{
			{Text: text},
		},
	}
}

// BuildFunctionResponseContent creates a content with function response.
func BuildFunctionResponseContent(name string, response map[string]any) Content {
	return Content{
		Role: "user",
		Parts: []Part{
			{
				FunctionResponse: &FunctionResponse{
					Name:     name,
					Response: response,
				},
			},
		},
	}
}

// GetFinishReason returns a normalized finish reason
func GetFinishReason(reason string) string {
	switch strings.ToUpper(reason) {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	default:
		return strings.ToLower(reason)
	}
}

// HasFunctionCalls checks if the content has function calls
func HasFunctionCalls(content *Content) bool {
	if content == nil {
		return false
	}
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}

// EmbedContent generates embeddings for a single text
func (c *Client) EmbedContent(ctx context.Context, model string, req *EmbedContentRequest) (*EmbedContentResponse, error) {
	if model == "" {
		model = "text-embedding-004"
	}
	model = sanitizeModelName(model)

	url := fmt.Sprintf("%s/models/%s:embedContent", c.baseURL, model)
	headers := c.getHeaders()

	var response EmbedContentResponse
	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     url,
		Headers: headers,
		Body:    req,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// BatchEmbedContents generates embeddings for multiple texts
func (c *Client) BatchEmbedContents(ctx context.Context, model string, req *BatchEmbedContentsRequest) (*BatchEmbedContentsResponse, error) {
	if model == "" {
		model = "text-embedding-004"
	}
	model = sanitizeModelName(model)

	url := fmt.Sprintf("%s/models/%s:batchEmbedContents", c.baseURL, model)
	headers := c.getHeaders()

	var response BatchEmbedContentsResponse
	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     url,
		Headers: headers,
		Body:    req,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// ListModels retrieves available models from the Gemini API
func (c *Client) ListModels(ctx context.Context, params *ModelsListParams) (*ModelsListResponse, error) {
	headers := c.getHeaders()
	var response ModelsListResponse

	// Build URL with query parameters
	url := c.baseURL + "/models"
	if params != nil {
		queryParts := []string{}
		if params.PageSize > 0 {
			queryParts = append(queryParts, fmt.Sprintf("pageSize=%d", params.PageSize))
		}
		if params.PageToken != "" {
			queryParts = append(queryParts, "pageToken="+params.PageToken)
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

// GetModel retrieves a specific model by name
func (c *Client) GetModel(ctx context.Context, modelName string) (*ModelInfo, error) {
	headers := c.getHeaders()
	var response ModelInfo

	// Ensure model name has the correct format
	if !strings.HasPrefix(modelName, "models/") {
		modelName = "models/" + sanitizeModelName(modelName)
	} else {
		modelName = "models/" + sanitizeModelName(strings.TrimPrefix(modelName, "models/"))
	}

	url := c.baseURL + "/" + modelName

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

// WrapError converts an httpclient.APIError to an llms.APIError with Gemini provider context.
// For other errors, it wraps them in an llms.ProviderError.
// Returns nil if err is nil.
//
// Using the unified llms.APIError type ensures the SDK's retry, circuit-breaker,
// and fallback logic can classify Gemini errors via errors.As(&*llms.APIError),
// which a raw fmt.Errorf wrap would defeat.
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
			Provider:      llms.ProviderGemini,
			RequestURL:    httpErr.RequestURL,
			RequestMethod: httpErr.RequestMethod,
		}
	}

	// Check if it's already an llms.APIError
	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Provider == "" {
			apiErr.Provider = llms.ProviderGemini
		}
		return apiErr
	}

	// For other errors, wrap with provider context
	return llms.WrapProviderError(llms.ProviderGemini, operation, err)
}

func sanitizeModelName(model string) string {
	return httpclient.SanitizeModelName(strings.TrimPrefix(model, "models/"))
}
