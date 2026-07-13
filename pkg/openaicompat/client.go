package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/nocturnium/llm-go-sdk/v5/internal/httpclient"
)

// Client is an OpenAI-compatible API client
type Client struct {
	httpClient   *httpclient.Client
	baseURL      string
	apiKey       string
	headers      map[string]string
	azureAPIKey  bool   // Use api-key header instead of Authorization
	azureVersion string // Azure API version (appended to URLs)
}

// ClientConfig configures the client
type ClientConfig struct {
	BaseURL      string
	APIKey       string
	Headers      map[string]string
	HTTPClient   *http.Client
	Timeout      time.Duration // Timeout for the underlying HTTP client; 0 uses the default.
	AzureAPIKey  bool          // Use api-key header instead of Authorization
	AzureVersion string        // Azure API version (appended to URLs)

	// AllowPrivateIPs allows requests to private/loopback IPs (SSRF opt-out).
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// NewClient creates a new OpenAI-compatible client
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

	return &Client{
		httpClient:   httpClient,
		baseURL:      config.BaseURL,
		apiKey:       config.APIKey,
		headers:      config.Headers,
		azureAPIKey:  config.AzureAPIKey,
		azureVersion: config.AzureVersion,
	}
}

// CreateChatCompletion sends a chat completion request
func (c *Client) CreateChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	headers := c.getHeaders()
	var response ChatCompletionResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.buildURL("/chat/completions"),
		Headers: headers,
		Body:    req,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// CreateChatCompletionStream sends a streaming chat completion request
func (c *Client) CreateChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (*StreamReader, error) {
	req.Stream = true
	headers := c.getHeaders()

	body, err := c.httpClient.DoStream(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.buildURL("/chat/completions"),
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
	headers := make(map[string]string)

	if c.azureAPIKey {
		// Azure uses api-key header
		headers["api-key"] = c.apiKey
	} else if c.apiKey != "" {
		// Standard OpenAI uses Bearer token
		headers["Authorization"] = "Bearer " + c.apiKey
	}

	for k, v := range c.headers {
		headers[k] = v
	}

	return headers
}

// buildURL constructs the full URL, appending Azure API version if needed.
func (c *Client) buildURL(path string) string {
	fullURL, err := appendPath(c.baseURL, path)
	if err != nil {
		fullURL = strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	}
	if c.azureVersion == "" {
		return fullURL
	}

	u, err := url.Parse(fullURL)
	if err != nil {
		return fullURL
	}
	values := u.Query()
	values.Set("api-version", c.azureVersion)
	u.RawQuery = values.Encode()
	return u.String()
}

// StreamReader reads streaming responses
type StreamReader struct {
	sseReader *httpclient.SSEReader
}

// Read reads the next chunk from the stream
// Returns io.EOF when the stream is done
func (r *StreamReader) Read() (*StreamChunk, error) {
	for {
		event, err := r.sseReader.Read()
		if err != nil {
			return nil, err
		}

		// Check for [DONE] marker
		if event.Data == "[DONE]" {
			return nil, io.EOF
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

// Close closes the stream
func (r *StreamReader) Close() error {
	return r.sseReader.Close()
}

// CreateEmbedding sends an embedding request
func (c *Client) CreateEmbedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	headers := c.getHeaders()
	var response EmbeddingResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     c.buildURL("/embeddings"),
		Headers: headers,
		Body:    req,
	}, &response)

	if err != nil {
		return nil, err
	}

	return &response, nil
}

// ListModels retrieves the list of available models.
// Handles both standard OpenAI format {"object": "list", "data": [...]}
// and raw array format [...] returned by some providers like TogetherAI.
func (c *Client) ListModels(ctx context.Context) (*ModelsListResponse, error) {
	headers := c.getHeaders()

	resp, err := c.doRawRequest(ctx, http.MethodGet, c.buildURL("/models"), headers, nil)
	if err != nil {
		return nil, err
	}

	return c.parseModelsResponse(resp)
}

// ListModelsWithQuery retrieves models with optional query parameters.
// Query parameters are properly URL-encoded to handle special characters safely.
// Handles both standard OpenAI format and raw array format.
func (c *Client) ListModelsWithQuery(ctx context.Context, queryParams map[string]string) (*ModelsListResponse, error) {
	headers := c.getHeaders()

	requestURL := c.buildURL("/models")
	if len(queryParams) > 0 {
		u, err := url.Parse(requestURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse request URL: %w", err)
		}
		values := url.Values{}
		for k, existing := range u.Query() {
			for _, v := range existing {
				values.Add(k, v)
			}
		}
		for k, v := range queryParams {
			values.Add(k, v)
		}
		u.RawQuery = values.Encode()
		requestURL = u.String()
	}

	resp, err := c.doRawRequest(ctx, http.MethodGet, requestURL, headers, nil)
	if err != nil {
		return nil, err
	}

	return c.parseModelsResponse(resp)
}

// doRawRequest performs an HTTP request and returns the raw response body.
func (c *Client) doRawRequest(ctx context.Context, method, url string, headers map[string]string, body any) ([]byte, error) {
	return c.httpClient.DoRaw(ctx, httpclient.Request{
		Method:  method,
		URL:     url,
		Headers: headers,
		Body:    body,
	})
}

// parseModelsResponse handles both OpenAI format {"object": "list", "data": [...]}
// and raw array format [...] returned by some providers like TogetherAI.
func (c *Client) parseModelsResponse(data []byte) (*ModelsListResponse, error) {
	// Trim whitespace to check the first character
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// Check if response is an array (starts with '[')
	if trimmed[0] == '[' {
		// Parse as raw array of models
		var models []ModelResponse
		if err := json.Unmarshal(data, &models); err != nil {
			return nil, fmt.Errorf("failed to decode response as array: %w", err)
		}
		return &ModelsListResponse{
			Object: "list",
			Data:   models,
		}, nil
	}

	// Parse as standard OpenAI format
	var response ModelsListResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

func appendPath(baseURL, suffix string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(u.Path, "/")
	suffixPath := strings.TrimLeft(suffix, "/")
	if suffixPath == "" {
		return u.String(), nil
	}
	u.Path = path.Join(basePath, suffixPath)
	return u.String(), nil
}
