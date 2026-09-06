package openrouter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

type videoFrame struct {
	Type      string                `json:"type"`
	ImageURL  openaicompat.ImageURL `json:"image_url"`
	FrameType string                `json:"frame_type"`
}
type videoRequest struct {
	ExtraBody       map[string]any `json:"-"`
	Model           string         `json:"model"`
	Prompt          string         `json:"prompt"`
	Duration        int            `json:"duration,omitempty"`
	Resolution      string         `json:"resolution,omitempty"`
	AspectRatio     string         `json:"aspect_ratio,omitempty"`
	Frames          []videoFrame   `json:"frame_images,omitempty"`
	InputReferences any            `json:"input_references,omitempty"`
	GenerateAudio   *bool          `json:"generate_audio,omitempty"`
	Seed            *int64         `json:"seed,omitempty"`
	CallbackURL     string         `json:"callback_url,omitempty"`
}
type videoObject struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	UnsignedURLs []string `json:"unsigned_urls"`
	Usage        struct {
		Cost *float64 `json:"cost"`
	} `json:"usage"`
	Error string `json:"error"`
}

func buildVideoRequest(prompt string, o *llms.VideoOptions) (*videoRequest, error) {
	req := &videoRequest{Model: o.Model, Prompt: prompt, Duration: o.DurationSeconds, Resolution: o.Resolution, AspectRatio: o.AspectRatio, GenerateAudio: o.Audio, Seed: o.Seed}
	if req.Model == "" {
		req.Model = DefaultVideoModel
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, llms.ErrEmptyPrompt
	}
	if o.DurationSeconds < 0 {
		return nil, llms.ErrInvalidParameters
	}
	for i, frame := range []*llms.MediaInput{o.FirstFrame, o.LastFrame} {
		if frame == nil {
			continue
		}
		value, err := frameURL(*frame)
		if err != nil {
			return nil, err
		}
		kind := "first_frame"
		if i == 1 {
			kind = "last_frame"
		}
		req.Frames = append(req.Frames, videoFrame{Type: "image_url", ImageURL: openaicompat.ImageURL{URL: value}, FrameType: kind})
	}
	// The native input_references item schema is provider-specific. Require it
	// explicitly through Extra rather than guessing a wire shape for ReferenceImages.
	if len(o.ReferenceImages) > 0 {
		return nil, fmt.Errorf("use video Extra input_references: %w", llms.ErrInvalidParameters)
	}
	req.InputReferences = o.Extra["input_references"]
	if value, ok := o.Extra["callback_url"]; ok {
		callback, ok := value.(string)
		if !ok {
			return nil, llms.ErrInvalidParameters
		}
		u, err := url.Parse(callback)
		if err != nil || u.Host == "" || u.Scheme != "https" {
			return nil, llms.ErrInvalidParameters
		}
		req.CallbackURL = callback
	}
	req.ExtraBody = o.Extra
	return req, nil
}
func frameURL(input llms.MediaInput) (string, error) {
	if err := input.Validate(); err != nil {
		return "", err
	}
	if input.FileID != "" {
		return "", fmt.Errorf("video frame requires URL or Data: %w", llms.ErrInvalidParameters)
	}
	if input.URL != "" {
		return input.URL, nil
	}
	if !strings.HasPrefix(input.MIMEType, "image/") {
		return "", llms.ErrInvalidParameters
	}
	return "data:" + input.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(input.Data), nil
}

// GenerateVideo submits a native OpenRouter job and returns a *llms.PollingVideoJob
// through the VideoJob interface. OpenRouter's duration, frame_images, states and
// unsigned_urls differ from OpenAI's video wire format. Polling and downloads
// use the configured endpoint and SSRF policy; polling_url is deliberately not
// followed to avoid forwarding credentials to an arbitrary response URL.
// Extra accepts HTTPS callback_url, input_references and additional native keys;
// typed fields are reserved. NegativePrompt and output_format are ignored.
// ReferenceImages and FileID
// frames return ErrInvalidParameters because their wire mapping is unverified.
// Cancel is unsupported. Use a bounded context with Wait.
func (c *Client) GenerateVideo(ctx context.Context, prompt string, opts ...llms.VideoOption) (llms.VideoJob, error) {
	o := llms.ApplyVideoOptions(opts...)
	req, err := buildVideoRequest(prompt, o)
	if err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "generate video", err)
	}
	var obj videoObject
	if err = c.request(ctx, http.MethodPost, "videos", nil, req, &obj); err != nil {
		return nil, err
	}
	if obj.ID == "" || obj.ID == "." || obj.ID == ".." || strings.ContainsAny(obj.ID, "/\\?#%") {
		return nil, fmt.Errorf("openrouter: invalid video ID: %w", llms.ErrInvalidParameters)
	}
	route := "videos/" + obj.ID
	job := &llms.PollingVideoJob{JobID: obj.ID}
	var observedRunning atomic.Bool
	observedRunning.Store(obj.Status == "in_progress")
	job.PollFn = func(ctx context.Context) (*llms.JobStatus, error) {
		var current videoObject
		if err := c.request(ctx, http.MethodGet, route, nil, nil, &current); err != nil {
			return nil, err
		}
		if current.Status == "in_progress" {
			observedRunning.Store(true)
		}
		return videoStatus(&current, observedRunning.Load()), nil
	}
	job.ResultFn = func(ctx context.Context) (*llms.VideoResponse, error) {
		return c.videoResult(ctx, route, req, observedRunning.Load())
	}
	return job, nil
}
func videoStatus(obj *videoObject, observedRunning bool) *llms.JobStatus {
	out := &llms.JobStatus{Cost: obj.Usage.Cost}
	switch obj.Status {
	case "pending":
		out.State = llms.JobQueued
	case "in_progress":
		out.State = llms.JobRunning
	case "completed":
		out.State = llms.JobSucceeded
	case "cancelled": //nolint:misspell // OpenRouter wire spelling.
		out.State = llms.JobCancelled
	case "expired":
		out.State = llms.JobFailed
		out.Err = fmt.Errorf("openrouter: %w", llms.ErrAssetExpired)
	case "failed":
		out.State = llms.JobFailed
		reason := obj.Error
		lower := strings.ToLower(reason)
		out.Err = fmt.Errorf("openrouter: video failed (%s): %w", reason, llms.ErrJobFailed)
		if strings.Contains(lower, "moderat") || strings.Contains(lower, "content_policy") || strings.Contains(lower, "safety") || strings.Contains(lower, "content_filter") {
			out.State = llms.JobModerated
			stage := llms.ModerationInput
			if observedRunning {
				stage = llms.ModerationOutput
			}
			out.Err = &llms.ModerationError{Provider: "openrouter", Stage: stage, Reasons: []string{reason}, Charged: out.Cost != nil && *out.Cost > 0}
		}
	default:
		out.State = llms.JobFailed
		out.Err = fmt.Errorf("openrouter: unknown video status %q: %w", obj.Status, llms.ErrJobFailed)
	}
	return out
}
func (c *Client) videoResult(ctx context.Context, route string, req *videoRequest, observedRunning bool) (*llms.VideoResponse, error) {
	var current videoObject
	if err := c.request(ctx, http.MethodGet, route, nil, nil, &current); err != nil {
		return nil, err
	}
	state := videoStatus(&current, observedRunning)
	if state.Err != nil {
		return nil, state.Err
	}
	if state.State != llms.JobSucceeded || len(current.UnsignedURLs) == 0 {
		return nil, fmt.Errorf("openrouter: video result unavailable: %w", llms.ErrJobFailed)
	}
	out := &llms.VideoResponse{Model: req.Model, Videos: make([]llms.MediaAsset, 0, len(current.UnsignedURLs)), Usage: llms.MediaUsage{Unit: llms.MediaUnitSecond, Quantity: float64(req.Duration), Cost: current.Usage.Cost}}
	if req.Duration == 0 {
		out.Usage.Unit = ""
	}
	for i, assetURL := range current.UnsignedURLs {
		data, err := c.transport.DoBinary(ctx, http.MethodGet, c.endpoint(route+"/content", url.Values{"index": {strconv.Itoa(i)}}), nil, c.headers)
		if err != nil {
			return nil, openaicompat.WrapError(c.Provider(), "video content", err)
		}
		out.Videos = append(out.Videos, llms.MediaAsset{Data: data.Data, MIMEType: "video/mp4", URL: assetURL})
	}
	return out, nil
}

// MarshalJSON merges remaining extensions last. Typed field names are reserved,
// even when omitted, so extras cannot bypass validation or change usage metadata.
func (r videoRequest) MarshalJSON() ([]byte, error) {
	type plain videoRequest
	data, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for k, v := range r.ExtraBody {
		switch k {
		case "model", "prompt", "duration", "resolution", "aspect_ratio", "frame_images", "input_references", "generate_audio", "seed", "callback_url", "negative_prompt", "output_format":
		default:
			fields[k] = v
		}
	}
	return json.Marshal(fields)
}
