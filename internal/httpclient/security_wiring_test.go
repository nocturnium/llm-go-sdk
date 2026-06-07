package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubTransport records the last request and returns a canned 200 response.
type stubTransport struct {
	lastURL string
	resp    *http.Response
	err     error
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.lastURL = req.URL.String()
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// TestClientValidation_BlocksSSRFByDefault proves the request path rejects
// private/loopback IPs and the cloud metadata endpoint by default, covering
// DoJSON, DoRaw and DoStream through one central choke point.
func TestClientValidation_BlocksSSRFByDefault(t *testing.T) {
	st := &stubTransport{}
	c := NewClient(WithHTTPClient(&http.Client{Transport: st}))

	blocked := []string{
		"http://169.254.169.254/latest/meta-data",
		"https://169.254.169.254/latest/meta-data",
		"http://10.0.0.5/v1/chat",
		"https://10.0.0.5/v1/chat",
		"https://172.16.0.1/v1",
		"https://192.168.1.10/v1",
		"https://127.0.0.1/v1",
		"https://localhost/v1",
	}

	for _, u := range blocked {
		t.Run(u, func(t *testing.T) {
			// DoJSON
			if err := c.DoJSON(context.Background(), Request{Method: http.MethodGet, URL: u}, nil); err == nil {
				t.Errorf("DoJSON(%q): expected validation error, got nil", u)
			} else if !strings.Contains(err.Error(), "URL validation failed") {
				t.Errorf("DoJSON(%q): expected validation error, got %v", u, err)
			}
			// DoRaw
			if _, err := c.DoRaw(context.Background(), Request{Method: http.MethodGet, URL: u}); err == nil {
				t.Errorf("DoRaw(%q): expected validation error, got nil", u)
			}
			// DoStream
			if _, err := c.DoStream(context.Background(), Request{Method: http.MethodGet, URL: u}); err == nil {
				t.Errorf("DoStream(%q): expected validation error, got nil", u)
			}
		})
	}

	if st.lastURL != "" {
		t.Fatalf("transport should never have been reached, but saw %q", st.lastURL)
	}
}

// TestClientValidation_AllowsPublicHTTPS proves a normal public HTTPS endpoint
// passes validation and the request actually reaches the transport.
func TestClientValidation_AllowsPublicHTTPS(t *testing.T) {
	st := &stubTransport{}
	c := NewClient(WithHTTPClient(&http.Client{Transport: st}))

	const u = "https://api.openai.com/v1/chat/completions"
	if err := c.DoJSON(context.Background(), Request{Method: http.MethodGet, URL: u}, nil); err != nil {
		t.Fatalf("DoJSON(%q): unexpected error: %v", u, err)
	}
	if st.lastURL != u {
		t.Fatalf("expected transport to be reached with %q, got %q", u, st.lastURL)
	}
}

// TestClientValidation_OptOutAllowsPrivate proves WithAllowPrivateIPs(true)+
// WithAllowHTTP(true) let a private/http endpoint through to the transport.
func TestClientValidation_OptOutAllowsPrivate(t *testing.T) {
	st := &stubTransport{}
	c := NewClient(
		WithHTTPClient(&http.Client{Transport: st}),
		WithAllowPrivateIPs(true),
		WithAllowHTTP(true),
	)

	const u = "http://10.0.0.5/v1/chat"
	if err := c.DoJSON(context.Background(), Request{Method: http.MethodGet, URL: u}, nil); err != nil {
		t.Fatalf("DoJSON(%q) with opt-out: unexpected error: %v", u, err)
	}
	if st.lastURL != u {
		t.Fatalf("expected transport to be reached with %q, got %q", u, st.lastURL)
	}
}

// TestClientValidation_AllowsLocalhostWhenEnabled proves localhost+http (the
// shape used by local providers) passes when private/http access is enabled.
func TestClientValidation_AllowsLocalhostWhenEnabled(t *testing.T) {
	st := &stubTransport{}
	c := NewClient(
		WithHTTPClient(&http.Client{Transport: st}),
		WithAllowPrivateIPs(true),
		WithAllowHTTP(true),
	)

	const u = "http://localhost:11434/v1/chat/completions"
	if err := c.DoJSON(context.Background(), Request{Method: http.MethodGet, URL: u}, nil); err != nil {
		t.Fatalf("DoJSON(%q): unexpected error: %v", u, err)
	}
	if st.lastURL != u {
		t.Fatalf("expected transport to be reached with %q, got %q", u, st.lastURL)
	}
}

// TestCheckRedirect_BlocksPrivateRedirect proves the redirect policy installed
// on the underlying *http.Client re-validates each hop and blocks a redirect to
// a private IP (classic SSRF-via-redirect).
func TestCheckRedirect_BlocksPrivateRedirect(t *testing.T) {
	c := NewClient()
	cr := c.httpClient.CheckRedirect
	if cr == nil {
		t.Fatal("expected CheckRedirect to be installed")
	}

	// A redirect hop targeting the metadata endpoint must be rejected.
	req, _ := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data", http.NoBody)
	if err := cr(req, nil); err == nil {
		t.Fatal("expected redirect to private IP to be blocked, got nil")
	} else if !strings.Contains(err.Error(), "redirect blocked") {
		t.Fatalf("expected redirect-blocked error, got %v", err)
	}

	// A redirect to a public host must be allowed.
	pub, _ := http.NewRequest(http.MethodGet, "https://api.openai.com/v1", http.NoBody)
	if err := cr(pub, nil); err != nil {
		t.Fatalf("expected public redirect to be allowed, got %v", err)
	}
}

// TestCheckRedirect_RevalidatesWithOptOut proves the redirect policy honors the
// client's opt-out flags (private redirect allowed when AllowPrivateIPs=true).
func TestCheckRedirect_RevalidatesWithOptOut(t *testing.T) {
	c := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	cr := c.httpClient.CheckRedirect
	if cr == nil {
		t.Fatal("expected CheckRedirect to be installed")
	}
	req, _ := http.NewRequest(http.MethodGet, "http://10.0.0.5/internal", http.NoBody)
	if err := cr(req, nil); err != nil {
		t.Fatalf("expected private redirect to be allowed with opt-out, got %v", err)
	}
}

// TestCheckRedirect_BoundsHops proves redirects are capped at maxRedirects.
func TestCheckRedirect_BoundsHops(t *testing.T) {
	c := NewClient()
	cr := c.httpClient.CheckRedirect

	via := make([]*http.Request, maxRedirects)
	req, _ := http.NewRequest(http.MethodGet, "https://api.openai.com/v1", http.NoBody)
	if err := cr(req, via); err == nil {
		t.Fatal("expected redirect cap to trigger, got nil")
	} else if !strings.Contains(err.Error(), "stopped after") {
		t.Fatalf("expected redirect-cap error, got %v", err)
	}
}

// TestCheckRedirect_LiveServerBlocked is an end-to-end check: a real test
// server (reached because private IPs are allowed for this client) issues a
// 302 to the cloud metadata endpoint, and the client's redirect policy blocks
// the hop instead of following it.
func TestCheckRedirect_LiveServerBlocked(t *testing.T) {
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to a target the policy always rejects regardless of the
		// AllowPrivateIPs/AllowHTTP flags: an unsupported (non-HTTP/S) scheme.
		http.Redirect(w, r, "ftp://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer redirector.Close()

	// Allow the initial loopback hop to the httptest server so the redirect
	// policy is exercised on the second hop.
	c := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	err := c.DoJSON(context.Background(), Request{Method: http.MethodGet, URL: redirector.URL}, nil)
	if err == nil {
		t.Fatal("expected redirect to internal host to be blocked, got nil")
	}
	if !strings.Contains(err.Error(), "redirect blocked") {
		t.Fatalf("expected redirect-blocked error, got %v", err)
	}
}
