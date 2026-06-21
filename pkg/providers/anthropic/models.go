package anthropic

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/internal/anthropicapi"
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

// Known Anthropic model metadata that isn't available from the API.
// Pricing is per million tokens (USD), verified against the official Anthropic
// models overview and pricing pages (June 2026).
var knownModels = map[string]modelMetadata{
	// Claude Fable 5 (current flagship; includes the full 1M-token context window
	// at standard pricing per the official long-context note).
	"claude-fable-5": {
		displayName:   "Claude Fable 5",
		contextLength: 1000000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 10.00, Output: 50.00},
	},
	// Claude Opus 4.8 (current most capable Opus-tier model)
	"claude-opus-4-8": {
		displayName:   "Claude Opus 4.8",
		contextLength: 1000000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 5.00, Output: 25.00},
	},
	// Claude Opus 4.7
	"claude-opus-4-7": {
		displayName:   "Claude Opus 4.7",
		contextLength: 1000000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 5.00, Output: 25.00},
	},
	// Claude Opus 4.6
	"claude-opus-4-6": {
		displayName:   "Claude Opus 4.6",
		contextLength: 1000000,
		maxOutput:     128000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 5.00, Output: 25.00},
	},
	// Claude Opus 4.5
	"claude-opus-4-5": {
		displayName:   "Claude Opus 4.5",
		contextLength: 200000,
		maxOutput:     64000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 5.00, Output: 25.00},
	},
	"claude-opus-4-5-20251101": {
		displayName:   "Claude Opus 4.5 (2025-11-01)",
		contextLength: 200000,
		maxOutput:     64000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 5.00, Output: 25.00},
	},
	// Claude Opus 4.1 (deprecated, retires 2026-08-05)
	"claude-opus-4-1": {
		displayName:   "Claude Opus 4.1",
		contextLength: 200000,
		maxOutput:     32000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 15.00, Output: 75.00},
	},
	"claude-opus-4-1-20250805": {
		displayName:   "Claude Opus 4.1 (2025-08-05)",
		contextLength: 200000,
		maxOutput:     32000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 15.00, Output: 75.00},
	},
	// Claude Sonnet 4.6 (best speed/intelligence balance)
	"claude-sonnet-4-6": {
		displayName:   "Claude Sonnet 4.6",
		contextLength: 1000000,
		maxOutput:     64000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 3.00, Output: 15.00},
	},
	// Claude Sonnet 4.5
	"claude-sonnet-4-5": {
		displayName:   "Claude Sonnet 4.5",
		contextLength: 200000,
		maxOutput:     64000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 3.00, Output: 15.00},
	},
	"claude-sonnet-4-5-20250929": {
		displayName:   "Claude Sonnet 4.5 (2025-09-29)",
		contextLength: 200000,
		maxOutput:     64000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 3.00, Output: 15.00},
	},
	// Claude Haiku 4.5 (fastest, near-frontier)
	"claude-haiku-4-5": {
		displayName:   "Claude Haiku 4.5",
		contextLength: 200000,
		maxOutput:     64000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 1.00, Output: 5.00},
	},
	"claude-haiku-4-5-20251001": {
		displayName:   "Claude Haiku 4.5 (2025-10-01)",
		contextLength: 200000,
		maxOutput:     64000,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 1.00, Output: 5.00},
	},
	// Claude 3.5 Sonnet (retired 2025-10-28; metadata retained for historical lookups)
	"claude-3-5-sonnet-latest": {
		displayName:   "Claude 3.5 Sonnet",
		contextLength: 200000,
		maxOutput:     8192,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 3.00, Output: 15.00},
	},
	"claude-3-5-sonnet-20241022": {
		displayName:   "Claude 3.5 Sonnet (2024-10-22)",
		contextLength: 200000,
		maxOutput:     8192,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 3.00, Output: 15.00},
	},
	"claude-3-5-sonnet-20240620": {
		displayName:   "Claude 3.5 Sonnet (2024-06-20)",
		contextLength: 200000,
		maxOutput:     8192,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 3.00, Output: 15.00},
	},
	// Claude 3.5 Haiku
	"claude-3-5-haiku-latest": {
		displayName:   "Claude 3.5 Haiku",
		contextLength: 200000,
		maxOutput:     8192,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 0.80, Output: 4.00},
	},
	"claude-3-5-haiku-20241022": {
		displayName:   "Claude 3.5 Haiku (2024-10-22)",
		contextLength: 200000,
		maxOutput:     8192,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 0.80, Output: 4.00},
	},
	// Claude 3 Opus
	"claude-3-opus-latest": {
		displayName:   "Claude 3 Opus",
		contextLength: 200000,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 15.00, Output: 75.00},
	},
	"claude-3-opus-20240229": {
		displayName:   "Claude 3 Opus (2024-02-29)",
		contextLength: 200000,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 15.00, Output: 75.00},
	},
	// Claude 3 Sonnet
	"claude-3-sonnet-20240229": {
		displayName:   "Claude 3 Sonnet (2024-02-29)",
		contextLength: 200000,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 3.00, Output: 15.00},
	},
	// Claude 3 Haiku
	"claude-3-haiku-20240307": {
		displayName:   "Claude 3 Haiku (2024-03-07)",
		contextLength: 200000,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision},
		pricing:       &llms.ModelPricing{Input: 0.25, Output: 1.25},
	},
	// Claude 2
	"claude-2.1": {
		displayName:   "Claude 2.1",
		contextLength: 200000,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       &llms.ModelPricing{Input: 8.00, Output: 24.00},
	},
	"claude-2.0": {
		displayName:   "Claude 2.0",
		contextLength: 100000,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       &llms.ModelPricing{Input: 8.00, Output: 24.00},
	},
	// Claude Instant
	"claude-instant-1.2": {
		displayName:   "Claude Instant 1.2",
		contextLength: 100000,
		maxOutput:     4096,
		types:         []llms.ModelType{llms.ModelTypeChat},
		pricing:       &llms.ModelPricing{Input: 0.80, Output: 2.40},
	},
}

type modelMetadata struct {
	displayName   string
	contextLength int
	maxOutput     int
	types         []llms.ModelType
	pricing       *llms.ModelPricing
}

// ListModels retrieves available models from the Anthropic API.
// The API returns models accessible to your account, sorted by newest first.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	// Check context early for fast cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	options := llms.ApplyListModelsOptions(opts...)

	// Build API params
	var params *anthropicapi.ModelsListParams
	if options.Limit > 0 || options.Cursor != "" {
		params = &anthropicapi.ModelsListParams{}
		if options.Limit > 0 {
			params.Limit = options.Limit
		}
		if options.Cursor != "" {
			params.AfterID = options.Cursor
		}
	}

	// Fetch models from API
	resp, err := c.client.ListModels(ctx, params)
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderAnthropic, "list models", err)
	}

	// Convert to unified ModelInfo format
	models := make([]llms.ModelInfo, 0, len(resp.Data))
	for _, m := range resp.Data {
		info := convertAnthropicModel(&m)
		models = append(models, info)
	}

	// apply type filter if specified
	if len(options.Types) > 0 {
		models = llms.FilterModelsByType(models, options.Types...)
	}

	// Determine next cursor for pagination
	var nextCursor string
	if resp.HasMore && resp.LastID != "" {
		nextCursor = resp.LastID
	}

	return &llms.ListModelsResult{
		Models:     models,
		NextCursor: nextCursor,
		HasMore:    resp.HasMore,
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

	// Anthropic has a dedicated endpoint for getting a single model
	resp, err := c.client.GetModel(ctx, modelID)
	if err != nil {
		wrapped := anthropicapi.WrapError("get model", err)
		if errors.Is(wrapped, llms.ErrModelNotFound) {
			return nil, llms.ErrModelNotFound
		}
		return nil, wrapped
	}

	info := convertAnthropicModel(resp)
	return &info, nil
}

// convertAnthropicModel converts an Anthropic model response to unified ModelInfo.
func convertAnthropicModel(m *anthropicapi.ModelInfo) llms.ModelInfo {
	info := llms.ModelInfo{
		ID:           m.ID,
		DisplayName:  m.DisplayName,
		Provider:     llms.ProviderAnthropic,
		Organization: "Anthropic",
	}

	// Parse created_at timestamp
	if m.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, m.CreatedAt); err == nil {
			info.CreatedAt = t
		}
	}

	// Check if we have known metadata for this model
	if metadata, ok := knownModels[m.ID]; ok {
		if info.DisplayName == "" {
			info.DisplayName = metadata.displayName
		}
		info.ContextLength = metadata.contextLength
		info.MaxOutput = metadata.maxOutput
		info.Types = metadata.types
		info.Pricing = metadata.pricing
	} else {
		// Infer from model ID
		if info.DisplayName == "" {
			info.DisplayName = formatClaudeModelName(m.ID)
		}
		info.Types = inferClaudeModelTypes(m.ID)
		// apply default context length for Claude models
		switch {
		case strings.HasPrefix(m.ID, "claude-3"):
			info.ContextLength = 200000
		case strings.HasPrefix(m.ID, "claude-2"):
			info.ContextLength = 200000
		case strings.HasPrefix(m.ID, "claude-instant"):
			info.ContextLength = 100000
		}
	}

	return info
}

// formatClaudeModelName creates a display name from a model ID.
func formatClaudeModelName(id string) string {
	// Handle common patterns
	name := id

	// Replace dashes with spaces and fix casing
	name = strings.ReplaceAll(name, "-", " ")

	// Capitalize "claude"
	if strings.HasPrefix(strings.ToLower(name), "claude") {
		name = "Claude" + name[6:]
	}

	// Fix version numbers (e.g., "3 5" -> "3.5")
	name = strings.ReplaceAll(name, " 5 ", ".5 ")
	name = strings.ReplaceAll(name, " 0 ", ".0 ")
	name = strings.ReplaceAll(name, " 1 ", ".1 ")
	name = strings.ReplaceAll(name, " 2 ", ".2 ")

	// Capitalize model tier names
	for _, tier := range []string{"opus", "sonnet", "haiku", "instant"} {
		if strings.Contains(strings.ToLower(name), tier) {
			idx := strings.Index(strings.ToLower(name), tier)
			if idx >= 0 {
				name = name[:idx] + titleCase(tier) + name[idx+len(tier):]
			}
		}
	}

	return name
}

// inferClaudeModelTypes infers the model types from the model ID.
func inferClaudeModelTypes(id string) []llms.ModelType {
	idLower := strings.ToLower(id)

	// All Claude 3+ models support vision
	if strings.HasPrefix(idLower, "claude-3") {
		return []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}
	}

	// Claude 2 and Instant are chat-only
	if strings.HasPrefix(idLower, "claude-2") ||
		strings.HasPrefix(idLower, "claude-instant") {
		return []llms.ModelType{llms.ModelTypeChat}
	}

	// Default: chat
	return []llms.ModelType{llms.ModelTypeChat}
}

// Ensure Client implements the ModelLister interface.
var _ llms.ModelLister = (*Client)(nil)
