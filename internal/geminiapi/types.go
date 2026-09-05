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
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	Contents          []Content         `json:"contents"`
	Tools             []Tool            `json:"tools,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings    []SafetySetting   `json:"safetySettings,omitempty"`
}

// Content represents a conversation message
type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

// Part represents a content part (text, image, or function call)
type Part struct {
	Text string `json:"text,omitempty"`

	// Thought marks this part as model reasoning ("thinking") rather than answer
	// content. ThoughtSignature authenticates a thought for multi-turn replay.
	Thought          bool   `json:"thought,omitempty"`
	ThoughtSignature string `json:"thoughtSignature,omitempty"`

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
	ResponseModalities []string      `json:"responseModalities,omitempty"`
	ImageConfig        *ImageConfig  `json:"imageConfig,omitempty"`
	SpeechConfig       *SpeechConfig `json:"speechConfig,omitempty"`
	// Temperature and TopP are pointers so an explicit 0.0 is serialized while an
	// unset (nil) value is omitted, letting the model apply its own default.
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"topP,omitempty"`
	TopK             int             `json:"topK,omitempty"`
	MaxOutputTokens  int             `json:"maxOutputTokens,omitempty"`
	StopSequences    []string        `json:"stopSequences,omitempty"`
	ResponseMimeType string          `json:"responseMimeType,omitempty"`
	ResponseSchema   any             `json:"responseSchema,omitempty"`
	ThinkingConfig   *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

// ThinkingConfig configures Gemini 2.5+ thinking. ThinkingBudget caps thinking
// tokens (0 disables, -1 lets the model decide dynamically); a nil budget uses
// the model default. IncludeThoughts asks the API to return thought summaries.
type ThinkingConfig struct {
	ThinkingBudget  *int `json:"thinkingBudget,omitempty"`
	IncludeThoughts bool `json:"includeThoughts,omitempty"`
}

// SafetySetting configures safety filters
type SafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// GenerateContentResponse represents the response from the Gemini API
type GenerateContentResponse struct {
	PromptFeedback *PromptFeedback `json:"promptFeedback,omitempty"`
	Candidates     []Candidate     `json:"candidates"`
	UsageMetadata  *UsageMetadata  `json:"usageMetadata,omitempty"`
	ModelVersion   string          `json:"modelVersion,omitempty"`
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
	PromptTokensDetails     []ModalityTokenCount `json:"promptTokensDetails,omitempty"`
	PromptTokenCount        int                  `json:"promptTokenCount"`
	CandidatesTokenCount    int                  `json:"candidatesTokenCount"`
	ThoughtsTokenCount      int                  `json:"thoughtsTokenCount,omitempty"`
	CachedContentTokenCount int                  `json:"cachedContentTokenCount,omitempty"`
	TotalTokenCount         int                  `json:"totalTokenCount"`
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
	Candidates    []Candidate    `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string         `json:"modelVersion,omitempty"`
}

// ExtractTextContent extracts the answer text from parts, skipping thought
// (reasoning) parts. Use ExtractThoughtContent for the reasoning text.
func ExtractTextContent(parts []Part) string {
	var text string
	for _, part := range parts {
		if part.Thought {
			continue
		}
		if part.Text != "" {
			text += part.Text
		}
	}
	return text
}

// ExtractThoughtContent extracts the model's reasoning ("thought") text from
// parts, returning "" if none are present.
func ExtractThoughtContent(parts []Part) string {
	var text string
	for _, part := range parts {
		if part.Thought && part.Text != "" {
			text += part.Text
		}
	}
	return text
}

// ExtractThoughtSignature returns the first non-empty thoughtSignature found on
// a thought (reasoning) part, or "" if none carry one. The signature
// authenticates the model's reasoning for multi-turn replay; note that Gemini
// attaches the round-trip-critical signature to the functionCall part when tool
// calling, which callers read from that part directly.
func ExtractThoughtSignature(parts []Part) string {
	for _, part := range parts {
		if part.Thought && part.ThoughtSignature != "" {
			return part.ThoughtSignature
		}
	}
	return ""
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

// ModalityTokenCount reports generateContent input tokens by modality.
type ModalityTokenCount struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

// ImageConfig selects native image aspect ratio and size.
type ImageConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
	ImageSize   string `json:"imageSize,omitempty"`
}

// SpeechConfig selects either one voice or named speaker voices.
type SpeechConfig struct {
	VoiceConfig             *VoiceConfig             `json:"voiceConfig,omitempty"`
	MultiSpeakerVoiceConfig *MultiSpeakerVoiceConfig `json:"multiSpeakerVoiceConfig,omitempty"`
}

// VoiceConfig selects a prebuilt voice.
type VoiceConfig struct {
	PrebuiltVoiceConfig PrebuiltVoiceConfig `json:"prebuiltVoiceConfig"`
}

// PrebuiltVoiceConfig identifies a Gemini voice.
type PrebuiltVoiceConfig struct {
	VoiceName string `json:"voiceName"`
}

// MultiSpeakerVoiceConfig maps script speakers to voices.
type MultiSpeakerVoiceConfig struct {
	SpeakerVoiceConfigs []SpeakerVoiceConfig `json:"speakerVoiceConfigs"`
}

// SpeakerVoiceConfig associates a speaker with a voice.
type SpeakerVoiceConfig struct {
	Speaker     string      `json:"speaker"`
	VoiceConfig VoiceConfig `json:"voiceConfig"`
}

// PredictLongRunningRequest submits a native Veo job.
type PredictLongRunningRequest struct {
	Instances  []VideoInstance `json:"instances"`
	Parameters VideoParameters `json:"parameters"`
}

// VideoImage holds an inline Veo image reference.
type VideoImage struct {
	BytesBase64Encoded string `json:"bytesBase64Encoded"`
	MIMEType           string `json:"mimeType"`
}

// VideoReferenceImage wraps a Veo reference image.
type VideoReferenceImage struct {
	Image VideoImage `json:"image"`
}

// VideoInstance describes the prompt and optional frames/references.
type VideoInstance struct {
	Prompt          string                `json:"prompt"`
	Image           *VideoImage           `json:"image,omitempty"`
	LastFrame       *VideoImage           `json:"lastFrame,omitempty"`
	ReferenceImages []VideoReferenceImage `json:"referenceImages,omitempty"`
}

// VideoParameters controls Veo output.
type VideoParameters struct {
	AspectRatio      string `json:"aspectRatio"`
	Resolution       string `json:"resolution"`
	DurationSeconds  int    `json:"durationSeconds"`
	NumberOfVideos   int    `json:"numberOfVideos"`
	Seed             *int64 `json:"seed,omitempty"`
	NegativePrompt   string `json:"negativePrompt,omitempty"`
	PersonGeneration string `json:"personGeneration,omitempty"`
}

// Operation is a Gemini long-running operation.
type Operation struct {
	Name     string                  `json:"name"`
	Done     bool                    `json:"done"`
	Error    *OperationError         `json:"error,omitempty"`
	Response *VideoOperationResponse `json:"response,omitempty"`
}

// OperationError describes an asynchronous provider failure.
type OperationError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// VideoOperationResponse holds the completed Veo result.
type VideoOperationResponse struct {
	GenerateVideoResponse GeneratedVideoResponse `json:"generateVideoResponse"`
}

// GeneratedVideoResponse contains generated files or filtering reasons.
type GeneratedVideoResponse struct {
	GeneratedSamples        []GeneratedVideoSample `json:"generatedSamples"`
	RAIMediaFilteredCount   int                    `json:"raiMediaFilteredCount"`
	RAIMediaFilteredReasons []string               `json:"raiMediaFilteredReasons"`
}

// GeneratedVideoSample points to a Gemini Files API video.
type GeneratedVideoSample struct {
	Video VideoFile `json:"video"`
}

// VideoFile identifies a downloadable video.
type VideoFile struct {
	URI string `json:"uri"`
}

// InteractionRequest submits audio for transcription.
type InteractionRequest struct {
	Model            string                      `json:"model"`
	Input            []InteractionInput          `json:"input"`
	GenerationConfig InteractionGenerationConfig `json:"generation_config"`
}

// InteractionInput contains inline or remote audio.
type InteractionInput struct {
	Type     string `json:"type"`
	MIMEType string `json:"mime_type"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// InteractionGenerationConfig holds transcription configuration.
type InteractionGenerationConfig struct {
	TranscriptionConfig TranscriptionConfig `json:"transcription_config"`
}

// TranscriptionConfig selects smart or verbatim transcription.
type TranscriptionConfig struct {
	LanguageCodes    []string `json:"language_codes,omitempty"`
	CustomVocabulary []string `json:"custom_vocabulary,omitempty"`
	// Mode is "smart" or a VerbatimMode object.
	Mode any `json:"mode"`
}

// VerbatimMode enables speaker labels and word timing.
type VerbatimMode struct {
	Type                   string   `json:"type"`
	DiarizationMode        string   `json:"diarization_mode,omitempty"`
	TimestampGranularities []string `json:"timestamp_granularities,omitempty"`
}

// Interaction is an Interactions API result, possibly still in progress.
type Interaction struct {
	Error  *InteractionError `json:"error,omitempty"`
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Usage  *InteractionUsage `json:"usage,omitempty"`
	Steps  []InteractionStep `json:"steps"`
}

// InteractionUsage reports transcription token consumption.
type InteractionUsage struct {
	// Nil means absent; a present empty array must not use aggregate output tokens.
	ModelInvocationTokenCounts []ModelInvocationTokenCount `json:"model_invocation_token_counts"`
	TotalInputTokens           int                         `json:"total_input_tokens"`
	TotalOutputTokens          int                         `json:"total_output_tokens"`
	InputTokensByModality      []InteractionModalityTokens `json:"input_tokens_by_modality"`
}

// InteractionModalityTokens reports input tokens for one modality.
type InteractionModalityTokens struct {
	Modality string `json:"modality"`
	Tokens   int    `json:"tokens"`
}

// InteractionStep contains model output content.
type InteractionStep struct {
	Type    string               `json:"type"`
	Content []InteractionContent `json:"content"`
}

// InteractionContent contains transcript text and word annotations.
type InteractionContent struct {
	Type        string           `json:"type"`
	Text        string           `json:"text"`
	Annotations []WordAnnotation `json:"annotations"`
}

// WordAnnotation describes a timed word within transcript text.
type WordAnnotation struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Speaker     string `json:"speaker"`
	StartOffset string `json:"start_offset"`
	EndOffset   string `json:"end_offset"`
	StartIndex  int    `json:"start_index"`
	EndIndex    int    `json:"end_index"`
}

// PromptFeedback describes input filtering before candidates are generated.
type PromptFeedback struct {
	BlockReason   string         `json:"blockReason,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
}

// ModelInvocationTokenCount reports candidate tokens for one model invocation.
type ModelInvocationTokenCount struct {
	CandidatesTokensDetails []InteractionModalityTokens `json:"candidates_tokens_details"`
}

// InteractionError retains an asynchronous Interactions failure message.
type InteractionError struct {
	Message string `json:"message"`
}
