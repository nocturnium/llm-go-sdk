package synthetic

import (
	"context"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

// cachedModels contains metadata for models available on Synthetic.new.
// Synthetic.new is a privacy-focused AI platform with an OpenAI-compatible API
// offering access to state-of-the-art open-source LLMs including Qwen 3, DeepSeek V3,
// GLM 4.x, Kimi K2, and Llama models. Updated from https://dev.synthetic.new/docs/api/models
var cachedModels = []llms.ModelInfo{
	// === Always-On Models (Synthetic Provider) ===
	// NOTE: hf:moonshotai/Kimi-K2-Thinking was removed 2026-05-20 — the
	// Synthetic API now returns 404 for it ("no longer supported. Try
	// using a different model, like hf:zai-org/GLM-5.1"). Keeping it in
	// the cached list caused the capability-aware selector to route to
	// a dead model in ~7% of calls (Run #144 hit 2/27). The live Moonshot
	// model in this list is Kimi-K2-Instruct-0905; production routes
	// most calls through hf:moonshotai/Kimi-K2.6 via model_preferences.
	{
		ID:            "hf:zai-org/GLM-4.7",
		DisplayName:   "GLM 4.7",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Zhipu AI",
		ContextLength: 198000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	// === Always-On Models (Fireworks Provider) ===
	{
		ID:            "hf:deepseek-ai/DeepSeek-R1-0528",
		DisplayName:   "DeepSeek R1",
		Provider:      llms.ProviderSynthetic,
		Organization:  "DeepSeek",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "hf:deepseek-ai/DeepSeek-V3-0324",
		DisplayName:   "DeepSeek V3 (0324)",
		Provider:      llms.ProviderSynthetic,
		Organization:  "DeepSeek",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "hf:deepseek-ai/DeepSeek-V3.1",
		DisplayName:   "DeepSeek V3.1",
		Provider:      llms.ProviderSynthetic,
		Organization:  "DeepSeek",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "hf:deepseek-ai/DeepSeek-V3.1-Terminus",
		DisplayName:   "DeepSeek V3.1 Terminus",
		Provider:      llms.ProviderSynthetic,
		Organization:  "DeepSeek",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "hf:deepseek-ai/DeepSeek-V3.2",
		DisplayName:   "DeepSeek V3.2",
		Provider:      llms.ProviderSynthetic,
		Organization:  "DeepSeek",
		ContextLength: 159000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "hf:meta-llama/Llama-3.3-70B-Instruct",
		DisplayName:   "Llama 3.3 70B Instruct",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Meta",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "hf:meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8",
		DisplayName:   "Llama 4 Maverick 17B",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Meta",
		ContextLength: 524000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "hf:moonshotai/Kimi-K2-Instruct-0905",
		DisplayName:   "Kimi K2 Instruct",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Moonshot AI",
		ContextLength: 256000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "hf:openai/gpt-oss-120b",
		DisplayName:   "GPT-OSS 120B",
		Provider:      llms.ProviderSynthetic,
		Organization:  "OpenAI",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "hf:Qwen/Qwen3-235B-A22B-Instruct-2507",
		DisplayName:   "Qwen 3 235B Instruct",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Qwen",
		ContextLength: 256000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "hf:Qwen/Qwen3-Coder-480B-A35B-Instruct",
		DisplayName:   "Qwen 3 Coder 480B",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Qwen",
		ContextLength: 256000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "hf:Qwen/Qwen3-VL-235B-A22B-Instruct",
		DisplayName:   "Qwen 3 VL 235B",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Qwen",
		ContextLength: 250000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		FromCache:     true,
	},
	{
		ID:            "hf:zai-org/GLM-4.5",
		DisplayName:   "GLM 4.5",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Zhipu AI",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "hf:zai-org/GLM-4.6",
		DisplayName:   "GLM 4.6",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Zhipu AI",
		ContextLength: 198000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	// === Always-On Models (Together AI Provider) ===
	{
		ID:            "hf:deepseek-ai/DeepSeek-V3",
		DisplayName:   "DeepSeek V3",
		Provider:      llms.ProviderSynthetic,
		Organization:  "DeepSeek",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode},
		FromCache:     true,
	},
	{
		ID:            "hf:Qwen/Qwen3-235B-A22B-Thinking-2507",
		DisplayName:   "Qwen 3 235B Thinking",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Qwen",
		ContextLength: 256000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// === LoRA Models (Fine-tuning Adapters via Together AI) ===
	{
		ID:            "meta-llama/Llama-3.2-1B-Instruct",
		DisplayName:   "Llama 3.2 1B Instruct",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Meta",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "meta-llama/Llama-3.2-3B-Instruct",
		DisplayName:   "Llama 3.2 3B Instruct",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Meta",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "meta-llama/Meta-Llama-3.1-8B-Instruct",
		DisplayName:   "Llama 3.1 8B Instruct",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Meta",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	{
		ID:            "meta-llama/Meta-Llama-3.1-70B-Instruct",
		DisplayName:   "Llama 3.1 70B Instruct",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Meta",
		ContextLength: 128000,
		MaxOutput:     8192,
		Types:         []llms.ModelType{llms.ModelTypeChat},
		FromCache:     true,
	},
	// === Embedding Models ===
	{
		ID:            "hf:nomic-ai/nomic-embed-text-v1.5",
		DisplayName:   "Nomic Embed Text v1.5",
		Provider:      llms.ProviderSynthetic,
		Organization:  "Nomic AI",
		ContextLength: 8000,
		MaxOutput:     0,
		Types:         []llms.ModelType{llms.ModelTypeEmbedding},
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

// ListModels returns a curated list of models available on Synthetic.new.
// Note: This returns a cached subset of commonly used coding-focused models.
// Synthetic.new specializes in privacy-focused access to open-source coding LLMs.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

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
func (c *Client) ModelInfo(ctx context.Context, modelID string) (*llms.ModelInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

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

// Ensure Client implements the ModelLister interface
var _ llms.ModelLister = (*Client)(nil)
