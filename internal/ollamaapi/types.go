// Package ollamaapi provides types and client for Ollama's native API.
// This complements the OpenAI-compatible endpoints with Ollama-specific features.
package ollamaapi

import "time"

// Model represents a local Ollama model.
type Model struct {
	Name       string        `json:"name"`
	Model      string        `json:"model"`
	ModifiedAt time.Time     `json:"modified_at"`
	Size       int64         `json:"size"`
	Digest     string        `json:"digest"`
	Details    *ModelDetails `json:"details,omitempty"`
}

// ModelDetails contains detailed model information.
type ModelDetails struct {
	ParentModel       string   `json:"parent_model,omitempty"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families,omitempty"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// ListModelsResponse is the response from /api/tags.
type ListModelsResponse struct {
	Models []Model `json:"models"`
}

// ShowRequest is the request body for /api/show.
type ShowRequest struct {
	Name    string `json:"name"`
	Verbose bool   `json:"verbose,omitempty"`
}

// ShowResponse is the response from /api/show.
type ShowResponse struct {
	License    string        `json:"license,omitempty"`
	Modelfile  string        `json:"modelfile"`
	Parameters string        `json:"parameters,omitempty"`
	Template   string        `json:"template"`
	Details    *ModelDetails `json:"details,omitempty"`
	ModelInfo  ModelInfo     `json:"model_info,omitempty"`
}

// ModelInfo contains extended model metadata.
type ModelInfo map[string]any

// PullRequest is the request body for /api/pull.
type PullRequest struct {
	Name     string `json:"name"`
	Insecure bool   `json:"insecure,omitempty"`
	Stream   bool   `json:"stream,omitempty"`
}

// PullResponse is a streaming response from /api/pull.
type PullResponse struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
}

// DeleteRequest is the request body for /api/delete.
type DeleteRequest struct {
	Name string `json:"name"`
}

// CopyRequest is the request body for /api/copy.
type CopyRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// RunningModel represents a currently loaded model.
type RunningModel struct {
	Name      string    `json:"name"`
	Model     string    `json:"model"`
	Size      int64     `json:"size"`
	Digest    string    `json:"digest"`
	ExpiresAt time.Time `json:"expires_at"`
	SizeVRAM  int64     `json:"size_vram"`
}

// ListRunningResponse is the response from /api/ps.
type ListRunningResponse struct {
	Models []RunningModel `json:"models"`
}

// VersionResponse is the response from /api/version.
type VersionResponse struct {
	Version string `json:"version"`
}

// GenerateRequest is the request body for /api/generate.
type GenerateRequest struct {
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt"`
	Suffix    string         `json:"suffix,omitempty"`
	System    string         `json:"system,omitempty"`
	Template  string         `json:"template,omitempty"`
	Context   []int          `json:"context,omitempty"`
	Stream    bool           `json:"stream"`
	Raw       bool           `json:"raw,omitempty"`
	Format    string         `json:"format,omitempty"`
	KeepAlive string         `json:"keep_alive,omitempty"`
	Images    []string       `json:"images,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

// GenerateResponse is the response from /api/generate.
type GenerateResponse struct {
	Model              string    `json:"model"`
	CreatedAt          time.Time `json:"created_at"`
	Response           string    `json:"response"`
	Done               bool      `json:"done"`
	DoneReason         string    `json:"done_reason,omitempty"`
	Context            []int     `json:"context,omitempty"`
	TotalDuration      int64     `json:"total_duration,omitempty"`
	LoadDuration       int64     `json:"load_duration,omitempty"`
	PromptEvalCount    int       `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64     `json:"prompt_eval_duration,omitempty"`
	EvalCount          int       `json:"eval_count,omitempty"`
	EvalDuration       int64     `json:"eval_duration,omitempty"`
}

// ChatRequest is the request body for /api/chat.
type ChatRequest struct {
	Model     string         `json:"model"`
	Messages  []ChatMessage  `json:"messages"`
	Stream    bool           `json:"stream"`
	Format    string         `json:"format,omitempty"`
	KeepAlive string         `json:"keep_alive,omitempty"`
	Tools     []Tool         `json:"tools,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

// ChatMessage represents a chat message.
type ChatMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Images    []string   `json:"images,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Tool represents a function tool.
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function defines a callable function.
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall represents a tool invocation.
type ToolCall struct {
	Function FunctionCall `json:"function"`
}

// FunctionCall is a function call with arguments.
type FunctionCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ChatResponse is the response from /api/chat.
type ChatResponse struct {
	Model              string      `json:"model"`
	CreatedAt          time.Time   `json:"created_at"`
	Message            ChatMessage `json:"message"`
	Done               bool        `json:"done"`
	DoneReason         string      `json:"done_reason,omitempty"`
	TotalDuration      int64       `json:"total_duration,omitempty"`
	LoadDuration       int64       `json:"load_duration,omitempty"`
	PromptEvalCount    int         `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64       `json:"prompt_eval_duration,omitempty"`
	EvalCount          int         `json:"eval_count,omitempty"`
	EvalDuration       int64       `json:"eval_duration,omitempty"`
}

// EmbedRequest is the request body for /api/embed.
type EmbedRequest struct {
	Model     string         `json:"model"`
	Input     any            `json:"input"` // string or []string
	Truncate  bool           `json:"truncate,omitempty"`
	KeepAlive string         `json:"keep_alive,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

// EmbedResponse is the response from /api/embed.
type EmbedResponse struct {
	Model           string      `json:"model"`
	Embeddings      [][]float32 `json:"embeddings"`
	TotalDuration   int64       `json:"total_duration,omitempty"`
	LoadDuration    int64       `json:"load_duration,omitempty"`
	PromptEvalCount int         `json:"prompt_eval_count,omitempty"`
}
