package fal

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func speechFixture(f *fakeQueue) {
	f.result = map[string]any{"audio": map[string]any{"url": f.assetURL("a.wav"), "content_type": "audio/wav", "file_name": "a.wav", "file_size": 99}}
	f.asset, f.assetType = []byte("RIFF-bytes"), "audio/wav"
}
func TestSpeechBody(t *testing.T) {
	speed := 1.2
	body, err := speechBody("Hello", DefaultSpeechModel, &llms.SpeechOptions{Voice: "am_adam", Speed: &speed, Format: llms.AudioFormat{Container: "wav"}, Extra: map[string]any{"custom": true}})
	if err != nil || !reflect.DeepEqual(body, map[string]any{"prompt": "Hello", "voice": "am_adam", "speed": 1.2, "custom": true}) {
		t.Fatalf("body: %v %v", err, body)
	}
	body, err = speechBody("Hello", DefaultSpeechModel, &llms.SpeechOptions{})
	if err != nil || len(body) != 1 {
		t.Fatalf("minimal: %v %v", err, body)
	}
	for voice := range kokoroAmericanVoices {
		if _, err = speechBody("Hello", DefaultSpeechModel, &llms.SpeechOptions{Voice: voice}); err != nil {
			t.Fatalf("%s: %v", voice, err)
		}
	}
	if _, err = speechBody("Hello", "fal-ai/kokoro/british-english", &llms.SpeechOptions{Voice: "bf_emma"}); err != nil {
		t.Fatalf("other model voice passthrough: %v", err)
	}
	if _, err = speechBody(" \n", DefaultSpeechModel, &llms.SpeechOptions{}); !errors.Is(err, llms.ErrEmptyText) {
		t.Fatal(err)
	}
	zero, nan, inf := 0.0, math.NaN(), math.Inf(1)
	invalidCases := map[string]*llms.SpeechOptions{
		"voice":       {Voice: "bf_emma"},
		"speed zero":  {Speed: &zero},
		"speed nan":   {Speed: &nan},
		"speed inf":   {Speed: &inf},
		"timestamps":  {Timestamps: true},
		"language":    {Language: "en"},
		"instruction": {Instructions: "cheerful"},
		"container":   {Format: llms.AudioFormat{Container: "mp3"}},
		"sample rate": {Format: llms.AudioFormat{SampleRate: 24000}},
		"bitrate":     {Format: llms.AudioFormat{BitRate: 64000}},
		"encoding":    {Format: llms.AudioFormat{Encoding: "pcm_s16le"}},
		"reserved":    {Extra: map[string]any{"prompt": "x"}},
		"reserved v":  {Extra: map[string]any{"voice": "x"}},
		"reserved s":  {Extra: map[string]any{"speed": 1}},
	}
	for name, o := range invalidCases {
		if _, err = speechBody("Hello", DefaultSpeechModel, o); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
func TestSynthesize(t *testing.T) {
	f := newFakeQueue(t)
	f.statuses = []map[string]any{{"status": "IN_PROGRESS"}, {"status": "COMPLETED"}}
	f.resultHeader = map[string]string{"X-Fal-Billable-Units": "1"}
	speechFixture(f)
	out, err := f.client().Synthesize(context.Background(), "Hello world", llms.WithSpeechVoice("af_sky"), llms.WithSpeechSpeed(0.9))
	if err != nil {
		t.Fatal(err)
	}
	rec := f.lastSubmit()
	if rec.Path != "/fal-ai/kokoro/american-english" || rec.Body["prompt"] != "Hello world" || rec.Body["voice"] != "af_sky" || rec.Body["speed"] != 0.9 {
		t.Fatalf("submit: %+v", rec)
	}
	if string(out.Audio.Data) != "RIFF-bytes" || out.Audio.MIMEType != "audio/wav" || out.Audio.URL != f.assetURL("a.wav") || out.Format.Container != "wav" || out.Model != DefaultSpeechModel || out.Alignment != nil {
		t.Fatalf("response: %+v", out)
	}
	if out.Usage.Unit != llms.MediaUnitKChar || out.Usage.Quantity != 0.011 || out.Usage.Cost != nil {
		t.Fatalf("usage: %+v", out.Usage)
	}
	if out.Metadata["request_id"] != "req-1" || out.Metadata["file_name"] != "a.wav" || out.Metadata["billable_units"] != 1.0 {
		t.Fatalf("metadata: %v", out.Metadata)
	}
}
func TestSynthesizeContainerFromContentType(t *testing.T) {
	f := newFakeQueue(t)
	f.result = map[string]any{"audio": map[string]any{"url": f.assetURL("a.mp3"), "content_type": "audio/mpeg"}}
	f.asset = []byte("mp3")
	out, err := f.client(WithSpeechModel("fal-ai/other-tts")).Synthesize(context.Background(), "Hi", llms.WithSpeechVoice("anything"))
	if err != nil || out.Format.Container != "mp3" || out.Model != "fal-ai/other-tts" {
		t.Fatalf("mp3: %v %+v", err, out)
	}
	for mime, container := range map[string]string{"audio/ogg": "ogg", "audio/flac": "flac", "audio/mp4": "m4a", "audio/x-wav; charset=binary": "wav", "": "wav"} {
		if got := audioContainer(mime); got != container {
			t.Fatalf("%s: %s", mime, got)
		}
	}
}
func TestSynthesizeErrors(t *testing.T) {
	f := newFakeQueue(t)
	speechFixture(f)
	c := f.client()
	if _, err := c.Synthesize(context.Background(), ""); !errors.Is(err, llms.ErrEmptyText) {
		t.Fatal(err)
	}
	if _, err := c.Synthesize(context.Background(), "Hi", llms.WithSpeechTimestamps(true)); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	f.submitStatus, f.submitBody = 403, map[string]any{"detail": "Forbidden"}
	if _, err := c.Synthesize(context.Background(), "Hi"); !errors.Is(err, llms.ErrAuthenticationFailed) {
		t.Fatal(err)
	}
	f.submitStatus = 0
	f.statuses = []map[string]any{{"status": "COMPLETED", "error": "boom", "error_type": "internal_server_error"}}
	if _, err := c.Synthesize(context.Background(), "Hi"); !errors.Is(err, llms.ErrServiceUnavailable) {
		t.Fatal(err)
	}
	f.statuses = nil
	f.result = map[string]any{"audio": map[string]any{}}
	if _, err := c.Synthesize(context.Background(), "Hi"); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatal(err)
	}
}
func TestStreamSpeechUnsupported(t *testing.T) {
	f := newFakeQueue(t)
	ch, err := f.client().StreamSpeech(context.Background(), "Hi")
	if ch != nil || !errors.Is(err, llms.ErrSpeechStreamNotSupported) {
		t.Fatal(err)
	}
}
