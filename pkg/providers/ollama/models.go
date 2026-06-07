package ollama

import (
	"context"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

// ListModels retrieves available models from Ollama.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	resp, err := c.Client().ListModels(ctx)
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderOllama, "list models", err)
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
		Provider:      llms.ProviderOllama,
		ContextLength: m.ContextLength,
		Organization:  inferOrganization(m.ID),
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

func inferOrganization(modelID string) string {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "llama"):
		return "Meta"
	case strings.Contains(lower, "mistral"), strings.Contains(lower, "mixtral"):
		return "Mistral AI"
	case strings.Contains(lower, "gemma"):
		return "Google"
	case strings.Contains(lower, "phi"):
		return "Microsoft"
	case strings.Contains(lower, "qwen"):
		return "Alibaba"
	case strings.Contains(lower, "nomic"):
		return "Nomic AI"
	default:
		return "Ollama"
	}
}

func inferModelTypes(modelID string) []llms.ModelType {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "embed"):
		return []llms.ModelType{llms.ModelTypeEmbedding}
	case strings.Contains(lower, "llava"), strings.Contains(lower, "vision"):
		return []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}
	case strings.Contains(lower, "code"):
		return []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode}
	default:
		return []llms.ModelType{llms.ModelTypeChat}
	}
}

var _ llms.ModelLister = (*Client)(nil)
