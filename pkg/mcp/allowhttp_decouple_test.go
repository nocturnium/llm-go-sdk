package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestAllowHTTPIndependentOfAllowPrivateIPs asserts that plain-HTTP access is an
// allowance independent of private-IP access — matching the llms.Config AllowHTTP
// decoupling. Enabling WithAllowPrivateIPs must NOT implicitly permit plain HTTP;
// reaching an http:// MCP server requires BOTH WithAllowPrivateIPs(true) and
// WithAllowHTTP(true).
//
// The first subtest is a load-bearing regression guard: before the decoupling the
// MCP client's allowHTTP defaulted to following allowPrivateIPs, so with private
// IPs allowed the plain-http request passed the scheme check and failed only on
// connection — making the "HTTP not allowed" assertion fail on the pre-fix code.
func TestAllowHTTPIndependentOfAllowPrivateIPs(t *testing.T) {
	// A closed loopback port over plain HTTP. The scheme check runs before the
	// private-IP check, so each subtest's error pinpoints which gate fired; the
	// port is closed so the "both allowed" case fails fast on dial.
	const httpURL = "http://127.0.0.1:1/mcp"

	newCtx := func(t *testing.T) context.Context {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(cancel)
		return ctx
	}

	t.Run("private_ips_allowed_but_http_not_rejects_on_scheme", func(t *testing.T) {
		_, err := NewHTTPClient(newCtx(t), httpURL, WithAllowPrivateIPs(true))
		if err == nil {
			t.Fatal("expected plain http to be rejected without WithAllowHTTP, got nil")
		}
		if !strings.Contains(err.Error(), "HTTP not allowed") {
			t.Fatalf("plain HTTP must be rejected on the scheme check independently of AllowPrivateIPs; got: %v", err)
		}
	})

	t.Run("both_allowed_passes_validation", func(t *testing.T) {
		// With both flags the scheme and IP checks pass; the handshake then fails
		// only because nothing is listening on the closed port — not a scheme
		// rejection. This guards against the decouple over-rejecting local http.
		_, err := NewHTTPClient(newCtx(t), httpURL, WithAllowPrivateIPs(true), WithAllowHTTP(true))
		if err == nil {
			t.Fatal("expected a connection error against a closed port, got nil")
		}
		if strings.Contains(err.Error(), "HTTP not allowed") {
			t.Fatalf("plain HTTP must be permitted when WithAllowHTTP(true) is set; got scheme rejection: %v", err)
		}
	})

	t.Run("http_allowed_but_private_ips_not_rejects_on_ip", func(t *testing.T) {
		// WithAllowHTTP(true) alone must not grant private-IP access: the scheme
		// passes, but the loopback address is still rejected by SSRF filtering.
		_, err := NewHTTPClient(newCtx(t), httpURL, WithAllowHTTP(true))
		if err == nil {
			t.Fatal("expected private-IP rejection without WithAllowPrivateIPs, got nil")
		}
		if strings.Contains(err.Error(), "HTTP not allowed") {
			t.Fatalf("expected an SSRF/private-IP rejection, not a scheme rejection; got: %v", err)
		}
	})
}
