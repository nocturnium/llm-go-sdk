package togetherai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

func TestVideo_NativeJob(t *testing.T) {
	var polls atomic.Int32
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/videos":
			if r.Method != http.MethodPost {
				t.Error(r.Method)
			}
			b := mediaJSON(t, r)
			if b["model"] != providerConfig.DefaultVideoModel || b["prompt"] != "clouds" {
				t.Error(b)
			}
			if b["seconds"] != "4" || b["generate_audio"] != true || b["ratio"] != "16:9" || b["media"] == nil {
				t.Error(b)
			}
			fmt.Fprint(w, `{"id":"job","status":"in_progress"}`)
		case "/v2/videos/job":
			if r.Method != http.MethodGet {
				t.Error(r.Method)
			}
			switch polls.Add(1) {
			case 1:
				fmt.Fprint(w, `{"status":"queued"}`)
				return
			case 2:
				fmt.Fprint(w, `{"status":"in_progress"}`)
				return
			}
			fmt.Fprintf(w, `{"status":"completed","seconds":"6","outputs":{"cost":123,"video_url":"http://%s/asset"}}`, r.Host)
		case "/asset":
			if r.Header.Get("Authorization") != "" {
				t.Error("credentials leaked to asset")
			}
			fmt.Fprint(w, "video")
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	})
	c.options.BaseURL += "/v1"
	job, err := c.GenerateVideo(context.Background(), "clouds", llms.WithVideoDuration(4), llms.WithVideoResolution("720p"), llms.WithVideoAspectRatio("16:9"), llms.WithVideoAudio(true), llms.WithVideoSeed(7), llms.WithVideoNegativePrompt("rain"), llms.WithVideoFormat("mp4"), llms.WithVideoFirstFrame(llms.MediaInput{URL: "https://example.com/first"}), llms.WithVideoLastFrame(llms.MediaInput{URL: "https://example.com/last"}))
	if err != nil {
		t.Fatal(err)
	}
	if job.ID() != "job" {
		t.Error(job.ID())
	}
	status, err := job.Poll(context.Background())
	if err != nil || status.State != llms.JobQueued {
		t.Fatalf("%+v %v", status, err)
	}
	job.(*llms.PollingVideoJob).Policy = httpclient.PollPolicy{Initial: time.Millisecond, Max: time.Millisecond}
	out, err := job.Wait(context.Background())
	if err != nil || len(out.Videos) != 1 || string(out.Videos[0].Data) != "video" {
		t.Fatalf("%+v %v", out, err)
	}
	if out.Usage.Unit != llms.MediaUnitSecond || out.Usage.Quantity != 6 || out.Usage.Cost != nil || out.Metadata["outputs_cost"] != float64(123) {
		t.Error(out.Usage)
	}
	if err := job.Cancel(context.Background()); !errors.Is(err, llms.ErrJobCancelNotSupported) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := job.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestVideo_Validation(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	if _, err := c.GenerateVideo(context.Background(), ""); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	for _, opts := range [][]llms.VideoOption{
		{llms.WithVideoDuration(-1)},
		{llms.WithVideoFirstFrame(llms.MediaInput{})},
		{llms.WithVideoFirstFrame(llms.MediaInput{Data: []byte("x")})},
		{llms.WithVideoReferenceImages([]llms.MediaInput{{URL: "https://example.com/image"}})},
		{llms.WithVideoExtra(map[string]any{"bad": make(chan int)})},
		{llms.WithVideoExtra(map[string]any{"seconds": -2})},
		{llms.WithVideoFormat("bad")},
	} {
		if _, err := c.GenerateVideo(context.Background(), "clouds", opts...); err == nil {
			t.Fatal("missing error")
		}
	}
}

func TestVideo_Failures(t *testing.T) {
	for _, body := range []string{`{"id":""}`, `{"id":"../escape"}`, `{"error":{"code":"1113","message":"balance"}}`} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := c.GenerateVideo(context.Background(), "clouds"); err == nil {
			t.Fatal("missing error")
		}
	}
	for _, state := range []string{"failed"} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"id":"job","status":%q}`, state) })
		job, err := c.GenerateVideo(context.Background(), "clouds")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := job.Wait(context.Background()); !errors.Is(err, llms.ErrJobFailed) {
			t.Fatal(err)
		}
		if _, err := job.(*llms.PollingVideoJob).ResultFn(context.Background()); !errors.Is(err, llms.ErrJobFailed) {
			t.Fatal(err)
		}
	}
	for _, response := range []string{"http-error", "envelope", "empty", "download"} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				fmt.Fprint(w, `{"id":"job","status":"in_progress"}`)
				return
			}
			switch response {
			case "http-error":
				w.WriteHeader(402)
			case "envelope":
				fmt.Fprint(w, `{"error":{"code":"1113","message":"balance"}}`)
			case "empty":
				fmt.Fprint(w, `{"status":"completed"}`)
			case "download":
				fmt.Fprint(w, `{"status":"completed","seconds":"6","outputs":{"video_url":"http://127.0.0.1:1/missing"}}`)
			}
		})
		job, err := c.GenerateVideo(context.Background(), "clouds")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := job.Wait(context.Background()); err == nil {
			t.Fatal("missing result error")
		}
		if _, err := job.(*llms.PollingVideoJob).ResultFn(context.Background()); err == nil {
			t.Fatal("missing direct result error")
		}
	}
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(402) })
	if _, err := c.GenerateVideo(context.Background(), "clouds"); !errors.Is(err, llms.ErrPlanRequired) {
		t.Fatal(err)
	}
}
