package llms_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/providers/openai"
)

type registryTestLLM struct{}

func (registryTestLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func (registryTestLLM) GenerateContent(context.Context, []llms.Message, ...llms.CallOption) (*llms.Response, error) {
	return nil, nil
}

func (registryTestLLM) Stream(context.Context, []llms.Message, ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	return make(chan llms.StreamChunk), nil
}

func (registryTestLLM) Provider() llms.Provider {
	return llms.Provider("registry-test")
}

func (registryTestLLM) Model() string {
	return "registry-test-model"
}

func TestRegistry_New(t *testing.T) {
	llms.RegisterProvider("RegistryTest", func(cfg llms.Config) (llms.LLM, error) {
		if cfg.APIKey != "test-key" {
			t.Fatalf("APIKey = %q, want test-key", cfg.APIKey)
		}
		if cfg.Extra["test_extra"] != "test-value" {
			t.Fatalf("Extra[test_extra] = %q, want test-value", cfg.Extra["test_extra"])
		}
		return registryTestLLM{}, nil
	})

	llm, err := llms.New("registrytest", llms.Config{
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
	llms.RegisterProvider("RegistryOverwrite", func(llms.Config) (llms.LLM, error) {
		return registryTestLLM{}, nil
	})
	llms.RegisterProvider("registryoverwrite", func(llms.Config) (llms.LLM, error) {
		return registryTestLLM{}, nil
	})

	if _, err := llms.New("REGISTRYOVERWRITE", llms.Config{}); err != nil {
		t.Fatalf("New after overwrite returned error: %v", err)
	}
}

func TestRegistry_UnknownProvider(t *testing.T) {
	llms.RegisterProvider("RegistryKnown", func(llms.Config) (llms.LLM, error) {
		return registryTestLLM{}, nil
	})

	_, err := llms.New("missing-provider", llms.Config{})
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

func TestConfigRequireExtra(t *testing.T) {
	cfg := llms.Config{Extra: map[string]string{"present": " value "}}

	value, err := cfg.RequireExtra("test-provider", "present")
	if err != nil {
		t.Fatalf("RequireExtra returned error: %v", err)
	}
	if value != " value " {
		t.Fatalf("RequireExtra value = %q, want original value", value)
	}

	for _, tc := range []struct {
		name string
		cfg  llms.Config
	}{
		{name: "missing", cfg: llms.Config{}},
		{name: "blank", cfg: llms.Config{Extra: map[string]string{"required": " \t\n"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.cfg.RequireExtra("test-provider", "required")
			if err == nil {
				t.Fatal("RequireExtra returned nil error")
			}
			if !errors.Is(err, llms.ErrInvalidParameters) {
				t.Fatalf("RequireExtra error = %v, want ErrInvalidParameters", err)
			}
			if !strings.Contains(err.Error(), "test-provider") {
				t.Fatalf("error %q does not include provider name", err.Error())
			}
			if !strings.Contains(err.Error(), "required") {
				t.Fatalf("error %q does not include required key", err.Error())
			}
		})
	}
}

func TestNewFromEnv_RequiresProvider(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_MODEL", "")

	_, err := llms.NewFromEnv()
	if err == nil {
		t.Fatal("NewFromEnv returned nil error without LLM_PROVIDER")
	}
	if !strings.Contains(err.Error(), "LLM_PROVIDER") {
		t.Fatalf("error %q does not mention LLM_PROVIDER", err.Error())
	}
}

func ExampleWithModel_twoLayers() {
	client, err := openai.New(
		openai.WithAPIKey("test-key"),
		openai.WithModel("provider-default-model"),
	)
	if err != nil {
		panic(err)
	}

	callOptions := llms.ApplyOptions(llms.WithModel("per-call-model"))

	fmt.Println(client.Model())
	fmt.Println(callOptions.Model)
	// Output:
	// provider-default-model
	// per-call-model
}

func TestWithModelProviderDefaultAndCallOverrideAreIndependent(t *testing.T) {
	client, err := openai.New(
		openai.WithAPIKey("test-key"),
		openai.WithModel("A"),
	)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if client.Model() != "A" {
		t.Fatalf("client.Model() = %q, want A", client.Model())
	}

	opts := llms.ApplyOptions(llms.WithModel("B"))
	if opts.Model != "B" {
		t.Fatalf("ApplyOptions(WithModel(\"B\")).Model = %q, want B", opts.Model)
	}
	if client.Model() != "A" {
		t.Fatalf("client.Model() after call option = %q, want A", client.Model())
	}
}
