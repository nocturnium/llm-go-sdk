package httpclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestDoWithRetry_ReplaysBodyWithoutMutatingCallerRequest(t *testing.T) {
	const requestBody = `{"message":"hello"}`

	var (
		mu             sync.Mutex
		receivedBodies []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}

		mu.Lock()
		receivedBodies = append(receivedBodies, string(body))
		attempt := len(receivedBodies)
		mu.Unlock()

		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("try again"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := NewClient(
		WithRetryPolicy(&RetryPolicy{
			MaxRetries:           1,
			InitialDelay:         0,
			MaxDelay:             0,
			Multiplier:           1,
			RetryableStatusCodes: []int{http.StatusServiceUnavailable},
		}),
		WithAllowPrivateIPs(true),
		WithAllowHTTP(true),
	)

	originalBody := io.NopCloser(bytes.NewReader([]byte(requestBody)))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, originalBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(requestBody))), nil
	}
	originalGetBody := req.GetBody

	resp, err := client.doWithRetry(context.Background(), req)
	if err != nil {
		t.Fatalf("doWithRetry returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	mu.Lock()
	gotBodies := append([]string(nil), receivedBodies...)
	mu.Unlock()

	if len(gotBodies) != 2 {
		t.Fatalf("attempt count = %d, want 2", len(gotBodies))
	}
	for i, got := range gotBodies {
		if got != requestBody {
			t.Fatalf("attempt %d body = %q, want %q", i+1, got, requestBody)
		}
	}

	if req.Body != originalBody {
		t.Fatal("caller request Body was replaced")
	}
	if req.GetBody == nil {
		t.Fatal("caller request GetBody was cleared")
	}
	bodyAfterCall, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read caller request body after call: %v", err)
	}
	if string(bodyAfterCall) != requestBody {
		t.Fatalf("caller request Body after call = %q, want %q", bodyAfterCall, requestBody)
	}
	freshBody, err := originalGetBody()
	if err != nil {
		t.Fatalf("caller request GetBody after call returned error: %v", err)
	}
	defer func() { _ = freshBody.Close() }()
	freshBodyBytes, err := io.ReadAll(freshBody)
	if err != nil {
		t.Fatalf("failed to read fresh body from GetBody: %v", err)
	}
	if string(freshBodyBytes) != requestBody {
		t.Fatalf("caller request GetBody body = %q, want %q", freshBodyBytes, requestBody)
	}
}

func TestDoWithRetry_DoesNotRetryBodyWithoutGetBody(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("try again"))
	}))
	defer server.Close()

	client := NewClient(
		WithRetryPolicy(&RetryPolicy{
			MaxRetries:           1,
			InitialDelay:         0,
			MaxDelay:             0,
			Multiplier:           1,
			RetryableStatusCodes: []int{http.StatusServiceUnavailable},
		}),
		WithAllowPrivateIPs(true),
		WithAllowHTTP(true),
	)

	originalBody := io.NopCloser(bytes.NewReader([]byte("not replayable")))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, originalBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.doWithRetry(context.Background(), req)
	if err != nil {
		t.Fatalf("doWithRetry returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if req.Body != originalBody {
		t.Fatal("caller request Body was replaced")
	}
	if req.GetBody != nil {
		t.Fatal("caller request GetBody was set")
	}
}

func TestDoWithRetry_DrainsRetryableResponseBeforeClose(t *testing.T) {
	retryBody := &trackingReadCloser{reader: bytes.NewReader([]byte("retry body"))}
	rt := &sequenceRoundTripper{
		responses: []*http.Response{
			{
				StatusCode: http.StatusServiceUnavailable,
				Body:       retryBody,
				Header:     make(http.Header),
			},
			{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
			},
		},
	}
	client := NewClient(
		WithHTTPClient(&http.Client{Transport: rt}),
		WithRetryPolicy(&RetryPolicy{
			MaxRetries:           1,
			InitialDelay:         0,
			MaxDelay:             0,
			Multiplier:           1,
			RetryableStatusCodes: []int{http.StatusServiceUnavailable},
		}),
	)

	data, err := client.DoRaw(context.Background(), Request{
		Method: http.MethodGet,
		URL:    "https://example.com/v1",
	})
	if err != nil {
		t.Fatalf("DoRaw returned error: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("response body = %q, want ok", string(data))
	}
	if retryBody.readBytes != len("retry body") {
		t.Fatalf("retry body read bytes = %d, want %d", retryBody.readBytes, len("retry body"))
	}
	if !retryBody.closed {
		t.Fatal("expected retry body to be closed")
	}
	if rt.calls != 2 {
		t.Fatalf("round trip calls = %d, want 2", rt.calls)
	}
}

type sequenceRoundTripper struct {
	responses []*http.Response
	calls     int
}

func (s *sequenceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.calls >= len(s.responses) {
		return nil, io.EOF
	}
	resp := s.responses[s.calls]
	s.calls++
	resp.Request = req
	return resp, nil
}

type trackingReadCloser struct {
	reader    *bytes.Reader
	readBytes int
	closed    bool
}

func (t *trackingReadCloser) Read(p []byte) (int, error) {
	n, err := t.reader.Read(p)
	t.readBytes += n
	return n, err
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return nil
}
