package testutil

import (
	"bytes"
	"net/http"
	"testing"
)

// TestCaptureRequest_RejectsMalformedBody pins that a body which does not decode
// is answered with 400. Capturing it as an empty map let assertions about the
// captured request pass on output a real provider would reject.
func TestCaptureRequest_RejectsMalformedBody(t *testing.T) {
	server := NewMockOpenAICompatibleServer()
	defer server.Close()

	resp, err := http.Post(server.URL()+"/chat/completions", "application/json", bytes.NewBufferString("{not json"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
