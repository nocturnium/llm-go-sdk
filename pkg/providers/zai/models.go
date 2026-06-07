package zai

import (
	"context"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk"
)

// GLM model IDs for Z.AI.
const (
	// ModelGLM47 is the flagship GLM-4.7 model with 200K context and 128K max tokens
	ModelGLM47 = "glm-4.7"

	// ModelGLM47FlashX is the optimized GLM-4.7-FlashX variant
	ModelGLM47FlashX = "glm-4.7-FlashX"

	// ModelGLM47Flash is the free tier GLM-4.7-Flash model
	ModelGLM47Flash = "glm-4.7-Flash"
)

// cachedModels provides metadata for Z.AI GLM models.
var cachedModels = []llms.ModelInfo{
	{
		ID:            ModelGLM47,
		DisplayName:   "GLM-4.7",
		Provider:      llms.ProviderZAI,
		ContextLength: 200000,
		MaxOutput:     128000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Flagship GLM-4.7 model with 200K context and 128K max tokens",
	},
	{
		ID:            ModelGLM47FlashX,
		DisplayName:   "GLM-4.7-FlashX",
		Provider:      llms.ProviderZAI,
		ContextLength: 200000,
		MaxOutput:     128000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Optimized GLM-4.7-FlashX variant with 200K context",
	},
	{
		ID:            ModelGLM47Flash,
		DisplayName:   "GLM-4.7-Flash",
		Provider:      llms.ProviderZAI,
		ContextLength: 200000,
		MaxOutput:     128000,
		Organization:  "Z.AI",
		Types:         []llms.ModelType{llms.ModelTypeChat},
		Description:   "Free tier GLM-4.7-Flash model with 200K context",
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
