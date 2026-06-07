package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recordingTransport returns a canned chat-completion response and records the
// URL it was asked to fetch, so tests can confirm whether a request reached the
// transport (i.e. passed SSRF validation) or was blocked before sending.
type recordingTransport struct {
	hit bool
	url string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.hit = true
	rt.url = req.URL.String()
	body, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ok"}}},
	})
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    req,
	}, nil
}

// TestSSRF_RemoteProviderBlocksPrivateByDefault proves a remote provider
// (openai) blocks requests to the cloud metadata endpoint and to RFC1918
// private IPs by default, before the request reaches the transport.
func TestSSRF_RemoteProviderBlocksPrivateByDefault(t *testing.T) {
	cases := []string{
		"http://169.254.169.254/v1",
		"https://169.254.169.254/v1",
		"http://10.0.0.5/v1",
		"https://10.0.0.5/v1",
		"https://192.168.1.10/v1",
		"https://172.16.0.1/v1",
		"https://127.0.0.1/v1",
		"https://localhost/v1",
	}
	for _, base := range cases {
		t.Run(base, func(t *testing.T) {
			rt := &recordingTransport{}
			client, err := New(
				WithAPIKey("test-key"),
				WithBaseURL(base),
				WithHTTPClient(&http.Client{Transport: rt}),
			)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = client.Call(context.Background(), "hi")
			if err == nil {
				t.Fatalf("expected SSRF validation error for %q, got nil", base)
			}
			if !strings.Contains(err.Error(), "URL validation failed") {
				t.Fatalf("expected URL validation error for %q, got %v", base, err)
			}
			if rt.hit {
				t.Fatalf("transport must not be reached for blocked URL %q (saw %q)", base, rt.url)
			}
		})
	}
}

// TestSSRF_DefaultBaseURLPasses proves openai's default https://api.openai.com
// base is not a false positive: validation passes and the request reaches the
// transport.
func TestSSRF_DefaultBaseURLPasses(t *testing.T) {
	rt := &recordingTransport{}
	client, err := New(
		WithAPIKey("test-key"),
		WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Call(context.Background(), "hi"); err != nil {
		t.Fatalf("unexpected error with default base URL: %v", err)
	}
	if !rt.hit {
		t.Fatal("expected request to reach the transport with default base URL")
	}
	if !strings.HasPrefix(rt.url, "https://api.openai.com/") {
		t.Fatalf("expected request to api.openai.com, got %q", rt.url)
	}
}

// TestSSRF_OptOutReachesPrivate proves WithAllowPrivateIPs(), WithAllowHTTP() lets a remote
// provider reach a private, plain-HTTP endpoint (opt-out works end-to-end).
func TestSSRF_OptOutReachesPrivate(t *testing.T) {
	rt := &recordingTransport{}
	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL("http://10.0.0.5/v1"),
		WithAllowPrivateIPs(), WithAllowHTTP(),
		WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Call(context.Background(), "hi"); err != nil {
		t.Fatalf("unexpected error with opt-out: %v", err)
	}
	if !rt.hit {
		t.Fatal("expected request to reach the transport with opt-out enabled")
	}
	if !strings.HasPrefix(rt.url, "http://10.0.0.5/") {
		t.Fatalf("expected request to 10.0.0.5, got %q", rt.url)
	}
}
