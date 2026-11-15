package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestParseAPIErrorBody_NumericCode pins that a numeric "code" still yields the
// message. Google sends the HTTP status as a number there, which failed the
// whole nested decode while the field was typed as a string.
func TestParseAPIErrorBody_NumericCode(t *testing.T) {
	body := []byte(`{"error":{"message":"API key not valid","status":"INVALID_ARGUMENT","code":400}}`)

	parsed, ok := parseAPIErrorBody(body)
	if !ok {
		t.Fatal("parseAPIErrorBody reported no structured error")
	}
	if parsed.Message != "API key not valid" {
		t.Errorf("Message = %q, want %q", parsed.Message, "API key not valid")
	}
	if parsed.Code != "400" {
		t.Errorf("Code = %q, want %q", parsed.Code, "400")
	}
}

// TestParseAPIErrorBody_StringCode keeps the OpenAI-shaped string code working.
func TestParseAPIErrorBody_StringCode(t *testing.T) {
	body := []byte(`{"error":{"message":"Incorrect API key","type":"invalid_request_error","code":"invalid_api_key"}}`)

	parsed, ok := parseAPIErrorBody(body)
	if !ok {
		t.Fatal("parseAPIErrorBody reported no structured error")
	}
	if parsed.Code != "invalid_api_key" {
		t.Errorf("Code = %q, want %q", parsed.Code, "invalid_api_key")
	}
	if parsed.Type != "invalid_request_error" {
		t.Errorf("Type = %q, want %q", parsed.Type, "invalid_request_error")
	}
}

// TestParseAPIErrorBody_CloudflareEnvelope reads the first entry of the
// Cloudflare v4 {"errors":[...]} envelope, observed live from Workers AI.
func TestParseAPIErrorBody_CloudflareEnvelope(t *testing.T) {
	body := []byte(`{"errors":[{"message":"Insufficient balance; add money to your gateway or use BYOK","code":2021}],"success":false,"result":{},"messages":[]}`)

	parsed, ok := parseAPIErrorBody(body)
	if !ok {
		t.Fatal("parseAPIErrorBody reported no structured error")
	}
	if parsed.Code != "2021" {
		t.Errorf("Code = %q, want %q", parsed.Code, "2021")
	}
	if parsed.Message != "Insufficient balance; add money to your gateway or use BYOK" {
		t.Errorf("Message = %q", parsed.Message)
	}
}

// TestParseAPIErrorBody_EmptyErrorsArray keeps a success-shaped envelope from
// being read as an error.
func TestParseAPIErrorBody_EmptyErrorsArray(t *testing.T) {
	if _, ok := parseAPIErrorBody([]byte(`{"errors":[],"success":true}`)); ok {
		t.Fatal("empty errors array parsed as an error")
	}
}

func TestWithErrorCodeMapper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"errors":[{"code":2021,"message":"Insufficient balance"}],"success":false}`))
	}))
	defer srv.Close()
	var seen [3]string
	c := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true), WithErrorCodeMapper(func(status int, code, message string) string {
		seen = [3]string{fmt.Sprint(status), code, message}
		return "insufficient_quota"
	}))
	err := c.DoJSON(context.Background(), Request{Method: http.MethodPost, URL: srv.URL, Body: map[string]any{}}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "insufficient_quota" {
		t.Fatalf("err = %v", err)
	}
	if seen != [3]string{"402", "2021", "Insufficient balance"} {
		t.Fatalf("mapper saw %v", seen)
	}
}

// TestWithErrorCodeMapper_SkipsLocalDiagnostics: an unreadable or oversized
// body produces a local diagnostic, not a provider error, so the mapper must
// not relabel it.
func TestWithErrorCodeMapper_SkipsLocalDiagnostics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "99999999")
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()
	called := false
	c := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true), WithErrorCodeMapper(func(int, string, string) string {
		called = true
		return "insufficient_quota"
	}))
	err := c.DoJSON(context.Background(), Request{Method: http.MethodPost, URL: srv.URL, Body: map[string]any{}}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 402 || apiErr.Code != "" || called {
		t.Fatalf("err = %v, mapper called = %v", err, called)
	}
}
