package cerebras

import (
	"context"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// ListModels retrieves available models from the Cerebras API.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	resp, err := c.Client().ListModels(ctx)
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderCerebras, "list models", err)
	}

	models := make([]llms.ModelInfo, 0, len(resp.Data))
	for _, m := range resp.Data {
		info := convertModelResponse(&m)
		models = append(models, info)
	}

	if len(options.Types) > 0 {
		models = llms.FilterModelsByType(models, options.Types...)
	}

	hasMore := false
	if options.Limit > 0 && len(models) > options.Limit {
		models = models[:options.Limit]
		hasMore = true
	}

	return &llms.ListModelsResult{
		Models:  models,
		HasMore: hasMore,
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

// modelOrganization prefers the publisher the API reports; the id-substring
// guess is the fallback for a response that omits it.
func modelOrganization(m *openaicompat.ModelResponse) string {
	if m.OwnedBy != "" {
		return m.OwnedBy
	}
	return inferOrganization(m.ID)
}

func convertModelResponse(m *openaicompat.ModelResponse) llms.ModelInfo {
	info := llms.ModelInfo{
		ID:            m.ID,
		DisplayName:   m.DisplayName,
		Provider:      llms.ProviderCerebras,
		ContextLength: m.ContextLength,
		Organization:  modelOrganization(m),
		FromCache:     false,
	}

	if info.DisplayName == "" {
		info.DisplayName = m.ID
	}

	info.Types = []llms.ModelType{llms.ModelTypeChat}

	if m.Created > 0 {
		info.CreatedAt = time.Unix(m.Created, 0)
	}

	return info
}

func inferOrganization(modelID string) string {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "llama"):
		return "Meta"
	default:
		return "Cerebras"
	}
}

var _ llms.ModelLister = (*Client)(nil)
