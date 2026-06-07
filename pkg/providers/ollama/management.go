package ollama

import (
	"context"
	"time"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/internal/ollamaapi"
)

// PullProgress represents the progress of a model pull operation.
type PullProgress struct {
	Status    string  // e.g., "pulling manifest", "downloading", "success"
	Digest    string  // Layer digest being downloaded
	Total     int64   // Total bytes to download
	Completed int64   // Bytes downloaded so far
	Percent   float64 // Download percentage (0-100)
}

// PullModel downloads a model from the Ollama registry.
// The callback is called for each progress update. Pass nil to ignore progress.
func (c *Client) PullModel(ctx context.Context, name string, callback func(PullProgress)) error {
	return c.nativeClient.PullModel(ctx, name, func(resp ollamaapi.PullResponse) {
		if callback != nil {
			var percent float64
			if resp.Total > 0 {
				percent = float64(resp.Completed) / float64(resp.Total) * 100
			}
			callback(PullProgress{
				Status:    resp.Status,
				Digest:    resp.Digest,
				Total:     resp.Total,
				Completed: resp.Completed,
				Percent:   percent,
			})
		}
	})
}

// DeleteModel removes a model from the local system.
func (c *Client) DeleteModel(ctx context.Context, name string) error {
	return c.nativeClient.DeleteModel(ctx, name)
}

// CopyModel duplicates a model with a new name.
func (c *Client) CopyModel(ctx context.Context, source, destination string) error {
	return c.nativeClient.CopyModel(ctx, source, destination)
}

// ModelDetails contains detailed information about a model.
type ModelDetails struct {
	Name              string
	License           string
	Modelfile         string
	Parameters        string
	Template          string
	Format            string
	Family            string
	Families          []string
	ParameterSize     string
	QuantizationLevel string
}

// ShowModel retrieves detailed information about a model.
func (c *Client) ShowModel(ctx context.Context, name string) (*ModelDetails, error) {
	resp, err := c.nativeClient.ShowModel(ctx, name, true)
	if err != nil {
		return nil, err
	}

	details := &ModelDetails{
		Name:       name,
		License:    resp.License,
		Modelfile:  resp.Modelfile,
		Parameters: resp.Parameters,
		Template:   resp.Template,
	}

	if resp.Details != nil {
		details.Format = resp.Details.Format
		details.Family = resp.Details.Family
		details.Families = resp.Details.Families
		details.ParameterSize = resp.Details.ParameterSize
		details.QuantizationLevel = resp.Details.QuantizationLevel
	}

	return details, nil
}

// RunningModel represents a currently loaded model.
type RunningModel struct {
	Name      string
	Size      int64     // Size in bytes
	SizeVRAM  int64     // VRAM usage in bytes
	ExpiresAt time.Time // When the model will be unloaded
}

// ListRunningModels returns models currently loaded in memory.
func (c *Client) ListRunningModels(ctx context.Context) ([]RunningModel, error) {
	resp, err := c.nativeClient.ListRunning(ctx)
	if err != nil {
		return nil, err
	}

	models := make([]RunningModel, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = RunningModel{
			Name:      m.Name,
			Size:      m.Size,
			SizeVRAM:  m.SizeVRAM,
			ExpiresAt: m.ExpiresAt,
		}
	}

	return models, nil
}

// Version returns the Ollama server version.
func (c *Client) Version(ctx context.Context) (string, error) {
	return c.nativeClient.GetVersion(ctx)
}

// LocalModel represents a model installed locally.
type LocalModel struct {
	Name       string
	Size       int64
	ModifiedAt time.Time
	Digest     string
	Details    *LocalModelDetails
}

// LocalModelDetails contains model metadata.
type LocalModelDetails struct {
	Format            string
	Family            string
	Families          []string
	ParameterSize     string
	QuantizationLevel string
}

// ListLocalModels returns all models installed locally.
// This uses the native Ollama API instead of the OpenAI-compatible endpoint.
func (c *Client) ListLocalModels(ctx context.Context) ([]LocalModel, error) {
	resp, err := c.nativeClient.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	models := make([]LocalModel, len(resp.Models))
	for i, m := range resp.Models {
		model := LocalModel{
			Name:       m.Name,
			Size:       m.Size,
			ModifiedAt: m.ModifiedAt,
			Digest:     m.Digest,
		}

		if m.Details != nil {
			model.Details = &LocalModelDetails{
				Format:            m.Details.Format,
				Family:            m.Details.Family,
				Families:          m.Details.Families,
				ParameterSize:     m.Details.ParameterSize,
				QuantizationLevel: m.Details.QuantizationLevel,
			}
		}

		models[i] = model
	}

	return models, nil
}

// ModelManager defines the interface for Ollama model management operations.
type ModelManager interface {
	PullModel(ctx context.Context, name string, callback func(PullProgress)) error
	DeleteModel(ctx context.Context, name string) error
	CopyModel(ctx context.Context, source, destination string) error
	ShowModel(ctx context.Context, name string) (*ModelDetails, error)
	ListLocalModels(ctx context.Context) ([]LocalModel, error)
	ListRunningModels(ctx context.Context) ([]RunningModel, error)
	Version(ctx context.Context) (string, error)
}

// Ensure Client implements ModelManager.
var _ ModelManager = (*Client)(nil)

// Ensure Client implements ModelLister.
var _ llms.ModelLister = (*Client)(nil)
