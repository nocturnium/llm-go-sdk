package runpod

import llms "github.com/nocturnium/llm-go-sdk/v3"

// Common model IDs for RunPod vLLM deployments.
// Note: These are example models; the actual available models depend on
// what you have deployed on your RunPod serverless endpoint. The IDs match
// the Hugging Face repository identifiers commonly served via vLLM.
const (
	// Llama models
	ModelLlama31_8B  = "meta-llama/Meta-Llama-3.1-8B-Instruct"
	ModelLlama31_70B = "meta-llama/Meta-Llama-3.1-70B-Instruct"
	// Deprecated: Meta-Llama-3.1-405B-Instruct is rarely deployed on serverless
	// vLLM due to its size; prefer Llama 3.3 70B or a Llama 4 MoE. Kept for
	// backward compatibility.
	ModelLlama31_405B = "meta-llama/Meta-Llama-3.1-405B-Instruct"
	ModelLlama32_1B   = "meta-llama/Llama-3.2-1B-Instruct"
	ModelLlama32_3B   = "meta-llama/Llama-3.2-3B-Instruct"
	ModelLlama33_70B  = "meta-llama/Llama-3.3-70B-Instruct"

	// Llama 4 (mixture-of-experts, natively multimodal)
	ModelLlama4Scout    = "meta-llama/Llama-4-Scout-17B-16E-Instruct"
	ModelLlama4Maverick = "meta-llama/Llama-4-Maverick-17B-128E-Instruct"

	// Mistral models
	ModelMistral7B = "mistralai/Mistral-7B-Instruct-v0.3"
	// Deprecated: Mixtral-8x7B-Instruct-v0.1 is superseded by Mistral Small 3.x
	// for most deployments. Kept for backward compatibility.
	ModelMixtral8x7B   = "mistralai/Mixtral-8x7B-Instruct-v0.1"
	ModelMistralSmall3 = "mistralai/Mistral-Small-3.2-24B-Instruct-2506"

	// Qwen models
	ModelQwen25_7B      = "Qwen/Qwen2.5-7B-Instruct"
	ModelQwen25_72B     = "Qwen/Qwen2.5-72B-Instruct"
	ModelQwen25Coder32B = "Qwen/Qwen2.5-Coder-32B-Instruct"
	ModelQwen3_32B      = "Qwen/Qwen3-32B"
	ModelQwen3_30BA3B   = "Qwen/Qwen3-30B-A3B"

	// DeepSeek models
	ModelDeepSeekR1 = "deepseek-ai/DeepSeek-R1"
	ModelDeepSeekV3 = "deepseek-ai/DeepSeek-V3"
)

// cachedModels provides metadata for common RunPod-deployed models.
// Note: Actual model availability depends on your deployed endpoint. Context
// windows reflect each model's native length per its Hugging Face model card
// (some are extendable further with YaRN; vLLM serves whatever --max-model-len
// you configure at deploy time).
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
		ID:            ModelLlama33_70B,
		DisplayName:   "Llama 3.3 70B Instruct",
		Provider:      llms.ProviderRunPod,
		ContextLength: 131072,
		MaxOutput:     8192,
		Organization:  "Meta",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Meta's Llama 3.3 70B multilingual instruction-tuned model (128K context)",
	},
	{
		ID:            ModelLlama4Scout,
		DisplayName:   "Llama 4 Scout 17B-16E Instruct",
		Provider:      llms.ProviderRunPod,
		ContextLength: 10485760,
		MaxOutput:     8192,
		Organization:  "Meta",
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		Description:   "Meta's Llama 4 Scout, a 17B-active/16-expert MoE, natively multimodal (advertised 10M context)",
	},
	{
		ID:            ModelLlama4Maverick,
		DisplayName:   "Llama 4 Maverick 17B-128E Instruct",
		Provider:      llms.ProviderRunPod,
		ContextLength: 1048576,
		MaxOutput:     8192,
		Organization:  "Meta",
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		Description:   "Meta's Llama 4 Maverick, a 17B-active/128-expert MoE, natively multimodal (1M context)",
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
		ID:            ModelMistralSmall3,
		DisplayName:   "Mistral Small 3.2 24B Instruct (2506)",
		Provider:      llms.ProviderRunPod,
		ContextLength: 131072,
		MaxOutput:     8192,
		Organization:  "Mistral AI",
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		Description:   "Mistral AI's Small 3.2 24B instruction-tuned model with improved instruction following (128K context)",
	},
	{
		ID:            ModelQwen25_7B,
		DisplayName:   "Qwen 2.5 7B Instruct",
		Provider:      llms.ProviderRunPod,
		ContextLength: 131072,
		MaxOutput:     8192,
		Organization:  "Alibaba",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Alibaba's Qwen 2.5 7B instruction-tuned model (131K context)",
	},
	{
		ID:            ModelQwen25Coder32B,
		DisplayName:   "Qwen 2.5 Coder 32B Instruct",
		Provider:      llms.ProviderRunPod,
		ContextLength: 131072,
		MaxOutput:     8192,
		Organization:  "Alibaba",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Alibaba's Qwen 2.5 Coder 32B instruction-tuned model for code (131K context)",
	},
	{
		ID:            ModelQwen3_32B,
		DisplayName:   "Qwen3 32B",
		Provider:      llms.ProviderRunPod,
		ContextLength: 32768,
		MaxOutput:     32768,
		Organization:  "Alibaba",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Alibaba's Qwen3 32B dense model with thinking/non-thinking modes (32K native, extendable to 131K via YaRN)",
	},
	{
		ID:            ModelQwen3_30BA3B,
		DisplayName:   "Qwen3 30B-A3B",
		Provider:      llms.ProviderRunPod,
		ContextLength: 32768,
		MaxOutput:     32768,
		Organization:  "Alibaba",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Alibaba's Qwen3 30B-A3B mixture-of-experts model (32K native, extendable to 131K via YaRN)",
	},
	{
		ID:            ModelDeepSeekR1,
		DisplayName:   "DeepSeek R1",
		Provider:      llms.ProviderRunPod,
		ContextLength: 131072,
		MaxOutput:     8192,
		Organization:  "DeepSeek",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "DeepSeek's R1 reasoning model (128K context)",
	},
	{
		ID:            ModelDeepSeekV3,
		DisplayName:   "DeepSeek V3",
		Provider:      llms.ProviderRunPod,
		ContextLength: 131072,
		MaxOutput:     8192,
		Organization:  "DeepSeek",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "DeepSeek's V3 671B-param mixture-of-experts model (128K context)",
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
