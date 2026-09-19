package zai

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestRegisterNewWithoutCodingExtra(t *testing.T) {
	llm, err := llms.New("zai", llms.Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New returned error without coding extra: %v", err)
	}
	if llm == nil {
		t.Fatal("New returned nil LLM")
	}
	if llm.Provider() != llms.ProviderZAI {
		t.Fatalf("Provider() = %q, want %q", llm.Provider(), llms.ProviderZAI)
	}
}

// TestRegistry_CodingExtraRejectsUnknownValue pins that an unrecognized value
// for the coding extra is an error. Silently treating "on" as false routed the
// caller to the standard endpoint with no signal.
func TestRegistry_CodingExtraRejectsUnknownValue(t *testing.T) {
	for _, value := range []string{"on", "enabled", "y"} {
		_, err := llms.New("zai", llms.Config{APIKey: "k", Extra: map[string]string{llms.ExtraZAICoding: value}})
		if !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("value %q: err = %v, want ErrInvalidParameters", value, err)
		}
	}

	for _, value := range []string{"", "false", "no", "0"} {
		if _, err := llms.New("zai", llms.Config{APIKey: "k", Extra: map[string]string{llms.ExtraZAICoding: value}}); err != nil {
			t.Errorf("value %q: err = %v, want nil", value, err)
		}
	}
}
