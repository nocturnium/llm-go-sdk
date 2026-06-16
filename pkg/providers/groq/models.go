package groq

import (
	"context"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/openaicompat"
)

// ListModels retrieves available models from the Groq API.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	// Check context early for fast cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	// Fetch models from API
	resp, err := c.Client().ListModels(ctx)
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderGroq, "list models", err)
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
		HasMore: false, // Groq doesn't paginate
	}, nil
}

// ModelInfo retrieves information for a specific model by ID.
// Returns nil, llms.ErrModelNotFound if the model is not found.
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

// convertModelResponse converts a Groq model response to unified ModelInfo.
func convertModelResponse(m *openaicompat.ModelResponse) llms.ModelInfo {
	info := llms.ModelInfo{
		ID:            m.ID,
		DisplayName:   m.DisplayName,
		Provider:      llms.ProviderGroq,
		ContextLength: m.ContextLength,
		Organization:  m.Organization,
		FromCache:     false,
	}

	// Set display name to ID if not provided
	if info.DisplayName == "" {
		info.DisplayName = m.ID
	}

	// Set organization based on model name patterns
	if info.Organization == "" {
		info.Organization = inferOrganization(m.ID)
	}

	// Infer model types from ID
	info.Types = inferModelTypes(m.ID)

	// Convert created timestamp
	if m.Created > 0 {
		info.CreatedAt = time.Unix(m.Created, 0)
	}

	return info
}

// inferOrganization infers the organization from model ID.
func inferOrganization(modelID string) string {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "llama"):
		return "Meta"
	case strings.Contains(lower, "mixtral"), strings.Contains(lower, "mistral"):
		return "Mistral AI"
	case strings.Contains(lower, "gemma"):
		return "Google"
	case strings.Contains(lower, "whisper"):
		return "OpenAI"
	default:
		return "Groq"
	}
}

// inferModelTypes infers model types from the model ID.
func inferModelTypes(modelID string) []llms.ModelType {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "whisper"):
		return []llms.ModelType{llms.ModelTypeAudio}
	case strings.Contains(lower, "vision"):
		return []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}
	default:
		return []llms.ModelType{llms.ModelTypeChat}
	}
}

// Ensure Client implements the ModelLister interface.
var _ llms.ModelLister = (*Client)(nil)
