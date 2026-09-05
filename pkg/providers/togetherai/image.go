package togetherai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// GenerateImage uses Together's generation route, defaulting to base64 output.
// Size must be WxH and maps to width/height. Extras merge last. URL responses
// are downloaded with a User-Agent; unknown dimensions leave Schnell unpriced.
func (c *Client) GenerateImage(ctx context.Context, prompt string, opts ...llms.ImageOption) (*llms.ImageResponse, error) {
	o := llms.ApplyImageOptions(opts...)
	body := map[string]any{"model": providerConfig.DefaultImageModel, "prompt": prompt, "response_format": "base64", "output_format": "jpeg"}
	if o.Model != "" {
		body["model"] = o.Model
	}
	if o.N != 0 {
		body["n"] = o.N
	}
	if o.Size != "" {
		dimensions := strings.Split(o.Size, "x")
		if len(dimensions) != 2 {
			return nil, c.mediaError(llms.ErrInvalidParameters)
		}
		w, e1 := strconv.Atoi(dimensions[0])
		h, e2 := strconv.Atoi(dimensions[1])
		if e1 != nil || e2 != nil || w <= 0 || h <= 0 {
			return nil, c.mediaError(llms.ErrInvalidParameters)
		}
		body["width"] = w
		body["height"] = h
	}
	if o.AspectRatio != "" {
		body["aspect_ratio"] = o.AspectRatio
	}
	if o.Seed != nil {
		body["seed"] = *o.Seed
	}
	if o.NegativePrompt != "" {
		body["negative_prompt"] = o.NegativePrompt
	}
	if o.OutputFormat != "" {
		body["output_format"] = o.OutputFormat
	}
	for k, v := range o.Extra {
		body[k] = v
	}
	// Decode the effective request to validate overrides and account for actual dimensions.
	var req struct {
		Model          string
		Prompt         string
		N              int
		Width, Height  int
		ResponseFormat string `json:"response_format"`
		OutputFormat   string `json:"output_format"`
		Stream         bool
	}
	data, err := json.Marshal(body)
	if err == nil {
		err = json.Unmarshal(data, &req)
	}
	if err != nil {
		return nil, c.mediaError(err)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, c.mediaError(llms.ErrEmptyPrompt)
	}
	if req.N < 0 || req.N > 4 || req.Width < 0 || req.Height < 0 || req.Stream || (req.ResponseFormat != "base64" && req.ResponseFormat != "url") {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	var wire openaicompat.ImageResponse
	if err := c.mediaRequest(ctx, http.MethodPost, c.mediaEndpoint("images/generations"), body, &wire); err != nil {
		return nil, err
	}
	out, err := openaicompat.ConvertImageResponse(&wire, req.Model, req.OutputFormat)
	if err != nil {
		return nil, c.mediaError(err)
	}
	if len(out.Images) == 0 {
		return nil, c.mediaError(fmt.Errorf("empty image response"))
	}
	for i := range out.Images {
		if len(out.Images[i].Data) == 0 {
			out.Images[i].Data, err = c.fetchMedia(ctx, out.Images[i].URL)
			if err != nil {
				return nil, err
			}
		}
	}
	out.Usage = llms.MediaUsage{}
	if rate, ok := llms.GetMediaRate("togetherai", req.Model); ok && rate.Unit == llms.MediaUnitImage {
		out.Usage = llms.MediaUsage{Unit: llms.MediaUnitImage, Quantity: float64(len(out.Images))}
	} else if ok && rate.Unit == llms.MediaUnitMegapixel {
		if req.Width > 0 && req.Height > 0 {
			out.Usage = llms.MediaUsage{Unit: llms.MediaUnitMegapixel, Quantity: float64(req.Width) * float64(req.Height) * float64(len(out.Images)) / 1e6}
		}
	}
	return out, nil
}
