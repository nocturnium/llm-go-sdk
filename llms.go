// The package-level documentation, including the API reference map, quick start,
// and provider capability matrix, lives in doc.go.
package llms

import (
	"context"
)

// Provider represents an LLM provider type
type Provider string

// Provider constants define the supported LLM providers.
const (
	// ProviderElevenLabs identifies the native media-only provider.
	ProviderElevenLabs Provider = "elevenlabs"
	// ProviderFal identifies the native media-only fal.ai queue provider.
	ProviderFal Provider = "fal"
	// ProviderOpenRouter is the OpenRouter provider.
	ProviderOpenRouter Provider = "openrouter"
	// ProviderOpenAI is the OpenAI provider.
	ProviderOpenAI      Provider = "openai"
	ProviderTogetherAI  Provider = "togetherai"
	ProviderAnthropic   Provider = "anthropic"
	ProviderGemini      Provider = "gemini"
	ProviderFeatherless Provider = "featherless"
	ProviderSynthetic   Provider = "synthetic"
	// ProviderGroq is the Groq provider.
	ProviderGroq        Provider = "groq"
	ProviderFireworks   Provider = "fireworks"
	ProviderPerplexity  Provider = "perplexity"
	ProviderMistral     Provider = "mistral"
	ProviderDeepSeek    Provider = "deepseek"
	ProviderCerebras    Provider = "cerebras"
	ProviderOllama      Provider = "ollama"
	ProviderAzure       Provider = "azure"
	ProviderInfinity    Provider = "infinity"
	ProviderRunPod      Provider = "runpod"
	ProviderZAI         Provider = "zai"
	ProviderLlamaCpp    Provider = "llamacpp"
	ProviderHuggingFace Provider = "huggingface"
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
	// ImageGeneration indicates support for ImageGeneration operations.
	ImageGeneration bool
	// VideoGeneration indicates support for VideoGeneration operations.
	VideoGeneration bool
	// Speech indicates support for Speech operations.
	Speech bool
	// Transcription indicates support for Transcription operations.
	Transcription bool

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

// HasImageGeneration checks the ImageGeneration Capabilities flag.
// For media, Supports*(any) asserts an interface; Has*(LLM) reads a Capabilities flag.
func HasImageGeneration(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.ImageGeneration })
}

// HasVideoGeneration checks the VideoGeneration Capabilities flag.
// For media, Supports*(any) asserts an interface; Has*(LLM) reads a Capabilities flag.
func HasVideoGeneration(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.VideoGeneration })
}

// HasSpeech checks the Speech Capabilities flag.
// For media, Supports*(any) asserts an interface; Has*(LLM) reads a Capabilities flag.
func HasSpeech(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.Speech })
}

// HasTranscription checks the Transcription Capabilities flag.
// For media, Supports*(any) asserts an interface; Has*(LLM) reads a Capabilities flag.
func HasTranscription(llm LLM) bool {
	return HasCapability(llm, func(c Capabilities) bool { return c.Transcription })
}
