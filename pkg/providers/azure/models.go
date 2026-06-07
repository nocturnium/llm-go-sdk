package azure

import (
	"context"
	"time"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

// ListModels retrieves available deployments from Azure OpenAI.
// Note: Azure OpenAI may not support the standard models endpoint.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	resp, err := c.Client().ListModels(ctx)
	if err != nil {
		// Azure might not support this endpoint; return the current deployment
		return c.getCurrentDeploymentAsModel(*options)
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

// getCurrentDeploymentAsModel returns the current deployment as a model.
func (c *Client) getCurrentDeploymentAsModel(options llms.ListModelsOptions) (*llms.ListModelsResult, error) {
	model := llms.ModelInfo{
		ID:           c.options.DeploymentName,
		DisplayName:  c.options.DeploymentName,
		Provider:     llms.ProviderAzure,
		Organization: "Microsoft Azure",
		Types:        []llms.ModelType{llms.ModelTypeChat},
		FromCache:    false,
	}

	models := []llms.ModelInfo{model}

	if len(options.Types) > 0 {
		models = llms.FilterModelsByType(models, options.Types...)
	}

	return &llms.ListModelsResult{
		Models:  models,
		HasMore: false,
	}, nil
}

// ModelInfo retrieves information for a specific deployment by ID.
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
		Provider:      llms.ProviderAzure,
		ContextLength: m.ContextLength,
		Organization:  "Microsoft Azure",
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

var _ llms.ModelLister = (*Client)(nil)
