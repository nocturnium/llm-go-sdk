package zai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestMedia_ContentSafetyCode(t *testing.T) {
	for _, status := range []int{200, 400} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"code":"1301","message":"content safety"}}`)
		})
		_, err := c.GenerateImage(context.Background(), "hi")
		var moderation *llms.ModerationError
		if !errors.As(err, &moderation) || moderation.Stage != llms.ModerationInput || !errors.Is(err, llms.ErrContentFiltered) {
			t.Fatal(err)
		}
	}
}
func TestImage_Extras(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			w.Header().Set("Content-Type", "image/jpeg")
			fmt.Fprint(w, "image")
			return
		}
		b := mediaJSON(t, r)
		if b["user_id"] != "user" || b["model"] != "custom" || b["prompt"] != "effective" {
			t.Error(b)
		}
		fmt.Fprintf(w, `{"data":[{"url":"http://%s/asset"}]}`, r.Host)
	})
	if _, err := c.GenerateImage(context.Background(), "hi", llms.WithImageExtra(map[string]any{"user_id": "user", "model": "custom", "prompt": "effective", "quality": "hd"})); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"n", "response_format"} {
		if _, err := c.GenerateImage(context.Background(), "hi", llms.WithImageExtra(map[string]any{key: 1})); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
}
func TestImage_InvalidEffectiveExtras(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	for _, extra := range []map[string]any{{"model": 1}, {"model": ""}, {"prompt": false}, {"prompt": ""}, {"quality": "bad"}, {"quality": 1}} {
		if _, err := c.GenerateImage(context.Background(), "hi", llms.WithImageExtra(extra)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
}

func TestTranscribe_OnlyWAVMP3(t *testing.T) {
	for _, mime := range []string{"audio/wav", "audio/mpeg", "audio/mp3", "audio/x-wav", "audio/mp4", "audio/flac", "audio/ogg", "audio/webm"} {
		allowed := mime == "audio/wav" || mime == "audio/mpeg" || mime == "audio/mp3"
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if !allowed {
				t.Error("unexpected HTTP")
			}
			fmt.Fprint(w, `{"text":"hi"}`)
		})
		_, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("audio"), MIMEType: mime})
		if allowed && err != nil || !allowed && !errors.Is(err, llms.ErrInvalidParameters) {
			t.Error(mime, err)
		}
	}
}

func TestTranscribe_UploadCap(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	_, err := c.Transcribe(context.Background(), llms.MediaInput{Data: make([]byte, 25*1024*1024+1), MIMEType: "audio/wav"})
	if !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}
