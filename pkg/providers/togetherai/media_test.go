package togetherai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func mediaTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := New(WithAPIKey("test"), WithBaseURL(server.URL), WithHTTPClient(server.Client()), WithTimeout(time.Second), WithAllowHTTP(), WithAllowPrivateIPs())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMediaCapabilities(t *testing.T) {
	c, err := New(WithAPIKey("test"))
	if err != nil {
		t.Fatal(err)
	}
	caps := c.Capabilities()
	if caps.ImageGeneration != true || caps.VideoGeneration != true || caps.Speech != true || caps.Transcription != true {
		t.Fatalf("capabilities: %+v", caps)
	}
	if _, err := c.EditImage(context.Background(), "x", nil); !errors.Is(err, llms.ErrImageEditNotSupported) {
		t.Fatal(err)
	}

}

func mediaJSON(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	if r.Header.Get("Authorization") != "Bearer test" {
		t.Error("missing authentication")
	}
	return body
}

func TestMediaSpeech(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" {
			t.Error(r.URL.Path)
		}
		b := mediaJSON(t, r)
		if b["model"] != providerConfig.DefaultSpeechModel || b["input"] != "hé" {
			t.Errorf("body: %v", b)
		}
		if b["voice"] != "af_bella" {
			t.Error(b)
		}
		w.Header().Set("Content-Type", "audio/wav")
		fmt.Fprint(w, "RIFF")
	})
	out, err := c.Synthesize(context.Background(), "hé")
	if err != nil || string(out.Audio.Data) != "RIFF" {
		t.Fatalf("%+v %v", out, err)
	}
	if out.Usage.Unit != llms.MediaUnitKChar || out.Usage.Quantity != .002 {
		t.Error(out.Usage)
	}
}

func TestMediaSpeech_Invalid(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	if _, err := c.Synthesize(context.Background(), " "); !errors.Is(err, llms.ErrEmptyText) {
		t.Fatal(err)
	}
	if _, err := c.Synthesize(context.Background(), "x", llms.WithSpeechFormat(llms.AudioFormat{Container: "invalid"})); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}

func TestMediaSpeech_Error(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		fmt.Fprint(w, `{"error":{"message":"plan"}}`)
	})
	if _, err := c.Synthesize(context.Background(), "hello"); !errors.Is(err, llms.ErrPlanRequired) {
		t.Fatal(err)
	}
}

func TestMediaTranscription(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Error(r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("model") != providerConfig.DefaultTranscriptionModel || r.FormValue("prompt") != "names" {
			t.Error(r.MultipartForm.Value)
		}
		file, h, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		if h.Filename != "audio.wav" || h.Header.Get("Content-Type") != "audio/wav" {
			t.Error(h)
		}
		fmt.Fprint(w, `{"text":"hello","duration":60,"words":[{"word":"hello","start":0,"end":1,"speaker_id":"A"}]}`)
	})
	out, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("RIFF"), MIMEType: "audio/wav"}, llms.WithTranscribePrompt("names"))
	if err != nil || out.Text != "hello" || len(out.Words) != 1 || out.Words[0].Speaker != "A" {
		t.Fatalf("%+v %v", out, err)
	}
	if out.Usage.Unit != llms.MediaUnitMinute || out.Usage.Quantity != 1 {
		t.Error(out.Usage)
	}
}

func TestMediaTranscription_Validation(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP") })
	for _, input := range []llms.MediaInput{{}, {FileID: "id"}, {Data: []byte("x"), MIMEType: "bad"}} {
		if _, err := c.Transcribe(context.Background(), input); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Error(err)
		}
	}
	input := llms.MediaInput{Data: []byte("x"), MIMEType: "audio/wav"}
	for _, extra := range []map[string]any{{"stream": true}, {"response_format": "bad"}, {"bad": map[string]any{}}} {
		if _, err := c.Transcribe(context.Background(), input, llms.WithTranscribeExtra(extra)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Error(err)
		}
	}
}

func TestMediaTranscription_Errors(t *testing.T) {
	for _, body := range []string{`{`, `{"error":{"code":"1113","message":"balance"}}`} {
		t.Run(body, func(t *testing.T) {
			c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			if _, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("x"), MIMEType: "audio/wav"}); err == nil {
				t.Fatal("missing error")
			}
		})
	}
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		fmt.Fprint(w, `{"error":{"message":"plan"}}`)
	})
	if _, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("x"), MIMEType: "audio/wav"}); !errors.Is(err, llms.ErrPlanRequired) {
		t.Fatal(err)
	}
}
