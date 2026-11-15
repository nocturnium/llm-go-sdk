package cloudflare

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// Live /ai/run bodies, 2026-11-14 (whisper trimmed to one segment).
const (
	liveRerank  = `{"result":{"meta":{"cost_metric_name_1":"input_tokens","cost_metric_value_1":34,"neurons":0.00960938},"response":[{"id":1,"score":0.2040037214756012},{"id":0,"score":0.00003741322143469006}]},"success":true,"errors":[],"messages":[]}`
	liveWhisper = `{"result":{"transcription_info":{"language":"en","language_probability":0.9995,"duration":1.728,"duration_after_vad":1.728},"text":"Hello from Worker's AI.","word_count":4,"segments":[{"start":0,"end":1.44,"text":" Hello from Worker's AI.","words":[{"word":" Hello","start":0,"end":0.58},{"word":" from","start":0.58,"end":0.84}]}],"vtt":"WEBVTT"},"success":true,"errors":[],"messages":[]}`
)

// jpegBytes starts with the JPEG magic the live flux response carried.
var jpegBytes = []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}

func TestRerank(t *testing.T) {
	var path string
	var body map[string]any
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, liveRerank)
	})
	docs := []string{"Paris is in France.", "Machine learning trains models from data."}
	resp, err := c.Rerank(context.Background(), "What is machine learning?", docs, llms.WithTopN(2), llms.WithReturnDocuments(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "/run/@cf/baai/bge-reranker-base") || body["top_k"] != 2.0 {
		t.Errorf("request %s %v", path, body)
	}
	if len(resp.Results) != 2 || resp.Results[0].Index != 1 || resp.Results[0].Document != docs[1] || resp.Results[0].Score != 0.2040037214756012 {
		t.Errorf("results %+v", resp.Results)
	}
	if resp.Usage.TotalTokens != 34 || resp.Model != DefaultRerankModel {
		t.Errorf("usage/model %+v %s", resp.Usage, resp.Model)
	}
}

func TestRerank_Rejections(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent") })
	for name, call := range map[string]func() error{
		"empty query":  func() error { _, err := c.Rerank(context.Background(), " ", []string{"a"}); return err },
		"no documents": func() error { _, err := c.Rerank(context.Background(), "q", nil); return err },
		"empty doc":    func() error { _, err := c.Rerank(context.Background(), "q", []string{""}); return err },
		"other model": func() error {
			_, err := c.Rerank(context.Background(), "q", []string{"a"}, llms.WithRerankModel("@cf/x/y"))
			return err
		},
	} {
		if err := call(); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRerank_IndexOutOfRange(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":{"response":[{"id":5,"score":1}]},"success":true}`)
	})
	if _, err := c.Rerank(context.Background(), "q", []string{"a"}); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateImage(t *testing.T) {
	var body map[string]any
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/run/@cf/black-forest-labs/flux-1-schnell") {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"result":{"image":"`+base64.StdEncoding.EncodeToString(jpegBytes)+`","usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"neurons":57.6}},"success":true,"errors":[]}`)
	})
	resp, err := c.GenerateImage(context.Background(), "a red square", llms.WithImageCount(1), llms.WithImageExtra(map[string]any{"steps": 2}))
	if err != nil {
		t.Fatal(err)
	}
	if body["prompt"] != "a red square" || body["steps"] != 2.0 || len(body) != 2 {
		t.Errorf("body %v", body)
	}
	if len(resp.Images) != 1 || resp.Images[0].MIMEType != "image/jpeg" || string(resp.Images[0].Data) != string(jpegBytes) || resp.Usage.Unit != llms.MediaUnitImage || resp.Metadata["neurons"] != 57.6 {
		t.Errorf("resp %+v", resp)
	}
}

func TestGenerateImage_Rejections(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent") })
	for name, opts := range map[string][]llms.ImageOption{
		"two images":   {llms.WithImageCount(2)},
		"size":         {llms.WithImageSize("1024x1024")},
		"seed":         {llms.WithImageSeed(1)},
		"bad steps":    {llms.WithImageExtra(map[string]any{"steps": 9})},
		"half steps":   {llms.WithImageExtra(map[string]any{"steps": 2.5})},
		"unknown key":  {llms.WithImageExtra(map[string]any{"guidance": 3})},
		"other model":  {llms.WithImageModel("@cf/stabilityai/stable-diffusion-xl-base-1.0")},
		"aspect ratio": {llms.WithImageAspectRatio("16:9")},
	} {
		if _, err := c.GenerateImage(context.Background(), "x", opts...); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := c.GenerateImage(context.Background(), " "); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Errorf("empty prompt: %v", err)
	}
	if _, err := c.GenerateImage(context.Background(), strings.Repeat("a", 2049)); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Errorf("long prompt: %v", err)
	}
}

func TestGenerateImage_BadBase64(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":{"image":"%%%"},"success":true}`)
	})
	if _, err := c.GenerateImage(context.Background(), "x"); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestSynthesize(t *testing.T) {
	var body map[string]any
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/run/@cf/deepgram/aura-2-en") {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Cf-Ai-Neurons", "60.00")
		_, _ = w.Write([]byte{0xff, 0xf3, 0x64, 0xc4})
	})
	resp, err := c.Synthesize(context.Background(), "Hello from Workers AI.", llms.WithSpeechVoice("luna"), llms.WithSpeechFormat(llms.AudioFormat{Encoding: "mp3", SampleRate: 24000}))
	if err != nil {
		t.Fatal(err)
	}
	if body["text"] != "Hello from Workers AI." || body["speaker"] != "luna" || body["encoding"] != "mp3" || body["sample_rate"] != 24000.0 {
		t.Errorf("body %v", body)
	}
	if resp.Audio.MIMEType != "audio/mpeg" || len(resp.Audio.Data) != 4 || resp.Usage.Unit != llms.MediaUnitKChar || resp.Usage.Quantity != 0.022 || resp.Metadata["neurons"] != 60.0 {
		t.Errorf("resp %+v", resp)
	}
}

func TestSynthesize_Rejections(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent") })
	for name, opts := range map[string][]llms.SpeechOption{
		"language":     {llms.WithSpeechLanguage("en")},
		"speed":        {llms.WithSpeechSpeed(1.5)},
		"instructions": {llms.WithSpeechInstructions("cheerful")},
		"other model":  {llms.WithSpeechModel("@cf/myshell-ai/melotts")},
		"reserved":     {llms.WithSpeechExtra(map[string]any{"speaker": "x"})},
		"model key":    {llms.WithSpeechExtra(map[string]any{"model": "x"})},
	} {
		if _, err := c.Synthesize(context.Background(), "hi", opts...); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := c.Synthesize(context.Background(), " "); !errors.Is(err, llms.ErrEmptyText) {
		t.Errorf("empty text: %v", err)
	}
	if _, err := c.StreamSpeech(context.Background(), "hi"); !errors.Is(err, llms.ErrSpeechStreamNotSupported) {
		t.Errorf("stream: %v", err)
	}
}

func TestSynthesize_NonAudioBody(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>error</html>")
	})
	if _, err := c.Synthesize(context.Background(), "hi"); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateImage_NotAnImage(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":{"image":"`+base64.StdEncoding.EncodeToString([]byte("plain text"))+`"},"success":true}`)
	})
	if _, err := c.GenerateImage(context.Background(), "x"); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestSynthesize_JSONErrorBody(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":{},"success":true}`)
	})
	if _, err := c.Synthesize(context.Background(), "hi"); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestTranscribe(t *testing.T) {
	var body map[string]any
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/run/@cf/openai/whisper-large-v3-turbo") {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, liveWhisper)
	})
	audio := llms.MediaInput{Data: []byte{1, 2, 3}, MIMEType: "audio/mpeg"}
	resp, err := c.Transcribe(context.Background(), audio, llms.WithTranscribeLanguage("en"), llms.WithTranscribePrompt("Workers AI"), llms.WithTranscribeWordTimestamps(true))
	if err != nil {
		t.Fatal(err)
	}
	if body["audio"] != "AQID" || body["language"] != "en" || body["initial_prompt"] != "Workers AI" {
		t.Errorf("body %v", body)
	}
	if resp.Text != "Hello from Worker's AI." || resp.Language != "en" || resp.DurationSeconds != 1.728 || len(resp.Segments) != 1 || resp.Segments[0].Text != "Hello from Worker's AI." {
		t.Errorf("resp %+v", resp)
	}
	if len(resp.Words) != 2 || resp.Words[0].Word != "Hello" || resp.Words[1].End != 0.84 {
		t.Errorf("words %+v", resp.Words)
	}
	if resp.Usage.Unit != llms.MediaUnitMinute || resp.Usage.Quantity != 1.728/60 {
		t.Errorf("usage %+v", resp.Usage)
	}
	// Words are omitted unless asked for.
	resp, err = c.Transcribe(context.Background(), audio)
	if err != nil || len(resp.Words) != 0 {
		t.Fatalf("words without WordTimestamps: %+v %v", resp, err)
	}
}

func TestTranscribe_Rejections(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent") })
	inline := llms.MediaInput{Data: []byte{1}, MIMEType: "audio/wav"}
	for name, tc := range map[string]struct {
		audio llms.MediaInput
		opts  []llms.TranscribeOption
	}{
		"url":         {audio: llms.MediaInput{URL: "https://example.com/a.wav"}},
		"diarize":     {audio: inline, opts: []llms.TranscribeOption{llms.WithTranscribeDiarization(true)}},
		"keyterms":    {audio: inline, opts: []llms.TranscribeOption{llms.WithTranscribeKeyterms([]string{"a"})}},
		"other model": {audio: inline, opts: []llms.TranscribeOption{llms.WithTranscribeModel("@cf/deepgram/nova-3")}},
		"reserved":    {audio: inline, opts: []llms.TranscribeOption{llms.WithTranscribeExtra(map[string]any{"audio": "x"})}},
		"unset typed": {audio: inline, opts: []llms.TranscribeOption{llms.WithTranscribeExtra(map[string]any{"language": "en"})}},
	} {
		if _, err := c.Transcribe(context.Background(), tc.audio, tc.opts...); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMediaCapabilities(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) {})
	if !llms.HasImageGeneration(c) || !llms.HasSpeech(c) || !llms.HasTranscription(c) {
		t.Fatalf("caps %+v", c.Capabilities())
	}
	if _, ok := any(c).(llms.Reranker); !ok {
		t.Fatal("not a Reranker")
	}
}
