package openaicompat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateResponse_FailedBodyIsAnError pins that a 200 carrying status
// "failed" is reported. The body used to come back as a success, so a direct
// caller read an empty response with a nil error.
func TestCreateResponse_FailedBodyIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"failed","error":{"message":"model overloaded","code":"server_error"}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "test", AllowHTTP: true, AllowPrivateIPs: true})
	_, err := client.CreateResponse(context.Background(), &ResponsesRequest{Model: "m"})
	if err == nil {
		t.Fatal("a failed Responses body was returned as a success")
	}
	if !strings.Contains(err.Error(), "model overloaded") {
		t.Errorf("error = %v, want the provider's message", err)
	}
}
