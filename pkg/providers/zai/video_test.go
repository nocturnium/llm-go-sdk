package zai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
		case "/videos/generations":
			if r.Method != http.MethodPost {
				t.Error(r.Method)
			}
			b := mediaJSON(t, r)
			if b["model"] != providerConfig.DefaultVideoModel || b["prompt"] != "clouds" {
				t.Error(b)
			}
			if b["duration"] != float64(5) || b["with_audio"] != true || b["size"] != "1280x720" || len(b["image_url"].([]any)) != 2 {
				t.Error(b)
			}
			fmt.Fprint(w, `{"id":"job","task_status":"PROCESSING"}`)
		case "/async-result/job":
			if r.Method != http.MethodGet {
				t.Error(r.Method)
			}
			if polls.Add(1) == 1 {
				fmt.Fprint(w, `{"task_status":"PROCESSING"}`)
				return
			}
			fmt.Fprintf(w, `{"task_status":"SUCCESS","video_result":[{"url":"http://%s/asset","cover_image_url":"https://example.com/cover"}]}`, r.Host)
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

	job, err := c.GenerateVideo(context.Background(), "clouds", llms.WithVideoDuration(5), llms.WithVideoResolution("1280x720"), llms.WithVideoAudio(true), llms.WithVideoFirstFrame(llms.MediaInput{URL: "https://example.com/first"}), llms.WithVideoLastFrame(llms.MediaInput{URL: "https://example.com/last"}))
	if err != nil {
		t.Fatal(err)
	}
	if job.ID() != "job" {
		t.Error(job.ID())
	}
	status, err := job.Poll(context.Background())
	if err != nil || status.State != llms.JobRunning {
		t.Fatalf("%+v %v", status, err)
	}
	job.(*llms.PollingVideoJob).Policy = httpclient.PollPolicy{Initial: time.Millisecond, Max: time.Millisecond}
	out, err := job.Wait(context.Background())
	if err != nil || len(out.Videos) != 1 || string(out.Videos[0].Data) != "video" {
		t.Fatalf("%+v %v", out, err)
	}
	if out.Usage.Unit != llms.MediaUnitSecond || out.Usage.Quantity != 5 || out.Usage.Cost == nil || *out.Usage.Cost != .20 {
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
		{llms.WithVideoExtra(map[string]any{"duration": -2})},
		{llms.WithVideoLastFrame(llms.MediaInput{URL: "https://example.com/last"})}, {llms.WithVideoExtra(map[string]any{"fps": 20})},
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
	for _, state := range []string{"FAIL", "unknown"} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"id":"job","task_status":%q}`, state) })
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
				fmt.Fprint(w, `{"id":"job","task_status":"PROCESSING"}`)
				return
			}
			switch response {
			case "http-error":
				w.WriteHeader(402)
			case "envelope":
				fmt.Fprint(w, `{"error":{"code":"1113","message":"balance"}}`)
			case "empty":
				fmt.Fprint(w, `{"task_status":"SUCCESS"}`)
			case "download":
				fmt.Fprint(w, `{"task_status":"SUCCESS","video_result":[{"url":"http://127.0.0.1:1/missing"}]}`)
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

func TestVideo_PromptLimit(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	if _, err := c.GenerateVideo(context.Background(), strings.Repeat("é", 513)); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}

func TestVideo_DefaultDurationBilled(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/videos/generations":
			if _, ok := mediaJSON(t, r)["duration"]; ok {
				t.Error("duration must be omitted when the caller did not set one")
			}
			fmt.Fprint(w, `{"id":"job","task_status":"PROCESSING"}`)
		case "/async-result/job":
			fmt.Fprintf(w, `{"task_status":"SUCCESS","video_result":[{"url":"http://%s/asset"}]}`, r.Host)
		case "/asset":
			fmt.Fprint(w, "video")
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	})
	job, err := c.GenerateVideo(context.Background(), "clouds")
	if err != nil {
		t.Fatal(err)
	}
	out, err := job.Wait(context.Background())
	if err != nil || out.Usage.Unit != llms.MediaUnitSecond || out.Usage.Quantity != defaultVideoDurationSeconds {
		t.Fatalf("%+v %v", out, err)
	}
}
