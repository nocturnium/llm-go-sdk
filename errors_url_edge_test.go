package llms

import (
	"strings"
	"testing"
)

// TestAPIError_ErrorRedactsAwkwardCredentialForms pins the shapes a
// misconfigured base URL produces: a credential carrying a reserved character,
// and an opaque URL whose credential never reaches url.URL.User.
func TestAPIError_ErrorRedactsAwkwardCredentialForms(t *testing.T) {
	cases := map[string]string{
		"slash in the password": "https://user:se/cret@host.example.com/v1",
		"opaque form":           "https:KEY-SECRET@host.example.com/v1",
		"hash in the password":  "https://user:se#cret@host.example.com/v1",
	}

	for name, raw := range cases {
		got := (&APIError{StatusCode: 500, Message: "boom", RequestURL: raw}).Error()
		for _, leaked := range []string{"secret", "cret", "KEY-SECRET"} {
			if strings.Contains(got, leaked) {
				t.Errorf("%s: error string leaks %q: %s", name, leaked, got)
			}
		}
	}
}
