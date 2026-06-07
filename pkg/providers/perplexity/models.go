package perplexity

import (
	"context"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

// ListModels retrieves available models from the Perplexity API.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	resp, err := c.Client().ListModels(ctx)
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderPerplexity, "list models", err)
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
		Provider:      llms.ProviderPerplexity,
		ContextLength: m.ContextLength,
		Organization:  "Perplexity AI",
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
	// All Perplexity sonar models have search capabilities
	if strings.Contains(lower, "sonar") && strings.Contains(lower, "online") {
		return []llms.ModelType{llms.ModelTypeChat}
	}
	return []llms.ModelType{llms.ModelTypeChat}
}

var _ llms.ModelLister = (*Client)(nil)
