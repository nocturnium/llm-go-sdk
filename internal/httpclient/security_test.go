package httpclient

import (
	"strings"
	"testing"
)

func TestValidateURL_ValidURLs(t *testing.T) {
	opts := DefaultURLValidationOptions()

	validURLs := []string{
		"https://api.openai.com/v1/chat",
		"https://api.anthropic.com/v1/messages",
		"https://example.com/api",
		"https://sub.example.com/api",
	}

	for _, url := range validURLs {
		if err := ValidateURL(url, opts); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", url, err)
		}
	}
}

func TestValidateURL_InvalidURLs(t *testing.T) {
	opts := DefaultURLValidationOptions()

	tests := []struct {
		url      string
		contains string
	}{
		{"", "cannot be empty"},
		{"not-a-url", "must have a scheme"},
		{"ftp://example.com", "unsupported URL scheme"},
		{"http://example.com", "must use HTTPS"},
		{"https://", "must have a host"},
		{"https://localhost/api", "localhost not allowed"},
		{"https://127.0.0.1/api", "loopback"},
		{"https://192.168.1.1/api", "private IP"},
		{"https://10.0.0.1/api", "private IP"},
		{"https://172.16.0.1/api", "private IP"},
		{"https://[::1]/api", "loopback"},
		{"https://server.local/api", "internal hostname"},
		{"https://server.internal/api", "internal hostname"},
	}

	for _, tc := range tests {
		err := ValidateURL(tc.url, opts)
		if err == nil {
			t.Errorf("expected %q to be invalid", tc.url)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.contains)) {
			t.Errorf("expected error for %q to contain %q, got: %v", tc.url, tc.contains, err)
		}
	}
}

func TestValidateURL_AllowHTTP(t *testing.T) {
	opts := &URLValidationOptions{
		AllowHTTP:       true,
		AllowPrivateIPs: false,
	}

	if err := ValidateURL("http://example.com/api", opts); err != nil {
		t.Errorf("expected HTTP to be allowed, got error: %v", err)
	}
}

func TestValidateURL_AllowPrivateIPs(t *testing.T) {
	opts := &URLValidationOptions{
		AllowPrivateIPs: true,
	}

	privateURLs := []string{
		"https://localhost/api",
		"https://127.0.0.1/api",
		"https://192.168.1.1/api",
		"https://10.0.0.1/api",
	}

	for _, url := range privateURLs {
		if err := ValidateURL(url, opts); err != nil {
			t.Errorf("expected %q to be valid with AllowPrivateIPs, got error: %v", url, err)
		}
	}
}

func TestValidateURL_AllowedHosts(t *testing.T) {
	opts := &URLValidationOptions{
		AllowedHosts: []string{"api.openai.com", "api.anthropic.com"},
	}

	t.Run("allowed host", func(t *testing.T) {
		if err := ValidateURL("https://api.openai.com/v1", opts); err != nil {
			t.Errorf("expected allowed host to pass, got error: %v", err)
		}
	})

	t.Run("subdomain of allowed host", func(t *testing.T) {
		if err := ValidateURL("https://beta.api.openai.com/v1", opts); err != nil {
			t.Errorf("expected subdomain to pass, got error: %v", err)
		}
	})

	t.Run("disallowed host", func(t *testing.T) {
		err := ValidateURL("https://example.com/api", opts)
		if err == nil {
			t.Error("expected disallowed host to fail")
		}
		if !strings.Contains(err.Error(), "not in allowed list") {
			t.Errorf("expected 'not in allowed list' error, got: %v", err)
		}
	})
}

func TestValidateURL_CustomPorts(t *testing.T) {
	t.Run("allowed by default", func(t *testing.T) {
		opts := DefaultURLValidationOptions()
		if err := ValidateURL("https://example.com:8443/api", opts); err != nil {
			t.Errorf("expected custom port to be allowed by default, got error: %v", err)
		}
	})

	t.Run("disallowed when configured", func(t *testing.T) {
		opts := &URLValidationOptions{
			AllowCustomPorts: false,
		}
		err := ValidateURL("https://example.com:8443/api", opts)
		if err == nil {
			t.Error("expected custom port to be disallowed")
		}
		if !strings.Contains(err.Error(), "custom ports not allowed") {
			t.Errorf("expected 'custom ports' error, got: %v", err)
		}
	})

	t.Run("standard port allowed", func(t *testing.T) {
		opts := &URLValidationOptions{
			AllowCustomPorts: false,
		}
		if err := ValidateURL("https://example.com:443/api", opts); err != nil {
			t.Errorf("expected standard port to be allowed, got error: %v", err)
		}
	})
}

func TestValidateURL_StrictOptions(t *testing.T) {
	opts := StrictURLValidationOptions()

	t.Run("known API hosts allowed", func(t *testing.T) {
		for _, host := range DefaultAllowedHosts {
			url := "https://" + host + "/api"
			if err := ValidateURL(url, opts); err != nil {
				t.Errorf("expected %q to be allowed in strict mode, got error: %v", url, err)
			}
		}
	})

	t.Run("unknown hosts blocked", func(t *testing.T) {
		err := ValidateURL("https://unknown-api.com/v1", opts)
		if err == nil {
			t.Error("expected unknown host to be blocked in strict mode")
		}
	})

	t.Run("custom ports blocked", func(t *testing.T) {
		err := ValidateURL("https://api.openai.com:8443/v1", opts)
		if err == nil {
			t.Error("expected custom port to be blocked in strict mode")
		}
	})
}

func TestValidateURL_NilOptions(t *testing.T) {
	// Should use default options when nil is passed
	if err := ValidateURL("https://example.com/api", nil); err != nil {
		t.Errorf("expected valid URL with nil options, got error: %v", err)
	}
}

func TestValidateURL_CarrierGradeNAT(t *testing.T) {
	opts := DefaultURLValidationOptions()

	// 100.64.0.0/10 range (carrier-grade NAT)
	cgnatIPs := []string{
		"https://100.64.0.1/api",
		"https://100.100.100.100/api",
		"https://100.127.255.255/api",
	}

	for _, url := range cgnatIPs {
		err := ValidateURL(url, opts)
		if err == nil {
			t.Errorf("expected CGNAT IP %q to be blocked", url)
		}
	}
}

func TestValidateURL_ObfuscatedIPv4Literals(t *testing.T) {
	opts := DefaultURLValidationOptions()

	tests := []string{
		"https://2130706433/api",
		"https://0177.0.0.1/api",
		"https://0x7f.0.0.1/api",
		"https://0x7f000001/api",
	}

	for _, url := range tests {
		err := ValidateURL(url, opts)
		if err == nil {
			t.Errorf("expected obfuscated loopback URL %q to be blocked", url)
		}
	}
}

func TestSanitizeModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"gpt-4", "gpt-4"},
		{"claude-sonnet-4-20250514", "claude-sonnet-4-20250514"},
		{"../../../etc/passwd", "---etc-passwd"},
		{"model/v1", "model-v1"},
		{"model\\v1", "model-v1"},
		{"model\x00name", "modelname"},
		{"meta-llama/Llama-3.3-70B", "meta-llama-Llama-3.3-70B"},
	}

	for _, tc := range tests {
		result := SanitizeModelName(tc.input)
		if result != tc.expected {
			t.Errorf("SanitizeModelName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestDefaultAllowedHosts(t *testing.T) {
	expectedHosts := []string{
		"api.openai.com",
		"api.anthropic.com",
		"generativelanguage.googleapis.com",
		"api.together.xyz",
		"api.featherless.ai",
	}

	if len(DefaultAllowedHosts) != len(expectedHosts) {
		t.Errorf("expected %d default hosts, got %d", len(expectedHosts), len(DefaultAllowedHosts))
	}

	for _, host := range expectedHosts {
		found := false
		for _, h := range DefaultAllowedHosts {
			if h == host {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in DefaultAllowedHosts", host)
		}
	}
}
