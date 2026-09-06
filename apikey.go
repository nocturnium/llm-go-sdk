package llms

import (
	"fmt"
	"os"
)

// ResolveAPIKey returns the first non-empty API key from the following sources
// (in order of precedence):
//  1. The explicit key parameter
//  2. Provider-specific environment variables (checked in order)
//  3. The LLM_API_KEY fallback environment variable
//
// Returns an empty string if no key is found.
func ResolveAPIKey(explicit string, providerEnvVars ...string) string {
	if explicit != "" {
		return explicit
	}

	for _, envVar := range providerEnvVars {
		if key := os.Getenv(envVar); key != "" {
			return key
		}
	}

	return os.Getenv("LLM_API_KEY")
}

// RequireAPIKey is like ResolveAPIKey but returns an error if no key is found.
// The providerName is used in the error message to help users identify which
// environment variable to set. The returned error wraps ErrMissingAPIKey and
// can be detected with errors.Is().
func RequireAPIKey(providerName, explicit string, providerEnvVars ...string) (string, error) {
	key := ResolveAPIKey(explicit, providerEnvVars...)
	if key == "" {
		if len(providerEnvVars) > 0 {
			return "", fmt.Errorf("%s: %w (set %s or LLM_API_KEY)", providerName, ErrMissingAPIKey, providerEnvVars[0])
		}
		return "", fmt.Errorf("%s: %w (set LLM_API_KEY)", providerName, ErrMissingAPIKey)
	}
	return key, nil
}

// Common environment variable names for providers.
// These constants help avoid typos and provide a single source of truth.
const (
	EnvElevenLabsAPIKey  = "ELEVENLABS_API_KEY"
	EnvFalAPIKey         = "FAL_KEY" // fal.ai's canonical variable name
	EnvOpenRouterAPIKey  = "OPENROUTER_API_KEY"
	EnvOpenAIAPIKey      = "OPENAI_API_KEY"
	EnvAnthropicAPIKey   = "ANTHROPIC_API_KEY"
	EnvGeminiAPIKey      = "GEMINI_API_KEY"
	EnvGoogleAPIKey      = "GOOGLE_API_KEY"
	EnvGroqAPIKey        = "GROQ_API_KEY"
	EnvTogetherAPIKey    = "TOGETHER_API_KEY"
	EnvFireworksAPIKey   = "FIREWORKS_API_KEY"
	EnvMistralAPIKey     = "MISTRAL_API_KEY"
	EnvDeepSeekAPIKey    = "DEEPSEEK_API_KEY"
	EnvCerebrasAPIKey    = "CEREBRAS_API_KEY"
	EnvPerplexityAPIKey  = "PERPLEXITY_API_KEY"
	EnvPPLXAPIKey        = "PPLX_API_KEY"
	EnvFeatherlessAPIKey = "FEATHERLESS_API_KEY"
	EnvSyntheticAPIKey   = "SYNTHETIC_API_KEY"
	EnvAzureAPIKey       = "AZURE_OPENAI_API_KEY"
	EnvAzureKey          = "AZURE_OPENAI_KEY"
	EnvInfinityAPIKey    = "INFINITY_API_KEY"
	EnvRunPodAPIKey      = "RUNPOD_API_KEY"
	EnvZAIAPIKey         = "ZAI_API_KEY"
	EnvLLMAPIKey         = "LLM_API_KEY"
	// EnvHuggingFaceAPIKey and EnvHuggingFaceToken are both consulted for the
	// HuggingFace Inference Endpoints token (HF_TOKEN is HuggingFace's own
	// conventional name).
	EnvHuggingFaceAPIKey = "HUGGINGFACE_API_KEY"
	EnvHuggingFaceToken  = "HF_TOKEN"
)
