package ollamaapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/internal/httpclient"
)

const defaultBaseURL = "http://localhost:11434"

// Client is a client for Ollama's native API.
type Client struct {
	httpClient *httpclient.Client
	baseURL    string
}

// ClientConfig configures the Ollama native API client.
type ClientConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration // Timeout for the underlying HTTP client; 0 uses the default.

	// AllowPrivateIPs allows requests to private/loopback IPs (SSRF opt-out).
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// NewClient creates a new Ollama native API client.
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

	baseURL := strings.TrimSuffix(config.BaseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// ListModels retrieves available models from /api/tags.
func (c *Client) ListModels(ctx context.Context) (*ListModelsResponse, error) {
	var response ListModelsResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method: http.MethodGet,
		URL:    c.baseURL + "/api/tags",
	}, &response)

	if err != nil {
		return nil, WrapError("list models", err)
	}

	return &response, nil
}

// ShowModel retrieves detailed information about a model.
func (c *Client) ShowModel(ctx context.Context, name string, verbose bool) (*ShowResponse, error) {
	req := ShowRequest{
		Name:    name,
		Verbose: verbose,
	}

	var response ShowResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/api/show",
		Body:   req,
	}, &response)

	if err != nil {
		return nil, WrapError("show model", err)
	}

	return &response, nil
}

// PullModel downloads a model from the registry.
// The callback is called for each progress update.
func (c *Client) PullModel(ctx context.Context, name string, callback func(PullResponse)) error {
	req := PullRequest{
		Name:   name,
		Stream: true,
	}

	body, err := c.httpClient.DoStream(ctx, httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/api/pull",
		Body:   req,
	})

	if err != nil {
		return WrapError("pull model", err)
	}
	defer func() { _ = body.Close() }()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var progress PullResponse
		if err := json.Unmarshal([]byte(line), &progress); err != nil {
			continue
		}

		if callback != nil {
			callback(progress)
		}

		if progress.Status == "success" {
			break
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return WrapError("pull model stream", err)
	}

	return nil
}

// DeleteModel removes a model from the local system.
func (c *Client) DeleteModel(ctx context.Context, name string) error {
	req := DeleteRequest{Name: name}

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method: http.MethodDelete,
		URL:    c.baseURL + "/api/delete",
		Body:   req,
	}, nil)

	if err != nil {
		return WrapError("delete model", err)
	}

	return nil
}

// CopyModel duplicates a model with a new name.
func (c *Client) CopyModel(ctx context.Context, source, destination string) error {
	req := CopyRequest{
		Source:      source,
		Destination: destination,
	}

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/api/copy",
		Body:   req,
	}, nil)

	if err != nil {
		return WrapError("copy model", err)
	}

	return nil
}

// ListRunning retrieves currently loaded models from /api/ps.
func (c *Client) ListRunning(ctx context.Context) (*ListRunningResponse, error) {
	var response ListRunningResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method: http.MethodGet,
		URL:    c.baseURL + "/api/ps",
	}, &response)

	if err != nil {
		return nil, WrapError("list running", err)
	}

	return &response, nil
}

// GetVersion retrieves the Ollama server version.
func (c *Client) GetVersion(ctx context.Context) (string, error) {
	var response VersionResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method: http.MethodGet,
		URL:    c.baseURL + "/api/version",
	}, &response)

	if err != nil {
		return "", WrapError("get version", err)
	}

	return response.Version, nil
}

// Generate sends a completion request to /api/generate.
func (c *Client) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	req.Stream = false

	var response GenerateResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/api/generate",
		Body:   req,
	}, &response)

	if err != nil {
		return nil, WrapError("generate", err)
	}

	return &response, nil
}

// GenerateStream sends a streaming completion request.
func (c *Client) GenerateStream(ctx context.Context, req *GenerateRequest) (*StreamReader, error) {
	req.Stream = true

	body, err := c.httpClient.DoStream(ctx, httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/api/generate",
		Body:   req,
	})

	if err != nil {
		return nil, WrapError("generate stream", err)
	}

	return &StreamReader{
		body:   body,
		reader: bufio.NewReader(body),
	}, nil
}

// Chat sends a chat request to /api/chat.
func (c *Client) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	req.Stream = false

	var response ChatResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/api/chat",
		Body:   req,
	}, &response)

	if err != nil {
		return nil, WrapError("chat", err)
	}

	return &response, nil
}

// ChatStream sends a streaming chat request.
func (c *Client) ChatStream(ctx context.Context, req *ChatRequest) (*ChatStreamReader, error) {
	req.Stream = true

	body, err := c.httpClient.DoStream(ctx, httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/api/chat",
		Body:   req,
	})

	if err != nil {
		return nil, WrapError("chat stream", err)
	}

	return &ChatStreamReader{
		body:   body,
		reader: bufio.NewReader(body),
	}, nil
}

// Embed generates embeddings via /api/embed.
func (c *Client) Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error) {
	var response EmbedResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/api/embed",
		Body:   req,
	}, &response)

	if err != nil {
		return nil, WrapError("embed", err)
	}

	return &response, nil
}

// StreamReader reads streaming generate responses.
type StreamReader struct {
	body   io.ReadCloser
	reader *bufio.Reader
}

// Read reads the next response from the stream.
func (r *StreamReader) Read() (*GenerateResponse, error) {
	line, err := readNDJSONLine(r.reader)
	if err != nil {
		return nil, err
	}

	var resp GenerateResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("ollama stream error: %s", resp.Error)
	}

	return &resp, nil
}

// Close closes the stream.
func (r *StreamReader) Close() error {
	return r.body.Close()
}

// ChatStreamReader reads streaming chat responses.
type ChatStreamReader struct {
	body   io.ReadCloser
	reader *bufio.Reader
}

// Read reads the next response from the stream.
func (r *ChatStreamReader) Read() (*ChatResponse, error) {
	line, err := readNDJSONLine(r.reader)
	if err != nil {
		return nil, err
	}

	var resp ChatResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("ollama stream error: %s", resp.Error)
	}

	return &resp, nil
}

// Close closes the stream.
func (r *ChatStreamReader) Close() error {
	return r.body.Close()
}

func readNDJSONLine(reader *bufio.Reader) (string, error) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if strings.TrimSpace(line) == "" {
					return "", io.EOF
				}
				return line, nil
			}
			return "", err
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		return line, nil
	}
}

// WrapError converts errors to llms.APIError or ProviderError.
func WrapError(operation string, err error) error {
	if err == nil {
		return nil
	}

	var httpErr *httpclient.APIError
	if errors.As(err, &httpErr) {
		return &llms.APIError{
			StatusCode:    httpErr.StatusCode,
			Message:       httpErr.Message,
			Type:          httpErr.Type,
			Code:          httpErr.Code,
			Provider:      llms.ProviderOllama,
			RequestURL:    httpErr.RequestURL,
			RequestMethod: httpErr.RequestMethod,
		}
	}

	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Provider == "" {
			apiErr.Provider = llms.ProviderOllama
		}
		return apiErr
	}

	return llms.WrapProviderError(llms.ProviderOllama, operation, err)
}
