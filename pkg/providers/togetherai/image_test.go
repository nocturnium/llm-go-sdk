package togetherai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestImage_Generation(t *testing.T) {
	for _, inline := range []bool{false, true} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/asset" {
				if r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") == "" {
					t.Error("download headers")
				}
				fmt.Fprint(w, "image")
				return
			}
			if r.URL.Path != "/images/generations" {
				t.Error(r.URL.Path)
			}
			b := mediaJSON(t, r)
			if b["model"] != providerConfig.DefaultImageModel || b["prompt"] != "tree" {
				t.Error(b)
			}
			if b["response_format"] != "base64" || b["width"] != float64(1000) || b["height"] != float64(1000) {
				t.Error(b)
			}
			if _, ok := b["size"]; ok {
				t.Error("size leaked to wire")
			}
			if inline {
				fmt.Fprint(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`)
			} else {
				fmt.Fprintf(w, `{"data":[{"url":"http://%s/asset"}]}`, r.Host)
			}
		})
		out, err := c.GenerateImage(context.Background(), "tree", llms.WithImageSize("1000x1000"), llms.WithImageCount(1), llms.WithImageAspectRatio("1:1"), llms.WithImageSeed(42), llms.WithImageNegativePrompt("blur"), llms.WithImageFormat("png"))
		if err != nil || len(out.Images) != 1 || string(out.Images[0].Data) != "image" {
			t.Fatalf("%+v %v", out, err)
		}
		if out.Usage.Unit != llms.MediaUnitMegapixel || out.Usage.Quantity != 1 {
			t.Error(out.Usage)
		}
		if !inline && out.Images[0].URL == "" {
			t.Error("missing URL")
		}
	}
}

func TestImage_Validation(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	if _, err := c.GenerateImage(context.Background(), ""); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	for _, opts := range [][]llms.ImageOption{
		{llms.WithImageCount(5)},
		{llms.WithImageSize("bad")}, {llms.WithImageSize("0x1")}, {llms.WithImageExtra(map[string]any{"n": "bad"})}, {llms.WithImageExtra(map[string]any{"response_format": "b64_json"})}, {llms.WithImageExtra(map[string]any{"bad": make(chan int)})},
	} {
		if _, err := c.GenerateImage(context.Background(), "tree", opts...); err == nil {
			t.Fatal("missing validation error")
		}
	}
}

func TestImage_ResponseErrors(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":[{"b64_json":"%%%"}]}`, `{"data":[{"url":"http://127.0.0.1:1/missing"}]}`} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := c.GenerateImage(context.Background(), "tree"); err == nil {
			t.Fatal("missing response error")
		}
	}
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(402) })
	if _, err := c.GenerateImage(context.Background(), "tree"); !errors.Is(err, llms.ErrPlanRequired) {
		t.Fatal(err)
	}
}
