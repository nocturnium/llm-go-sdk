package togetherai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestVideo_ProxyPrefix(t *testing.T) {
	for _, prefix := range []string{"", "/proxy", "/proxy/v10"} {
		for _, suffix := range []string{"", "/v1"} {
			t.Run(prefix+suffix, func(t *testing.T) {
				c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != prefix+"/v2/videos" {
						t.Error(r.URL.Path)
					}
					fmt.Fprint(w, `{"id":"job","status":"queued","seconds":"4"}`)
				})
				c.options.BaseURL += prefix + suffix
				if _, err := c.GenerateVideo(context.Background(), "clouds", llms.WithVideoDuration(4)); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	if defaultOptions().BaseURL != "https://api.together.xyz/v1" {
		t.Fatal("chat URL changed")
	}
}

func TestVideo_StatusAndSeconds(t *testing.T) {
	for wire, want := range map[string]llms.JobState{"queued": llms.JobQueued, "in_progress": llms.JobRunning, "completed": llms.JobSucceeded, "failed": llms.JobFailed, "cancelled": llms.JobCancelled, "future": llms.JobRunning} { //nolint:misspell // Together wire spelling.
		got := videoStatus(&videoObject{Status: wire})
		if got.State != want || got.Cost != nil {
			t.Errorf("%s: %+v", wire, got)
		}
	}
	for _, value := range []string{`"6"`, `6`} {
		var obj videoObject
		if err := json.Unmarshal([]byte(`{"seconds":`+value+`}`), &obj); err != nil || obj.Seconds == nil || *obj.Seconds != 6 {
			t.Fatalf("%+v %v", obj, err)
		}
	}
	for _, value := range []string{`"bad"`, `1.5`, `-1`, `true`, `"\uXXXX"`} {
		var seconds videoSeconds
		if err := json.Unmarshal([]byte(value), &seconds); err == nil {
			t.Error(value)
		}
	}
}

func TestSpeech_NativeStream(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		fail       bool
	}{
		{"success", "data: {\"object\":\"audio.tts.chunk\",\"b64\":\"aGk=\"}\n\ndata: [DONE]\n\n", false},
		{"base64", "data: {\"object\":\"audio.tts.chunk\",\"b64\":\"%%%\"}\n\n", true},
		{"json", "data: {\n\n", true},
		{"unknown", "data: {\"object\":\"error\"}\n\n", true},
		{"truncated", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				b := mediaJSON(t, r)
				if b["voice"] != "af_bella" || b["language"] != "en" || b["stream"] != true || b["response_format"] != "raw" {
					t.Error(b)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, tc.body)
			})
			ch, err := c.StreamSpeech(context.Background(), "hi", llms.WithSpeechLanguage("en"))
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			var usage *llms.MediaUsage
			for chunk := range ch {
				if chunk.Usage != nil {
					usage = chunk.Usage
					continue
				}
				count++
				if (chunk.Err != nil) != tc.fail {
					t.Error(chunk.Err)
				}
				if !tc.fail && string(chunk.Data) != "hi" {
					t.Error(chunk)
				}
			}
			if count != 1 {
				t.Fatal(count)
			}
			if tc.fail != (usage == nil) || (!tc.fail && (usage.Unit != llms.MediaUnitKChar || usage.Quantity != .002)) {
				t.Fatalf("usage = %+v fail = %v", usage, tc.fail)
			}
		})
	}
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(402) })
	if _, err := c.StreamSpeech(context.Background(), "hi"); !errors.Is(err, llms.ErrPlanRequired) {
		t.Fatal(err)
	}
	if _, err := c.StreamSpeech(context.Background(), ""); !errors.Is(err, llms.ErrEmptyText) {
		t.Fatal(err)
	}
	if _, err := c.StreamSpeech(context.Background(), "hi", llms.WithSpeechFormat(llms.AudioFormat{Container: "wav"})); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	if _, err := c.Synthesize(context.Background(), "hi", llms.WithSpeechFormat(llms.AudioFormat{Container: "mulaw"})); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}

func TestImage_DefaultJPEGAndRate(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b := mediaJSON(t, r)
		if b["output_format"] != "jpeg" {
			t.Error(b)
		}
		fmt.Fprint(w, `{"data":[{"b64_json":"aGk="}]}`)
	})
	for _, size := range []string{"", "1000x1000"} {
		out, err := c.GenerateImage(context.Background(), "x", llms.WithImageModel("google/imagen-4.0-fast"), llms.WithImageSize(size))
		if err != nil {
			t.Fatal(err)
		}
		if out.Images[0].MIMEType != "image/jpeg" {
			t.Error(out.Images)
		}
		cost, ok := llms.MediaCost("togetherai", out.Model, out.Usage)
		if size == "" && (ok || out.Usage.Unit != "") {
			t.Error(out.Usage)
		}
		if size != "" && (!ok || cost != .02 || out.Usage.Unit != llms.MediaUnitMegapixel) {
			t.Error(out.Usage)
		}
	}
}

func TestTranscribe_URLAndFormat(t *testing.T) {
	for _, format := range []string{"verbose_json", "json"} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			defer r.MultipartForm.RemoveAll()
			if r.FormValue("file") != "https://example.com/audio.wav" || r.FormValue("response_format") != format || r.FormValue("timestamp_granularities") != "word" || r.FormValue("timestamp_granularities[]") != "" {
				t.Error(r.MultipartForm)
			}
			fmt.Fprint(w, `{"text":"hi","duration":60}`)
		})
		opts := []llms.TranscribeOption{llms.WithTranscribeWordTimestamps(true)}
		if format == "json" {
			opts = append(opts, llms.WithTranscribeExtra(map[string]any{"response_format": "json"}))
		}
		out, err := c.Transcribe(context.Background(), llms.MediaInput{URL: "https://example.com/audio.wav"}, opts...)
		if err != nil || out.Usage.Quantity != 1 {
			t.Fatalf("%+v %v", out, err)
		}
	}
}

func TestTranscribe_UploadCap(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	_, err := c.Transcribe(context.Background(), llms.MediaInput{Data: make([]byte, 80*1024*1024+1), MIMEType: "audio/wav"})
	if !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}
