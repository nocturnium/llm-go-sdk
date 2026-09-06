package elevenlabs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestTranscribe(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/speech-to-text" || r.Method != "POST" {
			t.Error(r.URL)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		for k, v := range map[string]string{"model_id": "scribe_v2", "language_code": "en", "diarize": "true", "timestamps_granularity": "word", "num_speakers": "2", "tag_audio_events": "false"} {
			if r.FormValue(k) != v {
				t.Errorf("%s: %s", k, r.FormValue(k))
			}
		}
		if !reflect.DeepEqual(r.MultipartForm.Value["keyterms"], []string{"Nocturnium", "SDK"}) {
			t.Error(r.MultipartForm.Value)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = file.Close() }()
		data, _ := io.ReadAll(file)
		if header.Filename != "audio.mp3" || header.Header.Get("Content-Type") != "audio/mpeg" || string(data) != "ID3" {
			t.Error(header, string(data))
		}
		_, _ = w.Write([]byte(`{"language_code":"en","language_probability":0.9,"text":"Hi there","transcription_id":"tx","words":[{"text":"Hi","type":"word","start":0,"end":0.5,"speaker_id":"speaker_0"},{"text":" ","type":"spacing","start":0.5,"end":0.6},{"text":"there","type":"word","start":0.6,"end":2,"speaker_id":"speaker_1"},{"text":"noise","type":"audio_event","end":3}]}`))
	})
	out, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("ID3"), MIMEType: "audio/mpeg"}, llms.WithTranscribeDiarization(true), llms.WithTranscribeLanguage("en"), llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeKeyterms([]string{"Nocturnium", "SDK"}), llms.WithTranscribeExtra(map[string]any{"num_speakers": 2, "tag_audio_events": false, "timestamps_granularity": "none"}))
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "Hi there" || out.Language != "en" || out.Model != "scribe_v2" || len(out.Words) != 2 || out.Words[1].Speaker != "speaker_1" || out.Words[0].Word != "Hi" || out.DurationSeconds != 3 || out.Usage.Quantity != 3.0/60 || out.Usage.Unit != llms.MediaUnitMinute || out.Metadata["transcription_id"] != "tx" {
		t.Fatalf("%+v", out)
	}
}
func TestTranscribe_InputFormats(t *testing.T) {
	for mime, ext := range audioExtensions {
		t.Run(mime, func(t *testing.T) {
			fields, files, err := transcriptionInput(llms.MediaInput{Data: []byte("audio"), MIMEType: mime})
			if err != nil || len(fields) != 0 || len(files) != 1 || files[0].Filename != "audio."+ext || files[0].ContentType != mime {
				t.Fatal(fields, files, err)
			}
		})
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		if r.FormValue("cloud_storage_url") != "" || r.FormValue("source_url") != "https://example.com/audio.mp3" || r.FormValue("model_id") != "custom" || r.FormValue("timestamps_granularity") != "character" || len(r.MultipartForm.File) != 0 {
			t.Error(r.MultipartForm)
		}
		_, _ = w.Write([]byte(`{"text":"Hi","words":[]}`))
	})
	out, err := c.Transcribe(context.Background(), llms.MediaInput{URL: "https://example.com/audio.mp3"}, llms.WithTranscribeModel("custom"), llms.WithTranscribeExtra(map[string]any{"timestamps_granularity": "character"}))
	if err != nil || out.Usage.Unit != "" || out.Words == nil {
		t.Fatal(out, err)
	}
}
func TestTranscribe_Validation(t *testing.T) {
	c, _ := New(WithAPIKey("test"))
	for _, input := range []llms.MediaInput{{}, {FileID: "f"}, {URL: "http://example.com"}, {URL: "https://"}, {URL: "https://user:pass@example.com"}, {Data: []byte("x"), MIMEType: "audio/unknown"}, {Data: []byte("x"), URL: "https://example.com"}} {
		if _, err := c.Transcribe(context.Background(), input); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(input, err)
		}
	}
	input := llms.MediaInput{Data: []byte("x"), MIMEType: "audio/wav"}
	for _, extra := range []map[string]any{{"num_speakers": 0}, {"num_speakers": 33}, {"num_speakers": 1.5}, {"num_speakers": "1"}, {"timestamps_granularity": "bad"}, {"timestamps_granularity": 1}, {"tag_audio_events": "false"}} {
		if _, err := c.Transcribe(context.Background(), input, llms.WithTranscribeExtra(extra)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(extra, err)
		}
	}
	if _, err := c.Transcribe(context.Background(), input, llms.WithTranscribeKeyterms([]string{""})); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	c = testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"detail":"invalid"}`))
	})
	if _, err := c.Transcribe(context.Background(), input); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	c = testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"words":[{"type":"word","text":"Hi","start":1,"end":0}]}`))
	})
	if _, err := c.Transcribe(context.Background(), input); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}

func TestTranscribe_AudioDuration(t *testing.T) {
	for _, tc := range []struct {
		name, body, granularity string
		duration                float64
		unit                    llms.MediaUnit
	}{
		{"reported overrides words", `{"audio_duration_secs":1.5,"words":[{"type":"word","text":"Hi","end":3}]}`, "word", 1.5, llms.MediaUnitMinute},
		{"none still priced", `{"audio_duration_secs":12,"text":"Hi","words":[]}`, "none", 12, llms.MediaUnitMinute},
		{"zero falls back", `{"audio_duration_secs":0,"words":[{"type":"word","text":"Hi","end":3}]}`, "word", 3, llms.MediaUnitMinute},
		{"unknown duration", `{"audio_duration_secs":0,"words":[]}`, "none", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = r.MultipartForm.RemoveAll() }()
				if r.FormValue("timestamps_granularity") != tc.granularity {
					t.Error(r.MultipartForm)
				}
				_, _ = w.Write([]byte(tc.body))
			})
			out, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("audio"), MIMEType: "audio/mpeg"}, llms.WithTranscribeExtra(map[string]any{"timestamps_granularity": tc.granularity}))
			if err != nil {
				t.Fatal(err)
			}
			if out.DurationSeconds != tc.duration || out.Usage.Quantity != tc.duration/60 || out.Usage.Unit != tc.unit {
				t.Fatalf("%+v", out)
			}
		})
	}
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"audio_duration_secs":-1}`)) })
	if _, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("audio"), MIMEType: "audio/mpeg"}); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}
