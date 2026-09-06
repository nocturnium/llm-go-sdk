package fal

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func transcriptionFixture(f *fakeQueue) {
	f.result = map[string]any{
		"text":                 "Hello there.",
		"chunks":               []map[string]any{{"timestamp": []any{0.0, 1.5}, "text": " Hello", "speaker": "SPEAKER_00"}, {"timestamp": []any{1.5, nil}, "text": " there."}},
		"inferred_languages":   []string{"en", "de"},
		"diarization_segments": []map[string]any{{"timestamp": []any{0.0, 3.0}, "speaker": "SPEAKER_00"}},
	}
}
func TestAudioURL(t *testing.T) {
	got, err := audioURL(llms.MediaInput{URL: "https://example.com/a.mp3"})
	if err != nil || got != "https://example.com/a.mp3" {
		t.Fatalf("url: %v %s", err, got)
	}
	got, err = audioURL(llms.MediaInput{Data: []byte("abc"), MIMEType: "audio/mpeg"})
	if err != nil || got != "data:audio/mpeg;base64,"+base64.StdEncoding.EncodeToString([]byte("abc")) {
		t.Fatalf("data: %v %s", err, got)
	}
	if _, err = audioURL(llms.MediaInput{Data: []byte("abc"), MIMEType: "video/mp4"}); err != nil {
		t.Fatal(err)
	}
	invalidCases := map[string]llms.MediaInput{
		"none":      {},
		"two":       {URL: "https://x", Data: []byte("a")},
		"file id":   {FileID: "f"},
		"http":      {URL: "http://example.com/a.mp3"},
		"userinfo":  {URL: "https://u:p@example.com/a.mp3"},
		"no host":   {URL: "https:///a.mp3"},
		"no mime":   {Data: []byte("a")},
		"text mime": {Data: []byte("a"), MIMEType: "text/plain"},
		"too large": {Data: make([]byte, maxInlineAudio+1), MIMEType: "audio/wav"},
	}
	for name, input := range invalidCases {
		if _, err = audioURL(input); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
func TestTranscribeBody(t *testing.T) {
	input := llms.MediaInput{URL: "https://example.com/a.mp3"}
	body, err := transcribeBody(input, "transcribe", &llms.TranscribeOptions{Language: "en", Prompt: "names", Diarize: true, WordTimestamps: true, Extra: map[string]any{"num_speakers": 2, "batch_size": 8}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"audio_url": "https://example.com/a.mp3", "task": "transcribe", "language": "en", "prompt": "names", "diarize": true, "chunk_level": "word", "num_speakers": 2, "batch_size": 8}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body: %v", body)
	}
	body, err = transcribeBody(input, "translate", &llms.TranscribeOptions{})
	if err != nil || len(body) != 2 || body["task"] != "translate" {
		t.Fatalf("minimal: %v %v", err, body)
	}
	invalidCases := map[string]*llms.TranscribeOptions{
		"keyterms":      {Keyterms: []string{"x"}},
		"speakers zero": {Extra: map[string]any{"num_speakers": 0}},
		"speakers frac": {Extra: map[string]any{"num_speakers": 1.5}},
		"speakers text": {Extra: map[string]any{"num_speakers": "2"}},
		"reserved url":  {Extra: map[string]any{"audio_url": "x"}},
		"reserved task": {Extra: map[string]any{"task": "x"}},
		"reserved lang": {Extra: map[string]any{"language": "x"}},
		"reserved diar": {Extra: map[string]any{"diarize": true}},
		"reserved lvl":  {Extra: map[string]any{"chunk_level": "word"}},
		"reserved prmt": {Extra: map[string]any{"prompt": "x"}},
	}
	for name, o := range invalidCases {
		if _, err = transcribeBody(input, "transcribe", o); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err = transcribeBody(llms.MediaInput{FileID: "f"}, "transcribe", &llms.TranscribeOptions{}); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}
func TestTranscribe(t *testing.T) {
	f := newFakeQueue(t)
	f.statuses = []map[string]any{{"status": "IN_QUEUE"}, {"status": "COMPLETED"}}
	f.resultHeader = map[string]string{"X-Fal-Billable-Units": "0.05"}
	transcriptionFixture(f)
	out, err := f.client().Transcribe(context.Background(), llms.MediaInput{Data: []byte("wav"), MIMEType: "audio/wav"}, llms.WithTranscribeDiarization(true))
	if err != nil {
		t.Fatal(err)
	}
	rec := f.lastSubmit()
	if rec.Path != "/fal-ai/whisper" || rec.Body["task"] != "transcribe" || rec.Body["diarize"] != true || !strings.HasPrefix(rec.Body["audio_url"].(string), "data:audio/wav;base64,") {
		t.Fatalf("submit: %+v", rec)
	}
	if out.Text != "Hello there." || out.Language != "en" || out.Model != DefaultTranscriptionModel || out.DurationSeconds != 1.5 || len(out.Words) != 0 {
		t.Fatalf("response: %+v", out)
	}
	wantSegments := []llms.TranscriptSegment{{Start: 0, End: 1.5, Text: "Hello", Speaker: "SPEAKER_00"}, {Start: 1.5, End: 1.5, Text: "there."}}
	if !reflect.DeepEqual(out.Segments, wantSegments) {
		t.Fatalf("segments: %+v", out.Segments)
	}
	if out.Usage.Unit != llms.MediaUnitMinute || out.Usage.Quantity != 1.5/60 || out.Usage.Cost != nil {
		t.Fatalf("usage: %+v", out.Usage)
	}
	if out.Metadata["request_id"] != "req-1" || out.Metadata["task"] != "transcribe" || out.Metadata["billable_units"] != 0.05 || !reflect.DeepEqual(out.Metadata["inferred_languages"], []string{"en", "de"}) {
		t.Fatalf("metadata: %v", out.Metadata)
	}
	segments, ok := out.Metadata["diarization_segments"].([]map[string]any)
	if !ok || len(segments) != 1 || segments[0]["speaker"] != "SPEAKER_00" || segments[0]["end"] != 3.0 {
		t.Fatalf("diarization: %v", out.Metadata["diarization_segments"])
	}
}
func TestTranscribeWordsAndTranslate(t *testing.T) {
	f := newFakeQueue(t)
	f.result = map[string]any{"text": "Hi you", "chunks": []map[string]any{{"timestamp": []any{0.0, 0.4}, "text": " Hi"}, {"timestamp": []any{0.4, 0.9}, "text": " you"}}}
	out, err := f.client(WithTranscriptionModel("fal-ai/whisper-custom")).Translate(context.Background(), llms.MediaInput{URL: "https://example.com/a.mp3"}, llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeModel("fal-ai/wizper"))
	if err != nil {
		t.Fatal(err)
	}
	rec := f.lastSubmit()
	if rec.Path != "/fal-ai/wizper" || rec.Body["task"] != "translate" || rec.Body["chunk_level"] != "word" || rec.Body["audio_url"] != "https://example.com/a.mp3" {
		t.Fatalf("submit: %+v", rec)
	}
	wantWords := []llms.TranscriptWord{{Start: 0, End: 0.4, Word: "Hi"}, {Start: 0.4, End: 0.9, Word: "you"}}
	if !reflect.DeepEqual(out.Words, wantWords) || len(out.Segments) != 0 || out.Language != "" || out.Model != "fal-ai/wizper" || out.Metadata["task"] != "translate" {
		t.Fatalf("response: %+v", out)
	}
	if out.DurationSeconds != 0.9 || math.Abs(out.Usage.Quantity-0.9/60) > 1e-12 {
		t.Fatalf("usage: %+v", out.Usage)
	}
}
func TestTranscribeNoChunksAndErrors(t *testing.T) {
	f := newFakeQueue(t)
	f.result = map[string]any{"text": "silence"}
	c := f.client()
	out, err := c.Transcribe(context.Background(), llms.MediaInput{URL: "https://example.com/a.mp3"})
	if err != nil || out.Text != "silence" || out.Usage.Unit != "" || out.Usage.Quantity != 0 || out.DurationSeconds != 0 {
		t.Fatalf("no chunks: %v %+v", err, out)
	}
	f.result = map[string]any{"text": "bad good", "chunks": []map[string]any{{"timestamp": []any{2.0, 1.0}, "text": "bad"}, {"timestamp": []any{-1.0, 1.0}, "text": "neg"}, {"timestamp": []any{1.0, 2.0}, "text": "good"}}}
	out, err = c.Transcribe(context.Background(), llms.MediaInput{URL: "https://example.com/a.mp3"})
	if err != nil || out.Text != "bad good" || len(out.Segments) != 1 || out.Segments[0].Text != "good" || out.Metadata["skipped_chunks"] != 2 || out.DurationSeconds != 2 || out.Usage.Unit != llms.MediaUnitMinute {
		t.Fatalf("skipped chunks: %v %+v", err, out)
	}
	f.result = map[string]any{"text": "all bad", "chunks": []map[string]any{{"timestamp": []any{2.0, 1.0}, "text": "bad"}}}
	out, err = c.Transcribe(context.Background(), llms.MediaInput{URL: "https://example.com/a.mp3"})
	if err != nil || out.Metadata["skipped_chunks"] != 1 || out.Usage.Unit != "" || out.DurationSeconds != 0 {
		t.Fatalf("all skipped: %v %+v", err, out)
	}
	if _, err = c.Transcribe(context.Background(), llms.MediaInput{FileID: "f"}); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	f.submitStatus, f.submitBody = 422, map[string]any{"detail": []map[string]any{{"msg": "audio_url required", "type": "missing"}}}
	if _, err = c.Transcribe(context.Background(), llms.MediaInput{URL: "https://example.com/a.mp3"}); !errors.Is(err, llms.ErrInvalidParameters) || !strings.Contains(err.Error(), "audio_url required") {
		t.Fatal(err)
	}
	f.submitStatus = 0
	f.statuses = []map[string]any{{"status": "COMPLETED", "error": "slow", "error_type": "generation_timeout"}}
	if _, err = c.Transcribe(context.Background(), llms.MediaInput{URL: "https://example.com/a.mp3"}); !errors.Is(err, llms.ErrTimeout) {
		t.Fatal(err)
	}
}
func TestTimestamp(t *testing.T) {
	one, two := 1.0, 2.0
	if s, e := timestamp(nil); s != 0 || e != 0 {
		t.Fatal(s, e)
	}
	if s, e := timestamp([]*float64{&one}); s != 1 || e != 1 {
		t.Fatal(s, e)
	}
	if s, e := timestamp([]*float64{&one, &two}); s != 1 || e != 2 {
		t.Fatal(s, e)
	}
	if s, e := timestamp([]*float64{nil, &two}); s != 0 || e != 2 {
		t.Fatal(s, e)
	}
}
