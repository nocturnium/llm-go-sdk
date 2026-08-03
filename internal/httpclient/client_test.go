package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))

	if client.httpClient == nil {
		t.Error("expected httpClient to be set")
	}
	if client.retryPolicy == nil {
		t.Error("expected retryPolicy to be set")
	}
}

func TestNewClient_WithTimeout(t *testing.T) {
	client := NewClient(WithTimeout(5 * time.Second))

	if client.httpClient.Timeout != 5*time.Second {
		t.Errorf("expected timeout=5s, got %v", client.httpClient.Timeout)
	}
}

func TestNewClient_WithRetryPolicy(t *testing.T) {
	policy := NoRetryPolicy()
	client := NewClient(WithRetryPolicy(policy))

	if client.retryPolicy.MaxRetries != 0 {
		t.Error("expected no retry policy to be applied")
	}
}

func TestClient_DoJSON_Success(t *testing.T) {
	type testRequest struct {
		Name string `json:"name"`
	}
	type testResponse struct {
		Message string `json:"message"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type=application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("X-Custom") != "header-value" {
			t.Errorf("expected X-Custom=header-value, got %s", r.Header.Get("X-Custom"))
		}

		var req testRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Name != "test" {
			t.Errorf("expected name=test, got %s", req.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(testResponse{Message: "success"})
	}))
	defer server.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	var resp testResponse

	err := client.DoJSON(context.Background(), Request{
		Method:  http.MethodPost,
		URL:     server.URL,
		Headers: map[string]string{"X-Custom": "header-value"},
		Body:    testRequest{Name: "test"},
	}, &resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message != "success" {
		t.Errorf("expected message=success, got %s", resp.Message)
	}
}

func TestClient_DoJSON_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Invalid request",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer server.Close()

	client := NewClient(WithRetryPolicy(NoRetryPolicy()), WithAllowPrivateIPs(true), WithAllowHTTP(true))

	err := client.DoJSON(context.Background(), Request{
		Method: http.MethodPost,
		URL:    server.URL,
	}, nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("expected status=400, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "Invalid request" {
		t.Errorf("expected message='Invalid request', got '%s'", apiErr.Message)
	}
	if apiErr.Type != "invalid_request_error" {
		t.Errorf("expected type='invalid_request_error', got '%s'", apiErr.Type)
	}
}

func TestClient_DoJSON_AnthropicError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":    "authentication_error",
			"message": "Invalid API key",
		})
	}))
	defer server.Close()

	client := NewClient(WithRetryPolicy(NoRetryPolicy()), WithAllowPrivateIPs(true), WithAllowHTTP(true))

	err := client.DoJSON(context.Background(), Request{
		Method: http.MethodPost,
		URL:    server.URL,
	}, nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Message != "Invalid API key" {
		t.Errorf("expected message='Invalid API key', got '%s'", apiErr.Message)
	}
}

func TestClient_DoJSON_StringErrorEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"plain provider message"}`))
	}))
	defer server.Close()

	client := NewClient(WithRetryPolicy(NoRetryPolicy()), WithAllowPrivateIPs(true), WithAllowHTTP(true))

	err := client.DoJSON(context.Background(), Request{
		Method: http.MethodPost,
		URL:    server.URL,
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Message != "plain provider message" {
		t.Fatalf("Message = %q, want string error message", apiErr.Message)
	}
}

func TestClient_DoJSON_PlainTextError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error\r\n\x1b[31m"))
	}))
	defer server.Close()

	client := NewClient(WithRetryPolicy(NoRetryPolicy()), WithAllowPrivateIPs(true), WithAllowHTTP(true))

	err := client.DoJSON(context.Background(), Request{
		Method: http.MethodPost,
		URL:    server.URL,
	}, nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("expected status=500, got %d", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "Internal Server Error") {
		t.Errorf("expected message to contain 'Internal Server Error', got %q", apiErr.Message)
	}
	if strings.ContainsAny(apiErr.Message, "\r\n\x1b") {
		t.Errorf("expected fallback message to be sanitized, got %q", apiErr.Message)
	}
	if !strings.Contains(apiErr.Message, `\r\n\x1b`) {
		t.Errorf("expected fallback message to preserve escaped controls, got %q", apiErr.Message)
	}
}

func TestClient_DoJSON_JSONErrorPreservesCodeTypeForClassification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "model not found\ntry another",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
				"param":   "model",
			},
		})
	}))
	defer server.Close()

	client := NewClient(WithRetryPolicy(NoRetryPolicy()), WithAllowPrivateIPs(true), WithAllowHTTP(true))

	err := client.DoJSON(context.Background(), Request{
		Method: http.MethodPost,
		URL:    server.URL,
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Message != `model not found\ntry another` {
		t.Fatalf("Message = %q, want sanitized JSON error message", apiErr.Message)
	}
	if apiErr.Type != "invalid_request_error" {
		t.Fatalf("Type = %q, want invalid_request_error", apiErr.Type)
	}
	if apiErr.Code != "model_not_found" {
		t.Fatalf("Code = %q, want model_not_found", apiErr.Code)
	}
	if apiErr.Param != "model" {
		t.Fatalf("Param = %q, want model", apiErr.Param)
	}

	unified := llms.NewAPIErrorFromHTTP(llms.ProviderOpenAI, llms.HTTPAPIError{
		StatusCode: apiErr.StatusCode,
		Message:    apiErr.Message,
		Type:       apiErr.Type,
		Code:       apiErr.Code,
		Param:      apiErr.Param,
	})
	if !errors.Is(unified, llms.ErrModelNotFound) {
		t.Fatalf("errors.Is(unified, ErrModelNotFound) = false for %#v", unified)
	}
}

func TestClient_DoJSON_NoBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("expected empty body, got %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	var resp map[string]string

	err := client.DoJSON(context.Background(), Request{
		Method: http.MethodGet,
		URL:    server.URL,
	}, &resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %s", resp["status"])
	}
}

func TestClient_DoRawWithHeaders_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test-Header", "present")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	body, headers, err := client.DoRawWithHeaders(context.Background(), Request{
		Method: http.MethodGet,
		URL:    server.URL,
	})
	if err != nil {
		t.Fatalf("DoRawWithHeaders: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q, want JSON response", string(body))
	}
	if headers.Get("X-Test-Header") != "present" {
		t.Errorf("X-Test-Header = %q, want present", headers.Get("X-Test-Header"))
	}
}

func TestClient_DoRawWithHeaders_ResponseTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(maxResponseSize+1, 10))
	}))
	defer server.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	_, _, err := client.DoRawWithHeaders(context.Background(), Request{
		Method: http.MethodGet,
		URL:    server.URL,
	})
	if err == nil {
		t.Fatal("expected over-limit response error")
	}
	if !strings.Contains(err.Error(), "response exceeds maximum size") {
		t.Fatalf("error = %q, want maximum size error", err.Error())
	}
}

func TestClient_DoStream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected Accept=text/event-stream, got %s", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: hello\n\n"))
	}))
	defer server.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))

	body, err := client.DoStream(context.Background(), Request{
		Method: http.MethodPost,
		URL:    server.URL,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = body.Close() }()

	data, _ := io.ReadAll(body)
	if string(data) != "data: hello\n\n" {
		t.Errorf("expected 'data: hello\\n\\n', got '%s'", string(data))
	}
}

func TestClient_DoStream_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Unauthorized",
			},
		})
	}))
	defer server.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))

	_, err := client.DoStream(context.Background(), Request{
		Method: http.MethodPost,
		URL:    server.URL,
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClient_DoStream_OutlivesUnaryTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected ResponseWriter to support flushing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: start\n\n"))
		flusher.Flush()
		time.Sleep(75 * time.Millisecond)
		_, _ = w.Write([]byte("data: done\n\n"))
	}))
	defer server.Close()

	client := NewClient(
		WithTimeout(20*time.Millisecond),
		WithAllowPrivateIPs(true),
		WithAllowHTTP(true),
	)

	body, err := client.DoStream(context.Background(), Request{
		Method: http.MethodGet,
		URL:    server.URL,
	})
	if err != nil {
		t.Fatalf("DoStream returned error: %v", err)
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("expected stream body read to outlive unary timeout, got %v", err)
	}
	if !strings.Contains(string(data), "data: done") {
		t.Fatalf("stream data = %q, want final chunk", string(data))
	}
}

func TestClient_DoRawKeepsUnaryTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(75 * time.Millisecond)
		_, _ = w.Write([]byte("too late"))
	}))
	defer server.Close()

	client := NewClient(
		WithTimeout(20*time.Millisecond),
		WithAllowPrivateIPs(true),
		WithAllowHTTP(true),
	)

	_, err := client.DoRaw(context.Background(), Request{
		Method: http.MethodGet,
		URL:    server.URL,
	})
	if err == nil {
		t.Fatal("expected DoRaw to keep unary client timeout")
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("too late"))
	}))
	defer server.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := client.DoJSON(ctx, Request{
		Method: http.MethodGet,
		URL:    server.URL,
	}, nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		err      *APIError
		expected string
	}{
		{
			err:      &APIError{StatusCode: 400, Message: "Bad Request"},
			expected: "API error (status 400): Bad Request",
		},
		{
			err:      &APIError{StatusCode: 401, Message: "Unauthorized", Type: "auth_error"},
			expected: "API error (status 401, type auth_error): Unauthorized",
		},
		{
			err: &APIError{
				StatusCode:    400,
				Message:       "Bad Request",
				RequestMethod: http.MethodGet,
				RequestURL:    "https://user:secret@example.com/v1/chat?api_key=secret&prompt=test#frag",
			},
			expected: "API error (status 400): Bad Request [GET https://example.com/v1/chat]",
		},
	}

	for _, tc := range tests {
		result := tc.err.Error()
		if result != tc.expected {
			t.Errorf("expected '%s', got '%s'", tc.expected, result)
		}
	}
}
