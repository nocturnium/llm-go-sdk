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

func TestSpeech_CharacterLimit(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b := mediaJSON(t, r)
		if b["response_format"] != "wav" || b["input"] != strings.Repeat("é", 200) {
			t.Error(b)
		}
		fmt.Fprint(w, "RIFF")
	})
	if _, err := c.Synthesize(context.Background(), strings.Repeat("é", 200), llms.WithSpeechExtra(map[string]any{"response_format": "wav", "input": "bypass"})); err != nil {
		t.Fatal(err)
	}
	c = mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	for _, format := range []any{"mp3", "", nil, 42, []string{"wav"}} {
		if _, err := c.Synthesize(context.Background(), "hello", llms.WithSpeechExtra(map[string]any{"response_format": format})); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("response_format %v: %v", format, err)
		}
	}
	if _, err := c.Synthesize(context.Background(), strings.Repeat("é", 201)); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}

func TestTranscription_FormatsAndTranslation(t *testing.T) {
	for _, translate := range []bool{false, true} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			path := "/audio/transcriptions"
			if translate {
				path = "/audio/translations"
			}
			if r.URL.Path != path || r.FormValue("model") != "whisper-large-v3" || r.FormValue("url") != "https://example.com/audio.wav" {
				t.Error(r.URL.Path, r.MultipartForm.Value)
			}
			fmt.Fprint(w, "translated")
		})
		call := c.Transcribe
		if translate {
			call = c.Translate
		}
		out, err := call(context.Background(), llms.MediaInput{URL: "https://example.com/audio.wav"}, llms.WithTranscribeModel("whisper-large-v3"), llms.WithTranscribeLanguage("en"), llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeExtra(map[string]any{"response_format": "text", "timestamp_granularities[]": []string{"word"}}))
		if err != nil || out.Text != "translated" {
			t.Fatalf("%+v %v", out, err)
		}
	}
}
