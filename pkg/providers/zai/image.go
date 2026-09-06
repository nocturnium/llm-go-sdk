package zai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// GenerateImage requests one URL-only image and eagerly downloads it. URL expiry
// is thirty days. Filtering without image data returns ModerationError; code 1113
// wraps ErrQuotaExceeded. n and response_format extras are rejected.
func (c *Client) GenerateImage(ctx context.Context, prompt string, opts ...llms.ImageOption) (*llms.ImageResponse, error) {
	o := llms.ApplyImageOptions(opts...)
	if strings.TrimSpace(prompt) == "" {
		return nil, c.mediaError(llms.ErrEmptyPrompt)
	}
	if o.N < 0 || o.N > 1 || (o.Quality != "" && o.Quality != "hd" && o.Quality != "standard") {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	model := o.Model
	if model == "" {
		model = providerConfig.DefaultImageModel
	}
	body := map[string]any{"model": model, "prompt": prompt}
	if o.Size != "" {
		body["size"] = o.Size
	}
	if o.Quality != "" {
		body["quality"] = o.Quality
	}
	for k, v := range o.Extra {
		if k == "n" || k == "response_format" {
			return nil, c.mediaError(fmt.Errorf("unsupported image extra %q: %w", k, llms.ErrInvalidParameters))
		}
		body[k] = v
	}
	// Validate the effective identity after merging native extras.
	var ok bool
	model, ok = body["model"].(string)
	if !ok || strings.TrimSpace(model) == "" {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	effectivePrompt, ok := body["prompt"].(string)
	if !ok || strings.TrimSpace(effectivePrompt) == "" {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	if value, supplied := body["quality"]; supplied {
		quality, ok := value.(string)
		if !ok || (quality != "hd" && quality != "standard") {
			return nil, c.mediaError(llms.ErrInvalidParameters)
		}
	}
	var wire struct {
		openaicompat.ImageResponse
		ContentFilter []any `json:"content_filter"`
		Error         *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := c.mediaRequest(ctx, http.MethodPost, c.mediaEndpoint("images/generations"), body, &wire); err != nil {
		return nil, err
	}
	if wire.Error != nil {
		return nil, c.mediaError(&httpclient.APIError{Code: wire.Error.Code, Message: wire.Error.Message})
	}
	if len(wire.Data) == 0 {
		if len(wire.ContentFilter) > 0 {
			return nil, &llms.ModerationError{Provider: "zai", Stage: llms.ModerationOutput, Reasons: []string{"content_filter"}}
		}
		return nil, c.mediaError(fmt.Errorf("empty image response"))
	}
	out := &llms.ImageResponse{Model: model, Usage: llms.MediaUsage{Unit: llms.MediaUnitImage, Quantity: float64(len(wire.Data))}}
	for _, item := range wire.Data {
		data, err := c.fetchMedia(ctx, item.URL)
		if err != nil {
			return nil, err
		}
		out.Images = append(out.Images, llms.MediaAsset{URL: item.URL, Data: data.Data, ExpiresAt: time.Now().Add(30 * 24 * time.Hour), MIMEType: data.ContentType})
	}
	return out, nil
}
