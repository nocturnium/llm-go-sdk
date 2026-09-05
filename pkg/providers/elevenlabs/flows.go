package elevenlabs

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

type flowObject struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	URL     string `json:"content_url"`
	MIME    string `json:"content_mime_type"`
	Message string `json:"error_message"`
	Reason  string `json:"failure_reason"`
}

func flowStatus(obj flowObject, running bool) *llms.JobStatus {
	status := &llms.JobStatus{}
	switch obj.Status {
	case "pending":
		status.State = llms.JobQueued
	case "generating":
		status.State = llms.JobRunning
	case "completed":
		status.State = llms.JobSucceeded
	case "failed":
		status.State = llms.JobFailed
		status.Err = fmt.Errorf("elevenlabs: %s: %s: %w", obj.Reason, obj.Message, llms.ErrJobFailed)
		if obj.Reason == "moderated" {
			stage := llms.ModerationInput
			if running {
				stage = llms.ModerationOutput
			}
			status.State = llms.JobModerated
			status.Err = &llms.ModerationError{Provider: "elevenlabs", Stage: stage, Reasons: []string{obj.Message}, Charged: false}
		}
	default:
		status.State = llms.JobFailed
		status.Err = fmt.Errorf("elevenlabs: unknown flow status %q: %w", obj.Status, llms.ErrJobFailed)
	}
	return status
}
func (c *Client) createFlow(ctx context.Context, kind string, body map[string]any) (string, bool, error) {
	var obj flowObject
	if err := c.request(ctx, http.MethodPost, "flows/"+kind, body, &obj); err != nil {
		return "", false, err
	}
	if err := validateID(obj.ID); err != nil {
		return "", false, err
	}
	return "flows/" + kind + "/" + obj.ID, obj.Status == "generating", nil
}
func (c *Client) pollFlow(ctx context.Context, route string, running *atomic.Bool) (flowObject, *llms.JobStatus, error) {
	var obj flowObject
	if err := c.request(ctx, http.MethodGet, route, nil, &obj); err != nil {
		return obj, nil, err
	}
	if obj.Status == "generating" {
		running.Store(true)
	}
	return obj, flowStatus(obj, running.Load()), nil
}
func (c *Client) flowAsset(ctx context.Context, obj flowObject) (llms.MediaAsset, error) {
	// Signed content URLs are fetched without xi-api-key, even on the same host.
	data, err := c.transport.DoBinary(ctx, http.MethodGet, obj.URL, nil, nil)
	if err != nil {
		return llms.MediaAsset{}, WrapError("flow content", err)
	}
	return llms.MediaAsset{Data: data.Data, URL: obj.URL, MIMEType: obj.MIME, ExpiresAt: time.Now().Add(55 * time.Minute)}, nil
}
func inlineReference(input llms.MediaInput) (map[string]any, error) {
	if err := input.Validate(); err != nil {
		return nil, WrapError("image reference", err)
	}
	if len(input.Data) == 0 || !strings.HasPrefix(input.MIMEType, "image/") {
		return nil, invalid("image references require Data and an image MIMEType; URLs and FileID are unsupported")
	}
	return map[string]any{"type": "inline_base64", "content_base64": base64.StdEncoding.EncodeToString(input.Data), "mime_type": input.MIMEType}, nil
}
func mergeExtra(body, extra map[string]any) {
	for k, v := range extra {
		body[k] = v
	}
}
func putString(body map[string]any, key, value string) {
	if value != "" {
		body[key] = value
	}
}
func imageBody(prompt string, images []llms.MediaInput, o *llms.ImageOptions) (map[string]any, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, WrapError("image", llms.ErrEmptyPrompt)
	}
	if o.N < 0 || o.N > 1 {
		return nil, invalid("flows generate one image per request")
	}
	body := map[string]any{"model_id": o.Model, "prompt": prompt}
	putString(body, "aspect_ratio", o.AspectRatio)
	putString(body, "quality", o.Quality)
	if o.Seed != nil {
		body["seed"] = *o.Seed
	}
	if o.Size != "" {
		switch o.Size {
		case "1K", "2K", "4K", "512":
			body["resolution"] = o.Size
		default:
			return nil, invalid("image Size must be 1K, 2K, 4K or 512")
		}
	}
	refs := make([]map[string]any, 0, len(images))
	for _, input := range images {
		ref, err := inlineReference(input)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if len(refs) > 0 {
		body["images"] = refs
	}
	mergeExtra(body, o.Extra)
	// Validate effective model-specific fields after extras have overridden typed values.
	model, ok := body["model_id"].(string)
	if !ok || model == "" {
		return nil, invalid("model_id must be a nonempty string")
	}
	if _, ok = body["seed"]; ok && !strings.HasPrefix(model, "bytedance-seedream") {
		return nil, invalid("image seed requires Seedream")
	}
	if quality, exists := body["quality"]; exists {
		if !strings.HasPrefix(model, "gpt-image") || (quality != "low" && quality != "medium" && quality != "high") {
			return nil, invalid("quality requires gpt-image and low, medium or high")
		}
	}
	if _, ok = body["background"]; ok && model != "gpt-image-1" && model != "gpt-image-1.5" {
		return nil, invalid("background requires gpt-image-1 or gpt-image-1.5")
	}
	return body, nil
}

// GenerateImage creates, polls and downloads one Flows image. Pro plan access is
// required (ErrPlanRequired otherwise). Use a bounded ctx or WithPollPolicy timeout.
// Extra merges last; credit-priced Flows images have empty usage units.
func (c *Client) GenerateImage(ctx context.Context, prompt string, opts ...llms.ImageOption) (*llms.ImageResponse, error) {
	return c.generateImage(ctx, prompt, nil, opts...)
}

// EditImage sends inline Data references to Flows and returns the downloaded image.
// URL and FileID inputs return ErrInvalidParameters; Pro plan access is required.
func (c *Client) EditImage(ctx context.Context, prompt string, images []llms.MediaInput, opts ...llms.ImageOption) (*llms.ImageResponse, error) {
	if len(images) == 0 {
		return nil, invalid("image editing requires input images")
	}
	return c.generateImage(ctx, prompt, images, opts...)
}
func (c *Client) generateImage(ctx context.Context, prompt string, images []llms.MediaInput, opts ...llms.ImageOption) (*llms.ImageResponse, error) {
	o := llms.ApplyImageOptions(opts...)
	if o.Model == "" {
		o.Model = c.options.ImageModel
	}
	body, err := imageBody(prompt, images, o)
	if err != nil {
		return nil, err
	}
	route, observed, err := c.createFlow(ctx, "image", body)
	if err != nil {
		return nil, err
	}
	var running atomic.Bool
	running.Store(observed)
	var final flowObject
	err = httpclient.Poll(ctx, httpclient.PollPolicy(c.options.PollPolicy), func(ctx context.Context) (bool, error) {
		obj, status, e := c.pollFlow(ctx, route, &running)
		if e != nil {
			return false, e
		}
		final = obj
		return status.State.Terminal(), status.Err
	})
	if err != nil {
		return nil, WrapError("image polling", err)
	}
	asset, err := c.flowAsset(ctx, final)
	if err != nil {
		return nil, err
	}
	model, _ := body["model_id"].(string)
	return &llms.ImageResponse{Images: []llms.MediaAsset{asset}, Model: model}, nil
}
func videoBody(prompt string, o *llms.VideoOptions) (map[string]any, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, WrapError("video", llms.ErrEmptyPrompt)
	}
	if len(o.ReferenceImages) > 0 {
		return nil, invalid("video supports start_frame and end_frame only")
	}
	body := map[string]any{"model_id": o.Model, "prompt": prompt, "generate_audio": true}
	if o.Audio != nil {
		body["generate_audio"] = *o.Audio
	}
	if o.DurationSeconds != 0 {
		body["duration_secs"] = o.DurationSeconds
	}
	putString(body, "aspect_ratio", o.AspectRatio)
	putString(body, "resolution", o.Resolution)
	putString(body, "negative_prompt", o.NegativePrompt)
	if o.Seed != nil {
		body["seed"] = *o.Seed
	}
	for i, input := range []*llms.MediaInput{o.FirstFrame, o.LastFrame} {
		if input == nil {
			continue
		}
		ref, err := inlineReference(*input)
		if err != nil {
			return nil, err
		}
		key := "start_frame"
		if i == 1 {
			key = "end_frame"
		}
		body[key] = ref
	}
	mergeExtra(body, o.Extra)
	if err := validateVideoBody(body); err != nil {
		return nil, err
	}
	return body, nil
}
func validateVideoBody(body map[string]any) error {
	model, ok := body["model_id"].(string)
	if !ok || model == "" {
		return invalid("model_id must be a nonempty string")
	}
	if err := validateBools(body, "generate_audio", "enhance_prompt"); err != nil {
		return err
	}
	if value, exists := body["duration_secs"]; exists {
		n, valid := number(value)
		if !valid || n != float64(int(n)) || n <= 0 {
			return invalid("duration_secs must be a positive integer")
		}
		if strings.HasPrefix(model, "veo-3.1") && n != 4 && n != 6 && n != 8 {
			return invalid("Veo duration_secs must be 4, 6 or 8")
		}
		if strings.HasPrefix(model, "bytedance-seedance") && n > 15 {
			return invalid("Seedance duration_secs must be in [1,15]")
		}
	}
	if strings.HasPrefix(model, "veo-3.1") {
		if a, exists := body["aspect_ratio"]; exists && a != "16:9" && a != "9:16" {
			return invalid("Veo aspect_ratio must be 16:9 or 9:16")
		}
		if r, exists := body["resolution"]; exists && r != "720p" && r != "1080p" && r != "4K" {
			return invalid("Veo resolution must be 720p, 1080p or 4K")
		}
	}
	return nil
}

// GenerateVideo submits a Flows job as *llms.PollingVideoJob. Wait downloads the
// signed asset. Pro access is required; Cancel is unsupported. Usage is seconds
// when duration_secs is specified, otherwise unknown; credit costs remain nil.
func (c *Client) GenerateVideo(ctx context.Context, prompt string, opts ...llms.VideoOption) (llms.VideoJob, error) {
	o := llms.ApplyVideoOptions(opts...)
	if o.Model == "" {
		o.Model = c.options.VideoModel
	}
	body, err := videoBody(prompt, o)
	if err != nil {
		return nil, err
	}
	route, observed, err := c.createFlow(ctx, "video", body)
	if err != nil {
		return nil, err
	}
	var running atomic.Bool
	running.Store(observed)
	job := &llms.PollingVideoJob{JobID: strings.TrimPrefix(route, "flows/video/"), Policy: httpclient.PollPolicy(c.options.PollPolicy)}
	job.PollFn = func(ctx context.Context) (*llms.JobStatus, error) {
		_, status, e := c.pollFlow(ctx, route, &running)
		return status, e
	}
	model, _ := body["model_id"].(string)
	duration, hasDuration := number(body["duration_secs"])
	job.ResultFn = func(ctx context.Context) (*llms.VideoResponse, error) {
		obj, status, e := c.pollFlow(ctx, route, &running)
		if e != nil {
			return nil, e
		}
		if status.Err != nil {
			return nil, status.Err
		}
		if status.State != llms.JobSucceeded {
			return nil, WrapError("video result unavailable", llms.ErrJobFailed)
		}
		asset, e := c.flowAsset(ctx, obj)
		if e != nil {
			return nil, e
		}
		out := &llms.VideoResponse{Model: model, Videos: []llms.MediaAsset{asset}}
		if hasDuration {
			out.Usage = llms.MediaUsage{Unit: llms.MediaUnitSecond, Quantity: duration}
		}
		return out, nil
	}
	return job, nil
}
