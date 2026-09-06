package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestVideoValidation(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request reached server") })
	for _, opts := range [][]llms.VideoOption{
		{llms.WithVideoExtra(map[string]any{"callback_url": "http://example.com"})},
		{llms.WithVideoDuration(-1)}, {llms.WithVideoFirstFrame(llms.MediaInput{})},
		{llms.WithVideoFirstFrame(llms.MediaInput{FileID: "file"})}, {llms.WithVideoLastFrame(llms.MediaInput{Data: []byte("x")})},
		{llms.WithVideoReferenceImages([]llms.MediaInput{{URL: "https://example.com"}})},
		{llms.WithVideoExtra(map[string]any{"callback_url": 123})}, {llms.WithVideoExtra(map[string]any{"callback_url": "%"})},
	} {
		if _, err := c.GenerateVideo(context.Background(), "moon", opts...); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	if _, err := c.GenerateVideo(context.Background(), ""); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	for _, id := range []string{"", "..", ".", "v/1", "v?1"} {
		c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"id":%q}`, id) })
		if _, err := c.GenerateVideo(context.Background(), "moon"); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
}
func TestTranscriptionValidation(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request reached server") })
	audio := llms.MediaInput{Data: []byte("audio"), MIMEType: "audio/mpeg"}
	for _, opts := range [][]llms.TranscribeOption{
		{llms.WithTranscribeDiarization(true)},
		{llms.WithTranscribeExtra(map[string]any{"response_format": 123})},
		{llms.WithTranscribeExtra(map[string]any{"response_format": "text"})},
		{llms.WithTranscribeExtra(map[string]any{"model": "other/model"})},
		{llms.WithTranscribeExtra(map[string]any{"language": "fr"})},
		{llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeExtra(map[string]any{"bad": []int{1}})},
		{llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeExtra(map[string]any{"stream": true})},
	} {
		if _, err := c.Transcribe(context.Background(), audio, opts...); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	for _, audio := range []llms.MediaInput{{}, {URL: "https://example.com/audio"}, {Data: make([]byte, 25*1024*1024+1)}} {
		if _, err := c.Transcribe(context.Background(), audio, llms.WithTranscribeWordTimestamps(true)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
}
func TestVideoResultErrors(t *testing.T) {
	for _, mode := range []string{"poll-error", "content-error", "expired", "running", "no-assets", "decode-error"} {
		t.Run(mode, func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					fmt.Fprint(w, `{"id":"v1"}`)
					return
				}
				if mode == "poll-error" || (mode == "content-error" && r.URL.Path == "/api/v1/videos/v1/content") {
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"message":"denied"}}`)
					return
				}
				switch mode {
				case "expired":
					fmt.Fprint(w, `{"status":"expired"}`)
				case "running":
					fmt.Fprint(w, `{"status":"in_progress"}`)
				case "no-assets":
					fmt.Fprint(w, `{"status":"completed"}`)
				case "decode-error":
					fmt.Fprint(w, `invalid`)
				default:
					fmt.Fprint(w, `{"status":"completed","unsigned_urls":["https://example.com/asset"]}`)
				}
			})
			job, err := c.GenerateVideo(context.Background(), "moon")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "poll-error" {
				if _, err := job.Poll(context.Background()); !errors.Is(err, llms.ErrAuthenticationFailed) {
					t.Fatal(err)
				}
			}
			if _, err := job.(*llms.PollingVideoJob).ResultFn(context.Background()); err == nil {
				t.Fatal("expected result error")
			}
		})
	}
}
func TestDefaultSecurity(t *testing.T) {
	for _, opts := range [][]Option{nil, {WithAllowHTTP()}, {WithAllowPrivateIPs()}} {
		c, err := New(append([]Option{WithAPIKey("key"), WithBaseURL("http://127.0.0.1:1")}, opts...)...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ListVideoModels(context.Background()); err == nil || !strings.Contains(err.Error(), "URL validation failed") || (!strings.Contains(err.Error(), "HTTPS") && !strings.Contains(err.Error(), "private") && !strings.Contains(err.Error(), "loopback")) {
			t.Fatal("allowed insecure endpoint")
		}
	}
}
