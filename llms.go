// Package llms provides a unified interface for interacting with various LLM providers.
//
// This package offers a consistent API across multiple LLM backends including OpenAI,
// Anthropic, Google Gemini, TogetherAI, and Featherless. It uses native Go HTTP calls
// with no external LLM dependencies.
//
// # Quick Start
//
// Create a provider client and make requests:
//
//	client, err := openai.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Simple call
//	response, err := llms.Call(ctx, client, "Hello, world!")
//
//	// Or with messages for more control
//	messages := []llms.Message{
//	    {Role: llms.RoleSystem, Content: "You are helpful."},
//	    {Role: llms.RoleUser, Content: "What is Go?"},
//	}
//	resp, err := client.GenerateContent(ctx, messages,
//	    llms.WithTemperature(0.7),
//	    llms.WithMaxTokens(1024),
//	)
//
// # Streaming
//
// Stream responses in real-time:
//
//	chunks, err := client.Stream(ctx, messages)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	text, err := llms.StreamText(chunks)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Print(text)
//
// Or consume chunks incrementally:
//
//	chunks, err := client.Stream(ctx, messages)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        log.Fatal(chunk.Error)
//	    }
//	    if chunk.Done {
//	        break
//	    }
//	    fmt.Print(chunk.Content)
//	}
//
// # Tool Calling
//
// Define and use tools:
//
//	tool := llms.NewFunctionTool("get_weather", "Get weather", params)
//	resp, err := client.GenerateContent(ctx, messages,
//	    llms.WithTools([]llms.Tool{tool}),
//	)
//	if len(resp.ToolCalls) > 0 {
//	    // Handle tool call
//	}
//
// # Providers
//
// Import the provider you need from its canonical pkg/providers path:
//
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/openai"
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/anthropic"
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/gemini"
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/togetherai"
//	import "github.com/nocturnium/llm-go-sdk/v2/pkg/providers/featherless"
//
// Each provider reads its API key from environment variables by default.
// See provider documentation for configuration options.
//
// # Provider Capability Matrix
//
// The following matrix shows which features are supported by each provider:
//
//	| Provider    | Streaming | Tools | Vision | Embeddings | Reranking | JSON Mode | Model Listing |
//	|-------------|-----------|-------|--------|------------|-----------|-----------|---------------|
//	| OpenAI      | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Anthropic   | ✓         | ✓     | ✓      | ✗          | ✗         | ✗         | ✓             |
//	| Gemini      | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Groq        | ✓         | ✓     | ✓      | ✗          | ✗         | ✓         | ✓             |
//	| Mistral     | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| DeepSeek    | ✓         | ✓     | ✗      | ✗          | ✗         | ✓         | ✓             |
//	| Cerebras    | ✓         | ✓     | ✗      | ✗          | ✗         | ✓         | ✓             |
//	| Fireworks   | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| TogetherAI  | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Perplexity  | ✓         | ✗     | ✗      | ✗          | ✗         | ✓         | ✓             |
//	| Featherless | ✓         | ✓     | ✗      | ✗          | ✗         | ✓         | ✓             |
//	| Synthetic   | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Ollama      | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Azure       | ✓         | ✓     | ✓      | ✓          | ✗         | ✓         | ✓             |
//	| Infinity    | ✗         | ✗     | ✗      | ✓          | ✓         | ✗         | ✗             |
//
// Notes:
//   - Infinity is an embeddings-only service and does not implement the LLM interface
//   - Vision support may depend on the specific model being used
//   - Tool support may vary by model within a provider
//   - Use SupportsEmbeddings() and AsEmbedder() to check embedding capability at runtime
//   - Use SupportsReranking() and AsReranker() to check reranking capability at runtime
//   - Use SupportsModelListing() and AsModelLister() to check model listing capability at runtime
//   - Use AsCapableProvider() for capability introspection through middleware
package llms

import (
	"context"
)

// Provider represents an LLM provider type
type Provider string

// Provider constants define the supported LLM providers.
const (
	// ProviderOpenAI is the OpenAI provider.
	ProviderOpenAI      Provider = "openai"
	ProviderTogetherAI  Provider = "togetherai"
	ProviderAnthropic   Provider = "anthropic"
	ProviderGemini      Provider = "gemini"
	ProviderFeatherless Provider = "featherless"
	ProviderSynthetic   Provider = "synthetic"
	// ProviderGroq is the Groq provider.
	ProviderGroq       Provider = "groq"
	ProviderFireworks  Provider = "fireworks"
	ProviderPerplexity Provider = "perplexity"
	ProviderMistral    Provider = "mistral"
	ProviderDeepSeek   Provider = "deepseek"
	ProviderCerebras   Provider = "cerebras"
	ProviderOllama     Provider = "ollama"
	ProviderAzure      Provider = "azure"
	ProviderInfinity   Provider = "infinity"
	ProviderRunPod     Provider = "runpod"
	ProviderZAI        Provider = "zai"
	ProviderLlamaCpp   Provider = "llamacpp"
)

// LLM defines the interface that all LLM providers must implement
type LLM interface {
	// GenerateContent generates content with more control over messages
	GenerateContent(ctx context.Context, messages []Message, options ...CallOption) (*Response, error)

	// Stream generates content with streaming, returning chunks via channel
	Stream(ctx context.Context, messages []Message, options ...CallOption) (<-chan StreamChunk, error)

	// Provider returns the provider type
	Provider() Provider

	// Model returns the model name
	Model() string
}

// Call sends a single user prompt to the LLM and returns the response content.
func Call(ctx context.Context, llm LLM, prompt string, options ...CallOption) (string, error) {
	resp, err := llm.GenerateContent(ctx, []Message{{Role: RoleUser, Content: prompt}}, options...)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// Wrapper is implemented by types that wrap an LLM client (middleware).
// This allows for introspection and unwrapping of middleware chains.
type Wrapper interface {
	LLM
	// Unwrap returns the underlying LLM
	Unwrap() LLM
}

// UnwrapAll recursively unwraps all middleware to get the base LLM implementation.
func UnwrapAll(llm LLM) LLM {
	for {
		wrapper, ok := llm.(Wrapper)
		if !ok {
			return llm
		}
		llm = wrapper.Unwrap()
	}
}

// GetMiddleware extracts all middleware wrappers from the LLM chain.
// The returned slice is ordered from outermost to innermost wrapper.
func GetMiddleware(llm LLM) []Wrapper {
	var middleware []Wrapper
	current := llm
	for {
		wrapper, ok := current.(Wrapper)
		if !ok {
			break
		}
		middleware = append(middleware, wrapper)
		current = wrapper.Unwrap()
	}
	return middleware
}

// Capabilities describes what features a provider supports.
// This allows client code to check capabilities at runtime rather than
// relying on type assertions or trial-and-error.
type Capabilities struct {
	// Streaming indicates the provider supports streaming responses
	Streaming bool

	// Tools indicates the provider supports function/tool calling
	Tools bool

	// Vision indicates the provider supports image inputs
	Vision bool

	// Embeddings indicates the provider supports text embeddings
	Embeddings bool

	// Batch indicates the provider supports batch processing
	Batch bool

	// JSONMode indicates the provider supports structured JSON output
	JSONMode bool

	// Reasoning indicates the model supports reasoning ("thinking") output and
	// the WithReasoning* options.
	Reasoning bool

	// PromptCaching indicates the provider supports prompt caching (and reports
	// cache token usage).
	PromptCaching bool

	// MaxContextTokens is the maximum context length (0 = unknown)
	MaxContextTokens int

	// MaxOutputTokens is the maximum output tokens (0 = unknown)
	MaxOutputTokens int
}

// CapableProvider extends LLM with capability introspection.
// Providers that implement this interface allow clients to query
// supported features at runtime.
type CapableProvider interface {
	LLM
	// Capabilities returns the provider's capabilities
	Capabilities() Capabilities
}

// AsCapableProvider attempts to unwrap and cast an LLM to a CapableProvider.
// Returns the CapableProvider and true if successful, nil and false otherwise.
func AsCapableProvider(llm LLM) (CapableProvider, bool) {
	base := UnwrapAll(llm)
	cp, ok := base.(CapableProvider)
	return cp, ok
}

// HasCapability checks if an LLM has a specific capability.
// Returns false if the LLM doesn't implement CapableProvider.
//
// Example:
//
//	if llms.HasCapability(client, func(c llms.Capabilities) bool { return c.Vision }) {
//	    // Safe to use vision features
//	}
func HasCapability(llm LLM, check func(Capabilities) bool) bool {
	// Unwrap middleware to find the base provider
	base := UnwrapAll(llm)
	if cp, ok := base.(CapableProvider); ok {
		return check(cp.Capabilities())
	}
	return false
}

// GetCapabilities returns the capabilities of an LLM.
// Returns an empty Capabilities struct if the LLM doesn't implement CapableProvider.
func GetCapabilities(llm LLM) Capabilities {
	base := UnwrapAll(llm)
	if cp, ok := base.(CapableProvider); ok {
		return cp.Capabilities()
	}
	return Capabilities{}
}

// SupportsStreaming checks if the LLM supports streaming responses.
func SupportsStreaming(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.Streaming })
}

// SupportsTools checks if the LLM supports function/tool calling.
func SupportsTools(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.Tools })
}

// SupportsVision checks if the LLM supports image inputs.
func SupportsVision(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.Vision })
}

// SupportsBatch checks if the LLM supports batch processing.
func SupportsBatch(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.Batch })
}

// SupportsJSONMode checks if the LLM supports structured JSON output.
func SupportsJSONMode(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.JSONMode })
}

// SupportsReasoning checks if the LLM supports reasoning output.
func SupportsReasoning(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.Reasoning })
}

// SupportsPromptCaching checks if the LLM supports prompt caching.
func SupportsPromptCaching(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.PromptCaching })
}
