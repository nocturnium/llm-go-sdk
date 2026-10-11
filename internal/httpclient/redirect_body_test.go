package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A 307/308 redirect preserves the request body, so following a cross-origin
// redirect would replay the caller's JSON payload (prompts, user content,
// possibly credentials) to an origin the redirecting host chose. The policy
// must refuse the redirect rather than forward the body.
func TestCheckRedirect_BlocksCrossOriginBodyForwarding(t *testing.T) {
	var foreignHit bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHit = true
		_, _ = w.Write([]byte("ok"))
	}))
	defer foreign.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	err := client.DoJSON(context.Background(), Request{
		Method:  http.MethodPost,
		URL:     redirector.URL,
		Headers: map[string]string{"Authorization": "Bearer secret"},
		Body:    map[string]string{"secret": "value"},
	}, nil)
	if err == nil {
		t.Fatal("expected the cross-origin body redirect to be blocked")
	}
	if foreignHit {
		t.Fatal("the request body was forwarded to the foreign origin")
	}
}

// A same-origin 307 that only changes the path must still carry the body; only
// cross-origin redirects are refused.
func TestCheckRedirect_AllowsSameOriginBodyRedirect(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			buf := make([]byte, 1024)
			n, _ := r.Body.Read(buf)
			gotBody = string(buf[:n])
			_, _ = w.Write([]byte("ok"))
			return
		}
		http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	if err := client.DoJSON(context.Background(), Request{
		Method: http.MethodPost,
		URL:    server.URL,
		Body:   map[string]string{"secret": "value"},
	}, nil); err != nil {
		t.Fatalf("same-origin body redirect returned error: %v", err)
	}
	if gotBody != `{"secret":"value"}` {
		t.Errorf("got body %q, want the original payload", gotBody)
	}
}
