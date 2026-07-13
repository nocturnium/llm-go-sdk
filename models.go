package llms

import (
	"context"
	"time"
)

// ModelType represents the category/capability of a model
type ModelType string

const (
	// ModelTypeChat indicates models that support chat/conversation
	ModelTypeChat ModelType = "chat"

	// ModelTypeCompletion indicates models that support text completion
	ModelTypeCompletion ModelType = "completion"

	// ModelTypeEmbedding indicates models that generate embeddings
	ModelTypeEmbedding ModelType = "embedding"

	// ModelTypeImage indicates models that generate or process images
	ModelTypeImage ModelType = "image"

	// ModelTypeCode indicates models optimized for code generation
	ModelTypeCode ModelType = "code"

	// ModelTypeModeration indicates models for content moderation
	ModelTypeModeration ModelType = "moderation"

	// ModelTypeRerank indicates models for reranking search results
	ModelTypeRerank ModelType = "rerank"

	// ModelTypeAudio indicates models that process or generate audio
	ModelTypeAudio ModelType = "audio"

	// ModelTypeVision indicates models that can process images as input
	ModelTypeVision ModelType = "vision"
)

// ModelInfo represents metadata about an available model.
type ModelInfo struct {
	// ID is the model identifier used in API calls (e.g., "gpt-4o", "claude-3-opus-20240229")
	ID string `json:"id"`

	// DisplayName is a human-readable name for the model
	DisplayName string `json:"display_name,omitempty"`

	// Provider indicates which provider this model belongs to
	Provider Provider `json:"provider"`

	// Types lists the capabilities of this model (chat, embedding, vision, etc.)
	Types []ModelType `json:"types,omitempty"`

	// ContextLength is the maximum context window in tokens
	ContextLength int `json:"context_length,omitempty"`

	// MaxOutput is the maximum number of output tokens supported
	MaxOutput int `json:"max_output,omitempty"`

	// Organization is the creator or organization behind the model
	Organization string `json:"organization,omitempty"`

	// Description provides details about the model's capabilities
	Description string `json:"description,omitempty"`

	// License indicates the model's license type
	License string `json:"license,omitempty"`

	// Pricing contains cost information for using this model
	Pricing *Pricing `json:"pricing,omitempty"`

	// CreatedAt is when the model was released or added
	CreatedAt time.Time `json:"created_at,omitempty"`

	// FromCache indicates whether this information came from cached data
	// rather than a live API call
	FromCache bool `json:"from_cache"`
}

// HasType returns true if the model supports the given capability type.
func (m *ModelInfo) HasType(t ModelType) bool {
	for _, mt := range m.Types {
		if mt == t {
			return true
		}
	}
	return false
}

// IsChat returns true if this model supports chat/conversation.
func (m *ModelInfo) IsChat() bool {
	return m.HasType(ModelTypeChat)
}

// IsEmbedding returns true if this model generates embeddings.
func (m *ModelInfo) IsEmbedding() bool {
	return m.HasType(ModelTypeEmbedding)
}

// IsVision returns true if this model can process images as input.
func (m *ModelInfo) IsVision() bool {
	return m.HasType(ModelTypeVision)
}

// ListModelsOptions configures the ListModels request.
type ListModelsOptions struct {
	// Types filters models to those supporting these capabilities.
	// An empty slice returns all models.
	// Multiple types are treated as OR (returns models matching any type).
	Types []ModelType

	// Dedicated filters to dedicated/serverless models (TogetherAI-specific).
	// Ignored by other providers.
	Dedicated bool

	// Limit sets the maximum number of models to return.
	// Zero means no limit (return all models).
	Limit int

	// Cursor is used for pagination to fetch the next page of results.
	// The cursor format is provider-specific:
	//   - Anthropic: Model ID (maps to after_id parameter)
	//   - Gemini: Opaque page token from ListModelsResult.NextCursor
	//   - OpenAI: Not supported (always returns all models)
	//   - TogetherAI: Not supported (always returns all models)
	//   - Featherless/Synthetic: Model ID (last model in previous page)
	//
	// Important: Do not use a cursor from one provider with another provider.
	// Use the NextCursor from ListModelsResult for subsequent requests.
	// An empty cursor starts from the beginning.
	Cursor string
}

// ListModelsOption is a functional option for configuring ListModels.
type ListModelsOption func(*ListModelsOptions)

// WithModelTypes filters models to those supporting the specified types.
// Multiple types are treated as OR (returns models matching any type).
func WithModelTypes(types ...ModelType) ListModelsOption {
	return func(o *ListModelsOptions) {
		o.Types = types
	}
}

// WithDedicatedModels filters to dedicated models only.
// This is primarily used by TogetherAI.
func WithDedicatedModels() ListModelsOption {
	return func(o *ListModelsOptions) {
		o.Dedicated = true
	}
}

// WithModelsLimit sets the maximum number of models to return.
func WithModelsLimit(limit int) ListModelsOption {
	return func(o *ListModelsOptions) {
		o.Limit = limit
	}
}

// WithModelsCursor sets the pagination cursor for fetching the next page.
// Use the NextCursor value from a previous ListModelsResult.
// An empty string starts pagination from the beginning.
func WithModelsCursor(cursor string) ListModelsOption {
	return func(o *ListModelsOptions) {
		o.Cursor = cursor
	}
}

// ApplyListModelsOptions applies functional options and returns configured options.
func ApplyListModelsOptions(opts ...ListModelsOption) *ListModelsOptions {
	options := &ListModelsOptions{}
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// ListModelsResult contains the result of listing models.
type ListModelsResult struct {
	// Models is the list of available models for this page.
	Models []ModelInfo `json:"models"`

	// NextCursor is the cursor for fetching the next page.
	// Pass this to WithModelsCursor for subsequent requests.
	// Empty when HasMore is false or pagination is not supported.
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates whether more results are available.
	// When true, use NextCursor to fetch the next page.
	HasMore bool `json:"has_more"`
}

// ModelLister is implemented by providers that can list available models.
type ModelLister interface {
	// ListModels returns available models from this provider.
	// Use functional options to filter by type, paginate, etc.
	ListModels(ctx context.Context, opts ...ListModelsOption) (*ListModelsResult, error)

	// ModelInfo returns information for a specific model by ID.
	// Returns nil, ErrModelNotFound if the model is not found.
	ModelInfo(ctx context.Context, modelID string) (*ModelInfo, error)
}

// AsModelLister attempts to cast a value to a ModelLister.
// Returns the ModelLister and true if successful, nil and false otherwise.
func AsModelLister(v any) (ModelLister, bool) {
	ml, ok := v.(ModelLister)
	return ml, ok
}

// SupportsModelListing checks if the given value supports model listing.
// Returns true if the value implements the ModelLister interface.
func SupportsModelListing(v any) bool {
	_, ok := v.(ModelLister)
	return ok
}

// Note: ErrModelNotFound is defined in errors.go
