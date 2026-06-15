package httpclient

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestSSRFDialControl verifies the dial-time Control hook rejects connections to
// private/internal IPs (the case hostname checks miss: a name that resolves to a
// private address, or a redirect to one).
func TestSSRFDialControl(t *testing.T) {
	blocked := []string{
		"127.0.0.1:80",       // loopback
		"169.254.169.254:80", // cloud metadata (link-local)
		"10.0.0.5:443",       // private
		"192.168.1.1:8080",   // private
		"172.16.0.1:80",      // private
		"[::1]:80",           // IPv6 loopback
		"[fd00::1]:443",      // IPv6 ULA
	}
	for _, addr := range blocked {
		if err := ssrfDialControl("tcp", addr, nil); err == nil {
			t.Errorf("expected %s to be blocked by the SSRF dial control", addr)
		}
	}

	allowed := []string{"8.8.8.8:443", "1.1.1.1:80"}
	for _, addr := range allowed {
		if err := ssrfDialControl("tcp", addr, nil); err != nil {
			t.Errorf("expected %s to be allowed, got %v", addr, err)
		}
	}
}

// TestSSRFDialerInstalled verifies a default (secure) client gets an
// SSRF-validating transport, while WithAllowPrivateIPs leaves the transport
// untouched so local providers can dial private addresses.
func TestSSRFDialerInstalled(t *testing.T) {
	secure := NewClient()
	if tr, ok := secure.httpClient.Transport.(*http.Transport); !ok || tr.DialContext == nil {
		t.Error("expected an SSRF-validating DialContext on a default (secure) client")
	}

	relaxed := NewClient(WithAllowPrivateIPs(true))
	if relaxed.httpClient.Transport != nil {
		t.Error("expected no forced transport when private IPs are allowed")
	}
}

func TestSSRFDialerClonesDefaultTransport(t *testing.T) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("expected http.DefaultTransport to be an *http.Transport")
	}
	originalDialContext := dialContextPointer(defaultTransport.DialContext)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	_ = NewClient(WithHTTPClient(&http.Client{Transport: http.DefaultTransport}))

	if got := dialContextPointer(defaultTransport.DialContext); got != originalDialContext {
		t.Fatal("expected NewClient not to mutate http.DefaultTransport.DialContext")
	}

	resp, err := (&http.Client{Transport: http.DefaultTransport}).Get(server.URL)
	if err != nil {
		t.Fatalf("expected unrelated default-transport client to dial loopback server, got %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
}

func TestSSRFDialerClonesTransportAndPreservesDialContext(t *testing.T) {
	originalErr := errors.New("original dial")
	called := false
	base := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			called = true
			return nil, originalErr
		},
	}
	originalDialContext := dialContextPointer(base.DialContext)

	client := NewClient(WithHTTPClient(&http.Client{Transport: base}))
	if client.httpClient.Transport == base {
		t.Fatal("expected NewClient to clone caller-owned transport")
	}
	if got := dialContextPointer(base.DialContext); got != originalDialContext {
		t.Fatal("expected caller-owned transport DialContext to remain unchanged")
	}

	tr, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected cloned transport")
	}

	_, err := tr.DialContext(context.Background(), "tcp", "8.8.8.8:443")
	if !errors.Is(err, originalErr) {
		t.Fatalf("expected preserved DialContext error, got %v", err)
	}
	if !called {
		t.Fatal("expected preserved DialContext to be called for public address")
	}

	called = false
	_, err = tr.DialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected loopback address to be rejected before preserved DialContext, got %v", err)
	}
	if called {
		t.Fatal("expected preserved DialContext not to be called for blocked address")
	}
}

func dialContextPointer(fn func(context.Context, string, string) (net.Conn, error)) uintptr {
	if fn == nil {
		return 0
	}
	return reflect.ValueOf(fn).Pointer()
}
