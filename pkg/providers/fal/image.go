package fal

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

var aspectRatios = map[string]string{"1:1": "square_hd", "4:3": "landscape_4_3", "3:4": "portrait_4_3", "16:9": "landscape_16_9", "9:16": "portrait_16_9"}

type imageResult struct {
	Images []struct {
		URL         string `json:"url"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		ContentType string `json:"content_type"`
	} `json:"images"`
	Seed    *int64 `json:"seed"`
	NSFW    []bool `json:"has_nsfw_concepts"`
	Prompt  string `json:"prompt"`
	Timings any    `json:"timings"`
}

// imageSize parses "WxH" into a fal image_size object.
func imageSize(size string) (map[string]any, error) {
	parts := strings.Split(strings.ToLower(size), "x")
	if len(parts) != 2 {
		return nil, invalid("image Size must be WxH")
	}
	width, errW := strconv.Atoi(parts[0])
	height, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		return nil, invalid("image Size must be WxH with positive dimensions")
	}
	return map[string]any{"width": width, "height": height}, nil
}
func imageBody(prompt string, o *llms.ImageOptions) (map[string]any, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("fal: image: %w", llms.ErrEmptyPrompt)
	}
	if o.NegativePrompt != "" || o.Quality != "" || o.SafetyTolerance != nil {
		return nil, invalid("NegativePrompt, Quality and SafetyTolerance have no fal mapping; use Extra")
	}
	body := map[string]any{"prompt": prompt}
	switch {
	case o.Size != "":
		size, err := imageSize(o.Size)
		if err != nil {
			return nil, err
		}
		body["image_size"] = size
	case o.AspectRatio != "":
		preset, ok := aspectRatios[o.AspectRatio]
		if !ok {
			return nil, invalid("AspectRatio must be 1:1, 4:3, 3:4, 16:9 or 9:16")
		}
		body["image_size"] = preset
	}
	if o.N < 0 {
		return nil, invalid("image count must be nonnegative")
	}
	if o.N > 0 {
		body["num_images"] = o.N
	}
	if o.Seed != nil {
		body["seed"] = *o.Seed
	}
	switch strings.ToLower(o.OutputFormat) {
	case "":
	case "jpeg", "jpg":
		body["output_format"] = "jpeg"
	case "png":
		body["output_format"] = "png"
	default:
		return nil, invalid("OutputFormat must be jpeg or png")
	}
	if err := mergeExtra(body, o.Extra, "prompt", "image_size", "num_images", "seed", "output_format"); err != nil {
		return nil, err
	}
	return body, nil
}

// GenerateImage submits, polls and downloads images from the queue. Size (WxH)
// wins over AspectRatio presets; Count, Seed and Format (jpeg/png) map directly.
// Usage counts output megapixels (zero when any dimension is absent); Cost
// stays nil because fal per-model pricing is not tabulated. Fully NSFW-flagged
// results return an output-stage ModerationError; partial flags drop the flagged
// images and record their indices in Metadata["nsfw_indices"].
func (c *Client) GenerateImage(ctx context.Context, prompt string, opts ...llms.ImageOption) (result *llms.ImageResponse, err error) {
	o := llms.ApplyImageOptions(opts...)
	if o.Model == "" {
		o.Model = c.options.ImageModel
	}
	ctx, finish := c.startOperation(ctx, "generate image", o.Model)
	defer func() { finish(err) }()
	body, err := imageBody(prompt, o)
	if err != nil {
		return nil, err
	}
	q, err := c.submit(ctx, o.Model, body)
	if err != nil {
		return nil, err
	}
	if err = c.await(ctx, q); err != nil {
		return nil, err
	}
	var res imageResult
	header, err := c.result(ctx, q, &res)
	if err != nil {
		return nil, err
	}
	if len(res.Images) == 0 {
		return nil, fmt.Errorf("fal: no images generated: %w", llms.ErrIncompleteResponse)
	}
	out := &llms.ImageResponse{Model: o.Model, Metadata: map[string]any{"request_id": q.RequestID}}
	billableUnits(header, out.Metadata)
	flagged := []int{}
	var megapixels float64
	known := true
	for i, image := range res.Images {
		// Flagged images are billed too: usage covers every generated output.
		if image.Width <= 0 || image.Height <= 0 {
			known = false
		}
		megapixels += float64(image.Width) * float64(image.Height) / 1e6
		if i < len(res.NSFW) && res.NSFW[i] {
			flagged = append(flagged, i)
			continue
		}
		asset, e := c.fetchAsset(ctx, image.URL, image.ContentType)
		if e != nil {
			return nil, e
		}
		asset.Seed = res.Seed
		out.Images = append(out.Images, asset)
	}
	if len(flagged) == len(res.Images) {
		return nil, moderation(llms.ModerationOutput, "has_nsfw_concepts")
	}
	if len(flagged) > 0 {
		out.Metadata["nsfw_indices"] = flagged
	}
	// The unit stays megapixels per response; unknown dimensions yield zero
	// rather than switching units mid-provider.
	out.Usage = llms.MediaUsage{Unit: llms.MediaUnitMegapixel, Quantity: megapixels}
	if !known {
		out.Usage.Quantity = 0
	}
	return out, nil
}
