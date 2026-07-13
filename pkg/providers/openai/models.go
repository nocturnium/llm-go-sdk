package openai

import (
	"context"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/openaicompat"
)

// Known OpenAI model metadata that isn't available from the API.
// Pricing is per million tokens (verified against the official OpenAI
// pricing/models docs, June 2026).
var knownModels = map[string]modelMetadata{
	// GPT-5.4 / GPT-5.5 — current flagship lineup (developers.openai.com pricing,
	// June 2026). Context windows follow the gpt-5 family (400k in / 128k out).
	"gpt-5.5": {
		displayName:   "GPT-5.5",
		contextLength: 400000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-5.5"),
	},
	"gpt-5.5-pro": {
		displayName:   "GPT-5.5 Pro",
		contextLength: 400000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-5.5-pro"),
	},
	"gpt-5.4": {
		displayName:   "GPT-5.4",
		contextLength: 400000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-5.4"),
	},
	"gpt-5.4-mini": {
		displayName:   "GPT-5.4 Mini",
		contextLength: 400000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-5.4-mini"),
	},
	"gpt-5.4-nano": {
		displayName:   "GPT-5.4 Nano",
		contextLength: 400000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-5.4-nano"),
	},
	"gpt-5.4-pro": {
		displayName:   "GPT-5.4 Pro",
		contextLength: 400000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-5.4-pro"),
	},
	// GPT-5 family
	"gpt-5": {
		displayName:   "GPT-5",
		contextLength: 400000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-5"),
	},
	"gpt-5-mini": {
		displayName:   "GPT-5 Mini",
		contextLength: 400000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-5-mini"),
	},
	"gpt-5-nano": {
		displayName:   "GPT-5 Nano",
		contextLength: 400000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-5-nano"),
	},
	// GPT-4.1 family
	"gpt-4.1": {
		displayName:   "GPT-4.1",
		contextLength: 1047576,
		maxOutput:     32768,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-4.1"),
	},
	"gpt-4.1-mini": {
		displayName:   "GPT-4.1 Mini",
		contextLength: 1047576,
		maxOutput:     32768,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-4.1-mini"),
	},
	"gpt-4.1-nano": {
		displayName:   "GPT-4.1 Nano",
		contextLength: 1047576,
		maxOutput:     32768,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-4.1-nano"),
	},
	// GPT-4o models
	"gpt-4o": {
		displayName:   "GPT-4o",
		contextLength: 128000,
		maxOutput:     16384,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-4o"),
	},
	"gpt-4o-mini": {
		displayName:   "GPT-4o Mini",
		contextLength: 128000,
		maxOutput:     16384,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-4o-mini"),
	},
	"gpt-4o-2024-11-20": {
		displayName:   "GPT-4o (2024-11-20)",
		contextLength: 128000,
		maxOutput:     16384,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-4o-2024-11-20"),
	},
	"gpt-4o-2024-08-06": {
		displayName:   "GPT-4o (2024-08-06)",
		contextLength: 128000,
		maxOutput:     16384,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-4o-2024-08-06"),
	},
	"gpt-4o-mini-2024-07-18": {
		displayName:   "GPT-4o Mini (2024-07-18)",
		contextLength: 128000,
		maxOutput:     16384,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-4o-mini-2024-07-18"),
	},
	// GPT-4 Turbo
	"gpt-4-turbo": {
		displayName:   "GPT-4 Turbo",
		contextLength: 128000,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("gpt-4-turbo"),
	},
	"gpt-4-turbo-preview": {
		displayName:   "GPT-4 Turbo Preview",
		contextLength: 128000,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       tokenPricing("gpt-4-turbo-preview"),
	},
	// GPT-4
	"gpt-4": {
		displayName:   "GPT-4",
		contextLength: 8192,
		maxOutput:     8192,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       tokenPricing("gpt-4"),
	},
	"gpt-4-32k": {
		displayName:   "GPT-4 32K",
		contextLength: 32768,
		maxOutput:     32768,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       tokenPricing("gpt-4-32k"),
	},
	// GPT-3.5
	"gpt-3.5-turbo": {
		displayName:   "GPT-3.5 Turbo",
		contextLength: 16385,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       tokenPricing("gpt-3.5-turbo"),
	},
	"gpt-3.5-turbo-16k": {
		displayName:   "GPT-3.5 Turbo 16K",
		contextLength: 16385,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       tokenPricing("gpt-3.5-turbo-16k"),
	},
	// o-series reasoning models
	"o4-mini": {
		displayName:   "o4-mini",
		contextLength: 200000,
		maxOutput:     100000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("o4-mini"),
	},
	"o3": {
		displayName:   "o3",
		contextLength: 200000,
		maxOutput:     100000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("o3"),
	},
	"o3-mini": {
		displayName:   "o3-mini",
		contextLength: 200000,
		maxOutput:     100000,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       tokenPricing("o3-mini"),
	},
	"o1": {
		displayName:   "o1",
		contextLength: 200000,
		maxOutput:     100000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       tokenPricing("o1"),
	},
	"o1-preview": {
		displayName:   "o1 Preview",
		contextLength: 128000,
		maxOutput:     32768,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       tokenPricing("o1-preview"),
	},
	"o1-mini": {
		displayName:   "o1 Mini",
		contextLength: 128000,
		maxOutput:     65536,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       tokenPricing("o1-mini"),
	},
	// Embedding models
	"text-embedding-3-large": {
		displayName:   "Text Embedding 3 Large",
		contextLength: 8191,
		types:         []llms.ModelType{llms.ModelTypeEmbedding},
		pricing:       tokenPricing("text-embedding-3-large"),
	},
	"text-embedding-3-small": {
		displayName:   "Text Embedding 3 Small",
		contextLength: 8191,
		types:         []llms.ModelType{llms.ModelTypeEmbedding},
		pricing:       tokenPricing("text-embedding-3-small"),
	},
	"text-embedding-ada-002": {
		displayName:   "Text Embedding Ada 002",
		contextLength: 8191,
		types:         []llms.ModelType{llms.ModelTypeEmbedding},
		pricing:       tokenPricing("text-embedding-ada-002"),
	},
	// Image models
	"dall-e-3": {
		displayName: "DALL-E 3",
		types:       []llms.ModelType{llms.ModelTypeImage},
	},
	"dall-e-2": {
		displayName: "DALL-E 2",
		types:       []llms.ModelType{llms.ModelTypeImage},
	},
	// Audio models
	"whisper-1": {
		displayName: "Whisper",
		types:       []llms.ModelType{llms.ModelTypeAudio},
	},
	"tts-1": {
		displayName: "TTS 1",
		types:       []llms.ModelType{llms.ModelTypeAudio},
	},
	"tts-1-hd": {
		displayName: "TTS 1 HD",
		types:       []llms.ModelType{llms.ModelTypeAudio},
	},
	// Moderation
	"text-moderation-latest": {
		displayName: "Text Moderation Latest",
		types:       []llms.ModelType{llms.ModelTypeModeration},
	},
	"text-moderation-stable": {
		displayName: "Text Moderation Stable",
		types:       []llms.ModelType{llms.ModelTypeModeration},
	},
	"omni-moderation-latest": {
		displayName: "Omni Moderation Latest",
		types:       []llms.ModelType{llms.ModelTypeModeration},
	},
}

type modelMetadata struct {
	displayName   string
	contextLength int
	maxOutput     int
	types         []llms.ModelType
	pricing       *llms.Pricing
}

func tokenPricing(model string) *llms.Pricing {
	pricing, ok := llms.ModelTokenPricing(llms.ProviderOpenAI, model)
	if !ok {
		return nil
	}
	return pricing
}

// ListModels retrieves available models from the OpenAI API.
// The API returns all models accessible to your account, including fine-tuned models.
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
		return nil, llms.WrapProviderError(llms.ProviderOpenAI, "list models", err)
	}

	// Convert to unified ModelInfo format
	models := make([]llms.ModelInfo, 0, len(resp.Data))
	for _, m := range resp.Data {
		info := convertOpenAIModel(&m)
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
		HasMore: false, // OpenAI doesn't paginate the models endpoint
	}, nil
}

// ModelInfo retrieves information for a specific model by ID.
// Returns nil, llms.ErrModelNotFound if the model is not found.
func (c *Client) ModelInfo(ctx context.Context, modelID string) (*llms.ModelInfo, error) {
	// OpenAI doesn't have a dedicated single-model endpoint that returns rich data,
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

// convertOpenAIModel converts an OpenAI model response to unified ModelInfo.
func convertOpenAIModel(m *openaicompat.ModelResponse) llms.ModelInfo {
	info := llms.ModelInfo{
		ID:       m.ID,
		Provider: llms.ProviderOpenAI,
	}

	// Set organization from owned_by field
	if m.OwnedBy != "" {
		info.Organization = m.OwnedBy
	}

	// Convert created timestamp
	if m.Created > 0 {
		info.CreatedAt = time.Unix(m.Created, 0)
	}

	// Check if we have known metadata for this model
	if metadata, ok := knownModels[m.ID]; ok {
		info.DisplayName = metadata.displayName
		info.ContextLength = metadata.contextLength
		info.MaxOutput = metadata.maxOutput
		info.Types = metadata.types
		info.Pricing = metadata.pricing
	} else {
		// Infer from model ID
		info.DisplayName = formatModelName(m.ID)
		info.Types = inferModelTypes(m.ID)
	}

	return info
}

// formatModelName creates a display name from a model ID.
func formatModelName(id string) string {
	// Handle fine-tuned models (ft:gpt-3.5-turbo:org:suffix:id)
	if strings.HasPrefix(id, "ft:") {
		parts := strings.Split(id, ":")
		if len(parts) >= 2 {
			return "Fine-tuned " + formatModelName(parts[1])
		}
	}

	idLower := strings.ToLower(id)

	// Ordered list of prefixes (longer/more specific first)
	type replacement struct {
		prefix string
		name   string
	}
	replacements := []replacement{
		// GPT-4o variants (must come before gpt-4)
		{"gpt-4o-mini", "GPT-4o Mini"},
		{"gpt-4o", "GPT-4o"},
		// GPT-4 variants
		{"gpt-4-turbo", "GPT-4 Turbo"},
		{"gpt-4-32k", "GPT-4 32K"},
		{"gpt-4", "GPT-4"},
		// GPT-3.5 variants
		{"gpt-3.5-turbo-16k", "GPT-3.5 Turbo 16K"},
		{"gpt-3.5-turbo", "GPT-3.5 Turbo"},
		// o1 models
		{"o1-preview", "o1 Preview"},
		{"o1-mini", "o1 Mini"},
		{"o1", "o1"},
		// Embedding models
		{"text-embedding-3-large", "Text Embedding 3 Large"},
		{"text-embedding-3-small", "Text Embedding 3 Small"},
		{"text-embedding-3", "Text Embedding 3"},
		{"text-embedding-ada", "Text Embedding Ada"},
		// Image models
		{"dall-e-3", "DALL-E 3"},
		{"dall-e-2", "DALL-E 2"},
		{"dall-e", "DALL-E"},
		// Audio models
		{"whisper", "Whisper"},
		{"tts-1-hd", "TTS 1 HD"},
		{"tts-1", "TTS 1"},
		{"tts", "TTS"},
		// Moderation
		{"omni-moderation", "Omni Moderation"},
		{"text-moderation", "Text Moderation"},
		// Legacy models
		{"davinci", "Davinci"},
		{"curie", "Curie"},
		{"babbage", "Babbage"},
		{"ada", "Ada"},
	}

	for _, r := range replacements {
		if strings.HasPrefix(idLower, r.prefix) {
			suffix := id[len(r.prefix):]
			if suffix != "" && suffix[0] == '-' {
				suffix = suffix[1:]
			}
			if suffix != "" {
				return r.name + " " + suffix
			}
			return r.name
		}
	}

	return id
}

// inferModelTypes infers the model types from the model ID.
func inferModelTypes(id string) []llms.ModelType {
	idLower := strings.ToLower(id)

	// Fine-tuned models
	if strings.HasPrefix(idLower, "ft:") {
		// Extract base model and infer from that
		parts := strings.Split(id, ":")
		if len(parts) >= 2 {
			return inferModelTypes(parts[1])
		}
	}

	// Chat/completion models
	if strings.HasPrefix(idLower, "gpt-4o") {
		return []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}
	}
	if strings.HasPrefix(idLower, "gpt-4") ||
		strings.HasPrefix(idLower, "gpt-3.5") ||
		strings.HasPrefix(idLower, "o1") ||
		strings.HasPrefix(idLower, "chatgpt") {
		return []llms.ModelType{llms.ModelTypeChat}
	}

	// Embedding models
	if strings.Contains(idLower, "embedding") {
		return []llms.ModelType{llms.ModelTypeEmbedding}
	}

	// Image models
	if strings.HasPrefix(idLower, "dall-e") {
		return []llms.ModelType{llms.ModelTypeImage}
	}

	// Audio models
	if strings.HasPrefix(idLower, "whisper") ||
		strings.HasPrefix(idLower, "tts") {
		return []llms.ModelType{llms.ModelTypeAudio}
	}

	// Moderation models
	if strings.Contains(idLower, "moderation") {
		return []llms.ModelType{llms.ModelTypeModeration}
	}

	// Legacy completion models
	if strings.HasPrefix(idLower, "davinci") ||
		strings.HasPrefix(idLower, "curie") ||
		strings.HasPrefix(idLower, "babbage") ||
		strings.HasPrefix(idLower, "ada") {
		return []llms.ModelType{llms.ModelTypeCompletion}
	}

	// Default: unknown type
	return nil
}

// Ensure Client implements the ModelLister interface.
var _ llms.ModelLister = (*Client)(nil)
