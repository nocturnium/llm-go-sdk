package gemini

import (
	"context"
	"errors"
	"strings"
	"unicode"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/geminiapi"
)

// titleCase capitalizes the first letter of a string.
// This is a replacement for the deprecated strings.Title for simple ASCII strings.
func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// Known Gemini model metadata that isn't available from the API.
// Pricing is per million tokens, paid Standard tier, in USD (verified against
// the official Google AI pricing page, https://ai.google.dev/gemini-api/docs/pricing,
// as of June 2026). Context windows (input/output token limits) come from the API
// response in convertGeminiModel; the current Gemini 2.x lineup is 1,048,576 input /
// 65,536 output. Gemini 2.5 Pro uses tiered pricing keyed on prompt size (the values
// here are the base <=200k tier; see per-model comments for the >200k tier).
var knownModels = map[string]modelMetadata{
	// Native media metadata; token context limits remain API-supplied.
	"gemini-2.5-flash-image":        {displayName: "gemini-2.5-flash-image", types: []llms.ModelType{llms.ModelTypeImage}},
	"gemini-3.1-flash-image":        {displayName: "gemini-3.1-flash-image", types: []llms.ModelType{llms.ModelTypeImage}},
	"gemini-3.1-flash-lite-image":   {displayName: "gemini-3.1-flash-lite-image", types: []llms.ModelType{llms.ModelTypeImage}},
	"gemini-3-pro-image":            {displayName: "gemini-3-pro-image", types: []llms.ModelType{llms.ModelTypeImage}},
	"veo-3.1-generate-preview":      {displayName: "veo-3.1-generate-preview", types: []llms.ModelType{llms.ModelTypeVideo}},
	"veo-3.1-fast-generate-preview": {displayName: "veo-3.1-fast-generate-preview", types: []llms.ModelType{llms.ModelTypeVideo}},
	"veo-3.1-lite-generate-preview": {displayName: "veo-3.1-lite-generate-preview", types: []llms.ModelType{llms.ModelTypeVideo}},
	"gemini-3.1-flash-tts-preview":  {displayName: "gemini-3.1-flash-tts-preview", types: []llms.ModelType{llms.ModelTypeAudio}},
	"gemini-2.5-flash-preview-tts":  {displayName: "gemini-2.5-flash-preview-tts", types: []llms.ModelType{llms.ModelTypeAudio}},
	"gemini-2.5-pro-preview-tts":    {displayName: "gemini-2.5-pro-preview-tts", types: []llms.ModelType{llms.ModelTypeAudio}},
	"gemini-3.5-transcribe":         {displayName: "gemini-3.5-transcribe", types: []llms.ModelType{llms.ModelTypeAudio}},

	// Gemini 3.x — current family (ai.google.dev pricing, verified 2026-08-02).
	// Context follows the 2.5 family (1,048,576 in / 65,536 out).
	"gemini-3.6-flash": {
		displayName: "Gemini 3.6 Flash",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-3.6-flash"),
	},
	"gemini-3.5-flash": {
		displayName: "Gemini 3.5 Flash",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-3.5-flash"),
	},
	// Gemini 3.5 Flash-Lite — base text/image/video shown; audio input is higher.
	"gemini-3.5-flash-lite": {
		displayName: "Gemini 3.5 Flash-Lite",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-3.5-flash-lite"),
	},
	// Gemini 3.1 Pro — tiered: Input 2.00 (<=200k) / 4.00 (>200k); Output 12.00 / 18.00.
	"gemini-3.1-pro-preview": {
		displayName: "Gemini 3.1 Pro",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-3.1-pro-preview"),
	},
	// Gemini 3.1 Flash-Lite — base text/image/video shown; audio input is higher.
	"gemini-3.1-flash-lite": {
		displayName: "Gemini 3.1 Flash-Lite",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-3.1-flash-lite"),
	},
	// Gemini 3 Flash — base text/image/video shown; audio input is higher.
	"gemini-3-flash-preview": {
		displayName: "Gemini 3 Flash",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-3-flash-preview"),
	},
	// Gemini 2.5 Pro — context 1,048,576 in / 65,536 out.
	// Tiered: Input 1.25 (<=200k) / 2.50 (>200k); Output 10.00 (<=200k) / 15.00 (>200k).
	"gemini-2.5-pro": {
		displayName: "Gemini 2.5 Pro",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-2.5-pro"),
	},
	// Gemini 2.5 Flash — context 1,048,576 in / 65,536 out. No >200k tier.
	// (Audio input is priced higher at 1.00; base text/image/video shown here.)
	"gemini-2.5-flash": {
		displayName: "Gemini 2.5 Flash",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-2.5-flash"),
	},
	// Gemini 2.5 Flash-Lite — context 1,048,576 in / 65,536 out. No >200k tier.
	// (Audio input is priced higher at 0.30; base text/image/video shown here.)
	"gemini-2.5-flash-lite": {
		displayName: "Gemini 2.5 Flash-Lite",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-2.5-flash-lite"),
	},
	"gemini-2.0-flash-exp": {
		displayName: "Gemini 2.0 Flash (Experimental)",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-2.0-flash-exp"),
	},
	// Gemini 1.5 family — no longer listed on the official Gemini API pricing
	// page (June 2026). Prices below are the last published rates and are kept
	// unchanged for back-compat with any account still granted access.
	// Gemini 1.5 Pro
	"gemini-1.5-pro": {
		displayName: "Gemini 1.5 Pro",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.5-pro"), // Up to 128K context
	},
	"gemini-1.5-pro-latest": {
		displayName: "Gemini 1.5 Pro (Latest)",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.5-pro-latest"),
	},
	"gemini-1.5-pro-001": {
		displayName: "Gemini 1.5 Pro 001",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.5-pro-001"),
	},
	"gemini-1.5-pro-002": {
		displayName: "Gemini 1.5 Pro 002",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.5-pro-002"),
	},
	// Gemini 1.5 Flash
	"gemini-1.5-flash": {
		displayName: "Gemini 1.5 Flash",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.5-flash"), // Up to 128K context
	},
	"gemini-1.5-flash-latest": {
		displayName: "Gemini 1.5 Flash (Latest)",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.5-flash-latest"),
	},
	"gemini-1.5-flash-001": {
		displayName: "Gemini 1.5 Flash 001",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.5-flash-001"),
	},
	"gemini-1.5-flash-002": {
		displayName: "Gemini 1.5 Flash 002",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.5-flash-002"),
	},
	"gemini-1.5-flash-8b": {
		displayName: "Gemini 1.5 Flash 8B",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.5-flash-8b"),
	},
	// Gemini 1.0 Pro
	"gemini-1.0-pro": {
		displayName: "Gemini 1.0 Pro",
		types:       []llms.ModelType{llms.ModelTypeChat},
		pricing:     tokenPricing("gemini-1.0-pro"),
	},
	"gemini-1.0-pro-latest": {
		displayName: "Gemini 1.0 Pro (Latest)",
		types:       []llms.ModelType{llms.ModelTypeChat},
		pricing:     tokenPricing("gemini-1.0-pro-latest"),
	},
	"gemini-pro": {
		displayName: "Gemini Pro",
		types:       []llms.ModelType{llms.ModelTypeChat},
		pricing:     tokenPricing("gemini-pro"),
	},
	// Gemini 1.0 Pro Vision
	"gemini-1.0-pro-vision": {
		displayName: "Gemini 1.0 Pro Vision",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-1.0-pro-vision"),
	},
	"gemini-pro-vision": {
		displayName: "Gemini Pro Vision",
		types:       []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:     tokenPricing("gemini-pro-vision"),
	},
	// Embedding models
	// Gemini Embedding 001 — current paid embedding model (0.15 per 1M input
	// tokens per the official pricing page, June 2026).
	"gemini-embedding-001": {
		displayName: "Gemini Embedding 001",
		types:       []llms.ModelType{llms.ModelTypeEmbedding},
		pricing:     tokenPricing("gemini-embedding-001"),
	},
	"text-embedding-004": {
		displayName: "Text Embedding 004",
		types:       []llms.ModelType{llms.ModelTypeEmbedding},
		pricing:     tokenPricing("text-embedding-004"), // Free tier available
	},
	"embedding-001": {
		displayName: "Embedding 001",
		types:       []llms.ModelType{llms.ModelTypeEmbedding},
		pricing:     tokenPricing("embedding-001"),
	},
}

type modelMetadata struct {
	displayName string
	types       []llms.ModelType
	pricing     *llms.Pricing
}

func tokenPricing(model string) *llms.Pricing {
	pricing, ok := llms.ModelTokenPricing(llms.ProviderGemini, model)
	if !ok {
		return nil
	}
	return pricing
}

// ListModels retrieves available models from the Gemini API.
// The API returns models accessible to your account.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	// Check context early for fast cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	// Build API params
	var params *geminiapi.ModelsListParams
	if options.Limit > 0 || options.Cursor != "" {
		params = &geminiapi.ModelsListParams{}
		if options.Limit > 0 {
			params.PageSize = options.Limit
		}
		if options.Cursor != "" {
			params.PageToken = options.Cursor
		}
	}

	// Fetch models from API
	resp, err := c.client.ListModels(ctx, params)
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderGemini, "list models", err)
	}

	// Convert to unified ModelInfo format
	models := make([]llms.ModelInfo, 0, len(resp.Models))
	for _, m := range resp.Models {
		info := convertGeminiModel(&m)
		models = append(models, info)
	}

	// apply type filter if specified
	if len(options.Types) > 0 {
		models = llms.FilterModelsByType(models, options.Types...)
	}

	// Determine next cursor for pagination
	var nextCursor string
	hasMore := false
	if resp.NextPageToken != "" {
		nextCursor = resp.NextPageToken
		hasMore = true
	}

	return &llms.ListModelsResult{
		Models:     models,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// ModelInfo retrieves information for a specific model by ID.
// Returns nil, llms.ErrModelNotFound if the model is not found.
func (c *Client) ModelInfo(ctx context.Context, modelID string) (*llms.ModelInfo, error) {
	// Check context early for fast cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Gemini has a dedicated endpoint for getting a single model
	resp, err := c.client.GetModel(ctx, modelID)
	if err != nil {
		wrapped := geminiapi.WrapError("get model", err)
		if errors.Is(wrapped, llms.ErrModelNotFound) {
			return nil, llms.ErrModelNotFound
		}
		return nil, wrapped
	}

	info := convertGeminiModel(resp)
	return &info, nil
}

// convertGeminiModel converts a Gemini model response to unified ModelInfo
func convertGeminiModel(m *geminiapi.ModelInfo) llms.ModelInfo {
	// Extract model ID from resource name (e.g., "models/gemini-1.5-flash" -> "gemini-1.5-flash")
	modelID := strings.TrimPrefix(m.Name, "models/")

	info := llms.ModelInfo{
		ID:            modelID,
		DisplayName:   m.DisplayName,
		Provider:      llms.ProviderGemini,
		Organization:  "Google",
		ContextLength: m.InputTokenLimit,
		MaxOutput:     m.OutputTokenLimit,
	}

	// Check if we have known metadata for this model
	if metadata, ok := knownModels[modelID]; ok {
		if info.DisplayName == "" {
			info.DisplayName = metadata.displayName
		}
		info.Types = append([]llms.ModelType(nil), metadata.types...)
		info.Pricing = metadata.pricing
	} else {
		// Infer from model ID and supported methods
		if info.DisplayName == "" {
			info.DisplayName = formatGeminiModelName(modelID)
		}
		info.Types = inferGeminiModelTypes(modelID, m.SupportedGenerationMethods)
	}

	return info
}

// formatGeminiModelName creates a display name from a model ID
func formatGeminiModelName(id string) string {
	// Replace dashes with spaces
	name := strings.ReplaceAll(id, "-", " ")

	// Capitalize "gemini"
	if strings.HasPrefix(strings.ToLower(name), "gemini") {
		name = "Gemini" + name[6:]
	}

	// Fix version numbers (e.g., "1 5" -> "1.5", "2 0" -> "2.0")
	name = strings.ReplaceAll(name, " 5 ", ".5 ")
	name = strings.ReplaceAll(name, " 0 ", ".0 ")
	name = strings.ReplaceAll(name, " 1 ", ".1 ")
	name = strings.ReplaceAll(name, " 2 ", ".2 ")

	// Capitalize common tier names
	for _, tier := range []string{"pro", "flash", "ultra", "nano"} {
		if strings.Contains(strings.ToLower(name), tier) {
			idx := strings.Index(strings.ToLower(name), tier)
			if idx >= 0 {
				name = name[:idx] + titleCase(tier) + name[idx+len(tier):]
			}
		}
	}

	return name
}

// inferGeminiModelTypes infers the model types from the model ID and supported methods
func inferGeminiModelTypes(id string, methods []string) []llms.ModelType {
	idLower := strings.ToLower(id)

	// Check if it's an embedding model
	if strings.Contains(idLower, "embedding") {
		return []llms.ModelType{llms.ModelTypeEmbedding}
	}

	// Check supported methods for generation capability
	hasGenerateContent := false
	for _, method := range methods {
		if method == "generateContent" || method == "streamGenerateContent" {
			hasGenerateContent = true
			break
		}
	}

	if !hasGenerateContent {
		// If it doesn't support content generation, might be embedding-only
		for _, method := range methods {
			if method == "embedContent" || method == "batchEmbedContents" {
				return []llms.ModelType{llms.ModelTypeEmbedding}
			}
		}
	}

	// Gemini 1.5+ and 2.0 models support vision
	if strings.HasPrefix(idLower, "gemini-1.5") ||
		strings.HasPrefix(idLower, "gemini-2") ||
		strings.Contains(idLower, "vision") {
		return []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}
	}

	// Default: chat only for older Gemini models
	if strings.HasPrefix(idLower, "gemini") {
		return []llms.ModelType{llms.ModelTypeChat}
	}

	// Unknown model type
	return []llms.ModelType{llms.ModelTypeChat}
}

// Ensure Client implements the ModelLister interface
var _ llms.ModelLister = (*Client)(nil)
