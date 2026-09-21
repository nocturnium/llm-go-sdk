package llms

import (
	"strings"
	"testing"
)

// TestAPIError_ErrorRedactsCredentials pins that the error string carries no
// credentials. It appended RequestURL raw, so a key in the query or userinfo
// reached every log line the caller wrote, undoing the sanitizing the HTTP
// layer had already done.
func TestAPIError_ErrorRedactsCredentials(t *testing.T) {
	err := &APIError{
		StatusCode:    401,
		Message:       "bad key",
		RequestMethod: "POST",
		RequestURL:    "https://svc:hunter2@api.example.com/v1/chat?api-key=sk-SECRET123&x=1#frag",
	}

	got := err.Error()
	for _, secret := range []string{"hunter2", "sk-SECRET123", "api-key", "frag"} {
		if strings.Contains(got, secret) {
			t.Errorf("error string leaks %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "api.example.com/v1/chat") {
		t.Errorf("error string lost the endpoint: %s", got)
	}
}
