package mistral

import (
	"context"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/openaicompat"
)

// ListModels retrieves available models from the Mistral API.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	resp, err := c.Client().ListModels(ctx)
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderMistral, "list models", err)
	}

	models := make([]llms.ModelInfo, 0, len(resp.Data))
	for _, m := range resp.Data {
		info := convertModelResponse(&m)
		models = append(models, info)
	}

	if len(options.Types) > 0 {
		models = llms.FilterModelsByType(models, options.Types...)
	}

	if options.Limit > 0 && len(models) > options.Limit {
		models = models[:options.Limit]
	}

	return &llms.ListModelsResult{
		Models:  models,
		HasMore: false,
	}, nil
}

// ModelInfo retrieves information for a specific model by ID.
func (c *Client) ModelInfo(ctx context.Context, modelID string) (*llms.ModelInfo, error) {
	result, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	for _, m := range result.Models {
		if m.ID == modelID {
			return &m, nil
		}
	}

	return nil, llms.ErrModelNotFound
}

func convertModelResponse(m *openaicompat.ModelResponse) llms.ModelInfo {
	info := llms.ModelInfo{
		ID:            m.ID,
		DisplayName:   m.DisplayName,
		Provider:      llms.ProviderMistral,
		ContextLength: m.ContextLength,
		Organization:  "Mistral AI",
		FromCache:     false,
	}

	if info.DisplayName == "" {
		info.DisplayName = m.ID
	}

	info.Types = inferModelTypes(m.ID)

	if m.Created > 0 {
		info.CreatedAt = time.Unix(m.Created, 0)
	}

	return info
}

func inferModelTypes(modelID string) []llms.ModelType {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "embed"):
		return []llms.ModelType{llms.ModelTypeEmbedding}
	case strings.Contains(lower, "codestral"):
		return []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode}
	case strings.Contains(lower, "pixtral"):
		return []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}
	default:
		return []llms.ModelType{llms.ModelTypeChat}
	}
}

var _ llms.ModelLister = (*Client)(nil)
