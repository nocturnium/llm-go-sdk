package featherless

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
	if caps.ImageGeneration != false || caps.VideoGeneration != false || caps.Speech != true || caps.Transcription != false {
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
	if out.Usage.Unit != llms.MediaUnitKChar || out.Usage.Quantity != .002 || out.Usage.Cost != nil {
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
