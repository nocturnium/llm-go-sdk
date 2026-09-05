package groq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestSpeech_ArabicVoice(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b := mediaJSON(t, r)
		if b["voice"] != "fahad" {
			t.Error(b)
		}
		fmt.Fprint(w, "wav")
	})
	opts := []llms.SpeechOption{llms.WithSpeechModel("canopylabs/orpheus-arabic-saudi")}
	if _, err := c.Synthesize(context.Background(), "hi", opts...); !errors.Is(err, llms.ErrInvalidParameters) || !strings.Contains(err.Error(), "voice") {
		t.Fatal(err)
	}
	opts = append(opts, llms.WithSpeechVoice("fahad"))
	if _, err := c.Synthesize(context.Background(), "hi", opts...); err != nil {
		t.Fatal(err)
	}
}
func TestTranscribe_DefaultFormat(t *testing.T) {
	for _, format := range []string{"verbose_json", "json"} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			defer r.MultipartForm.RemoveAll()
			if r.FormValue("response_format") != format {
				t.Error(r.MultipartForm)
			}
			fmt.Fprint(w, `{"text":"hi","duration":60}`)
		})
		var opts []llms.TranscribeOption
		if format == "json" {
			opts = append(opts, llms.WithTranscribeExtra(map[string]any{"response_format": "json"}))
		}
		out, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("wav"), MIMEType: "audio/wav"}, opts...)
		if err != nil || out.Usage.Quantity != 1 {
			t.Fatalf("%+v %v", out, err)
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
