package openaicompat_test

import (
	"context"
	"fmt"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/openaicompat"
)

// customClient embeds BaseProvider to inherit the full llms.LLM and
// llms.Embedder implementation (Call, GenerateContent, Stream, Embed, ...).
type customClient struct {
	openaicompat.BaseProvider
}

// newCustomClient builds a provider for an arbitrary OpenAI-compatible endpoint.
func newCustomClient(apiKey string) (*customClient, error) {
	key, err := llms.RequireAPIKey("myprovider", apiKey, "MYPROVIDER_API_KEY")
	if err != nil {
		return nil, err
	}

	oc := openaicompat.NewClient(openaicompat.ClientConfig{
		BaseURL: "https://api.myprovider.com/v1",
		APIKey:  key,
		Headers: map[string]string{"User-Agent": "my-provider-client/1.0"},
	})

	cfg := openaicompat.ProviderConfig{
		Provider:     llms.ProviderOpenAI,
		ProviderName: "myprovider",
		DefaultModel: "my-default-model",
		Capabilities: llms.Capabilities{Streaming: true, Tools: true},
	}

	return &customClient{
		BaseProvider: openaicompat.NewBaseProvider(oc, cfg),
	}, nil
}

// ExampleBaseProvider shows how to build a custom OpenAI-compatible provider by
// embedding BaseProvider. The resulting type satisfies the llms.LLM interface.
func ExampleBaseProvider() {
	client, err := newCustomClient("test-key")
	if err != nil {
		fmt.Println("setup error:", err)
		return
	}

	// client implements llms.LLM, so it works anywhere an LLM is expected.
	var _ llms.LLM = client

	// Network calls are omitted here; this just demonstrates the call surface.
	_ = func(ctx context.Context) (string, error) {
		return client.Call(ctx, "Hello!")
	}

	fmt.Println(client.Model(), client.Provider())
	// Output: my-default-model openai
}
