package mistral

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestMedia_ForbiddenWithoutModeration(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error":{"message":"unauthorized","code":"forbidden"}}`)
	})
	_, err := c.Synthesize(context.Background(), "hi")
	var moderation *llms.ModerationError
	if err == nil || errors.As(err, &moderation) || errors.Is(err, llms.ErrContentFiltered) {
		t.Fatal(err)
	}
}
func TestTranscribe_AudioUsageAndRepeatedContext(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		defer r.MultipartForm.RemoveAll()
		if !reflect.DeepEqual(r.MultipartForm.Value["context_bias"], []string{"one", "two"}) || r.FormValue("context_bias[]") != "" {
			t.Error(r.MultipartForm)
		}
		fmt.Fprint(w, `{"text":"hi","usage":{"prompt_audio_seconds":90}}`)
	})
	out, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("wav"), MIMEType: "audio/wav"}, llms.WithTranscribeKeyterms([]string{"one", "two"}))
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.Unit != llms.MediaUnitMinute || out.Usage.Quantity != 1.5 {
		t.Error(out.Usage)
	}
	if cost, ok := llms.MediaCost("mistral", out.Model, out.Usage); !ok || math.Abs(cost-.0045) > 1e-12 {
		t.Errorf("%v %v", cost, ok)
	}
}
func TestSpeech_MIMETypes(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"audio_data":"aGk="}`) })
	for format, mime := range map[string]string{"mp3": "audio/mpeg", "pcm": "audio/L16", "opus": "audio/ogg", "wav": "audio/wav", "flac": "audio/flac"} {
		out, err := c.Synthesize(context.Background(), "hi", llms.WithSpeechFormat(llms.AudioFormat{Container: format}))
		if err != nil || out.Audio.MIMEType != mime {
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
