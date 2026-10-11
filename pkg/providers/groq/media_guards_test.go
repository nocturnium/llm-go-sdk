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
			if r.URL.Path != path || r.FormValue("model") != "whisper-large-v3" || r.FormValue("url") != "" {
				t.Error(r.URL.Path, r.MultipartForm.Value)
			}
			fmt.Fprint(w, "translated")
		})
		call := c.Transcribe
		if translate {
			call = c.Translate
		}
		out, err := call(context.Background(), llms.MediaInput{Data: []byte("wav bytes"), MIMEType: "audio/wav"}, llms.WithTranscribeModel("whisper-large-v3"), llms.WithTranscribeLanguage("en"), llms.WithTranscribeExtra(map[string]any{"response_format": "text"}))
		if err != nil || out.Text != "translated" {
			t.Fatalf("%+v %v", out, err)
		}
	}
}

// Groq has no url field on its transcription routes, so a URL-only input must
// fail locally instead of sending an unsupported multipart field that the
// provider rejects.
func TestTranscribe_RejectsURLInput(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP request for rejected URL input") })
	if _, err := c.Transcribe(context.Background(), llms.MediaInput{URL: "https://example.com/audio.wav"}); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatalf("Transcribe(url) = %v, want ErrInvalidParameters", err)
	}
	if _, err := c.Translate(context.Background(), llms.MediaInput{URL: "https://example.com/audio.wav"}); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatalf("Translate(url) = %v, want ErrInvalidParameters", err)
	}
}

// A transcription Extra key that has a typed option is refused rather than
// silently replacing what the caller asked for; response_format has no typed
// option and stays open.
func TestTranscribe_ExtraCannotOverrideTypedFields(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	for _, key := range []string{"model", "file", "url", "language", "prompt"} {
		_, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("wav"), MIMEType: "audio/wav"},
			llms.WithTranscribeExtra(map[string]any{key: "hijacked"}))
		if !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("Extra %q: err = %v, want ErrInvalidParameters", key, err)
		}
	}
}

// Groq's speech route accepts neither Instructions nor Speed, so a caller that
// sets one is told rather than having it dropped.
func TestSynthesize_RejectsUnsupportedTypedOptions(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	for name, opt := range map[string]llms.SpeechOption{
		"instructions": llms.WithSpeechInstructions("read slowly"),
		"speed":        llms.WithSpeechSpeed(1.5),
	} {
		if _, err := c.Synthesize(context.Background(), "hi", llms.WithSpeechVoice("autumn"), opt); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// Word timestamps are valid only with verbose_json; text or json would be
// rejected by Groq, so the request must fail locally.
func TestTranscribe_WordTimestampsRequireVerboseJSON(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP request for a rejected format") })
	for _, format := range []string{"text", "json"} {
		_, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("wav bytes"), MIMEType: "audio/wav"}, llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeExtra(map[string]any{"response_format": format}))
		if !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("format %q: err = %v, want ErrInvalidParameters", format, err)
		}
	}
}
