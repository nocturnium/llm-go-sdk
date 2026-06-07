package llamacpp

import (
	"context"
)

// SlotState represents the state of an inference slot.
type SlotState int

const (
	// SlotIdle indicates the slot is idle and available.
	SlotIdle SlotState = 0
	// SlotProcessing indicates the slot is currently processing a request.
	SlotProcessing SlotState = 1
)

// String returns a human-readable representation of the slot state.
func (s SlotState) String() string {
	switch s {
	case SlotIdle:
		return "idle"
	case SlotProcessing:
		return "processing"
	default:
		return "unknown"
	}
}

// HealthStatus represents the server health status.
type HealthStatus struct {
	Status      string // "ok", "loading model", "error", "no slot available"
	SlotsIdle   int
	SlotsActive int
}

// ModelProps contains properties of the loaded model.
type ModelProps struct {
	ModelName    string
	TotalSlots   int
	ChatTemplate string
	NCtx         int // Context window size
	NPredict     int // Max tokens to predict
	Temperature  float64
	TopP         float64
	TopK         int
}

// Slot represents an inference slot.
type Slot struct {
	ID         int
	State      SlotState
	Model      string
	NCtx       int
	TokensUsed int // Tokens currently in context (n_past)
	Prompt     string
}

// Health returns the server health status.
func (c *Client) Health(ctx context.Context) (*HealthStatus, error) {
	resp, err := c.nativeClient.GetHealth(ctx)
	if err != nil {
		return nil, err
	}

	return &HealthStatus{
		Status:      resp.Status,
		SlotsIdle:   resp.SlotsIdle,
		SlotsActive: resp.SlotsProcessing,
	}, nil
}

// ModelProps returns properties of the loaded model.
func (c *Client) ModelProps(ctx context.Context) (*ModelProps, error) {
	resp, err := c.nativeClient.GetProps(ctx)
	if err != nil {
		return nil, err
	}

	return &ModelProps{
		ModelName:    resp.DefaultGenerationSettings.Model,
		TotalSlots:   resp.TotalSlots,
		ChatTemplate: resp.ChatTemplate,
		NCtx:         resp.DefaultGenerationSettings.NCtx,
		NPredict:     resp.DefaultGenerationSettings.NPredict,
		Temperature:  resp.DefaultGenerationSettings.Temperature,
		TopP:         resp.DefaultGenerationSettings.TopP,
		TopK:         resp.DefaultGenerationSettings.TopK,
	}, nil
}

// ListSlots returns the current state of inference slots.
func (c *Client) ListSlots(ctx context.Context) ([]Slot, error) {
	resp, err := c.nativeClient.GetSlots(ctx)
	if err != nil {
		return nil, err
	}

	slots := make([]Slot, len(resp))
	for i, s := range resp {
		slots[i] = Slot{
			ID:         s.ID,
			State:      SlotState(s.State),
			Model:      s.Model,
			NCtx:       s.NCtx,
			TokensUsed: s.NPast,
			Prompt:     s.Prompt,
		}
	}

	return slots, nil
}

// IsHealthy is a convenience method that returns true if server is ready.
func (c *Client) IsHealthy(ctx context.Context) bool {
	health, err := c.Health(ctx)
	if err != nil {
		return false
	}
	return health.Status == "ok"
}

// Manager defines the interface for llama.cpp server management operations.
type Manager interface {
	Health(ctx context.Context) (*HealthStatus, error)
	ModelProps(ctx context.Context) (*ModelProps, error)
	ListSlots(ctx context.Context) ([]Slot, error)
	IsHealthy(ctx context.Context) bool
}

// Ensure Client implements Manager.
var _ Manager = (*Client)(nil)
