package httpclient

import (
	"net/http"
	"testing"
)

// TestCheckRedirect_StripsCredentialsOnOriginChange pins that a redirect which
// keeps the hostname but changes the scheme or the port is treated as a new
// origin. Comparing hostnames alone carried the credential headers onto a
// cleartext hop or a different port on the same name.
func TestCheckRedirect_StripsCredentialsOnOriginChange(t *testing.T) {
	c := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	cr := c.httpClient.CheckRedirect
	if cr == nil {
		t.Fatal("expected CheckRedirect to be installed")
	}

	original, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1/chat", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	for _, target := range []string{
		"http://api.example.com/v1/chat",
		"https://api.example.com:8443/v1/chat",
	} {
		req, err := http.NewRequest(http.MethodGet, target, http.NoBody)
		if err != nil {
			t.Fatalf("new request %s: %v", target, err)
		}
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("x-api-key", "secret")

		if err := cr(req, []*http.Request{original}); err != nil {
			t.Fatalf("%s: CheckRedirect returned %v", target, err)
		}
		if got := req.Header.Get("Authorization"); got != "" {
			t.Errorf("%s kept Authorization %q", target, got)
		}
		if got := req.Header.Get("x-api-key"); got != "" {
			t.Errorf("%s kept x-api-key %q", target, got)
		}
	}
}

// TestCheckRedirect_KeepsCredentialsOnSameOrigin pins the other direction: an
// explicit default port is the same origin as an omitted one.
func TestCheckRedirect_KeepsCredentialsOnSameOrigin(t *testing.T) {
	c := NewClient()
	cr := c.httpClient.CheckRedirect

	original, _ := http.NewRequest(http.MethodGet, "https://api.example.com/v1/chat", http.NoBody)
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com:443/v1/chat/completions", http.NoBody)
	req.Header.Set("Authorization", "Bearer secret")

	if err := cr(req, []*http.Request{original}); err != nil {
		t.Fatalf("CheckRedirect returned %v", err)
	}
	if req.Header.Get("Authorization") == "" {
		t.Error("same-origin redirect lost its Authorization header")
	}
}
