package zai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

type videoObject struct {
	ID          string `json:"id"`
	Status      string `json:"task_status"`
	VideoResult []struct {
		URL           string `json:"url"`
		CoverImageURL string `json:"cover_image_url"`
	} `json:"video_result"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// GenerateVideo submits a native asynchronous video job because this provider's
// request and polling wire differs from OpenAI. Wait polls and downloads the
// result with the configured transport policy. Cancel is unsupported.
// Frame inputs must be URLs; unsupported sources return ErrInvalidParameters.
// defaultVideoDurationSeconds is the duration Z.AI renders when the request
// omits one; usage accounting assumes it so a billed job never reports zero.
const defaultVideoDurationSeconds = 5

func (c *Client) GenerateVideo(ctx context.Context, prompt string, opts ...llms.VideoOption) (llms.VideoJob, error) {
	o := llms.ApplyVideoOptions(opts...)
	if strings.TrimSpace(prompt) == "" {
		return nil, c.mediaError(llms.ErrEmptyPrompt)
	}
	if utf8.RuneCountInString(prompt) > 512 || (o.DurationSeconds != 0 && o.DurationSeconds != 5 && o.DurationSeconds != 10) || len(o.ReferenceImages) > 0 {
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
		body["duration"] = o.DurationSeconds
	}
	if o.Resolution != "" {
		body["size"] = o.Resolution
	}
	if o.Audio != nil {
		body["with_audio"] = *o.Audio
	}
	frames := []string{}
	if o.LastFrame != nil && o.FirstFrame == nil {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	for _, frame := range []*llms.MediaInput{o.FirstFrame, o.LastFrame} {
		if frame == nil {
			continue
		}
		value, err := videoFrame(frame)
		if err != nil {
			return nil, c.mediaError(err)
		}
		frames = append(frames, value)
	}
	if len(frames) > 0 {
		body["image_url"] = frames
	}
	// Resolve native extras before validating and accounting for billed duration.
	var effective struct {
		Duration int    `json:"duration"`
		FPS      int    `json:"fps"`
		Quality  string `json:"quality"`
		Format   string `json:"output_format"`
	}
	encoded, encodeErr := json.Marshal(body)
	if encodeErr == nil {
		encodeErr = json.Unmarshal(encoded, &effective)
	}
	if encodeErr != nil {
		return nil, c.mediaError(encodeErr)
	}
	o.DurationSeconds = effective.Duration
	if (o.DurationSeconds != 0 && o.DurationSeconds != 5 && o.DurationSeconds != 10) || (effective.FPS != 0 && effective.FPS != 30 && effective.FPS != 60) || (effective.Quality != "" && effective.Quality != "speed" && effective.Quality != "quality") {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	endpoint := c.mediaEndpoint(providerConfig.Media.VideosPath)

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
	pollURL, _ := url.JoinPath(c.mediaEndpoint("async-result"), obj.ID)
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
		if len(current.VideoResult) == 0 {
			return nil, c.mediaError(llms.ErrJobFailed)
		}
		billed := o.DurationSeconds
		if billed == 0 {
			billed = defaultVideoDurationSeconds
		}
		usage := llms.MediaUsage{Unit: llms.MediaUnitSecond, Quantity: float64(billed)}
		if rate, ok := llms.GetMediaRate("zai", model); ok && rate.Unit == "" {
			cost := rate.USD
			usage.Cost = &cost
		}
		out := &llms.VideoResponse{Model: model, Usage: usage}
		covers := []string{}
		for _, item := range current.VideoResult {
			data, err := c.fetchMedia(ctx, item.URL)
			if err != nil {
				return nil, err
			}
			out.Videos = append(out.Videos, llms.MediaAsset{URL: item.URL, Data: data.Data, MIMEType: data.ContentType})
			covers = append(covers, item.CoverImageURL)
		}
		out.Metadata = map[string]any{"cover_image_urls": covers}
		return out, nil
	}
	return job, nil
}

func videoStatus(obj *videoObject) *llms.JobStatus {
	out := &llms.JobStatus{}
	switch obj.Status {
	case "PROCESSING":
		out.State = llms.JobRunning
	case "SUCCESS":
		out.State = llms.JobSucceeded
	default:
		out.State = llms.JobFailed
		out.Err = fmt.Errorf("zai: video state %q: %w", obj.Status, llms.ErrJobFailed)
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
