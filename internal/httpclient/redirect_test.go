package httpclient

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCheckRedirect_StripsCredentialsOnCrossHostRedirect(t *testing.T) {
	foreignHeaders := make(chan http.Header, 1)
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHeaders <- r.Header.Clone()
		_, _ = w.Write([]byte("ok"))
	}))
	defer foreign.Close()

	foreignURL, err := url.Parse(foreign.URL)
	if err != nil {
		t.Fatalf("parse foreign URL: %v", err)
	}
	_, foreignPort, err := net.SplitHostPort(foreignURL.Host)
	if err != nil {
		t.Fatalf("split foreign host: %v", err)
	}
	foreignURL.Host = net.JoinHostPort("localhost", foreignPort)

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreignURL.String(), http.StatusFound)
	}))
	defer redirector.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	if _, err := client.DoRaw(context.Background(), Request{
		Method:  http.MethodGet,
		URL:     redirector.URL,
		Headers: redirectCredentialTestHeaders(),
	}); err != nil {
		t.Fatalf("DoRaw returned error: %v", err)
	}

	headers := <-foreignHeaders
	for _, header := range []string{"x-goog-api-key", "api-key", "x-api-key", "xi-api-key", "x-key", "Authorization"} {
		if got := headers.Get(header); got != "" {
			t.Errorf("expected %s to be stripped, got %q", header, got)
		}
	}
}

func TestCheckRedirect_RetainsCredentialsOnSameHostRedirect(t *testing.T) {
	secondHopHeaders := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			secondHopHeaders <- r.Header.Clone()
			_, _ = w.Write([]byte("ok"))
			return
		}
		http.Redirect(w, r, "/redirected?query=1", http.StatusFound)
	}))
	defer server.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	if _, err := client.DoRaw(context.Background(), Request{
		Method:  http.MethodGet,
		URL:     server.URL,
		Headers: redirectCredentialTestHeaders(),
	}); err != nil {
		t.Fatalf("DoRaw returned error: %v", err)
	}

	headers := <-secondHopHeaders
	for header, want := range redirectCredentialTestHeaders() {
		if got := headers.Get(header); got != want {
			t.Errorf("expected %s=%q, got %q", header, want, got)
		}
	}
}

func redirectCredentialTestHeaders() map[string]string {
	return map[string]string{
		"x-goog-api-key": "gemini-secret",
		"api-key":        "azure-secret",
		"x-api-key":      "anthropic-secret",
		"xi-api-key":     "elevenlabs-secret",
		"x-key":          "bfl-secret",
		"Authorization":  "Bearer openai-secret",
	}
}
