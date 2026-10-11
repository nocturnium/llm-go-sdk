package llms

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	if resp == nil || len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("embed query: %w", ErrIncompleteResponse)
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

	if resp == nil {
		return nil, fmt.Errorf("embed documents: %w", ErrIncompleteResponse)
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed documents: got %d embeddings for %d inputs: %w", len(resp.Embeddings), len(texts), ErrIncompleteResponse)
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

// UnmarshalJSON accepts both the OpenAI-compatible `data` array and the
// `embeddings` key. The standard wire form uses `data`, but some providers and
// hand-written fixtures use `embeddings`; decoding only one would silently
// leave Embeddings empty for the other.
func (r *EmbeddingResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		Data       []Embedding    `json:"data"`
		Embeddings []Embedding    `json:"embeddings"`
		Model      string         `json:"model"`
		Usage      EmbeddingUsage `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Model = raw.Model
	r.Usage = raw.Usage
	if raw.Data != nil {
		r.Embeddings = raw.Data
	} else {
		r.Embeddings = raw.Embeddings
	}
	return nil
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

// UnmarshalJSON accepts both the default float-array form and the base64 string
// form that WithEncodingFormat("base64") requests. The base64 payload is the
// little-endian IEEE-754 float32 sequence OpenAI documents, so decoding only the
// array form would fail every response for a caller who asked for base64.
func (e *Embedding) UnmarshalJSON(data []byte) error {
	var raw struct {
		Index     int             `json:"index"`
		Embedding json.RawMessage `json:"embedding"`
		Object    string          `json:"object"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	vector, err := DecodeEmbeddingVector(raw.Embedding)
	if err != nil {
		return err
	}
	e.Index = raw.Index
	e.Vector = vector
	e.Object = raw.Object
	return nil
}

// DecodeEmbeddingVector decodes an embedding value that may be either a JSON
// array of floats or a base64 string of little-endian float32s. It is exported
// so OpenAI-compatible provider wire types can share the same tolerant decode.
func DecodeEmbeddingVector(raw json.RawMessage) ([]float32, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	if trimmed[0] != '"' {
		var vector []float32
		if err := json.Unmarshal(trimmed, &vector); err != nil {
			return nil, fmt.Errorf("embedding: decode float array: %w", err)
		}
		return vector, nil
	}
	var encoded string
	if err := json.Unmarshal(trimmed, &encoded); err != nil {
		return nil, fmt.Errorf("embedding: decode base64 string: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("embedding: decode base64 payload: %w", err)
	}
	if len(decoded)%4 != 0 {
		return nil, fmt.Errorf("embedding: base64 payload length %d is not a multiple of 4", len(decoded))
	}
	vector := make([]float32, len(decoded)/4)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(decoded[i*4:]))
	}
	return vector, nil
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
		if opt == nil {
			continue
		}
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
		if opt == nil {
			continue
		}
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
