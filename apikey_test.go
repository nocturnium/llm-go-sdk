package llms

import (
	"os"
	"testing"
)

func TestResolveAPIKey(t *testing.T) {
	// Save and restore environment
	originalEnv := map[string]string{}
	envVars := []string{"TEST_PROVIDER_KEY", "TEST_FALLBACK_KEY", "LLM_API_KEY"}
	for _, v := range envVars {
		originalEnv[v] = os.Getenv(v)
	}
	defer func() {
		for k, v := range originalEnv {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()

	// Clear all test env vars
	for _, v := range envVars {
		_ = os.Unsetenv(v)
	}

	t.Run("explicit key takes precedence", func(t *testing.T) {
		t.Setenv("TEST_PROVIDER_KEY", "env-key")
		t.Setenv("LLM_API_KEY", "fallback-key")

		key := ResolveAPIKey("explicit-key", "TEST_PROVIDER_KEY")
		if key != "explicit-key" {
			t.Errorf("got %q, want %q", key, "explicit-key")
		}
	})

	t.Run("provider env var takes precedence over fallback", func(t *testing.T) {
		t.Setenv("TEST_PROVIDER_KEY", "provider-key")
		t.Setenv("LLM_API_KEY", "fallback-key")

		key := ResolveAPIKey("", "TEST_PROVIDER_KEY")
		if key != "provider-key" {
			t.Errorf("got %q, want %q", key, "provider-key")
		}
	})

	t.Run("checks provider env vars in order", func(t *testing.T) {
		t.Setenv("TEST_FALLBACK_KEY", "second-key")

		key := ResolveAPIKey("", "TEST_PROVIDER_KEY", "TEST_FALLBACK_KEY")
		if key != "second-key" {
			t.Errorf("got %q, want %q", key, "second-key")
		}
	})

	t.Run("falls back to LLM_API_KEY", func(t *testing.T) {
		t.Setenv("LLM_API_KEY", "fallback-key")

		key := ResolveAPIKey("", "TEST_PROVIDER_KEY")
		if key != "fallback-key" {
			t.Errorf("got %q, want %q", key, "fallback-key")
		}
	})

	t.Run("returns empty string if no key found", func(t *testing.T) {
		key := ResolveAPIKey("", "TEST_PROVIDER_KEY")
		if key != "" {
			t.Errorf("got %q, want empty string", key)
		}
	})

	t.Run("works with no provider env vars", func(t *testing.T) {
		t.Setenv("LLM_API_KEY", "fallback-key")

		key := ResolveAPIKey("")
		if key != "fallback-key" {
			t.Errorf("got %q, want %q", key, "fallback-key")
		}
	})
}

func TestRequireAPIKey(t *testing.T) {
	// Save and restore environment
	originalLLMKey := os.Getenv("LLM_API_KEY")
	originalTestKey := os.Getenv("TEST_PROVIDER_KEY")
	defer func() {
		if originalLLMKey == "" {
			t.Setenv("LLM_API_KEY", "")
		} else {
			t.Setenv("LLM_API_KEY", originalLLMKey)
		}
		if originalTestKey == "" {
			t.Setenv("TEST_PROVIDER_KEY", "")
		} else {
			t.Setenv("TEST_PROVIDER_KEY", originalTestKey)
		}
	}()

	t.Setenv("LLM_API_KEY", "")
	t.Setenv("TEST_PROVIDER_KEY", "")

	t.Run("returns key when found", func(t *testing.T) {
		key, err := RequireAPIKey("testprovider", "explicit-key", "TEST_PROVIDER_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "explicit-key" {
			t.Errorf("got %q, want %q", key, "explicit-key")
		}
	})

	t.Run("returns error with provider env var hint", func(t *testing.T) {
		_, err := RequireAPIKey("testprovider", "", "TEST_PROVIDER_KEY")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		errStr := err.Error()
		if errStr != "testprovider: API key is required (set TEST_PROVIDER_KEY or LLM_API_KEY)" {
			t.Errorf("unexpected error message: %s", errStr)
		}
	})

	t.Run("returns error with LLM_API_KEY hint when no provider vars", func(t *testing.T) {
		_, err := RequireAPIKey("testprovider", "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		errStr := err.Error()
		if errStr != "testprovider: API key is required (set LLM_API_KEY)" {
			t.Errorf("unexpected error message: %s", errStr)
		}
	})

	t.Run("error includes provider name", func(t *testing.T) {
		_, err := RequireAPIKey("myprovider", "", "MY_KEY")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !apiKeyTestContains(err.Error(), "myprovider") {
			t.Errorf("error should contain provider name: %s", err.Error())
		}
	})
}

func apiKeyTestContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
