package featherless

import (
	"context"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

// cachedModels is a curated, ILLUSTRATIVE subset of the open-weight models
// Featherless AI serves via its OpenAI-compatible API. Featherless hosts tens of
// thousands of Hugging Face models, so this is intentionally a small, representative
// sample of popular models, NOT the full catalog (use ListModels against the live
// API for that). Model IDs are the underlying Hugging Face repo IDs.
//
// ContextLength here reflects the context window Featherless SERVES the model at
// (from https://api.featherless.ai/v1/models, the platform's `context_length`),
// which for most models on the serverless plans is 32768 even when the base model
// supports more; the newest large models are served at 131072 or 262144. Featherless
// has no per-model pricing field (plans are flat subscriptions), so none is recorded.
//
// Verified June 2026 against https://featherless.ai/models and the Featherless models
// API. A handful of legacy entries (e.g. Mixtral-8x7B, DeepSeek-V2.5, Command R+) are
// retained as illustrative even though they are no longer in the live serverless
// catalog; their context windows are left at the base model's native values.
var cachedModels = []llms.ModelInfo{
	// Llama 4 family — not served under canonical meta-llama/ IDs on Featherless as of
	// June 2026, so intentionally omitted rather than guessed. See notes in the PR.

	// Llama 3.x Models (Meta)
	{
		ID:            "meta-llama/Llama-3.3-70B-Instruct",
		DisplayName:   "Llama 3.3 70B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "meta-llama/Llama-3.1-8B-Instruct",
		DisplayName:   "Llama 3.1 8B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "meta-llama/Llama-3.1-70B-Instruct",
		DisplayName:   "Llama 3.1 70B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "meta-llama/Llama-3.2-3B-Instruct",
		DisplayName:   "Llama 3.2 3B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Meta",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Qwen3 family (Alibaba)
	{
		ID:            "Qwen/Qwen3-32B",
		DisplayName:   "Qwen3 32B",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "Qwen/Qwen3-235B-A22B-Thinking-2507",
		DisplayName:   "Qwen3 235B A22B Thinking 2507",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "Qwen/Qwen3-Next-80B-A3B-Instruct",
		DisplayName:   "Qwen3 Next 80B A3B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Qwen3 Coder models
	{
		ID:            "Qwen/Qwen3-Coder-30B-A3B-Instruct",
		DisplayName:   "Qwen3 Coder 30B A3B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "Qwen/Qwen3-Coder-480B-A35B-Instruct",
		DisplayName:   "Qwen3 Coder 480B A35B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Qwen",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	// Qwen 2.5 Models (still widely served)
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
	// DeepSeek Models
	{
		ID:            "deepseek-ai/DeepSeek-V3.1-Terminus",
		DisplayName:   "DeepSeek V3.1 Terminus",
		Provider:      llms.ProviderFeatherless,
		Organization:  "DeepSeek",
		ContextLength: 131072,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "deepseek-ai/DeepSeek-R1-0528",
		DisplayName:   "DeepSeek R1 (0528)",
		Provider:      llms.ProviderFeatherless,
		Organization:  "DeepSeek",
		ContextLength: 131072,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "deepseek-ai/DeepSeek-V4-Flash",
		DisplayName:   "DeepSeek V4 Flash",
		Provider:      llms.ProviderFeatherless,
		Organization:  "DeepSeek",
		ContextLength: 262144,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Mistral AI Models
	{
		ID:            "mistralai/Mistral-Small-3.2-24B-Instruct-2506",
		DisplayName:   "Mistral Small 3.2 24B Instruct (2506)",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Mistral AI",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "mistralai/Mistral-Nemo-Instruct-2407",
		DisplayName:   "Mistral Nemo Instruct (2407)",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Mistral AI",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "mistralai/Devstral-Small-2507",
		DisplayName:   "Devstral Small (2507)",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Mistral AI",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "mistralai/Magistral-Small-2509",
		DisplayName:   "Magistral Small (2509)",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Mistral AI",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Legacy Mistral entry — retained as illustrative; no longer in the live serverless
	// catalog under this exact ID. Context window left at the model's native 32K.
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
	// OpenAI gpt-oss (open-weight) Models
	{
		ID:            "openai/gpt-oss-120b",
		DisplayName:   "gpt-oss 120B",
		Provider:      llms.ProviderFeatherless,
		Organization:  "OpenAI",
		ContextLength: 131072,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "openai/gpt-oss-20b",
		DisplayName:   "gpt-oss 20B",
		Provider:      llms.ProviderFeatherless,
		Organization:  "OpenAI",
		ContextLength: 131072,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Z.ai GLM Models
	{
		ID:            "zai-org/GLM-5.2",
		DisplayName:   "GLM-5.2",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Z.ai",
		ContextLength: 262144,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "zai-org/GLM-4.6",
		DisplayName:   "GLM-4.6",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Z.ai",
		ContextLength: 202752,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Moonshot AI Kimi
	{
		ID:            "moonshotai/Kimi-K2-Instruct",
		DisplayName:   "Kimi K2 Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Moonshot AI",
		ContextLength: 32768,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// DeepSeek legacy entry — retained as illustrative; no longer in the live serverless
	// catalog under this exact ID. Context window left at the model's native 32K.
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
	// DeepSeek legacy coder entry — retained as illustrative.
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
	// Phi legacy entry — retained as illustrative; context left at the model's native 128K.
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
	// Gemma Models (Google)
	{
		ID:            "google/gemma-3-27b-it",
		DisplayName:   "Gemma 3 27B Instruct",
		Provider:      llms.ProviderFeatherless,
		Organization:  "Google",
		ContextLength: 32768,
		MaxOutput:     4096,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// Gemma 2 legacy entry — retained as illustrative; context left at the model's native 8K.
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
	// Command R+ legacy entry — retained as illustrative; no longer in the live
	// serverless catalog under this exact ID. Context left at the model's native 128K.
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
		ContextLength: 32768,
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
