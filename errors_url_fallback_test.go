package llms

import (
	"strings"
	"testing"
)

// TestAPIError_ErrorRedactsMalformedURL pins the parse-failure path. A URL
// that url.Parse rejects (a bad percent-escape from a misconfigured base URL)
// took a fallback that cut only the query, leaving the password in place.
func TestAPIError_ErrorRedactsMalformedURL(t *testing.T) {
	err := &APIError{
		StatusCode: 500,
		Message:    "boom",
		RequestURL: "https://key:secret@host.example.com/p?x=%zz",
	}

	got := err.Error()
	for _, leaked := range []string{"secret", "key:", "%zz"} {
		if strings.Contains(got, leaked) {
			t.Errorf("error string leaks %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "host.example.com/p") {
		t.Errorf("error string lost the endpoint: %s", got)
	}
}
