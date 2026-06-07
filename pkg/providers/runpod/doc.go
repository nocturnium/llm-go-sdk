// Package runpod provides a RunPod LLM client using native HTTP.
//
// This package implements the llms.LLM interface for RunPod's serverless
// endpoints, supporting both the native RunPod API and OpenAI-compatible
// endpoints (via vLLM workers).
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - RUNPOD_API_KEY (primary)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
// For OpenAI-compatible endpoints (vLLM):
//
//	client, err := runpod.New(
//	    runpod.WithEndpointID("abc123"),
//	    runpod.WithModel("meta-llama/Meta-Llama-3.1-8B-Instruct"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Endpoint Types
//
// RunPod supports two API styles:
//   - OpenAI-compatible: Standard /openai/v1 endpoints (default, recommended)
//   - Native RunPod: Custom /runsync and /run endpoints for more control
//
// By default, this package uses OpenAI-compatible mode which works with
// vLLM-based endpoints.
//
// # Supported Features
//
//   - Chat completions (via OpenAI-compatible API)
//   - Streaming responses
//   - Function/tool calling (model-dependent)
//   - Custom serverless endpoints
//   - Flexible GPU scaling
//
// # Configuration Options
//
//	client, err := runpod.New(
//	    runpod.WithAPIKey("..."),
//	    runpod.WithEndpointID("your-endpoint-id"),
//	    runpod.WithModel("meta-llama/Meta-Llama-3.1-8B-Instruct"),
//	    runpod.WithHTTPClient(customHTTPClient),
//	)
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package runpod
