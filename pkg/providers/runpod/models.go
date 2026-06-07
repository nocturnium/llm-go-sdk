package runpod

import llms "github.com/nocturnium/llm-go-sdk"

// Common model IDs for RunPod vLLM deployments.
// Note: These are example models; the actual available models depend on
// what you have deployed on your RunPod serverless endpoint.
const (
	// Llama models
	ModelLlama31_8B   = "meta-llama/Meta-Llama-3.1-8B-Instruct"
	ModelLlama31_70B  = "meta-llama/Meta-Llama-3.1-70B-Instruct"
	ModelLlama31_405B = "meta-llama/Meta-Llama-3.1-405B-Instruct"
	ModelLlama32_1B   = "meta-llama/Llama-3.2-1B-Instruct"
	ModelLlama32_3B   = "meta-llama/Llama-3.2-3B-Instruct"

	// Mistral models
	ModelMistral7B   = "mistralai/Mistral-7B-Instruct-v0.3"
	ModelMixtral8x7B = "mistralai/Mixtral-8x7B-Instruct-v0.1"

	// Qwen models
	ModelQwen25_7B  = "Qwen/Qwen2.5-7B-Instruct"
	ModelQwen25_72B = "Qwen/Qwen2.5-72B-Instruct"

	// DeepSeek models
	ModelDeepSeekR1 = "deepseek-ai/DeepSeek-R1"
)

// cachedModels provides metadata for common RunPod-deployed models.
// Note: Actual model availability depends on your deployed endpoint.
var cachedModels = []llms.ModelInfo{
	{
		ID:            ModelLlama31_8B,
		DisplayName:   "Llama 3.1 8B Instruct",
		Provider:      llms.ProviderRunPod,
		ContextLength: 131072,
		MaxOutput:     8192,
		Organization:  "Meta",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Meta's Llama 3.1 8B instruction-tuned model",
	},
	{
		ID:            ModelLlama31_70B,
		DisplayName:   "Llama 3.1 70B Instruct",
		Provider:      llms.ProviderRunPod,
		ContextLength: 131072,
		MaxOutput:     8192,
		Organization:  "Meta",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Meta's Llama 3.1 70B instruction-tuned model",
	},
	{
		ID:            ModelMistral7B,
		DisplayName:   "Mistral 7B Instruct v0.3",
		Provider:      llms.ProviderRunPod,
		ContextLength: 32768,
		MaxOutput:     8192,
		Organization:  "Mistral AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Mistral AI's 7B instruction-tuned model",
	},
	{
		ID:            ModelQwen25_7B,
		DisplayName:   "Qwen 2.5 7B Instruct",
		Provider:      llms.ProviderRunPod,
		ContextLength: 32768,
		MaxOutput:     8192,
		Organization:  "Alibaba",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Alibaba's Qwen 2.5 7B instruction-tuned model",
	},
}

// ListModels returns metadata for common RunPod-deployed models.
// Note: Actual model availability depends on your deployed endpoint.
func ListModels() []llms.ModelInfo {
	return cachedModels
}

// ModelInfo returns information about a specific model.
// Returns nil if the model is not in the cached list.
func ModelInfo(modelID string) *llms.ModelInfo {
	for _, m := range cachedModels {
		if m.ID == modelID {
			return &m
		}
	}
	return nil
}
