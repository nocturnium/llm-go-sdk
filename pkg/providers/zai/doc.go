// Package zai provides a client for Z.AI's GLM models via their OpenAI-compatible API.
//
// # Overview
//
// Z.AI offers the GLM (General Language Model) series with a 200K context window
// and up to 128K max output tokens. The platform provides three model variants:
//   - GLM-4.7 (flagship model)
//   - GLM-4.7-FlashX (optimized variant)
//   - GLM-4.7-Flash (free tier)
//
// # Authentication
//
// Z.AI requires an API key for authentication. You can provide it in three ways:
//  1. Via the WithAPIKey() option
//  2. Via the ZAI_API_KEY environment variable
//  3. Via the LLM_API_KEY fallback environment variable
//
// # Usage
//
// Basic usage with environment variable:
//
//	import "github.com/nocturnium/llm-go-sdk/pkg/providers/zai"
//
//	// Create client (uses ZAI_API_KEY environment variable)
//	client, err := zai.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Make a simple call
//	response, err := client.Call(ctx, "What is the capital of France?")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(response)
//
// With explicit API key and model selection:
//
//	client, err := zai.New(
//	    zai.WithAPIKey("your-api-key"),
//	    zai.WithModel(zai.ModelGLM47Flash), // Use free tier model
//	)
//
// # Coding API Endpoint
//
// Z.AI provides a specialized endpoint for coding scenarios. To use it:
//
//	client, err := zai.New(
//	    zai.WithAPIKey("your-api-key"),
//	    zai.WithUseCodingAPI(), // Uses https://api.z.ai/api/coding/paas/v4
//	)
//
// Note: The Coding API endpoint is only for coding scenarios and is not
// applicable to general API scenarios.
//
// # Streaming
//
// Z.AI supports streaming responses:
//
//	messages := []llms.Message{
//	    {Role: "user", Content: "Explain quantum computing"},
//	}
//
//	stream, err := client.Stream(ctx, messages)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer stream.Close()
//
//	for stream.Next() {
//	    chunk := stream.Chunk()
//	    fmt.Print(chunk.Content)
//	}
//	if err := stream.Err(); err != nil {
//	    log.Fatal(err)
//	}
//
// # Capabilities
//
// The Z.AI provider supports:
//   - Streaming responses
//   - Tool/function calling
//   - Vision (image inputs)
//   - JSON mode
//   - 200K context length
//   - 128K max output tokens
//
// Note: Z.AI does not provide embedding models in the documented API.
//
// # Custom Configuration
//
// For advanced configuration, supply a standard library *http.Client (for
// timeouts, proxies, or a custom transport) and use a convenience option for
// the timeout. For retries and circuit breaking, wrap the client with
// llms.NewResilientClient.
//
//	client, err := zai.New(
//	    zai.WithAPIKey("your-api-key"),
//	    zai.WithModel(zai.ModelGLM47FlashX),
//	    zai.WithBaseURL("https://custom.api.z.ai/api/paas/v4"),
//	    zai.WithTimeout(60*time.Second),
//	    zai.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
//	)
//
// # References
//
//   - Z.AI Documentation: https://docs.z.ai/
//   - API Reference: https://docs.z.ai/guides/llm/glm-4.7
package zai
