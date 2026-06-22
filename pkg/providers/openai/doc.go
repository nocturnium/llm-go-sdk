// Package openai provides an OpenAI LLM client using native HTTP.
//
// This package implements the llms.LLM interface for OpenAI's chat completion API,
// supporting GPT-4o, GPT-4, and GPT-3.5 model families.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - OPENAI_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := openai.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions (all GPT models)
//   - Streaming responses
//   - Function/tool calling
//   - Vision (GPT-4o, GPT-4-vision)
//   - Embeddings (text-embedding-3-small, text-embedding-3-large)
//   - JSON mode
//   - Custom base URL (for proxies or Azure OpenAI)
//
// # Configuration Options
//
//	client, err := openai.New(
//	    openai.WithAPIKey("sk-..."),
//	    openai.WithModel("gpt-4o-mini"),
//	    openai.WithBaseURL("https://custom-endpoint.com/v1"),
//	    openai.WithOrganization("org-..."),
//	    openai.WithHTTPClient(customHTTPClient),
//	)
//
// # Embeddings
//
//	client, err := openai.New(openai.WithEmbeddingModel("text-embedding-3-small"))
//	embedder, ok := llms.AsEmbedder(client)
//	if ok {
//	    vectors, err := llms.EmbedDocuments(ctx, embedder, []string{"text1", "text2"})
//	}
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package openai
