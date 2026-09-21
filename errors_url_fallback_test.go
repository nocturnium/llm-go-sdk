package llms

import (
	"net/url"
	"strings"
	"testing"
)

// TestAPIError_ErrorRedactsMalformedURL pins the parse-failure path. A URL
// that url.Parse rejects (a bad percent-escape from a misconfigured base URL)
// took a fallback that cut only the query, leaving the password in place.
func TestAPIError_ErrorRedactsMalformedURL(t *testing.T) {
	// A control character in the host is what url.Parse rejects; an
	// invalid percent-escape in the query parses fine and would take the
	// structured path instead of the fallback this test is about.
	// Assembled from parts so staticcheck does not evaluate it as a literal
	// URL: the control character is the point, since that is what url.Parse
	// rejects and what sends this through the fallback.
	host := "host.example.com"
	raw := "https://key:pass@word:secret@" + host + string(rune(0x7f)) + "/p?x=1"
	if _, parseErr := url.Parse(raw); parseErr == nil {
		t.Fatalf("test URL parses cleanly, so it does not reach the fallback")
	}
	err := &APIError{StatusCode: 500, Message: "boom", RequestURL: raw}

	got := err.Error()
	for _, leaked := range []string{"secret", "key:", "pass@word", "x=1"} {
		if strings.Contains(got, leaked) {
			t.Errorf("error string leaks %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "host.example.com") {
		t.Errorf("error string lost the endpoint: %s", got)
	}
}
