// Package llamacppapi provides types and client for llama.cpp's native API.
package llamacppapi

// PropsResponse is the response from /props endpoint.
type PropsResponse struct {
	AssistantName             string             `json:"assistant_name,omitempty"`
	UserName                  string             `json:"user_name,omitempty"`
	DefaultGenerationSettings GenerationSettings `json:"default_generation_settings"`
	TotalSlots                int                `json:"total_slots"`
	ChatTemplate              string             `json:"chat_template,omitempty"`
}

// GenerationSettings contains default generation parameters.
type GenerationSettings struct {
	NCtx             int     `json:"n_ctx"`
	NPredict         int     `json:"n_predict"`
	Model            string  `json:"model"`
	Seed             int     `json:"seed"`
	Temperature      float64 `json:"temperature"`
	TopK             int     `json:"top_k"`
	TopP             float64 `json:"top_p"`
	MinP             float64 `json:"min_p"`
	TfsZ             float64 `json:"tfs_z"`
	TypicalP         float64 `json:"typical_p"`
	RepeatPenalty    float64 `json:"repeat_penalty"`
	RepeatLastN      int     `json:"repeat_last_n"`
	PenalizeNl       bool    `json:"penalize_nl"`
	PresencePenalty  float64 `json:"presence_penalty"`
	FrequencyPenalty float64 `json:"frequency_penalty"`
	Mirostat         int     `json:"mirostat"`
	MirostatTau      float64 `json:"mirostat_tau"`
	MirostatEta      float64 `json:"mirostat_eta"`
	Grammar          string  `json:"grammar"`
	NProbs           int     `json:"n_probs"`
	MinKeep          int     `json:"min_keep"`
}

// HealthResponse is the response from /health endpoint.
type HealthResponse struct {
	Status          string `json:"status"` // "ok", "loading model", "error", "no slot available"
	SlotsIdle       int    `json:"slots_idle"`
	SlotsProcessing int    `json:"slots_processing"`
}

// Slot represents an inference slot from /slots endpoint.
type Slot struct {
	ID          int     `json:"id"`
	State       int     `json:"state"` // 0=idle, 1=processing
	Model       string  `json:"model,omitempty"`
	Prompt      string  `json:"prompt,omitempty"`
	NCtx        int     `json:"n_ctx"`
	NPast       int     `json:"n_past"` // Tokens used in context
	NPredict    int     `json:"n_predict"`
	Seed        int     `json:"seed"`
	Temperature float64 `json:"temperature"`
	TopK        int     `json:"top_k"`
	TopP        float64 `json:"top_p"`
	NextToken   Token   `json:"next_token,omitempty"`
}

// Token represents a token with its probability.
type Token struct {
	ID    int     `json:"id"`
	Piece string  `json:"piece"`
	Prob  float64 `json:"prob,omitempty"`
}

// SlotsResponse is the response from /slots endpoint.
type SlotsResponse []Slot
