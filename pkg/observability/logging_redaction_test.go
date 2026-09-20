package observability

import (
	"strings"
	"testing"
)

// TestJSONLogger_RedactionClearsMetadata pins that the default redaction
// covers the caller-supplied maps too. Metadata and RequestParameters were
// left in place and marshaled whole, so a key stored there was emitted
// verbatim by the redacting default.
func TestJSONLogger_RedactionClearsMetadata(t *testing.T) {
	var written []byte
	logger := NewJSONLogger(func(b []byte) error {
		written = append(written, b...)
		return nil
	})

	logger.LogResponse(t.Context(), &LogEntry{
		Provider:          "openai",
		Content:           "the completion",
		Metadata:          map[string]any{"api_key": "sk-live-DEADBEEF"},
		RequestParameters: map[string]any{"user_prompt": "SECRET-PROMPT-TEXT"},
	})

	for _, secret := range []string{"sk-live-DEADBEEF", "SECRET-PROMPT-TEXT", "the completion"} {
		if strings.Contains(string(written), secret) {
			t.Errorf("redacted entry leaks %q: %s", secret, written)
		}
	}
	if !strings.Contains(string(written), "openai") {
		t.Errorf("redaction removed the provider too: %s", written)
	}
}
