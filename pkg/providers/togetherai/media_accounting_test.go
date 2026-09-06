package togetherai

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestImage_Accounting(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[{"b64_json":"aW1hZ2U="}]}`) })
	out, err := c.GenerateImage(context.Background(), "tree")
	if err != nil {
		t.Fatal(err)
	}
	if _, known := llms.MediaCost("togetherai", out.Model, out.Usage); known {
		t.Error("Schnell without dimensions was priced")
	}
	out, err = c.GenerateImage(context.Background(), "tree", llms.WithImageModel("black-forest-labs/FLUX.2-pro"))
	if err != nil {
		t.Fatal(err)
	}
	if cost, known := llms.MediaCost("togetherai", out.Model, out.Usage); !known || cost != .03 {
		t.Error(cost, known)
	}
}

func TestVideo_FlatCost(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/asset" {
			fmt.Fprint(w, "video")
			return
		}
		fmt.Fprintf(w, `{"id":"job","status":"completed","outputs":{"video_url":"http://%s/asset"}}`, r.Host)
	})
	job, err := c.GenerateVideo(context.Background(), "clouds", llms.WithVideoModel("openai/sora-2"), llms.WithVideoExtra(map[string]any{"seconds": 8, "output_format": "WEBM"}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := job.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.Cost == nil || *out.Usage.Cost != .8 || out.Usage.Quantity != 8 || out.Videos[0].MIMEType != "video/webm" {
		t.Error(out)
	}
}

func TestSpeech_Extras(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b := mediaJSON(t, r)
		if b["voice"] != "voice-id" || b["sample_rate"] != float64(24000) || b["response_encoding"] != "pcm_s16le" || b["stream"] != false {
			t.Error(b)
		}
		fmt.Fprint(w, "audio")
	})
	if _, err := c.Synthesize(context.Background(), "hello", llms.WithSpeechVoice("voice-id"), llms.WithSpeechFormat(llms.AudioFormat{Container: "wav", SampleRate: 24000}), llms.WithSpeechExtra(map[string]any{"response_encoding": "pcm_s16le", "stream": true})); err != nil {
		t.Fatal(err)
	}
}

func TestTranscribe_Diarization(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("diarize") != "true" || r.FormValue("timestamp_granularities") != "word" || r.FormValue("response_format") != "verbose_json" {
			t.Error(r.MultipartForm.Value)
		}
		fmt.Fprint(w, `{"text":"hello","words":[{"word":"hello","speaker_id":3}]}`)
	})
	out, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("x"), MIMEType: "audio/wav"}, llms.WithTranscribeDiarization(true), llms.WithTranscribeWordTimestamps(true))
	if err != nil {
		t.Fatal(err)
	}
	if out.Words[0].Speaker != "3" {
		t.Error(out.Words)
	}
}
