package togetherai

import (
	"context"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/openaicompat"
)

// ListModels retrieves available models from the TogetherAI API.
// Supports filtering by model type and the dedicated option.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	// Check context early for fast cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	// Build query parameters
	queryParams := make(map[string]string)
	if options.Dedicated {
		queryParams["dedicated"] = "true"
	}

	// Fetch models from API
	var resp *openaicompat.ModelsListResponse
	var err error

	if len(queryParams) > 0 {
		resp, err = c.Client().ListModelsWithQuery(ctx, queryParams)
	} else {
		resp, err = c.Client().ListModels(ctx)
	}

	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderTogetherAI, "list models", err)
	}

	// Convert to unified ModelInfo format
	models := make([]llms.ModelInfo, 0, len(resp.Data))
	for _, m := range resp.Data {
		info := convertModelResponse(&m)
		models = append(models, info)
	}

	// apply type filter if specified
	if len(options.Types) > 0 {
		models = llms.FilterModelsByType(models, options.Types...)
	}

	// apply limit if specified
	if options.Limit > 0 && len(models) > options.Limit {
		models = models[:options.Limit]
	}

	return &llms.ListModelsResult{
		Models:  models,
		HasMore: false, // TogetherAI doesn't paginate
	}, nil
}

// ModelInfo retrieves information for a specific model by ID.
// Returns nil, llms.ErrModelNotFound if the model is not found.
func (c *Client) ModelInfo(ctx context.Context, modelID string) (*llms.ModelInfo, error) {
	// TogetherAI doesn't have a dedicated single-model endpoint,
	// so we fetch all models and filter
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

// convertModelResponse converts a TogetherAI model response to unified ModelInfo
func convertModelResponse(m *openaicompat.ModelResponse) llms.ModelInfo {
	info := llms.ModelInfo{
		ID:            m.ID,
		DisplayName:   m.DisplayName,
		Provider:      llms.ProviderTogetherAI,
		ContextLength: m.ContextLength,
		Organization:  m.Organization,
		License:       m.License,
		FromCache:     false,
	}

	// Set display name to ID if not provided
	if info.DisplayName == "" {
		// Extract readable name from ID (e.g., "meta-llama/Llama-3.3-70B" -> "Llama-3.3-70B")
		parts := strings.Split(m.ID, "/")
		if len(parts) > 1 {
			info.DisplayName = parts[len(parts)-1]
		} else {
			info.DisplayName = m.ID
		}
	}

	// Set organization from ID if not provided
	if info.Organization == "" && strings.Contains(m.ID, "/") {
		parts := strings.Split(m.ID, "/")
		info.Organization = parts[0]
	}

	// Convert model type
	info.Types = convertModelType(m.Type)

	// Convert pricing if available
	if m.Pricing != nil {
		info.Pricing = m.Pricing
	}

	// Convert created timestamp
	if m.Created > 0 {
		info.CreatedAt = time.Unix(m.Created, 0)
	}

	return info
}

// convertModelType converts TogetherAI model type string to llms.ModelType slice
func convertModelType(typeStr string) []llms.ModelType {
	switch strings.ToLower(typeStr) {
	case "chat":
		return []llms.ModelType{llms.ModelTypeChat}
	case "language":
		return []llms.ModelType{llms.ModelTypeCompletion}
	case "code":
		return []llms.ModelType{llms.ModelTypeCode, llms.ModelTypeChat}
	case "image":
		return []llms.ModelType{llms.ModelTypeImage}
	case "embedding":
		return []llms.ModelType{llms.ModelTypeEmbedding}
	case "moderation":
		return []llms.ModelType{llms.ModelTypeModeration}
	case "rerank":
		return []llms.ModelType{llms.ModelTypeRerank}
	default:
		// Default to chat for unknown types
		if typeStr != "" {
			return []llms.ModelType{llms.ModelTypeChat}
		}
		return nil
	}
}

// Ensure Client implements the ModelLister interface
var _ llms.ModelLister = (*Client)(nil)
