// Package openaicompat provides building blocks for OpenAI-compatible LLM providers.
//
// Many providers (OpenAI, TogetherAI, Featherless, Synthetic, Groq, Azure, and
// most "run your own endpoint" services) speak the OpenAI chat completions wire
// format. This package factors out the shared machinery so a new provider can be
// implemented in a few lines instead of re-deriving request/response handling.
//
// It provides three layers:
//
//   - Wire types matching the OpenAI API (ChatCompletionRequest,
//     ChatCompletionResponse, StreamChunk, EmbeddingRequest, ...).
//   - A low-level [Client] that handles auth headers, JSON and SSE streaming,
//     and error decoding against an OpenAI-compatible endpoint.
//   - A high-level [BaseProvider] that adapts the SDK's llms.LLM and
//     llms.Embedder interfaces onto the Client, including message preparation,
//     streaming, token estimation, and error wrapping.
//
// # Building a custom provider
//
// The fast path is to embed [BaseProvider]. It implements Call, GenerateContent,
// Stream, Embed, Provider, Model, and Capabilities, so your provider only
// has to construct a [Client] and a [ProviderConfig]:
//
//	package myprovider
//
//	import (
//		llms "github.com/nocturnium/llm-go-sdk/v4"
//		"github.com/nocturnium/llm-go-sdk/v4/pkg/openaicompat"
//	)
//
//	// Client embeds BaseProvider to inherit the llms.LLM implementation.
//	type Client struct {
//		openaicompat.BaseProvider
//	}
//
//	func New(apiKey string) (*Client, error) {
//		key, err := llms.RequireAPIKey("myprovider", apiKey, "MYPROVIDER_API_KEY")
//		if err != nil {
//			return nil, err
//		}
//
//		oc := openaicompat.NewClient(openaicompat.ClientConfig{
//			BaseURL: "https://api.myprovider.com/v1",
//			APIKey:  key,
//			Headers: map[string]string{"User-Agent": "my-provider-client/1.0"},
//		})
//
//		cfg := openaicompat.ProviderConfig{
//			Provider:     llms.ProviderOpenAI, // a Provider value used in errors/metadata
//			DefaultModel: "my-default-model",
//			Capabilities: llms.Capabilities{Streaming: true, Tools: true},
//		}
//
//		return &Client{
//			BaseProvider: openaicompat.NewBaseProvider(oc, cfg),
//		}, nil
//	}
//
// With that, callers can use the provider through the standard interface:
//
//	c, _ := myprovider.New("")              // reads MYPROVIDER_API_KEY
//	text, _ := c.Call(ctx, "Hello!")        // GenerateContent / Stream also work
//
// See ExampleBaseProvider in this package and the pkg/providers/openai package
// for complete reference implementations.
//
// # Type conversions
//
// When you need to drive the [Client] directly (e.g. for an endpoint that needs a
// custom request shape) the package exposes the conversions BaseProvider uses
// internally:
//
//   - [BuildChatRequest]: build a ChatCompletionRequest from llms types and CallOptions.
//   - [ConvertMessages]: convert llms.Message values to OpenAI ChatMessage values.
//   - [ConvertResponse]: convert a ChatCompletionResponse to an llms.Response.
//   - [ConvertEmbeddingResponse]: convert an EmbeddingResponse to llms.EmbeddingResponse.
//   - [WrapError]: attach provider context to transport/API errors.
//
// # HTTP client
//
// [Client] handles authentication (Bearer token, or the Azure "api-key" header
// and api-version query parameter), JSON request/response encoding, SSE stream
// decoding via [StreamReader], and tolerant /models parsing (both the standard
// {"object":"list","data":[...]} envelope and the bare-array form some providers
// return). Use [ClientConfig.HTTPClient] to supply a configured
// *net/http.Client for a custom transport, and [ClientConfig.Timeout] to set
// the request timeout.
//
// # Extensibility
//
//   - Embed [BaseProvider] for the common case; add provider-specific fields alongside it.
//   - Use [ChatCompletionRequest.ExtraBody] to pass provider-specific top-level
//     request fields (e.g. a LoRA adapter id) without forking the type.
//   - For full control, hold a [Client] directly and call CreateChatCompletion,
//     CreateChatCompletionStream, CreateEmbedding, or ListModels yourself.
package openaicompat
