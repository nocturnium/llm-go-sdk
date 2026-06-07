package llms

import (
	"context"
	"strings"
	"testing"
)

type registryTestLLM struct{}

func (registryTestLLM) Call(context.Context, string, ...CallOption) (string, error) {
	return "", nil
}

func (registryTestLLM) GenerateContent(context.Context, []Message, ...CallOption) (*Response, error) {
	return nil, nil
}

func (registryTestLLM) Stream(context.Context, []Message, ...CallOption) (<-chan StreamChunk, error) {
	return make(chan StreamChunk), nil
}

func (registryTestLLM) Provider() Provider {
	return Provider("registry-test")
}

func (registryTestLLM) Model() string {
	return "registry-test-model"
}

func TestRegistry_New(t *testing.T) {
	RegisterProvider("RegistryTest", func(cfg Config) (LLM, error) {
		if cfg.APIKey != "test-key" {
			t.Fatalf("APIKey = %q, want test-key", cfg.APIKey)
		}
		if cfg.Extra["test_extra"] != "test-value" {
			t.Fatalf("Extra[test_extra] = %q, want test-value", cfg.Extra["test_extra"])
		}
		return registryTestLLM{}, nil
	})

	llm, err := New("registrytest", Config{
		APIKey: "test-key",
		Extra:  map[string]string{"test_extra": "test-value"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if llm == nil {
		t.Fatal("New returned nil LLM")
	}
}

func TestRegistry_Overwrite(t *testing.T) {
	RegisterProvider("RegistryOverwrite", func(Config) (LLM, error) {
		return registryTestLLM{}, nil
	})
	RegisterProvider("registryoverwrite", func(Config) (LLM, error) {
		return registryTestLLM{}, nil
	})

	if _, err := New("REGISTRYOVERWRITE", Config{}); err != nil {
		t.Fatalf("New after overwrite returned error: %v", err)
	}
}

func TestRegistry_UnknownProvider(t *testing.T) {
	RegisterProvider("RegistryKnown", func(Config) (LLM, error) {
		return registryTestLLM{}, nil
	})

	_, err := New("missing-provider", Config{})
	if err == nil {
		t.Fatal("New returned nil error for unknown provider")
	}
	if !strings.Contains(err.Error(), "missing-provider") {
		t.Fatalf("error %q does not include missing provider name", err.Error())
	}
	if !strings.Contains(err.Error(), "registryknown") {
		t.Fatalf("error %q does not include registered provider list", err.Error())
	}
}

func TestNewFromEnv_RequiresProvider(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_MODEL", "")

	_, err := NewFromEnv()
	if err == nil {
		t.Fatal("NewFromEnv returned nil error without LLM_PROVIDER")
	}
	if !strings.Contains(err.Error(), "LLM_PROVIDER") {
		t.Fatalf("error %q does not mention LLM_PROVIDER", err.Error())
	}
}
