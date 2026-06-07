// Package azure provides an Azure OpenAI LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Azure OpenAI's
// API, providing enterprise-grade access with custom deployments.
//
// # Configuration
//
// Azure OpenAI requires specific configuration:
//   - AZURE_OPENAI_API_KEY or AZURE_OPENAI_KEY - API key
//   - AZURE_OPENAI_ENDPOINT - Full endpoint URL
//   - AZURE_OPENAI_DEPLOYMENT - Deployment name
//
// Or provide them explicitly with options.
//
// # Quick Start
//
//	client, err := azure.New(
//	    azure.WithEndpoint("https://myresource.openai.azure.com"),
//	    azure.WithDeployment("my-gpt4-deployment"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions
//   - Streaming responses
//   - Function/tool calling
//   - JSON mode
//   - Embeddings (deployment dependent)
//   - Enterprise compliance (SOC2, HIPAA)
//   - Content filtering
//   - PTU (Provisioned Throughput Units)
//
// # Configuration Options
//
//	client, err := azure.New(
//	    azure.WithAPIKey("..."),
//	    azure.WithEndpoint("https://myresource.openai.azure.com"),
//	    azure.WithDeployment("gpt-4"),
//	    azure.WithAPIVersion("2024-02-15-preview"),
//	)
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package azure
