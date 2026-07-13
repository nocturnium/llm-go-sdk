// Package httpclient provides HTTP utilities for LLM API calls.
//
// This package includes:
//   - An HTTP client with configurable retry logic and exponential backoff
//   - Server-Sent Events (SSE) parser for streaming responses
//   - JSON request/response helpers
//
// Retries are OFF by default: a client created with NewClient does not retry.
// Retry behavior is opt-in via WithRetryPolicy. This keeps the HTTP layer from
// silently multiplying upstream calls when a resilience wrapper
// (resilience.NewResilientClient) is layered on top — that wrapper is the single retry
// authority. When WithRetryPolicy is set, transient failures (429, 5xx) are
// retried with exponential backoff. Streaming requests are never retried to
// avoid duplicate content.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nocturnium/llm-go-sdk/v5/internal/logsanitize"
)

// maxErrorResponseSize is the maximum size of error response body to read.
// This prevents memory exhaustion from malicious or misconfigured servers.
const maxErrorResponseSize = 1 * 1024 * 1024 // 1MB

// maxResponseSize bounds non-streaming response bodies (DoJSON/DoRaw) to prevent
// memory exhaustion from a hostile or misconfigured server.
const maxResponseSize = 100 * 1024 * 1024 // 100MB

// maxRedirects bounds the number of redirect hops the client will follow.
// Each hop is re-validated against the same SSRF policy as the initial request.
const maxRedirects = 10

// crossHostRedirectCredentialHeaders are provider API-key/auth headers that
// must not be forwarded when a redirect leaves the original request host.
var crossHostRedirectCredentialHeaders = []string{
	"x-goog-api-key",
	"api-key",
	"x-api-key",
	"Authorization",
	"Proxy-Authorization",
}

// Client is an HTTP client with retry support.
type Client struct {
	httpClient  *http.Client
	retryPolicy *RetryPolicy

	// SSRF validation policy. Both default to false (secure): requests to
	// private/loopback IPs and plain HTTP are rejected unless explicitly allowed.
	allowPrivateIPs bool
	allowHTTP       bool
}

// ClientOption configures the client.
type ClientOption func(*Client)

// NewClient creates a new HTTP client with the given options.
// Default timeout is 5 minutes, which can be overridden with WithTimeout or
// the LLM_HTTP_TIMEOUT environment variable (e.g., "10m", "300s").
//
// Retries are disabled by default (NoRetryPolicy). Callers that want retries
// should either wrap the provider with resilience.NewResilientClient (the recommended
// single retry authority) or opt in at the HTTP layer with WithRetryPolicy.
func NewClient(opts ...ClientOption) *Client {
	// Default timeout, can be overridden by LLM_HTTP_TIMEOUT env var
	timeout := 5 * time.Minute
	if envTimeout := getEnvTimeout("LLM_HTTP_TIMEOUT"); envTimeout > 0 {
		timeout = envTimeout
	}

	c := &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		retryPolicy: NoRetryPolicy(),
	}
	for _, opt := range opts {
		opt(c)
	}

	// Install a redirect policy that re-validates every hop against the same
	// SSRF rules as the initial request. This runs even when the caller
	// supplied their own *http.Client via WithHTTPClient, so validation cannot
	// be bypassed by a redirect to a private IP (e.g. 169.254.169.254).
	c.installRedirectPolicy()

	// Validate the *resolved* IP at connect time. The hostname-based checks above
	// (and on redirects) cannot stop a name that resolves to a private IP or a
	// DNS-rebinding attack; the dialer Control hook closes that gap by rejecting
	// connections to private/loopback/link-local addresses.
	c.installSSRFDialer()

	return c
}

// installSSRFDialer sets a dialer Control hook that rejects connections to
// private/internal IPs based on the address actually resolved at dial time. It is
// a no-op when private IPs are allowed, or when the client uses a non-standard
// RoundTripper that cannot accept a dialer (the URL/redirect host checks still
// apply in that case).
func (c *Client) installSSRFDialer() {
	if c.allowPrivateIPs {
		return
	}
	tr, ok := c.httpClient.Transport.(*http.Transport)
	callerProvidedTransport := ok
	if !ok {
		if c.httpClient.Transport != nil {
			return // custom RoundTripper; cannot inject a dialer
		}
		// No transport configured: start from a clone of the default.
		if base, baseOK := http.DefaultTransport.(*http.Transport); baseOK {
			tr = base.Clone()
		} else {
			tr = &http.Transport{}
		}
	}
	tr = tr.Clone()
	c.httpClient.Transport = tr

	originalDialContext := tr.DialContext
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   ssrfDialControl,
	}
	if callerProvidedTransport && originalDialContext != nil {
		tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			if err := validateNotPrivateHostFromAddress(address); err != nil {
				return nil, err
			}
			conn, err := originalDialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			if err := validateConnRemoteAddr(conn); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return conn, nil
		}
		return
	}
	tr.DialContext = dialer.DialContext
}

func validateConnRemoteAddr(conn net.Conn) error {
	if conn == nil || conn.RemoteAddr() == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		host = conn.RemoteAddr().String()
	}
	if ip := net.ParseIP(host); ip != nil {
		return validateNotPrivateIP(ip)
	}
	return nil
}

// ssrfDialControl is a net.Dialer Control hook that rejects connections whose
// resolved address is a private/internal IP. The dialer resolves the hostname
// before invoking Control, so address is the concrete IP:port being dialed —
// closing the DNS-rebinding / redirect-to-private gap that hostname checks miss.
func ssrfDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	if ip := net.ParseIP(host); ip != nil {
		return validateNotPrivateIP(ip)
	}
	return nil
}

// installRedirectPolicy wraps the underlying *http.Client.CheckRedirect so that
// each redirect hop's URL is re-validated with the client's SSRF options.
// It bounds redirects to maxRedirects.
func (c *Client) installRedirectPolicy() {
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	c.httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if err := c.validateURL(req.URL.String()); err != nil {
			return fmt.Errorf("redirect blocked: %w", err)
		}
		if len(via) > 0 && !sameRedirectHostname(req, via[0]) {
			for _, header := range crossHostRedirectCredentialHeaders {
				req.Header.Del(header)
			}
		}
		return nil
	}
}

func sameRedirectHostname(req, original *http.Request) bool {
	if req == nil || req.URL == nil || original == nil || original.URL == nil {
		return false
	}
	return strings.EqualFold(req.URL.Hostname(), original.URL.Hostname())
}

// validationOptions builds the URL validation policy from the client's flags.
// It starts from the secure defaults and relaxes only the requested aspects,
// keeping AllowCustomPorts=true so ports like :11434 work and AllowedHosts nil
// (any non-private host is permitted).
func (c *Client) validationOptions() *URLValidationOptions {
	opts := DefaultURLValidationOptions()
	opts.AllowPrivateIPs = c.allowPrivateIPs
	opts.AllowHTTP = c.allowHTTP
	return opts
}

// validateURL checks a single URL against the client's SSRF policy.
func (c *Client) validateURL(rawURL string) error {
	return ValidateURL(rawURL, c.validationOptions())
}

// getEnvTimeout reads a duration from an environment variable.
func getEnvTimeout(key string) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return 0
}

// debugEnabled returns true if LLM_DEBUG_REQUESTS environment variable is set.
// Set LLM_DEBUG_REQUESTS=1 to enable detailed request/response logging.
func debugEnabled() bool {
	return os.Getenv("LLM_DEBUG_REQUESTS") != ""
}

// sanitizeLogValue escapes control characters in a value before it is written
// to the debug log.
func sanitizeLogValue(s string) string {
	return logsanitize.Value(s)
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// WithRetryPolicy sets a custom retry policy, opting this client into HTTP-layer
// retries. Retries are off by default (see NewClient); pass DefaultRetryPolicy()
// to enable standard 429/5xx exponential backoff. Prefer wrapping the provider
// with resilience.NewResilientClient instead, so retries have a single authority.
func WithRetryPolicy(policy *RetryPolicy) ClientOption {
	return func(c *Client) {
		c.retryPolicy = policy
	}
}

// WithHTTPClient sets a custom HTTP client.
//
// The supplied client is shallow-copied before SDK redirect and SSRF policies
// are installed, so the caller-owned *http.Client is not mutated.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		if client == nil {
			c.httpClient = nil
			return
		}
		copied := *client
		c.httpClient = &copied
	}
}

// WithAllowPrivateIPs allows (true) or rejects (false) requests to
// private/loopback/link-local IP addresses and internal hostnames. Default is
// false (secure) to guard against SSRF.
func WithAllowPrivateIPs(allow bool) ClientOption {
	return func(c *Client) {
		c.allowPrivateIPs = allow
	}
}

// WithAllowHTTP allows (true) or rejects (false) plain-HTTP (non-HTTPS) URLs.
// Default is false (secure); required for self-hosted/local endpoints.
func WithAllowHTTP(allow bool) ClientOption {
	return func(c *Client) {
		c.allowHTTP = allow
	}
}

// Request represents an HTTP request configuration.
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    any
}

// DoJSON performs an HTTP request with JSON body and unmarshals the response.
func (c *Client) DoJSON(ctx context.Context, req Request, response any) error {
	var bodyBytes []byte
	var err error

	if req.Body != nil {
		bodyBytes, err = json.Marshal(req.Body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	// Debug logging when LLM_DEBUG_REQUESTS is set
	if debugEnabled() {
		fmt.Printf("[DEBUG] LLM Request URL: %s %s\n", sanitizeLogValue(req.Method), sanitizeLogValue(req.URL))
		if len(bodyBytes) > 0 {
			// Pretty print the JSON for readability (truncate if too large)
			var prettyJSON bytes.Buffer
			if err := json.Indent(&prettyJSON, bodyBytes, "", "  "); err == nil {
				body := prettyJSON.String()
				if len(body) > 5000 {
					body = body[:5000] + "...[truncated]"
				}
				fmt.Printf("[DEBUG] LLM Request Body:\n%s\n", body)
			}
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set GetBody to enable retries - the body bytes are captured in the closure
	if len(bodyBytes) > 0 {
		httpReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.doWithRetry(ctx, httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return c.handleErrorResponse(resp)
	}

	if response != nil {
		// Bound the body so a hostile server can't exhaust memory.
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseSize)).Decode(response); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// DoRaw performs an HTTP request with an optional JSON body and returns the raw
// response body. It uses the same retry, timeout, and error handling path as
// DoJSON.
func (c *Client) DoRaw(ctx context.Context, req Request) ([]byte, error) {
	data, _, err := c.DoRawWithHeaders(ctx, req)
	return data, err
}

// DoRawWithHeaders performs an HTTP request with an optional JSON body and
// returns the raw response body and response headers. It uses the same retry,
// timeout, error handling, and response-size limit path as DoRaw.
func (c *Client) DoRawWithHeaders(ctx context.Context, req Request) ([]byte, http.Header, error) {
	var bodyBytes []byte
	var err error

	if req.Body != nil {
		bodyBytes, err = json.Marshal(req.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	if len(bodyBytes) > 0 {
		httpReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.doWithRetry(ctx, httpReq)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, nil, c.handleErrorResponse(resp)
	}

	if resp.ContentLength > maxResponseSize {
		return nil, nil, fmt.Errorf("response exceeds maximum size of %d bytes", maxResponseSize)
	}

	// Bound the body so a hostile server can't exhaust memory; reading one byte
	// past the cap lets us detect and reject an over-limit response explicitly.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > maxResponseSize {
		return nil, nil, fmt.Errorf("response exceeds maximum size of %d bytes", maxResponseSize)
	}
	return data, resp.Header.Clone(), nil
}

// DoStream performs an HTTP request and returns the response body for streaming.
// The caller is responsible for closing the response body.
func (c *Client) DoStream(ctx context.Context, req Request) (io.ReadCloser, error) {
	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// SSRF guard: validate the destination before issuing the request.
	if err := c.validateURL(httpReq.URL.String()); err != nil {
		return nil, fmt.Errorf("URL validation failed: %w", err)
	}

	// For streaming, we don't retry on non-error responses. Use a timeout-free
	// shallow copy so http.Client.Timeout does not keep running while the caller
	// consumes resp.Body; cancellation is controlled by the request context.
	resp, err := c.streamHTTPClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, c.handleErrorResponse(resp)
	}

	return resp.Body, nil
}

func (c *Client) streamHTTPClient() *http.Client {
	streamClient := *c.httpClient
	streamClient.Timeout = 0
	return &streamClient
}

func (c *Client) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	// SSRF guard: validate the destination before issuing the request. This is
	// the central choke point for DoJSON and DoRaw; DoStream validates inline.
	if err := c.validateURL(req.URL.String()); err != nil {
		return nil, fmt.Errorf("URL validation failed: %w", err)
	}

	var lastErr error
	var retryAfter time.Duration
	for attempt := 0; attempt <= c.retryPolicy.MaxRetries; attempt++ {
		// Check context before each attempt to fail fast
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("context canceled after %d attempts: %w (last error: %w)", attempt, ctx.Err(), lastErr)
			}
			return nil, ctx.Err()
		default:
		}

		if attempt > 0 {
			// Jitter the exponential backoff to avoid a synchronized thundering
			// herd, and honor a server Retry-After when it exceeds our backoff
			// (bounded by MaxDelay).
			delay := jitterDelay(c.retryPolicy.GetDelay(attempt))
			if retryAfter > delay {
				delay = retryAfter
			}
			if c.retryPolicy.MaxDelay > 0 && delay > c.retryPolicy.MaxDelay {
				delay = c.retryPolicy.MaxDelay
			}
			retryAfter = 0
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		attemptReq := req.Clone(ctx)
		if req.Body != nil {
			if req.GetBody == nil {
				if attempt > 0 {
					return nil, fmt.Errorf("cannot retry request with body that cannot be re-read")
				}
				attemptReq.Body = req.Body
			} else {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("failed to get request body for attempt %d: %w", attempt+1, err)
				}
				attemptReq.Body = body
			}
		}

		resp, err := c.httpClient.Do(attemptReq)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			lastErr = err
			if !IsRetryable(0, err) {
				return nil, fmt.Errorf("request failed: %w", err)
			}
			continue
		}

		// Check if we should retry based on status code
		if IsRetryable(resp.StatusCode, nil) && c.retryPolicy.ShouldRetry(resp.StatusCode) {
			// On the final attempt, return the response so the caller can parse the
			// provider's *APIError (status, Retry-After, request id) instead of
			// receiving a generic "retries exhausted" error.
			if attempt >= c.retryPolicy.MaxRetries {
				return resp, nil
			}
			// A request body that cannot be re-read cannot be retried.
			if req.Body != nil && req.GetBody == nil {
				return resp, nil
			}
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
			drainAndClose(resp.Body)
			lastErr = fmt.Errorf("received retryable status code: %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("request failed after %d retries: %w", c.retryPolicy.MaxRetries, lastErr)
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxErrorResponseSize))
	_ = body.Close()
}

// parseRetryAfter parses a Retry-After header value, which per RFC 9110 may be a
// non-negative integer number of seconds or an HTTP-date. Returns 0 for an empty,
// malformed, zero, or already-past value.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// jitterDelay applies equal jitter to a backoff delay: half is fixed and half is
// randomized, spreading retries across concurrent clients to avoid a synchronized
// thundering herd on a recovering upstream. Uses math/rand/v2 — this is
// scheduling jitter, not a security-sensitive value.
func jitterDelay(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	half := int64(d) / 2
	return time.Duration(half + rand.Int64N(half+1)) // #nosec G404 -- non-cryptographic backoff jitter
}

// IsRetryable returns true when a status code or error represents a transient
// HTTP failure. Context cancellation and deadline errors are never retryable.
func IsRetryable(statusCode int, err error) bool {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return true
	}

	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		529: // Site Overloaded (Anthropic; no net/http constant). Must match retry.go's policy list.
		return true
	}
	return false
}

func (c *Client) handleErrorResponse(resp *http.Response) error {
	// Extract request context for debugging (if available)
	var reqURL, reqMethod string
	if resp.Request != nil {
		reqMethod = resp.Request.Method
		if resp.Request.URL != nil {
			reqURL = resp.Request.URL.String()
		}
	}

	// Check Content-Length if present to fail fast on oversized responses
	if resp.ContentLength > maxErrorResponseSize {
		return &APIError{
			StatusCode:    resp.StatusCode,
			Message:       fmt.Sprintf("error response too large: %d bytes exceeds limit of %d bytes", resp.ContentLength, maxErrorResponseSize),
			RequestURL:    reqURL,
			RequestMethod: reqMethod,
		}
	}

	// Use LimitReader to prevent unbounded memory allocation from malicious servers
	limitedReader := io.LimitReader(resp.Body, maxErrorResponseSize+1)
	body, readErr := io.ReadAll(limitedReader)
	if readErr != nil {
		return &APIError{
			StatusCode:    resp.StatusCode,
			Message:       fmt.Sprintf("failed to read error response: %v", readErr),
			RequestURL:    reqURL,
			RequestMethod: reqMethod,
		}
	}

	// Debug logging for error responses
	if debugEnabled() {
		fmt.Printf("[DEBUG] LLM Error Response (status %d) from %s %s:\n%s\n", resp.StatusCode, sanitizeLogValue(reqMethod), sanitizeLogValue(reqURL), sanitizeLogValue(string(body)))
	}

	// Check if we hit the limit (response was truncated)
	if len(body) > maxErrorResponseSize {
		return &APIError{
			StatusCode:    resp.StatusCode,
			Message:       fmt.Sprintf("error response truncated: exceeded maximum size of %d bytes", maxErrorResponseSize),
			RequestURL:    reqURL,
			RequestMethod: reqMethod,
		}
	}

	// Extract request ID from headers if present
	requestID := resp.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = resp.Header.Get("Request-Id")
	}

	// Extract retry-after header (seconds or HTTP-date) for rate limiting.
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	if parsed, ok := parseAPIErrorBody(body); ok {
		return &APIError{
			StatusCode:    resp.StatusCode,
			Message:       parsed.Message,
			Type:          parsed.Type,
			Code:          parsed.Code,
			Param:         parsed.Param,
			RequestID:     requestID,
			RetryAfter:    retryAfter,
			RequestURL:    reqURL,
			RequestMethod: reqMethod,
		}
	}

	return &APIError{
		StatusCode:    resp.StatusCode,
		Message:       sanitizeLogValue(string(body)),
		RequestID:     requestID,
		RetryAfter:    retryAfter,
		RequestURL:    reqURL,
		RequestMethod: reqMethod,
	}
}

type parsedAPIErrorBody struct {
	Message string
	Type    string
	Code    string
	Param   string
}

func parseAPIErrorBody(body []byte) (parsedAPIErrorBody, bool) {
	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Type    string          `json:"type"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return parsedAPIErrorBody{}, false
	}

	if len(envelope.Error) > 0 && !bytes.Equal(bytes.TrimSpace(envelope.Error), []byte("null")) {
		var nested struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
			Param   string `json:"param"`
		}
		if err := json.Unmarshal(envelope.Error, &nested); err == nil && nested.Message != "" {
			return parsedAPIErrorBody{
				Message: sanitizeLogValue(nested.Message),
				Type:    sanitizeLogValue(nested.Type),
				Code:    sanitizeLogValue(nested.Code),
				Param:   sanitizeLogValue(nested.Param),
			}, true
		}

		var message string
		if err := json.Unmarshal(envelope.Error, &message); err == nil && message != "" {
			return parsedAPIErrorBody{Message: sanitizeLogValue(message)}, true
		}
	}

	if envelope.Message != "" {
		return parsedAPIErrorBody{
			Message: sanitizeLogValue(envelope.Message),
			Type:    sanitizeLogValue(envelope.Type),
		}, true
	}

	return parsedAPIErrorBody{}, false
}

// APIError represents an error from the LLM API with full request context
// for debugging and troubleshooting.
type APIError struct {
	StatusCode int
	Message    string
	Type       string
	Code       string
	Param      string        // The parameter that caused the error
	RequestID  string        // Request ID for debugging
	RetryAfter time.Duration // Suggested retry delay for rate limiting

	// Request context for debugging
	RequestURL    string // The URL that was called
	RequestMethod string // The HTTP method used
}

// Error implements the error interface.
func (e *APIError) Error() string {
	var sb strings.Builder
	sb.WriteString("API error")

	// Add status code
	sb.WriteString(fmt.Sprintf(" (status %d", e.StatusCode))

	// Add type and code if present
	if e.Type != "" {
		sb.WriteString(", type ")
		sb.WriteString(e.Type)
	}
	if e.Code != "" {
		sb.WriteString(", code ")
		sb.WriteString(e.Code)
	}

	sb.WriteString("): ")
	sb.WriteString(e.Message)

	// Add request context if available
	if e.RequestMethod != "" || e.RequestURL != "" {
		sb.WriteString(" [")
		if e.RequestMethod != "" {
			sb.WriteString(e.RequestMethod)
			sb.WriteString(" ")
		}
		if e.RequestURL != "" {
			sb.WriteString(sanitizeRequestURL(e.RequestURL))
		}
		sb.WriteString("]")
	}

	// Add request ID if available
	if e.RequestID != "" {
		sb.WriteString(" (request_id: ")
		sb.WriteString(e.RequestID)
		sb.WriteString(")")
	}

	return sb.String()
}

func sanitizeRequestURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		if idx := strings.Index(rawURL, "?"); idx >= 0 {
			return rawURL[:idx]
		}
		return rawURL
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String()
}

// IsRetryable returns true if the error is likely transient and can be retried.
func (e *APIError) IsRetryable() bool {
	switch e.StatusCode {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}
