// Package llamacpp provides a llama.cpp LLM client using native HTTP.
//
// This package implements the llms.LLM interface for llama.cpp's OpenAI-compatible
// API, providing local inference without requiring an internet connection.
// It also includes native llama.cpp API support for server management operations.
//
// # Configuration
//
// By default, the client connects to http://localhost:8080.
// Override with LLAMA_CPP_HOST environment variable or WithBaseURL option.
// No API key is required for local servers (optional for authenticated servers).
//
// # Quick Start
//
//	client, err := llamacpp.New()
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
//   - Function/tool calling (if model supports it)
//   - JSON mode (grammar-based)
//   - Embeddings (if model supports it)
//   - Vision (LLaVA and similar models)
//   - Local inference (no internet required)
//   - GPU acceleration
//
// # Server Management
//
// The client provides native llama.cpp API methods for monitoring the server:
//
//	// Check server health
//	health, err := client.Health(ctx)
//	fmt.Printf("Status: %s, Idle slots: %d\n", health.Status, health.SlotsIdle)
//
//	// Get model properties
//	props, err := client.ModelProps(ctx)
//	fmt.Printf("Model: %s, Context: %d\n", props.ModelName, props.NCtx)
//
//	// List inference slots
//	slots, err := client.ListSlots(ctx)
//	for _, slot := range slots {
//	    fmt.Printf("Slot %d: %s\n", slot.ID, slot.State)
//	}
//
//	// Quick health check
//	if client.IsHealthy(ctx) {
//	    fmt.Println("Server is ready")
//	}
//
// # Configuration Options
//
//	client, err := llamacpp.New(
//	    llamacpp.WithModel("llama-3.2-1b"),
//	    llamacpp.WithBaseURL("http://localhost:8080"),
//	    llamacpp.WithAPIKey("optional-api-key"),
//	)
//
// # Environment Variables
//
//   - LLAMA_CPP_HOST: Server URL (overrides default localhost:8080)
//   - LLAMA_CPP_API_KEY: API key for authenticated servers
//   - LLM_API_KEY: Fallback API key
//
// # Architecture Note
//
// This provider uses two APIs:
//
//   - OpenAI-compatible API (/v1/) for chat, streaming, and embeddings
//   - Native llama.cpp API for server management (/props, /slots, /health)
//
// The OpenAI-compatible endpoint is used for inference as it provides a
// standardized interface, while the native API provides llama.cpp-specific
// server monitoring and model information.
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package llamacpp
