package zai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

func TestMedia_QuotaExceeded(t *testing.T) {
	for _, status := range []int{200, 400} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"code":"1113","message":"Insufficient balance"}}`)
		})
		calls := []func() error{
			func() error { _, err := c.GenerateImage(context.Background(), "tree"); return err },
			func() error { _, err := c.GenerateVideo(context.Background(), "clouds"); return err },
			func() error {
				_, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("x"), MIMEType: "audio/wav"})
				return err
			},
		}
		for _, call := range calls {
			err := call()
			var api *httpclient.APIError
			if !errors.Is(err, llms.ErrQuotaExceeded) || !errors.As(err, &api) || api.Code != "1113" {
				t.Fatal(err)
			}
		}
	}
}

func TestImage_Moderation(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[],"content_filter":[{"role":"assistant"}]}`)
	})
	_, err := c.GenerateImage(context.Background(), "tree")
	var moderation *llms.ModerationError
	if !errors.As(err, &moderation) || moderation.Stage != llms.ModerationOutput || !errors.Is(err, llms.ErrContentFiltered) {
		t.Fatal(err)
	}
}

func TestTranscribe_Hotwords(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("hotwords[]") != "Codex" || r.FormValue("model") != "custom" {
			t.Error(r.MultipartForm.Value)
		}
		fmt.Fprint(w, `{"text":"Codex"}`)
	})
	if _, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("x"), MIMEType: "audio/wav"}, llms.WithTranscribeModel("custom"), llms.WithTranscribeKeyterms([]string{"Codex"})); err != nil {
		t.Fatal(err)
	}
}
