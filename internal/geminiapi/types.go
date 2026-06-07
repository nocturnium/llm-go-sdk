// Package geminiapi provides types and a client for the Google Gemini API.
//
// This package implements the Gemini generateContent API including:
//   - Request/response types with the contents/parts structure
//   - Streaming via streamGenerateContent with SSE
//   - Function calling with FunctionDeclaration and FunctionCall/FunctionResponse parts
package geminiapi

import "encoding/json"

// GenerateContentRequest represents a request to the Gemini API
type GenerateContentRequest struct {
	Contents         []Content         `json:"contents"`
	Tools            []Tool            `json:"tools,omitempty"`
	GenerationConfig *GenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings   []SafetySetting   `json:"safetySettings,omitempty"`
}

// Content represents a conversation message
type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part represents a content part (text, image, or function call)
type Part struct {
	Text string `json:"text,omitempty"`

	// Inline data for images
	InlineData *InlineData `json:"inlineData,omitempty"`

	// Function call from model
	FunctionCall *FunctionCall `json:"functionCall,omitempty"`

	// Function response from user
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

// InlineData represents inline data like images
type InlineData struct {
	MimeType string `json:"mimeType"` // e.g., "image/png", "image/jpeg"
	Data     string `json:"data"`     // Base64-encoded data
}

// FunctionCall represents a function call from the model
type FunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// FunctionResponse represents a function response from the user
type FunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// Tool represents a tool definition
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// FunctionDeclaration defines a function that can be called
type FunctionDeclaration struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
}

// GenerationConfig contains generation settings
type GenerationConfig struct {
	// Temperature and TopP are pointers so an explicit 0.0 is serialized while an
	// unset (nil) value is omitted, letting the model apply its own default.
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"topP,omitempty"`
	TopK             int      `json:"topK,omitempty"`
	MaxOutputTokens  int      `json:"maxOutputTokens,omitempty"`
	StopSequences    []string `json:"stopSequences,omitempty"`
	ResponseMimeType string   `json:"responseMimeType,omitempty"`
	ResponseSchema   any      `json:"responseSchema,omitempty"`
}

// SafetySetting configures safety filters
type SafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// GenerateContentResponse represents the response from the Gemini API
type GenerateContentResponse struct {
	Candidates    []Candidate    `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string         `json:"modelVersion,omitempty"`
}

// Candidate represents a response candidate
type Candidate struct {
	Content       *Content       `json:"content,omitempty"`
	FinishReason  string         `json:"finishReason,omitempty"`
	Index         int            `json:"index,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
}

// SafetyRating represents a safety rating
type SafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

// UsageMetadata contains token usage information
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
	Candidates    []Candidate    `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string         `json:"modelVersion,omitempty"`
}

// ExtractTextContent extracts all text from parts
func ExtractTextContent(parts []Part) string {
	var text string
	for _, part := range parts {
		if part.Text != "" {
			text += part.Text
		}
	}
	return text
}

// FunctionCallToJSON converts a FunctionCall's Args to JSON string
func FunctionCallToJSON(fc *FunctionCall) string {
	if fc == nil || fc.Args == nil {
		return "{}"
	}
	data, err := json.Marshal(fc.Args)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// EmbedContentRequest represents a request to the Gemini embeddings API
type EmbedContentRequest struct {
	Model    string           `json:"model,omitempty"`
	Content  EmbeddingContent `json:"content"`
	TaskType string           `json:"taskType,omitempty"`
	Title    string           `json:"title,omitempty"`
}

// EmbeddingContent represents the content to embed
type EmbeddingContent struct {
	Parts []EmbeddingPart `json:"parts"`
}

// EmbeddingPart represents a text part for embedding
type EmbeddingPart struct {
	Text string `json:"text"`
}

// EmbedContentResponse represents the response from the Gemini embeddings API
type EmbedContentResponse struct {
	Embedding EmbeddingValues `json:"embedding"`
}

// EmbeddingValues contains the embedding vector
type EmbeddingValues struct {
	Values []float32 `json:"values"`
}

// BatchEmbedContentsRequest represents a batch embedding request
type BatchEmbedContentsRequest struct {
	Requests []EmbedContentRequest `json:"requests"`
}

// BatchEmbedContentsResponse represents a batch embedding response
type BatchEmbedContentsResponse struct {
	Embeddings []EmbeddingValues `json:"embeddings"`
}

// ModelInfo represents a model in the Gemini API response
type ModelInfo struct {
	Name                       string   `json:"name"`                       // Resource name (e.g., "models/gemini-1.5-flash")
	BaseModelID                string   `json:"baseModelId,omitempty"`      // Base model identifier
	Version                    string   `json:"version,omitempty"`          // Version number
	DisplayName                string   `json:"displayName,omitempty"`      // Human-readable name
	Description                string   `json:"description,omitempty"`      // Model description
	InputTokenLimit            int      `json:"inputTokenLimit,omitempty"`  // Max input tokens
	OutputTokenLimit           int      `json:"outputTokenLimit,omitempty"` // Max output tokens
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"` // e.g., ["generateContent", "countTokens"]
	Temperature                float64  `json:"temperature,omitempty"`      // Default temperature
	MaxTemperature             float64  `json:"maxTemperature,omitempty"`   // Maximum temperature
	TopP                       float64  `json:"topP,omitempty"`             // Default top-p
	TopK                       *int     `json:"topK,omitempty"`             // Default top-k (nil if unsupported)
}

// ModelsListResponse represents the response from listing models
type ModelsListResponse struct {
	Models        []ModelInfo `json:"models"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
}

// ModelsListParams represents query parameters for listing models
type ModelsListParams struct {
	PageSize  int    // Max results per page (default 50, max 1000)
	PageToken string // Pagination token
}
