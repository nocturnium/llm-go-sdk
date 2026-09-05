package togetherai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

type videoObject struct {
	Seconds *videoSeconds `json:"seconds"`
	ID      string        `json:"id"`
	Status  string        `json:"status"`
	Outputs struct {
		Cost     *float64 `json:"cost"`
		VideoURL string   `json:"video_url"`
	} `json:"outputs"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// GenerateVideo submits a native asynchronous video job because this provider's
// request and polling wire differs from OpenAI. Wait polls and downloads the
// result with the configured transport policy. Cancel is unsupported.
// Frame inputs must be URLs; unsupported sources return ErrInvalidParameters.
func (c *Client) GenerateVideo(ctx context.Context, prompt string, opts ...llms.VideoOption) (llms.VideoJob, error) {
	o := llms.ApplyVideoOptions(opts...)
	if strings.TrimSpace(prompt) == "" {
		return nil, c.mediaError(llms.ErrEmptyPrompt)
	}
	if o.DurationSeconds < 0 {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	model := o.Model
	if model == "" {
		model = providerConfig.DefaultVideoModel
	}
	body := map[string]any{}
	for k, v := range o.Extra {
		body[k] = v
	}
	body["model"] = model
	body["prompt"] = prompt
	if o.DurationSeconds != 0 {
		body["seconds"] = strconv.Itoa(o.DurationSeconds)
	}
	if o.Resolution != "" {
		body["resolution"] = o.Resolution
	}
	if o.AspectRatio != "" {
		body["ratio"] = o.AspectRatio
	}
	if o.Audio != nil {
		body["generate_audio"] = *o.Audio
	}
	if o.Seed != nil {
		body["seed"] = *o.Seed
	}
	if o.NegativePrompt != "" {
		body["negative_prompt"] = o.NegativePrompt
	}
	if o.OutputFormat != "" {
		body["output_format"] = strings.ToUpper(o.OutputFormat)
	}
	frames := []map[string]string{}
	for i, frame := range []*llms.MediaInput{o.FirstFrame, o.LastFrame} {
		if frame == nil {
			continue
		}
		value, err := videoFrame(frame)
		if err != nil {
			return nil, c.mediaError(err)
		}
		frames = append(frames, map[string]string{"input_image": value, "frame": []string{"first", "last"}[i]})
	}
	// The schema of native reference_images is not verified; pass it via Extra.media.
	if len(o.ReferenceImages) > 0 {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	if len(frames) > 0 {
		body["media"] = map[string]any{"frame_images": frames}
	}
	// Resolve native extras before validating and accounting for billed duration.
	var effective struct {
		Duration videoSeconds `json:"seconds"`
		Format   string       `json:"output_format"`
	}
	encoded, encodeErr := json.Marshal(body)
	if encodeErr == nil {
		encodeErr = json.Unmarshal(encoded, &effective)
	}
	if encodeErr != nil {
		return nil, c.mediaError(encodeErr)
	}
	o.DurationSeconds = int(effective.Duration)
	if _, ok := body["seconds"]; ok {
		body["seconds"] = strconv.Itoa(o.DurationSeconds)
	}
	if o.DurationSeconds < 0 || (effective.Format != "" && effective.Format != "MP4" && effective.Format != "WEBM") {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	// Preserve proxy prefixes, removing only the trailing v1 API segment.
	base, err := url.Parse(c.options.BaseURL)
	if err != nil {
		return nil, c.mediaError(err)
	}
	base.Path = strings.TrimSuffix(strings.TrimRight(base.Path, "/"), "/v1")
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	endpoint, err := url.JoinPath(base.String(), providerConfig.Media.VideosPath)
	if err != nil {
		return nil, c.mediaError(err)
	}
	var obj videoObject
	if err := c.mediaRequest(ctx, http.MethodPost, endpoint, body, &obj); err != nil {
		return nil, err
	}
	if obj.Error != nil {
		return nil, c.mediaError(&httpclient.APIError{Code: obj.Error.Code, Message: obj.Error.Message})
	}
	if obj.ID == "" || obj.ID == "." || obj.ID == ".." || strings.ContainsAny(obj.ID, "/\\?#%") {
		return nil, c.mediaError(fmt.Errorf("invalid video ID: %w", llms.ErrInvalidParameters))
	}
	pollURL, _ := url.JoinPath(endpoint, obj.ID)
	get := func(ctx context.Context) (*videoObject, error) {
		var current videoObject
		if err := c.mediaRequest(ctx, http.MethodGet, pollURL, nil, &current); err != nil {
			return nil, err
		}
		if current.Error != nil {
			return nil, c.mediaError(&httpclient.APIError{Code: current.Error.Code, Message: current.Error.Message})
		}
		return &current, nil
	}
	job := &llms.PollingVideoJob{JobID: obj.ID}
	job.PollFn = func(ctx context.Context) (*llms.JobStatus, error) {
		current, err := get(ctx)
		if err != nil {
			return nil, err
		}
		return videoStatus(current), nil
	}
	job.ResultFn = func(ctx context.Context) (*llms.VideoResponse, error) {
		current, err := get(ctx)
		if err != nil {
			return nil, err
		}
		if videoStatus(current).State != llms.JobSucceeded {
			return nil, c.mediaError(llms.ErrJobFailed)
		}
		if current.Outputs.VideoURL == "" {
			return nil, c.mediaError(llms.ErrJobFailed)
		}
		data, err := c.fetchMedia(ctx, current.Outputs.VideoURL)
		if err != nil {
			return nil, err
		}
		usage := llms.MediaUsage{Unit: llms.MediaUnitSecond, Quantity: float64(o.DurationSeconds)}
		if current.Seconds != nil {
			usage.Quantity = float64(*current.Seconds)
		}
		if usage.Cost == nil {
			if rate, ok := llms.GetMediaRate("togetherai", model); ok && rate.Unit == "" {
				cost := rate.USD
				usage.Cost = &cost
			}
		}
		out := &llms.VideoResponse{Model: model, Usage: usage, Videos: []llms.MediaAsset{{URL: current.Outputs.VideoURL, Data: data, MIMEType: videoMIME(effective.Format)}}}
		if current.Outputs.Cost != nil {
			out.Metadata = map[string]any{"outputs_cost": *current.Outputs.Cost}
		}
		return out, nil
	}
	return job, nil
}

func videoStatus(obj *videoObject) *llms.JobStatus {
	out := &llms.JobStatus{}
	switch obj.Status {
	case "queued":
		out.State = llms.JobQueued
	case "cancelled": //nolint:misspell // Together wire spelling.
		out.State = llms.JobCancelled
	case "in_progress":
		out.State = llms.JobRunning
	case "completed":
		out.State = llms.JobSucceeded
	case "failed":
		out.State = llms.JobFailed
		out.Err = fmt.Errorf("togetherai: video state %q: %w", obj.Status, llms.ErrJobFailed)
	default:
		out.State = llms.JobRunning
	}
	return out
}

func videoFrame(input *llms.MediaInput) (string, error) {
	if err := input.Validate(); err != nil {
		return "", err
	}
	if input.URL == "" {
		return "", fmt.Errorf("video frames require URLs: %w", llms.ErrInvalidParameters)
	}
	return input.URL, nil
}

func videoMIME(format string) string {
	if format == "WEBM" {
		return "video/webm"
	}
	return "video/mp4"
}

// videoSeconds accepts the documented string and numeric responses from proxies.
type videoSeconds int

func (s *videoSeconds) UnmarshalJSON(data []byte) error {
	var value string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	} else {
		value = string(data)
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fmt.Errorf("invalid seconds %q: %w", value, llms.ErrInvalidParameters)
	}
	*s = videoSeconds(n)
	return nil
}
