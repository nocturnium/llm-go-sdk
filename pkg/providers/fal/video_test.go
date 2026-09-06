package fal

import (
	"context"
	"errors"
	"reflect"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func videoFixture(f *fakeQueue) {
	f.result = map[string]any{"video": map[string]any{"url": f.assetURL("v.mp4"), "content_type": "video/mp4", "file_name": "v.mp4", "file_size": 1234}}
	f.asset, f.assetType = []byte("mp4-bytes"), "video/mp4"
}
func TestVideoBody(t *testing.T) {
	body, duration, err := videoBody("wave", &llms.VideoOptions{DurationSeconds: 10, Extra: map[string]any{"prompt_optimizer": false}})
	if err != nil || duration != 10 || !reflect.DeepEqual(body, map[string]any{"prompt": "wave", "duration": "10", "prompt_optimizer": false}) {
		t.Fatalf("body: %v %d %v", err, duration, body)
	}
	body, duration, err = videoBody("wave", &llms.VideoOptions{})
	if err != nil || duration != 6 || len(body) != 1 {
		t.Fatalf("default: %v %d %v", err, duration, body)
	}
	body, duration, err = videoBody("wave", &llms.VideoOptions{DurationSeconds: 6})
	if err != nil || duration != 6 || body["duration"] != "6" {
		t.Fatalf("six: %v %d %v", err, duration, body)
	}
	if _, _, err = videoBody("", &llms.VideoOptions{}); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	audio, seed := true, int64(1)
	frame := llms.MediaInput{URL: "https://example.com/a.png"}
	invalidCases := map[string]*llms.VideoOptions{
		"duration":   {DurationSeconds: 5},
		"resolution": {Resolution: "720p"},
		"aspect":     {AspectRatio: "16:9"},
		"audio":      {Audio: &audio},
		"seed":       {Seed: &seed},
		"negative":   {NegativePrompt: "x"},
		"first":      {FirstFrame: &frame},
		"last":       {LastFrame: &frame},
		"references": {ReferenceImages: []llms.MediaInput{frame}},
		"format":     {OutputFormat: "mp4"},
		"reserved":   {Extra: map[string]any{"duration": "6"}},
		"reserved p": {Extra: map[string]any{"prompt": "x"}},
	}
	for name, o := range invalidCases {
		if _, _, err = videoBody("wave", o); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
func TestGenerateVideoWait(t *testing.T) {
	f := newFakeQueue(t)
	f.statuses = []map[string]any{{"status": "IN_QUEUE", "queue_position": 4}, {"status": "IN_PROGRESS", "logs": []any{}}, {"status": "COMPLETED"}}
	f.resultHeader = map[string]string{"X-Fal-Billable-Units": "1"}
	videoFixture(f)
	job, err := f.client().GenerateVideo(context.Background(), "wave", llms.WithVideoDuration(10))
	if err != nil {
		t.Fatal(err)
	}
	rec := f.lastSubmit()
	if rec.Path != "/fal-ai/minimax/hailuo-02/standard/text-to-video" || rec.Body["duration"] != "10" {
		t.Fatalf("submit: %+v", rec)
	}
	if job.ID() != "req-1" {
		t.Fatal(job.ID())
	}
	status, err := job.Poll(context.Background())
	if err != nil || status.State != llms.JobQueued || *status.Progress != 0 {
		t.Fatalf("queued: %v %+v", err, status)
	}
	status, err = job.Poll(context.Background())
	if err != nil || status.State != llms.JobRunning {
		t.Fatalf("running: %v %+v", err, status)
	}
	out, err := job.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Videos) != 1 || string(out.Videos[0].Data) != "mp4-bytes" || out.Videos[0].MIMEType != "video/mp4" || out.Videos[0].URL != f.assetURL("v.mp4") {
		t.Fatalf("videos: %+v", out.Videos)
	}
	if out.Model != DefaultVideoModel || out.Usage.Unit != llms.MediaUnitSecond || out.Usage.Quantity != 10 || out.Usage.Cost != nil {
		t.Fatalf("usage: %+v", out.Usage)
	}
	if out.Metadata["request_id"] != "req-1" || out.Metadata["file_name"] != "v.mp4" || out.Metadata["file_size"] != int64(1234) || out.Metadata["billable_units"] != 1.0 {
		t.Fatalf("metadata: %v", out.Metadata)
	}
}
func TestGenerateVideoDefaultDurationAndModel(t *testing.T) {
	f := newFakeQueue(t)
	videoFixture(f)
	job, err := f.client(WithVideoModel("fal-ai/custom/video")).GenerateVideo(context.Background(), "wave")
	if err != nil {
		t.Fatal(err)
	}
	rec := f.lastSubmit()
	if _, ok := rec.Body["duration"]; ok || rec.Path != "/fal-ai/custom/video" {
		t.Fatalf("submit: %+v", rec)
	}
	out, err := job.Wait(context.Background())
	if err != nil || out.Usage.Quantity != 6 || out.Model != "fal-ai/custom/video" {
		t.Fatalf("wait: %v %+v", err, out)
	}
}
func TestGenerateVideoFailures(t *testing.T) {
	f := newFakeQueue(t)
	videoFixture(f)
	c := f.client()
	if _, err := c.GenerateVideo(context.Background(), "wave", llms.WithVideoDuration(7)); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	f.submitStatus, f.submitBody = 429, map[string]any{"detail": "Too many", "error_type": "rate_limited"}
	if _, err := c.GenerateVideo(context.Background(), "wave"); !errors.Is(err, llms.ErrRateLimited) {
		t.Fatal(err)
	}
	f.submitStatus = 0
	f.statuses = []map[string]any{{"status": "COMPLETED", "error": "nsfw", "error_type": "content_policy_violation"}}
	job, err := c.GenerateVideo(context.Background(), "wave")
	if err != nil {
		t.Fatal(err)
	}
	status, err := job.Poll(context.Background())
	if err != nil || status.State != llms.JobModerated {
		t.Fatalf("moderated poll: %v %+v", err, status)
	}
	var m *llms.ModerationError
	if _, err = job.Wait(context.Background()); !errors.As(err, &m) || m.Stage != llms.ModerationInput {
		t.Fatalf("moderated wait: %v", err)
	}
	f.statuses = []map[string]any{{"status": "COMPLETED", "error": "boom", "error_type": "downstream_service_error"}}
	if _, err = job.Wait(context.Background()); !errors.Is(err, llms.ErrJobFailed) || !errors.Is(err, llms.ErrServiceUnavailable) {
		t.Fatalf("failed wait: %v", err)
	}
	f.statusCode, f.statusBody = 500, map[string]any{"detail": "boom"}
	if _, err = job.Poll(context.Background()); !errors.Is(err, llms.ErrServiceUnavailable) {
		t.Fatal(err)
	}
	f.statusCode, f.statuses = 0, nil
	f.resultStatus, f.result = 504, map[string]any{"detail": "slow", "error_type": "generation_timeout"}
	if _, err = job.Wait(context.Background()); !errors.Is(err, llms.ErrTimeout) {
		t.Fatal(err)
	}
	f.resultStatus, f.result = 0, map[string]any{"video": map[string]any{}}
	if _, err = job.Wait(context.Background()); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatal(err)
	}
	videoFixture(f)
	f.assetStatus = 403
	if _, err = job.Wait(context.Background()); !errors.Is(err, llms.ErrAuthenticationFailed) {
		t.Fatal(err)
	}
}
func TestGenerateVideoCancel(t *testing.T) {
	f := newFakeQueue(t)
	videoFixture(f)
	job, err := f.client().GenerateVideo(context.Background(), "wave")
	if err != nil {
		t.Fatal(err)
	}
	if err = job.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.cancelStatus, f.cancelBody = 400, map[string]any{"status": "ALREADY_COMPLETED"}
	if err = job.Cancel(context.Background()); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = job.Cancel(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
