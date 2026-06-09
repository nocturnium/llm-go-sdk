package httpclient

import (
	"net/http"
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
