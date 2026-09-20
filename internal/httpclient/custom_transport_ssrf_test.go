package httpclient

import (
	"net"
	"net/http"
	"testing"
)

// wrappingRoundTripper stands in for an instrumented transport such as
// otelhttp, which is not an *http.Transport and so cannot take a dial guard.
type wrappingRoundTripper struct{ base http.RoundTripper }

func (rt *wrappingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt.base.RoundTrip(req)
}

// TestCustomRoundTripper_FallsBackToResolving pins that a client whose
// transport cannot take the dial guard switches on the resolving check
// instead. Without it such a client keeps only the non-resolving name check,
// so a public name pointing at a private address is dialed unchecked.
func TestCustomRoundTripper_FallsBackToResolving(t *testing.T) {
	c := NewClient(WithHTTPClient(&http.Client{Transport: &wrappingRoundTripper{base: http.DefaultTransport}}))
	if !c.resolveBeforeRequest {
		t.Fatal("client did not fall back to resolving before the request")
	}

	plain := NewClient()
	if plain.resolveBeforeRequest {
		t.Error("a client with an injectable transport should use the dial guard, not the resolver")
	}
}

// TestValidateURL_RefusesPrivateResolution pins the resolving check itself
// against a name the local resolver answers with a loopback address.
func TestValidateURL_RefusesPrivateResolution(t *testing.T) {
	addrs, err := net.DefaultResolver.LookupIPAddr(t.Context(), "localhost")
	if err != nil || len(addrs) == 0 {
		t.Skip("localhost does not resolve on this host")
	}

	c := NewClient(WithHTTPClient(&http.Client{Transport: &wrappingRoundTripper{base: http.DefaultTransport}}), WithAllowHTTP(true))
	// A name carrying no private-looking string, aliased to localhost by the
	// resolver, is what the dial guard would have caught.
	if err := c.validateURL("http://localhost:8080/v1"); err == nil {
		t.Error("a loopback host passed validation")
	}
}
