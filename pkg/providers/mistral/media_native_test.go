package mistral

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

func TestSpeech_NativeJSON(t *testing.T) {
	for _, body := range []string{`{"audio_data":"UklGRg=="}`, `{}`, `{"audio_data":"%%%"}`} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			b := mediaJSON(t, r)
			if b["voice_id"] != "speaker" || b["model"] != "custom" || b["response_format"] != "pcm" {
				t.Error(b)
			}
			if _, ok := b["ref_audio"]; ok {
				t.Error("reference overrides explicit voice")
			}
			fmt.Fprint(w, body)
		})
		out, err := c.Synthesize(context.Background(), "hello", llms.WithSpeechModel("custom"), llms.WithSpeechVoice("speaker"), llms.WithSpeechFormat(llms.AudioFormat{Container: "pcm"}), llms.WithSpeechExtra(map[string]any{"ref_audio": "reference"}))
		if body == `{"audio_data":"UklGRg=="}` {
			if err != nil || string(out.Audio.Data) != "RIFF" {
				t.Fatalf("%+v %v", out, err)
			}
		} else if err == nil {
			t.Fatal("missing decode error")
		}
	}
}

func TestMedia_Moderation(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error":{"message":"moderated","code":"content_filtered"}}`)
	})
	_, err := c.Synthesize(context.Background(), "hello")
	var moderation *llms.ModerationError
	var api *httpclient.APIError
	if !errors.As(err, &moderation) || !errors.As(err, &api) || !errors.Is(err, llms.ErrContentFiltered) {
		t.Fatal(err)
	}
}

func TestTranscribe_NativeFields(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		for k, v := range map[string]string{"file_url": "https://example.com/audio.wav", "diarize": "true", "timestamp_granularities": "word", "context_bias": "Codex", "language": "en"} {
			if r.FormValue(k) != v {
				t.Errorf("%s: %v", k, r.MultipartForm.Value)
			}
		}
		if r.FormValue("response_format") != "" {
			t.Error("OpenAI-only field")
		}
		fmt.Fprint(w, `{"text":"hello"}`)
	})
	if _, err := c.Transcribe(context.Background(), llms.MediaInput{URL: "https://example.com/audio.wav"}, llms.WithTranscribeLanguage("en"), llms.WithTranscribeDiarization(true), llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeKeyterms([]string{"Codex"})); err != nil {
		t.Fatal(err)
	}
}
