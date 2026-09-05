package elevenlabs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestGenerateImage(t *testing.T) {
	var polls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/flows/image":
			body := requestBody(t, r)
			if body["model_id"] != "bytedance-seedream-5-lite" || body["prompt"] != "moon" || body["aspect_ratio"] != "16:9" || body["resolution"] != "2K" || body["seed"] != float64(42) {
				t.Error(body)
			}
			writeJSON(t, w, map[string]any{"id": "image-1", "status": "pending"})
		case "/v1/flows/image/image-1":
			if polls.Add(1) == 1 {
				writeJSON(t, w, map[string]any{"id": "image-1", "status": "generating"})
				return
			}
			writeJSON(t, w, map[string]any{"id": "image-1", "status": "completed", "content_url": "http://" + r.Host + "/asset", "content_mime_type": "image/png"})
		case "/asset":
			if r.Header.Get("xi-api-key") != "" {
				t.Error("key leaked to content")
			}
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	})
	out, err := c.GenerateImage(context.Background(), "moon", llms.WithImageModel("bytedance-seedream-5-lite"), llms.WithImageSeed(42), llms.WithImageSize("1K"), llms.WithImageAspectRatio("16:9"), llms.WithImageExtra(map[string]any{"resolution": "2K"}))
	if err != nil {
		t.Fatal(err)
	}
	a := out.Images[0]
	if out.Model != "bytedance-seedream-5-lite" || out.Usage.Unit != "" || a.MIMEType != "image/png" || string(a.Data) != "\x89PNG\r\n\x1a\n" || !strings.HasSuffix(a.URL, "/asset") || time.Until(a.ExpiresAt) < 54*time.Minute || time.Until(a.ExpiresAt) > 55*time.Minute {
		t.Fatal(out)
	}
}
func TestEditImage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			body := requestBody(t, r)
			refs, ok := body["images"].([]any)
			if !ok || len(refs) != 1 {
				t.Error(body)
			} else {
				ref, ok := refs[0].(map[string]any)
				if !ok || ref["type"] != "inline_base64" || ref["content_base64"] != "aW1hZ2U=" || ref["mime_type"] != "image/png" {
					t.Error(ref)
				}
			}
			if body["quality"] != "high" || body["background"] != "transparent" {
				t.Error(body)
			}
			writeJSON(t, w, map[string]any{"id": "edit", "status": "pending"})
		case "GET":
			if r.URL.Path == "/asset" {
				_, _ = w.Write([]byte("image"))
				return
			}
			writeJSON(t, w, map[string]any{"id": "edit", "status": "completed", "content_url": "http://" + r.Host + "/asset", "content_mime_type": "image/png"})
		}
	})
	out, err := c.EditImage(context.Background(), "moon", []llms.MediaInput{{Data: []byte("image"), MIMEType: "image/png"}}, llms.WithImageModel("gpt-image-1.5"), llms.WithImageQuality("high"), llms.WithImageExtra(map[string]any{"background": "transparent"}))
	if err != nil || string(out.Images[0].Data) != "image" {
		t.Fatal(out, err)
	}
}
func TestFlows_Moderation(t *testing.T) {
	for _, running := range []bool{false, true} {
		for _, kind := range []string{"image", "video"} {
			t.Run(kind+strconv.FormatBool(running), func(t *testing.T) {
				var polls atomic.Int32
				c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "POST" {
						writeJSON(t, w, map[string]any{"id": "job", "status": "pending"})
						return
					}
					if polls.Add(1) == 1 && running {
						writeJSON(t, w, map[string]any{"id": "job", "status": "generating"})
						return
					}
					writeJSON(t, w, map[string]any{"id": "job", "status": "failed", "failure_reason": "moderated", "error_message": "safety"})
				})
				var err error
				if kind == "image" {
					_, err = c.GenerateImage(context.Background(), "moon")
				} else {
					job, e := c.GenerateVideo(context.Background(), "moon")
					if e != nil {
						t.Fatal(e)
					}
					_, err = job.Wait(context.Background())
				}
				var moderation *llms.ModerationError
				stage := llms.ModerationInput
				if running {
					stage = llms.ModerationOutput
				}
				if !errors.Is(err, llms.ErrContentFiltered) || !errors.As(err, &moderation) || moderation.Stage != stage || moderation.Charged || moderation.Provider != "elevenlabs" {
					t.Fatalf("%+v %v", moderation, err)
				}
			})
		}
	}
}
func TestGenerateVideo(t *testing.T) {
	for _, duration := range []int{0, 4} {
		t.Run(strconv.Itoa(duration), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					body := requestBody(t, r)
					if body["model_id"] != "veo-3.1-fast-generate-001" || body["generate_audio"] != false || body["resolution"] != "720p" || body["aspect_ratio"] != "16:9" || body["negative_prompt"] != "noise" || body["seed"] != float64(7) || body["enhance_prompt"] != true || body["start_frame"] == nil || body["end_frame"] == nil {
						t.Error(body)
					}
					writeJSON(t, w, map[string]any{"id": "video-1", "status": "generating"})
					return
				}
				if r.URL.Path == "/asset" {
					if r.Header.Get("xi-api-key") != "" {
						t.Error("key leaked")
					}
					_, _ = w.Write([]byte("video"))
					return
				}
				writeJSON(t, w, map[string]any{"id": "video-1", "status": "completed", "content_url": "http://" + r.Host + "/asset", "content_mime_type": "video/mp4"})
			})
			frame := llms.MediaInput{Data: []byte("image"), MIMEType: "image/png"}
			job, err := c.GenerateVideo(context.Background(), "moon", llms.WithVideoDuration(duration), llms.WithVideoAudio(false), llms.WithVideoResolution("720p"), llms.WithVideoAspectRatio("16:9"), llms.WithVideoSeed(7), llms.WithVideoNegativePrompt("noise"), llms.WithVideoFirstFrame(frame), llms.WithVideoLastFrame(frame), llms.WithVideoExtra(map[string]any{"enhance_prompt": true}))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := job.(*llms.PollingVideoJob); !ok || job.ID() != "video-1" {
				t.Fatal(job)
			}
			out, err := job.Wait(context.Background())
			if err != nil || string(out.Videos[0].Data) != "video" || out.Videos[0].MIMEType != "video/mp4" || out.Usage.Cost != nil || out.Usage.Quantity != float64(duration) {
				t.Fatal(out, err)
			}
			unit := llms.MediaUnitSecond
			if duration == 0 {
				unit = ""
			}
			if out.Usage.Unit != unit {
				t.Fatal(out.Usage)
			}
			if !errors.Is(job.Cancel(context.Background()), llms.ErrJobCancelNotSupported) {
				t.Fatal("cancel")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestFlows_DownloadSSRF(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(strconv.FormatBool(allow), func(t *testing.T) {
			var downloads atomic.Int32
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := `{"id":"job","status":"pending"}`
				if r.Method == "GET" {
					body = `{"id":"job","status":"completed","content_url":"https://127.0.0.1/asset","content_mime_type":"image/png"}`
				}
				if r.URL.Path == "/asset" {
					downloads.Add(1)
					if r.Header.Get("xi-api-key") != "" {
						t.Error("key leaked")
					}
					body = "image"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			opts := []Option{WithAPIKey("test"), WithHTTPClient(&http.Client{Transport: transport})}
			if allow {
				opts = append(opts, WithAllowPrivateIPs())
			}
			c, err := New(opts...)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.GenerateImage(context.Background(), "moon")
			if allow {
				if err != nil || downloads.Load() != 1 {
					t.Fatal(err)
				}
			} else if err == nil || downloads.Load() != 0 {
				t.Fatal("private asset request escaped SSRF", err)
			}
		})
	}
}
func TestFlows_Validation(t *testing.T) {
	c, _ := New(WithAPIKey("test"))
	ctx := context.Background()
	for _, opts := range [][]llms.ImageOption{{llms.WithImageCount(2)}, {llms.WithImageSize("1024x1024")}, {llms.WithImageSeed(1)}, {llms.WithImageQuality("high")}, {llms.WithImageExtra(map[string]any{"model_id": 1})}, {llms.WithImageExtra(map[string]any{"background": "transparent"})}, {llms.WithImageModel("gpt-image-1"), llms.WithImageQuality("bad")}} {
		if _, err := c.GenerateImage(ctx, "moon", opts...); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	if _, err := c.GenerateImage(ctx, ""); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	for _, images := range [][]llms.MediaInput{nil, {{}}, {{URL: "https://example.com"}}, {{FileID: "id"}}, {{Data: []byte("x"), MIMEType: "audio/wav"}}} {
		if _, err := c.EditImage(ctx, "moon", images); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	if _, err := c.GenerateVideo(ctx, ""); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	for _, opts := range [][]llms.VideoOption{{llms.WithVideoDuration(-1)}, {llms.WithVideoDuration(5)}, {llms.WithVideoModel("bytedance-seedance-v2"), llms.WithVideoDuration(16)}, {llms.WithVideoResolution("bad")}, {llms.WithVideoAspectRatio("1:1")}, {llms.WithVideoReferenceImages([]llms.MediaInput{{Data: []byte("x")}})}, {llms.WithVideoFirstFrame(llms.MediaInput{URL: "https://example.com"})}, {llms.WithVideoExtra(map[string]any{"model_id": nil})}, {llms.WithVideoExtra(map[string]any{"generate_audio": "true"})}, {llms.WithVideoExtra(map[string]any{"duration_secs": 1.5})}} {
		if _, err := c.GenerateVideo(ctx, "moon", opts...); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
}
func TestFlows_Errors(t *testing.T) {
	for _, kind := range []string{"image", "video"} {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(402)
			_, _ = w.Write([]byte(`{"detail":{"code":"paid_plan_required","message":"Pro required"}}`))
		})
		if kind == "image" {
			if _, err := c.GenerateImage(context.Background(), "moon"); !errors.Is(err, llms.ErrPlanRequired) {
				t.Fatal(err)
			}
		} else if _, err := c.GenerateVideo(context.Background(), "moon"); !errors.Is(err, llms.ErrPlanRequired) {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		status, reason, id string
		pollCode           int
	}{{"failed", "model_error", "job", 200}, {"unexpected", "", "job", 200}, {"pending", "", "../x", 200}, {"pending", "", "job", 500}} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				writeJSON(t, w, map[string]any{"id": tc.id, "status": "pending"})
				return
			}
			w.WriteHeader(tc.pollCode)
			writeJSON(t, w, map[string]any{"status": tc.status, "failure_reason": tc.reason, "error_message": "broken"})
		})
		if _, err := c.GenerateImage(context.Background(), "moon"); err == nil {
			t.Fatal(tc)
		}
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"id": "job", "status": "pending"})
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GenerateImage(ctx, "moon"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c.options.PollPolicy.Timeout = 5 * time.Millisecond
	if _, err := c.GenerateImage(context.Background(), "moon"); err == nil {
		t.Fatal("poll timeout ignored")
	}
}
func TestVideo_ResultErrors(t *testing.T) {
	for _, status := range []string{"pending", "failed", "completed", "http-error"} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				writeJSON(t, w, map[string]any{"id": "job", "status": "pending"})
				return
			}
			if status == "http-error" {
				w.WriteHeader(500)
				return
			}
			writeJSON(t, w, map[string]any{"id": "job", "status": status, "failure_reason": "model_error", "error_message": "broken", "content_url": "invalid"})
		})
		job, err := c.GenerateVideo(context.Background(), "moon")
		if err != nil {
			t.Fatal(err)
		}
		p, ok := job.(*llms.PollingVideoJob)
		if !ok {
			t.Fatal(job)
		}
		if _, err = p.ResultFn(context.Background()); err == nil {
			t.Fatal(status)
		}
	}
}
