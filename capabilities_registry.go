package llms

import (
	"strings"
	"sync"
)

// ModelCapabilities contains capability information for a specific model.
// This allows accurate per-model capability reporting instead of static
// provider-level defaults.
type ModelCapabilities struct {
	MaxContextTokens      int  // Maximum input context window
	MaxOutputTokens       int  // Maximum output tokens
	SupportsVision        bool // Can process images
	SupportsTools         bool // Supports function/tool calling
	SupportsStreaming     bool // Supports streaming responses
	SupportsEmbeddings    bool // Supports text embeddings
	SupportsBatch         bool // Supports batch processing
	SupportsJSON          bool // Supports JSON mode output
	SupportsReasoning     bool // Supports reasoning ("thinking") output
	SupportsPromptCaching bool // Supports prompt caching
}

// CapabilityRegistry provides per-model capability lookups.
// This addresses the issue of hardcoded capabilities that don't vary by model variant.
type CapabilityRegistry struct {
	mu           sync.RWMutex
	capabilities map[string]ModelCapabilities // key: "provider:model"
	defaults     map[Provider]ModelCapabilities
}

// globalRegistry is the default capability registry.
var globalRegistry = NewCapabilityRegistry()

// NewCapabilityRegistry creates a new empty capability registry.
func NewCapabilityRegistry() *CapabilityRegistry {
	r := &CapabilityRegistry{
		capabilities: make(map[string]ModelCapabilities),
		defaults:     make(map[Provider]ModelCapabilities),
	}
	r.registerDefaults()
	return r
}

// DefaultCapabilityRegistry returns the global capability registry.
func DefaultCapabilityRegistry() *CapabilityRegistry {
	return globalRegistry
}

// Register adds capabilities for a specific model.
func (r *CapabilityRegistry) Register(provider Provider, modelID string, caps ModelCapabilities) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.makeKey(provider, modelID)
	r.capabilities[key] = caps
}

// RegisterDefault sets the default capabilities for a provider.
// These are used when a specific model's capabilities are not registered.
func (r *CapabilityRegistry) RegisterDefault(provider Provider, caps ModelCapabilities) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaults[provider] = caps
}

// Get retrieves capabilities for a specific model.
// Falls back to provider defaults if the model is not registered.
// Returns zero values if neither model nor provider defaults exist.
func (r *CapabilityRegistry) Get(provider Provider, modelID string) ModelCapabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try exact match first
	key := r.makeKey(provider, modelID)
	if caps, ok := r.capabilities[key]; ok {
		return caps
	}

	// Try case-insensitive match
	keyLower := r.makeKey(provider, strings.ToLower(modelID))
	if caps, ok := r.capabilities[keyLower]; ok {
		return caps
	}

	// Fall back to provider defaults
	if caps, ok := r.defaults[provider]; ok {
		return caps
	}

	return ModelCapabilities{}
}

// GetWithDefaults returns capabilities with explicit defaults applied.
// This is useful for Capabilities() implementations.
func (r *CapabilityRegistry) GetWithDefaults(provider Provider, modelID string, defaults ModelCapabilities) ModelCapabilities {
	caps := r.Get(provider, modelID)

	// Apply defaults for zero values
	if caps.MaxContextTokens == 0 {
		caps.MaxContextTokens = defaults.MaxContextTokens
	}
	if caps.MaxOutputTokens == 0 {
		caps.MaxOutputTokens = defaults.MaxOutputTokens
	}

	return caps
}

func (r *CapabilityRegistry) makeKey(provider Provider, modelID string) string {
	return string(provider) + ":" + modelID
}

// registerDefaults populates the registry with known model capabilities.
// This data should be periodically updated as models change.
func (r *CapabilityRegistry) registerDefaults() {
	// OpenAI defaults
	r.defaults[ProviderOpenAI] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   16384,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// OpenAI specific models
	r.capabilities["openai:gpt-4o"] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   16384,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["openai:gpt-4o-mini"] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   16384,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["openai:gpt-4-turbo"] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   4096,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["openai:gpt-4"] = ModelCapabilities{
		MaxContextTokens:  8192,
		MaxOutputTokens:   4096,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["openai:gpt-3.5-turbo"] = ModelCapabilities{
		MaxContextTokens:  16385,
		MaxOutputTokens:   4096,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["openai:o1"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   100000,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["openai:o1-mini"] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   65536,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["openai:o3"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   100000,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["openai:o4-mini"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   100000,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["openai:gpt-4.1"] = ModelCapabilities{
		MaxContextTokens:  1000000,
		MaxOutputTokens:   32768,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Anthropic defaults
	r.defaults[ProviderAnthropic] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   8192,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      false, // Uses tool_choice for structured output
	}

	// Anthropic specific models
	r.capabilities["anthropic:claude-3-5-sonnet-20241022"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   8192,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      false,
	}
	r.capabilities["anthropic:claude-sonnet-4"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   64000,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      false,
	}
	r.capabilities["anthropic:claude-sonnet-4-20250514"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   64000,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      false,
	}
	r.capabilities["anthropic:claude-3-5-haiku-20241022"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   8192,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      false,
	}
	r.capabilities["anthropic:claude-3-opus-20240229"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   4096,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      false,
	}
	r.capabilities["anthropic:claude-3-sonnet-20240229"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   4096,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      false,
	}
	r.capabilities["anthropic:claude-3-haiku-20240307"] = ModelCapabilities{
		MaxContextTokens:  200000,
		MaxOutputTokens:   4096,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      false,
	}

	// Gemini defaults
	r.defaults[ProviderGemini] = ModelCapabilities{
		MaxContextTokens:  1000000,
		MaxOutputTokens:   8192,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Gemini specific models
	r.capabilities["gemini:gemini-1.5-pro"] = ModelCapabilities{
		MaxContextTokens:  2097152, // 2M tokens
		MaxOutputTokens:   8192,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["gemini:gemini-1.5-flash"] = ModelCapabilities{
		MaxContextTokens:  1048576, // 1M tokens
		MaxOutputTokens:   8192,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["gemini:gemini-2.0-flash"] = ModelCapabilities{
		MaxContextTokens:  1048576,
		MaxOutputTokens:   8192,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["gemini:gemini-2.5-pro"] = ModelCapabilities{
		MaxContextTokens:  1048576,
		MaxOutputTokens:   65536,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["gemini:gemini-2.5-flash"] = ModelCapabilities{
		MaxContextTokens:  1048576,
		MaxOutputTokens:   65536,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Groq defaults (fast inference)
	r.defaults[ProviderGroq] = ModelCapabilities{
		MaxContextTokens:  131072,
		MaxOutputTokens:   8192,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Together AI defaults
	r.defaults[ProviderTogetherAI] = ModelCapabilities{
		MaxContextTokens:  32768,
		MaxOutputTokens:   4096,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// DeepSeek defaults
	r.defaults[ProviderDeepSeek] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   8192,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Mistral defaults
	r.defaults[ProviderMistral] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   8192,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Featherless defaults are conservative because serverless open model capabilities vary.
	r.defaults[ProviderFeatherless] = ModelCapabilities{
		MaxContextTokens:  32768,
		MaxOutputTokens:   4096,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Perplexity defaults cover online Sonar chat models; web search is provider-specific.
	r.defaults[ProviderPerplexity] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   8192,
		SupportsVision:    false,
		SupportsTools:     false,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Cerebras defaults cover fast Llama/Qwen inference.
	r.defaults[ProviderCerebras] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   8192,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Azure hosts OpenAI-compatible model deployments, so defaults mirror OpenAI.
	r.defaults[ProviderAzure] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   16384,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Infinity serves embeddings and reranking, not chat completions.
	r.defaults[ProviderInfinity] = ModelCapabilities{
		MaxContextTokens: 8192,
		MaxOutputTokens:  0,
	}

	// Z.AI defaults cover GLM-4.x chat models; vision is model-dependent.
	r.defaults[ProviderZAI] = ModelCapabilities{
		MaxContextTokens:  128000,
		MaxOutputTokens:   16384,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// llama.cpp capabilities depend on the local model and server build.
	r.defaults[ProviderLlamaCpp] = ModelCapabilities{
		MaxContextTokens:  8192,
		MaxOutputTokens:   4096,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Fireworks defaults are conservative because model capabilities vary.
	r.defaults[ProviderFireworks] = ModelCapabilities{
		MaxContextTokens:  131072,
		MaxOutputTokens:   16384,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["fireworks:accounts/fireworks/models/llama-v3p1-70b-instruct"] = ModelCapabilities{
		MaxContextTokens:  131072,
		MaxOutputTokens:   16384,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Synthetic defaults are conservative; use model-specific entries when known.
	r.defaults[ProviderSynthetic] = ModelCapabilities{
		MaxContextTokens:  32768,
		MaxOutputTokens:   8192,
		SupportsVision:    false,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["synthetic:hf:Qwen/Qwen3-Coder-480B-A35B-Instruct"] = ModelCapabilities{
		MaxContextTokens:  256000,
		MaxOutputTokens:   8192,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}
	r.capabilities["synthetic:hf:Qwen/Qwen3-VL-235B-A22B-Instruct"] = ModelCapabilities{
		MaxContextTokens:  250000,
		MaxOutputTokens:   8192,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// RunPod capabilities depend on the deployed endpoint.
	r.defaults[ProviderRunPod] = ModelCapabilities{
		MaxContextTokens:  8192,
		MaxOutputTokens:   4096,
		SupportsVision:    false,
		SupportsTools:     false,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	// Ollama capabilities depend on locally installed models.
	r.defaults[ProviderOllama] = ModelCapabilities{
		MaxContextTokens:  8192,
		MaxOutputTokens:   4096,
		SupportsVision:    false,
		SupportsTools:     false,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	r.registerReasoningAndCaching()
}

// registerReasoningAndCaching flips the SupportsReasoning / SupportsPromptCaching
// flags on the model and provider entries registered above. These are applied as
// a post-pass so the per-model literals stay focused on the core capabilities.
func (r *CapabilityRegistry) registerReasoningAndCaching() {
	// Reasoning ("thinking") is model-specific. Mark known reasoning models...
	reasoningModels := []string{
		"openai:o1", "openai:o1-mini", "openai:o3", "openai:o4-mini",
		"anthropic:claude-sonnet-4", "anthropic:claude-sonnet-4-20250514",
		"gemini:gemini-2.5-pro", "gemini:gemini-2.5-flash",
	}
	for _, key := range reasoningModels {
		if caps, ok := r.capabilities[key]; ok {
			caps.SupportsReasoning = true
			r.capabilities[key] = caps
		}
	}
	// ...and providers whose chat surface is reasoning-capable as a whole. Only
	// ZAI qualifies: its GLM-4.x chat models broadly expose a thinking toggle, and
	// it is represented solely by a default. Reasoning is otherwise model-specific,
	// so flagging a provider default that also has exact model entries (Anthropic,
	// Gemini, OpenAI) would disagree with those entries. DeepSeek is mixed —
	// deepseek-reasoner reasons but the default deepseek-chat does not — so it is
	// handled as a per-model entry below rather than a provider default.
	if caps, ok := r.defaults[ProviderZAI]; ok {
		caps.SupportsReasoning = true
		r.defaults[ProviderZAI] = caps
	}
	if caps, ok := r.defaults[ProviderDeepSeek]; ok {
		rc := caps // full copy of the provider defaults...
		rc.SupportsReasoning = true
		r.capabilities[r.makeKey(ProviderDeepSeek, "deepseek-reasoner")] = rc // ...with reasoning on
	}

	// Prompt caching: Anthropic (explicit breakpoints), OpenAI/Azure/DeepSeek
	// (automatic), and Gemini (implicit on 2.5) all report cache token usage.
	cachingProviders := map[Provider]bool{
		ProviderAnthropic: true, ProviderOpenAI: true, ProviderAzure: true,
		ProviderDeepSeek: true, ProviderGemini: true,
	}
	for p := range cachingProviders {
		if caps, ok := r.defaults[p]; ok {
			caps.SupportsPromptCaching = true
			r.defaults[p] = caps
		}
	}
	// Propagate the caching flag to per-model entries of those providers, since an
	// exact model match in Get does not inherit the provider default.
	for key, caps := range r.capabilities {
		provider := Provider(key[:strings.IndexByte(key, ':')])
		if cachingProviders[provider] {
			caps.SupportsPromptCaching = true
			r.capabilities[key] = caps
		}
	}
}

// GetModelCapabilities is a convenience function to get capabilities from the global registry.
func GetModelCapabilities(provider Provider, modelID string) ModelCapabilities {
	return globalRegistry.Get(provider, modelID)
}

// RegisterModelCapabilities registers capabilities in the global registry.
func RegisterModelCapabilities(provider Provider, modelID string, caps ModelCapabilities) {
	globalRegistry.Register(provider, modelID, caps)
}

// ToCapabilities converts ModelCapabilities to the provider Capabilities struct.
// This allows easy integration with existing Capabilities() implementations.
func (mc ModelCapabilities) ToCapabilities() Capabilities {
	return Capabilities{
		Streaming:        mc.SupportsStreaming,
		Tools:            mc.SupportsTools,
		Vision:           mc.SupportsVision,
		Embeddings:       mc.SupportsEmbeddings,
		Batch:            mc.SupportsBatch,
		JSONMode:         mc.SupportsJSON,
		Reasoning:        mc.SupportsReasoning,
		PromptCaching:    mc.SupportsPromptCaching,
		MaxContextTokens: mc.MaxContextTokens,
		MaxOutputTokens:  mc.MaxOutputTokens,
	}
}
