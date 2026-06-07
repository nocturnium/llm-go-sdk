package llms

import (
	"context"
	"errors"
)

// Common embeddings errors.
var (
	ErrEmbeddingsNotSupported = errors.New("embeddings not supported by this provider")
	ErrEmptyInput             = errors.New("input text is empty")
	ErrEmbeddingModelRequired = errors.New("embedding model not specified: use WithEmbeddingModel option or WithEmbedModel call option")
)

// Embedder is an interface for providers that support text embeddings.
// Not all LLM providers support embeddings. Use SupportsEmbeddings to check
// if a provider implements this interface.
//
// Example:
//
//	client, _ := openai.New()
//	if embedder, ok := llms.AsEmbedder(client); ok {
//	    embeddings, err := embedder.Embed(ctx, []string{"Hello", "World"})
//	    // ...
//	}
type Embedder interface {
	// Embed generates embeddings for one or more texts
	Embed(ctx context.Context, texts []string, options ...EmbedOption) (*EmbeddingResponse, error)
}

// EmbedQuery embeds a single query text using the provided Embedder.
// It returns the first embedding vector from the response.
func EmbedQuery(ctx context.Context, e Embedder, text string, options ...EmbedOption) ([]float32, error) {
	resp, err := e.Embed(ctx, []string{text}, options...)
	if err != nil {
		return nil, err
	}
	if len(resp.Embeddings) == 0 {
		return nil, ErrEmptyInput
	}
	return resp.Embeddings[0].Vector, nil
}

// EmbedDocuments embeds multiple document texts using the provided Embedder.
// It returns the embedding vectors in response order.
func EmbedDocuments(ctx context.Context, e Embedder, texts []string, options ...EmbedOption) ([][]float32, error) {
	resp, err := e.Embed(ctx, texts, options...)
	if err != nil {
		return nil, err
	}

	result := make([][]float32, len(resp.Embeddings))
	for i, emb := range resp.Embeddings {
		result[i] = emb.Vector
	}
	return result, nil
}

// SupportsEmbeddings checks if the given value supports embeddings.
// Returns true if the value implements the Embedder interface.
func SupportsEmbeddings(v any) bool {
	_, ok := v.(Embedder)
	return ok
}

// AsEmbedder attempts to cast a value to an Embedder.
// Returns the Embedder and true if successful, nil and false otherwise.
// This is useful for providers that may or may not support embeddings.
func AsEmbedder(v any) (Embedder, bool) {
	embedder, ok := v.(Embedder)
	return embedder, ok
}

// EmbeddingResponse represents the response from an embeddings API call.
type EmbeddingResponse struct {
	// Embeddings contains the vector representations
	Embeddings []Embedding `json:"embeddings"`

	// Model is the model used to generate the embeddings
	Model string `json:"model,omitempty"`

	// Usage contains token usage information
	Usage EmbeddingUsage `json:"usage"`
}

// Embedding represents a single embedding vector.
type Embedding struct {
	// Index is the index of the input text this embedding corresponds to
	Index int `json:"index"`

	// Vector is the embedding vector
	Vector []float32 `json:"embedding"`

	// Object type (usually "embedding")
	Object string `json:"object,omitempty"`
}

// EmbeddingUsage contains token usage for embeddings.
type EmbeddingUsage struct {
	// PromptTokens is the number of tokens in the input
	PromptTokens int `json:"prompt_tokens"`

	// TotalTokens is the total number of tokens used
	TotalTokens int `json:"total_tokens"`
}

// EmbedOptions contains options for embedding requests.
type EmbedOptions struct {
	// Model overrides the default model for this request
	Model string

	// Dimensions specifies the number of dimensions for the output embeddings
	// Only supported by some models (e.g., text-embedding-3-small, text-embedding-3-large)
	Dimensions int

	// EncodingFormat specifies the format for embeddings (e.g., "float", "base64")
	// Default is "float"
	EncodingFormat string

	// User is an optional unique identifier for the end-user
	User string

	// TaskType specifies the type of task (for Gemini)
	// Options: RETRIEVAL_QUERY, RETRIEVAL_DOCUMENT, SEMANTIC_SIMILARITY, CLASSIFICATION, CLUSTERING
	TaskType string
}

// EmbedOption is a function that modifies EmbedOptions.
type EmbedOption func(*EmbedOptions)

// ApplyEmbedOptions applies the given options and returns the resulting EmbedOptions.
func ApplyEmbedOptions(options ...EmbedOption) *EmbedOptions {
	opts := &EmbedOptions{}
	for _, opt := range options {
		opt(opts)
	}
	return opts
}

// WithEmbedModel sets the model for the embedding request.
func WithEmbedModel(model string) EmbedOption {
	return func(o *EmbedOptions) {
		o.Model = model
	}
}

// WithDimensions sets the output dimensions for embeddings.
func WithDimensions(dimensions int) EmbedOption {
	return func(o *EmbedOptions) {
		o.Dimensions = dimensions
	}
}

// WithEncodingFormat sets the encoding format for embeddings.
func WithEncodingFormat(format string) EmbedOption {
	return func(o *EmbedOptions) {
		o.EncodingFormat = format
	}
}

// WithEmbedUser sets the user identifier for the embedding request.
func WithEmbedUser(user string) EmbedOption {
	return func(o *EmbedOptions) {
		o.User = user
	}
}

// WithTaskType sets the task type for embeddings (Gemini-specific).
func WithTaskType(taskType string) EmbedOption {
	return func(o *EmbedOptions) {
		o.TaskType = taskType
	}
}

// Task types for Gemini embeddings.
const (
	TaskTypeRetrievalQuery    = "RETRIEVAL_QUERY"
	TaskTypeRetrievalDocument = "RETRIEVAL_DOCUMENT"
	TaskTypeSemantic          = "SEMANTIC_SIMILARITY"
	TaskTypeClassification    = "CLASSIFICATION"
	TaskTypeClustering        = "CLUSTERING"
)

// ValidateEmbedInput validates embedding input texts.
func ValidateEmbedInput(texts []string) error {
	if len(texts) == 0 {
		return ErrEmptyInput
	}
	for _, text := range texts {
		if text == "" {
			return ErrEmptyInput
		}
	}
	return nil
}

// Reranker is an interface for providers that support document reranking.
// Reranking scores and sorts documents by relevance to a query.
//
// Example:
//
//	client, _ := infinity.New()
//	if reranker, ok := llms.AsReranker(client); ok {
//	    result, err := reranker.Rerank(ctx, "What is machine learning?",
//	        []string{"ML is a subset of AI", "The weather is nice"},
//	    )
//	    // result.Results is sorted by relevance score
//	}
type Reranker interface {
	// Rerank scores and ranks documents by relevance to the query.
	// Returns results sorted by relevance score in descending order.
	Rerank(ctx context.Context, query string, documents []string, options ...RerankOption) (*RerankResponse, error)
}

// SupportsReranking checks if the given value supports reranking.
func SupportsReranking(v any) bool {
	_, ok := v.(Reranker)
	return ok
}

// AsReranker attempts to cast a value to a Reranker.
func AsReranker(v any) (Reranker, bool) {
	reranker, ok := v.(Reranker)
	return reranker, ok
}

// RerankResponse represents the response from a reranking API call.
type RerankResponse struct {
	// Results contains the reranked documents with scores
	Results []RerankResult `json:"results"`

	// Model is the model used for reranking
	Model string `json:"model,omitempty"`

	// Usage contains token usage information
	Usage RerankUsage `json:"usage,omitempty"`
}

// RerankResult represents a single reranked document.
type RerankResult struct {
	// Index is the original index of the document in the input
	Index int `json:"index"`

	// Score is the relevance score (higher = more relevant)
	Score float64 `json:"relevance_score"`

	// Document is the original document text (optional, returned if requested)
	Document string `json:"document,omitempty"`
}

// RerankUsage contains token usage for reranking.
type RerankUsage struct {
	// TotalTokens is the total number of tokens processed
	TotalTokens int `json:"total_tokens,omitempty"`
}

// RerankOptions contains options for reranking requests.
type RerankOptions struct {
	// Model overrides the default reranking model
	Model string

	// TopN limits the number of results returned (default: all)
	TopN int

	// ReturnDocuments includes the document text in results
	ReturnDocuments bool
}

// RerankOption is a function that modifies RerankOptions.
type RerankOption func(*RerankOptions)

// ApplyRerankOptions applies the given options and returns the resulting RerankOptions.
func ApplyRerankOptions(options ...RerankOption) *RerankOptions {
	opts := &RerankOptions{}
	for _, opt := range options {
		opt(opts)
	}
	return opts
}

// WithRerankModel sets the model for the rerank request.
func WithRerankModel(model string) RerankOption {
	return func(o *RerankOptions) {
		o.Model = model
	}
}

// WithTopN limits the number of reranked results returned.
func WithTopN(n int) RerankOption {
	return func(o *RerankOptions) {
		o.TopN = n
	}
}

// WithReturnDocuments includes the document text in rerank results.
func WithReturnDocuments(include bool) RerankOption {
	return func(o *RerankOptions) {
		o.ReturnDocuments = include
	}
}
