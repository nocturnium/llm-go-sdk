package all_test

import (
	"reflect"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	_ "github.com/nocturnium/llm-go-sdk/v6/pkg/providers/all"
)

func TestAllRegistersChatProviders(t *testing.T) {
	want := []string{
		"anthropic",
		"azure",
		"cerebras",
		"deepseek",
		"featherless",
		"fireworks",
		"gemini",
		"groq",
		"llamacpp",
		"mistral",
		"ollama",
		"openai",
		"openrouter",
		"perplexity",
		"runpod",
		"synthetic",
		"togetherai",
		"zai",
	}

	got := llms.RegisteredProviders()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RegisteredProviders() = %#v, want %#v", got, want)
	}
}

func TestAllDoesNotRegisterInfinity(t *testing.T) {
	for _, name := range llms.RegisteredProviders() {
		if name == "infinity" {
			t.Fatal("infinity should not be registered as a chat provider")
		}
	}
}

func TestAllConstructsOpenAI(t *testing.T) {
	llm, err := llms.New("OPENAI", llms.Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if llm == nil {
		t.Fatal("New returned nil LLM")
	}
	if llm.Provider() != llms.ProviderOpenAI {
		t.Fatalf("Provider() = %q, want %q", llm.Provider(), llms.ProviderOpenAI)
	}
}

func TestAllConstructsRunPodWithEndpointIDExtra(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "test-key")

	llm, err := llms.New("runpod", llms.Config{
		Extra: map[string]string{llms.ExtraRunPodEndpointID: "ep-123"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if llm == nil {
		t.Fatal("New returned nil LLM")
	}
	if llm.Provider() != llms.ProviderRunPod {
		t.Fatalf("Provider() = %q, want %q", llm.Provider(), llms.ProviderRunPod)
	}
}

func TestAllUnknownProvider(t *testing.T) {
	_, err := llms.New("does-not-exist", llms.Config{})
	if err == nil {
		t.Fatal("New returned nil error for unknown provider")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("error %q does not include unknown provider name", err.Error())
	}
}
