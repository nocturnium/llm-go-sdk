package zai

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
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
