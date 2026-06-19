package zai

import (
	"context"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

// GLM model IDs for Z.AI. Model identifiers are verified against the live
// https://api.z.ai/api/paas/v4/models listing; context/output limits come from
// the official model guides at https://docs.z.ai/guides/llm.
const (
	// ModelGLM52 is the flagship GLM-5.2 model (1M context, 128K max output).
	ModelGLM52 = "glm-5.2"
	// ModelGLM51 is GLM-5.1 (200K context, 128K max output).
	ModelGLM51 = "glm-5.1"
	// ModelGLM5 is the base GLM-5 model.
	ModelGLM5 = "glm-5"
	// ModelGLM5Turbo is the fast GLM-5-Turbo variant for agentic/coding use.
	ModelGLM5Turbo = "glm-5-turbo"
	// ModelGLM47 is the GLM-4.7 model (200K context, 128K max output).
	ModelGLM47 = "glm-4.7"
	// ModelGLM46 is the GLM-4.6 model (200K context, 128K max output).
	ModelGLM46 = "glm-4.6"
	// ModelGLM45 is the GLM-4.5 model (128K context, 96K max output).
	ModelGLM45 = "glm-4.5"
	// ModelGLM45Air is the lighter GLM-4.5-Air model (128K context, 96K max output).
	ModelGLM45Air = "glm-4.5-air"

	// ModelGLM47FlashX was the GLM-4.7-FlashX variant.
	//
	// Deprecated: no longer offered by the Z.AI API (absent from the live model
	// listing). Use ModelGLM5Turbo or another current model.
	ModelGLM47FlashX = "glm-4.7-FlashX"
	// ModelGLM47Flash was the free-tier GLM-4.7-Flash model.
	//
	// Deprecated: no longer offered by the Z.AI API (absent from the live model
	// listing). Use a current model such as ModelGLM45Air.
	ModelGLM47Flash = "glm-4.7-Flash"
)

// cachedModels provides metadata for Z.AI GLM models, newest first. IDs are from
// the live /models endpoint; limits are from the official model guides.
var cachedModels = []llms.ModelInfo{
	{
		ID:            ModelGLM52,
		DisplayName:   "GLM-5.2",
		Provider:      llms.ProviderZAI,
		ContextLength: 1000000,
		MaxOutput:     128000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Flagship GLM-5.2 model with 1M context and 128K max output",
	},
	{
		ID:            ModelGLM51,
		DisplayName:   "GLM-5.1",
		Provider:      llms.ProviderZAI,
		ContextLength: 200000,
		MaxOutput:     128000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "GLM-5.1 with 200K context and 128K max output",
	},
	{
		ID:            ModelGLM5,
		DisplayName:   "GLM-5",
		Provider:      llms.ProviderZAI,
		ContextLength: 200000,
		MaxOutput:     128000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "GLM-5 base model",
	},
	{
		ID:            ModelGLM5Turbo,
		DisplayName:   "GLM-5-Turbo",
		Provider:      llms.ProviderZAI,
		ContextLength: 200000,
		MaxOutput:     128000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Fast GLM-5-Turbo variant for agentic and coding use (200K context)",
	},
	{
		ID:            ModelGLM47,
		DisplayName:   "GLM-4.7",
		Provider:      llms.ProviderZAI,
		ContextLength: 200000,
		MaxOutput:     128000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "GLM-4.7 with 200K context and 128K max output",
	},
	{
		ID:            ModelGLM46,
		DisplayName:   "GLM-4.6",
		Provider:      llms.ProviderZAI,
		ContextLength: 200000,
		MaxOutput:     128000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "GLM-4.6 with 200K context and 128K max output",
	},
	{
		ID:            ModelGLM45,
		DisplayName:   "GLM-4.5",
		Provider:      llms.ProviderZAI,
		ContextLength: 128000,
		MaxOutput:     96000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "GLM-4.5 (MoE) with 128K context and 96K max output",
	},
	{
		ID:            ModelGLM45Air,
		DisplayName:   "GLM-4.5-Air",
		Provider:      llms.ProviderZAI,
		ContextLength: 128000,
		MaxOutput:     96000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Lighter GLM-4.5-Air (MoE) with 128K context and 96K max output",
	},
}

// ListModels returns metadata for Z.AI GLM models (package-level convenience function).
func ListModels() []llms.ModelInfo {
	return cachedModels
}

// ModelInfo returns information about a specific model (package-level convenience function).
// Returns nil if the model is not in the cached list.
func ModelInfo(modelID string) *llms.ModelInfo {
	for _, m := range cachedModels {
		if m.ID == modelID {
			return &m
		}
	}
	return nil
}

// ListModels implements llms.ModelLister for the ZAI Client.
// Returns the static cached model list with optional type filtering and pagination.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	models := make([]llms.ModelInfo, len(cachedModels))
	copy(models, cachedModels)

	// apply type filter if specified
	if len(options.Types) > 0 {
		models = llms.FilterModelsByType(models, options.Types...)
	}

	return &llms.ListModelsResult{
		Models:  models,
		HasMore: false,
	}, nil
}

// ModelInfo implements llms.ModelLister for the ZAI Client.
func (c *Client) ModelInfo(ctx context.Context, modelID string) (*llms.ModelInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	for _, m := range cachedModels {
		if m.ID == modelID || strings.EqualFold(m.ID, modelID) {
			info := m
			return &info, nil
		}
	}
	return nil, llms.ErrModelNotFound
}
