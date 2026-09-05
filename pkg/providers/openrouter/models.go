package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// ModelArchitecture describes discovery input and output modalities.
type ModelArchitecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// ParameterValues describes an enumerated image parameter.
type ParameterValues struct {
	Values []string `json:"values"`
}

// ParameterRange describes discovery parameter bounds.
type ParameterRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// ImageParameters describes supported image generation controls.
type ImageParameters struct {
	Resolution      ParameterValues `json:"resolution"`
	AspectRatio     ParameterValues `json:"aspect_ratio"`
	N               ParameterRange  `json:"n"`
	InputReferences ParameterRange  `json:"input_references"`
}

// ImageModel is an entry returned by the native image discovery route.
type ImageModel struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Architecture        ModelArchitecture `json:"architecture"`
	SupportedParameters ImageParameters   `json:"supported_parameters"`
	SupportsStreaming   bool              `json:"supports_streaming"`
	// Endpoints is the relative discovery URL for this model's endpoints.
	Endpoints string `json:"endpoints"`
}

// VideoModel is an entry returned by the native video discovery route.
type VideoModel struct {
	Name                  string            `json:"name"`
	PricingSKUs           map[string]string `json:"pricing_skus"`
	SupportedSizes        []string          `json:"supported_sizes"`
	ID                    string            `json:"id"`
	SupportedResolutions  []string          `json:"supported_resolutions"`
	SupportedAspectRatios []string          `json:"supported_aspect_ratios"`
	SupportedDurations    []int             `json:"supported_durations"`
	SupportedFrameImages  []string          `json:"supported_frame_images"`
}

// ListImageModels retrieves native image models with supported parameters.
// Results are uncached and owned by the caller; transport errors are wrapped.
func (c *Client) ListImageModels(ctx context.Context) ([]ImageModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out := struct {
		Data []ImageModel `json:"data"`
	}{Data: []ImageModel{}}
	err := c.request(ctx, http.MethodGet, "images/models", nil, nil, &out)
	return out.Data, err
}

// ListVideoModels retrieves native video models and their supported settings.
// Results are uncached and owned by the caller; transport errors are wrapped.
func (c *Client) ListVideoModels(ctx context.Context) ([]VideoModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out := struct {
		Data []VideoModel `json:"data"`
	}{Data: []VideoModel{}}
	err := c.request(ctx, http.MethodGet, "videos/models", nil, nil, &out)
	return out.Data, err
}

// ListModels fetches /models. Limit and type filters are applied locally;
// cursors are unsupported and return ErrInvalidParameters. Each call returns
// independent metadata, with no cached pointers or assumed token pricing.
func (c *Client) ListModels(ctx context.Context, opts ...llms.ListModelsOption) (*llms.ListModelsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	o := llms.ApplyListModelsOptions(opts...)
	if o.Cursor != "" || o.Limit < 0 {
		return nil, fmt.Errorf("openrouter: unsupported cursor or negative limit: %w", llms.ErrInvalidParameters)
	}
	entries, err := c.listCatalogModels(ctx, nil)
	if err != nil {
		return nil, err
	}
	models := make([]llms.ModelInfo, 0, len(entries))
	for _, m := range entries {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		info := llms.ModelInfo{ID: m.ID, DisplayName: name, Provider: c.Provider(), ContextLength: m.ContextLength, Types: []llms.ModelType{}}
		for _, modality := range m.Architecture.OutputModalities {
			switch modality {
			case "text":
				info.Types = append(info.Types, llms.ModelTypeChat)
			case "image":
				info.Types = append(info.Types, llms.ModelTypeImage)
			case "audio", "speech":
				info.Types = append(info.Types, llms.ModelTypeAudio)
			case "video":
				info.Types = append(info.Types, llms.ModelTypeVideo)
			}
		}
		for _, modality := range m.Architecture.InputModalities {
			if modality == "image" {
				info.Types = append(info.Types, llms.ModelTypeVision)
			}
		}
		if m.Created > 0 {
			info.CreatedAt = time.Unix(m.Created, 0)
		}
		models = append(models, info)
	}
	if len(o.Types) > 0 {
		models = llms.FilterModelsByType(models, o.Types...)
	}
	if o.Limit > 0 && len(models) > o.Limit {
		models = models[:o.Limit]
	}
	return &llms.ListModelsResult{Models: models}, nil
}

// ModelInfo fetches a fresh catalog and matches id case-insensitively.
// Unknown IDs return a wrapped ErrModelNotFound.
func (c *Client) ModelInfo(ctx context.Context, id string) (*llms.ModelInfo, error) {
	result, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range result.Models {
		if strings.EqualFold(m.ID, id) {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("openrouter: model %q: %w", id, llms.ErrModelNotFound)
}

// SpeechPricing contains provider-reported speech pricing.
type SpeechPricing struct {
	// InputChar is USD per input character, absent when not reported.
	InputChar *string `json:"input_char,omitempty"`
}

// SpeechModel describes a model returned by speech-filtered discovery.
type SpeechModel struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Architecture ModelArchitecture `json:"architecture"`
	Pricing      *SpeechPricing    `json:"pricing,omitempty"`
}

type catalogModel struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	ContextLength int               `json:"context_length"`
	Created       int64             `json:"created"`
	Architecture  ModelArchitecture `json:"architecture"`
	Pricing       *SpeechPricing    `json:"pricing,omitempty"`
}

func (c *Client) listCatalogModels(ctx context.Context, query url.Values) ([]catalogModel, error) {
	var response struct {
		Data []catalogModel `json:"data"`
	}
	err := c.request(ctx, http.MethodGet, "models", query, nil, &response)
	return response.Data, err
}

// ListSpeechModels retrieves /models?output_modalities=speech, including reported
// character pricing. Results are uncached and caller-owned; errors are wrapped.
func (c *Client) ListSpeechModels(ctx context.Context) ([]SpeechModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	entries, err := c.listCatalogModels(ctx, url.Values{"output_modalities": {"speech"}})
	if err != nil {
		return nil, err
	}
	models := make([]SpeechModel, 0, len(entries))
	for _, m := range entries {
		models = append(models, SpeechModel{ID: m.ID, Name: m.Name, Architecture: m.Architecture, Pricing: m.Pricing})
	}
	return models, nil
}
