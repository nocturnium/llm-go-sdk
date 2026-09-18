package httpclient

import "testing"

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
