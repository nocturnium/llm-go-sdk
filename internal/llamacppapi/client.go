package llamacppapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/internal/httpclient"
)

const defaultBaseURL = "http://localhost:8080"

// Client is a client for llama.cpp's native API.
type Client struct {
	httpClient *httpclient.Client
	baseURL    string
	apiKey     string
}

// ClientConfig configures the llama.cpp native API client.
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

// NewClient creates a new llama.cpp native API client.
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
		apiKey:     config.APIKey,
	}
}

// GetProps retrieves server properties from /props.
func (c *Client) GetProps(ctx context.Context) (*PropsResponse, error) {
	var response PropsResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodGet,
		URL:     c.baseURL + "/props",
		Headers: c.getHeaders(),
	}, &response)

	if err != nil {
		return nil, WrapError("get props", err)
	}

	return &response, nil
}

// GetHealth retrieves server health from /health.
func (c *Client) GetHealth(ctx context.Context) (*HealthResponse, error) {
	var response HealthResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodGet,
		URL:     c.baseURL + "/health",
		Headers: c.getHeaders(),
	}, &response)

	if err != nil {
		return nil, WrapError("get health", err)
	}

	return &response, nil
}

// GetSlots retrieves inference slot states from /slots.
func (c *Client) GetSlots(ctx context.Context) ([]Slot, error) {
	var response SlotsResponse

	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  http.MethodGet,
		URL:     c.baseURL + "/slots",
		Headers: c.getHeaders(),
	}, &response)

	if err != nil {
		return nil, WrapError("get slots", err)
	}

	return response, nil
}

func (c *Client) getHeaders() map[string]string {
	headers := make(map[string]string)
	if c.apiKey != "" {
		headers["Authorization"] = "Bearer " + c.apiKey
	}
	return headers
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
			Provider:      llms.ProviderLlamaCpp,
			RequestURL:    httpErr.RequestURL,
			RequestMethod: httpErr.RequestMethod,
		}
	}

	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Provider == "" {
			apiErr.Provider = llms.ProviderLlamaCpp
		}
		return apiErr
	}

	return llms.WrapProviderError(llms.ProviderLlamaCpp, operation, err)
}
