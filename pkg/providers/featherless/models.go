package featherless

import (
	"context"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk"
)

// cachedModels contains metadata for popular models available on Featherless.
// Featherless provides access to thousands of open-source models via an OpenAI-compatible API.
// This is a curated list of commonly used models - the actual catalog is much larger.
// Pricing on Featherless is typically $0.10-0.20 per million tokens for most models.
var cachedModels = []llms.ModelInfo{
	// Llama 3.3 Models
	{
		ID:            "meta-llama/Llama-3.3-70B-Instruct",
		DisplayName:   "Llama 3.3 70B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 128000,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Llama 3.1 Models
	{
		ID:            "meta-llama/Llama-3.1-8B-Instruct",
		DisplayName:   "Llama 3.1 8B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 128000,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "meta-llama/Llama-3.1-70B-Instruct",
		DisplayName:   "Llama 3.1 70B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 128000,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "meta-llama/Llama-3.1-405B-Instruct",
		DisplayName:   "Llama 3.1 405B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 128000,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Llama 3 Models
	{
		ID:            "meta-llama/Meta-Llama-3-8B-Instruct",
		DisplayName:   "Llama 3 8B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 8192,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "meta-llama/Meta-Llama-3-70B-Instruct",
		DisplayName:   "Llama 3 70B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 8192,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Qwen 2.5 Models
	{
		ID:            "Qwen/Qwen2.5-7B-Instruct",
		DisplayName:   "Qwen 2.5 7B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "Qwen/Qwen2.5-14B-Instruct",
		DisplayName:   "Qwen 2.5 14B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "Qwen/Qwen2.5-32B-Instruct",
		DisplayName:   "Qwen 2.5 32B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "Qwen/Qwen2.5-72B-Instruct",
		DisplayName:   "Qwen 2.5 72B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Qwen 2.5 Coder Models
	{
		ID:            "Qwen/Qwen2.5-Coder-7B-Instruct",
		DisplayName:   "Qwen 2.5 Coder 7B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "Qwen/Qwen2.5-Coder-32B-Instruct",
		DisplayName:   "Qwen 2.5 Coder 32B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	// Mistral Models
	{
		ID:            "mistralai/Mistral-7B-Instruct-v0.3",
		DisplayName:   "Mistral 7B Instruct v0.3",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Mistral AI",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "mistralai/Mixtral-8x7B-Instruct-v0.1",
		DisplayName:   "Mixtral 8x7B Instruct v0.1",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Mistral AI",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "mistralai/Mixtral-8x22B-Instruct-v0.1",
		DisplayName:   "Mixtral 8x22B Instruct v0.1",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Mistral AI",
		ContextLength: 65536,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// DeepSeek Models
	{
		ID:            "deepseek-ai/DeepSeek-V2.5",
		DisplayName:   "DeepSeek V2.5",
		Provider:      llms.ProviderFeatherless,
		Organization:  "DeepSeek",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "deepseek-ai/DeepSeek-Coder-V2-Instruct",
		DisplayName:   "DeepSeek Coder V2 Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "DeepSeek",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	// Yi Models
	{
		ID:            "01-ai/Yi-1.5-34B-Chat",
		DisplayName:   "Yi 1.5 34B Chat",
		Provider:      llms.ProviderFeatherless,
		Organization:  "01.AI",
		ContextLength: 4096,
		MaxOutput:     2048,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Phi Models
	{
		ID:            "microsoft/Phi-3-medium-128k-instruct",
		DisplayName:   "Phi 3 Medium 128K Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Microsoft",
		ContextLength: 128000,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "microsoft/Phi-3.5-mini-instruct",
		DisplayName:   "Phi 3.5 Mini Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Microsoft",
		ContextLength: 128000,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Gemma Models
	{
		ID:            "google/gemma-2-9b-it",
		DisplayName:   "Gemma 2 9B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Google",
		ContextLength: 8192,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "google/gemma-2-27b-it",
		DisplayName:   "Gemma 2 27B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Google",
		ContextLength: 8192,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Command R Models
	{
		ID:            "CohereForAI/c4ai-command-r-plus",
		DisplayName:   "Command R+",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Cohere",
		ContextLength: 128000,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// NVIDIA Models
	{
		ID:            "nvidia/Llama-3.1-Nemotron-70B-Instruct-HF",
		DisplayName:   "Llama 3.1 Nemotron 70B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "NVIDIA",
		ContextLength: 128000,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
}

// modelIndex is a map for quick lookups by model ID.
// Stores by value to avoid data races from pointer aliasing.
var modelIndex map[string]llms.ModelInfo

func init() {
	modelIndex = make(map[string]llms.ModelInfo, len(cachedModels)*2)
	for _, model := range cachedModels {
		modelIndex[model.ID] = model
		// Also index by lowercase for case-insensitive lookups
		modelIndex[strings.ToLower(model.ID)] = model
	}
}

// ListModels returns a curated list of popular models available on Featherless.
// Note: Featherless hosts thousands of open-source models. This returns a cached
// subset of commonly used models. For the full catalog, visit featherless.ai.
func (c *Client) ListModels(_ context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	options := llms.ApplyListModelsOptions(opts...)

	// Start with all cached models
	models := make([]llms.ModelInfo, len(cachedModels))
	copy(models, cachedModels)

	// apply type filter if specified
	if len(options.Types) > 0 {
		models = llms.FilterModelsByType(models, options.Types...)
	}

	// apply pagination
	start := 0
	if options.Cursor != "" {
		// Find the index after the cursor
		for i, m := range models {
			if m.ID == options.Cursor {
				start = i + 1
				break
			}
		}
	}

	// Slice from start
	if start >= len(models) {
		return &llms.ListModelsResult{
			Models:  []llms.ModelInfo{},
			HasMore: false,
		}, nil
	}
	models = models[start:]

	// apply limit
	hasMore := false
	nextCursor := ""
	if options.Limit > 0 && len(models) > options.Limit {
		nextCursor = models[options.Limit-1].ID
		models = models[:options.Limit]
		hasMore = true
	}

	return &llms.ListModelsResult{
		Models:     models,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// ModelInfo retrieves information for a specific model by ID.
// Returns nil, llms.ErrModelNotFound if the model is not found in the cached catalog.
func (c *Client) ModelInfo(_ context.Context, modelID string) (*llms.ModelInfo, error) {
	// Try exact match first
	if info, ok := modelIndex[modelID]; ok {
		return copyModelInfo(&info), nil
	}

	// Try case-insensitive match
	if info, ok := modelIndex[strings.ToLower(modelID)]; ok {
		return copyModelInfo(&info), nil
	}

	// Model not found in cache
	return nil, llms.ErrModelNotFound
}

// copyModelInfo creates a deep copy of a ModelInfo to prevent aliasing.
func copyModelInfo(info *llms.ModelInfo) *llms.ModelInfo {
	result := *info
	// Deep copy the Types slice to prevent aliasing
	if len(info.Types) > 0 {
		result.Types = make([]llms.ModelType, len(info.Types))
		copy(result.Types, info.Types)
	}
	return &result
}

// Ensure Client implements the ModelLister interface.
var _ llms.ModelLister = (*Client)(nil)
