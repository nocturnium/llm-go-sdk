package fal

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// defaultVideoDurationSeconds is what Hailuo renders (and bills) when the
// request omits duration.
const defaultVideoDurationSeconds = 6

type videoResult struct {
	Video struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		FileName    string `json:"file_name"`
		FileSize    int64  `json:"file_size"`
	} `json:"video"`
}

func videoBody(prompt string, o *llms.VideoOptions) (map[string]any, int, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, 0, fmt.Errorf("fal: video: %w", llms.ErrEmptyPrompt)
	}
	if o.Resolution != "" || o.AspectRatio != "" {
		return nil, 0, invalid("Resolution and AspectRatio are not configurable on this video endpoint")
	}
	if o.Audio != nil || o.Seed != nil || o.NegativePrompt != "" || o.FirstFrame != nil || o.LastFrame != nil || len(o.ReferenceImages) > 0 || o.OutputFormat != "" {
		return nil, 0, invalid("Audio, Seed, NegativePrompt, frames, ReferenceImages and OutputFormat have no mapping on this video endpoint")
	}
	body := map[string]any{"prompt": prompt}
	duration := defaultVideoDurationSeconds
	switch o.DurationSeconds {
	case 0:
	case 6, 10:
		duration = o.DurationSeconds
		body["duration"] = strconv.Itoa(duration)
	default:
		return nil, 0, invalid("DurationSeconds must be 6 or 10")
	}
	if err := mergeExtra(body, o.Extra, "prompt", "duration"); err != nil {
		return nil, 0, err
	}
	return body, duration, nil
}

// GenerateVideo submits a text-to-video request and returns *llms.PollingVideoJob.
// DurationSeconds accepts 6 or 10 (omitted bills 6). Wait downloads the MP4
// eagerly; Cancel issues the queue cancel route. Usage is billed seconds with
// Cost nil because fal per-model pricing is not tabulated.
func (c *Client) GenerateVideo(ctx context.Context, prompt string, opts ...llms.VideoOption) (job llms.VideoJob, err error) {
	o := llms.ApplyVideoOptions(opts...)
	if o.Model == "" {
		o.Model = c.options.VideoModel
	}
	ctx, finish := c.startOperation(ctx, "generate video", o.Model)
	defer func() { finish(err) }()
	body, duration, err := videoBody(prompt, o)
	if err != nil {
		return nil, err
	}
	q, err := c.submit(ctx, o.Model, body)
	if err != nil {
		return nil, err
	}
	model := o.Model
	polling := &llms.PollingVideoJob{JobID: q.RequestID, Policy: httpclient.PollPolicy(c.options.PollPolicy)}
	polling.PollFn = func(ctx context.Context) (*llms.JobStatus, error) {
		s, e := c.status(ctx, q)
		if e != nil {
			return nil, e
		}
		return jobStatus(s), nil
	}
	polling.CancelFn = func(ctx context.Context) error { return c.cancel(ctx, q) }
	polling.ResultFn = func(ctx context.Context) (*llms.VideoResponse, error) {
		var res videoResult
		header, e := c.result(ctx, q, &res)
		if e != nil {
			return nil, e
		}
		asset, e := c.fetchAsset(ctx, res.Video.URL, res.Video.ContentType)
		if e != nil {
			return nil, e
		}
		out := &llms.VideoResponse{Model: model, Videos: []llms.MediaAsset{asset}, Usage: llms.MediaUsage{Unit: llms.MediaUnitSecond, Quantity: float64(duration)}, Metadata: map[string]any{"request_id": q.RequestID}}
		if res.Video.FileName != "" {
			out.Metadata["file_name"] = res.Video.FileName
		}
		if res.Video.FileSize > 0 {
			out.Metadata["file_size"] = res.Video.FileSize
		}
		billableUnits(header, out.Metadata)
		return out, nil
	}
	return polling, nil
}
