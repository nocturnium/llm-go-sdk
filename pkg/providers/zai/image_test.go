package zai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestImage_Generation(t *testing.T) {
	for _, filtered := range []bool{false, true} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/asset" {
				if r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") == "" {
					t.Error("download headers")
				}
				w.Header().Set("Content-Type", "image/jpeg")
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
			if _, ok := b["n"]; ok {
				t.Error("n is unsupported")
			}
			if _, ok := b["response_format"]; ok {
				t.Error("response_format unsupported")
			}
			if filtered {
				fmt.Fprintf(w, `{"data":[{"url":"http://%s/asset"}],"content_filter":[{"role":"assistant"}]}`, r.Host)
			} else {
				fmt.Fprintf(w, `{"data":[{"url":"http://%s/asset"}]}`, r.Host)
			}
		})
		out, err := c.GenerateImage(context.Background(), "tree", llms.WithImageSize("1024x1024"), llms.WithImageQuality("standard"))
		if err != nil || len(out.Images) != 1 || string(out.Images[0].Data) != "image" {
			t.Fatalf("%+v %v", out, err)
		}
		if out.Usage.Unit != llms.MediaUnitImage || out.Usage.Quantity != 1 || time.Until(out.Images[0].ExpiresAt) < 29*24*time.Hour || time.Until(out.Images[0].ExpiresAt) > 30*24*time.Hour {
			t.Error(out)
		}
		if out.Images[0].MIMEType != "image/jpeg" {
			t.Error(out.Images[0].MIMEType)
		}
		if out.Images[0].URL == "" {
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
		{llms.WithImageQuality("bad")},
		{llms.WithImageExtra(map[string]any{"response_format": "base64"})},
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
