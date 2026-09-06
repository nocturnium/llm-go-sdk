package openaicompat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
)

func mediaTestClient(t *testing.T, options ...testutil.MockOpenAICompatibleOption) (*Client, *testutil.MockOpenAICompatibleServer) {
	t.Helper()
	server := testutil.NewMockOpenAICompatibleServer(options...)
	t.Cleanup(server.Close)
	return NewClient(ClientConfig{BaseURL: server.URL(), APIKey: "test", AllowHTTP: true, AllowPrivateIPs: true}), server
}

func TestMediaClientRoutes(t *testing.T) {
	client, server := mediaTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	image, err := client.CreateImage(ctx, &ImageGenerationRequest{Model: "image", Prompt: "moon"})
	if err != nil || len(image.Data) != 1 || image.Usage.OutputTokens != 10 {
		t.Fatalf("image: %+v %v", image, err)
	}
	if server.LastRequest().Path != "/images/generations" {
		t.Fatal(server.LastRequest())
	}
	images, err := client.CreateImageStream(ctx, &ImageGenerationRequest{Prompt: "moon"})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for event := range images {
		if event.Err != nil || event.B64JSON == "" {
			t.Fatal(event)
		}
		count++
	}
	if count != 2 {
		t.Fatal(count)
	}
	mask := llms.MediaInput{Data: []byte("mask"), MIMEType: "image/png"}
	_, err = client.EditImage(ctx, &ImageEditRequest{Model: "image", Prompt: "edit", Images: []llms.MediaInput{mask, mask}, Mask: &mask, InputFidelity: "high"})
	if err != nil {
		t.Fatal(err)
	}
	body := server.LastRequest().Body
	if len(body["image[]"].([]any)) != 2 || body["input_fidelity"] != "high" || body["mask"] == nil {
		t.Fatal(body)
	}
	data, mime, err := client.CreateSpeech(ctx, &SpeechRequest{Input: "Hi.", Model: "tts", Voice: "alloy"})
	if err != nil || string(data) != "RIFFmockWAVE" || mime != "audio/wav" {
		t.Fatalf("speech: %s %s %v", data, mime, err)
	}
	speech, err := client.CreateSpeechStream(ctx, &SpeechRequest{Input: "Hi."})
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for event := range speech {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		count++
	}
	if count != 2 {
		t.Fatal(count)
	}
	for _, format := range []string{"json", "verbose_json", "diarized_json", "text", "srt", "vtt"} {
		req := &TranscriptionRequest{Model: "gpt-4o-mini-transcribe", File: llms.MediaInput{Data: data, MIMEType: mime}, ResponseFormat: format}
		if format == "verbose_json" {
			req.Model = "whisper-1"
			req.TimestampGranularities = []string{"word", "segment"}
		}
		if format == "diarized_json" {
			req.Model = "gpt-4o-transcribe-diarize"
		}
		transcript, err := client.CreateTranscription(ctx, req)
		expected := "Hi."
		switch format {
		case "srt":
			expected = "1\n00:00:00,000 --> 00:00:02,000\nHi.\n"
		case "vtt":
			expected = "WEBVTT\n\n00:00.000 --> 00:02.000\nHi.\n"
		}
		if err != nil || transcript.Text != expected {
			t.Fatalf("%s: %+v %v", format, transcript, err)
		}
		if format == "verbose_json" && (transcript.Duration != 2 || len(transcript.Words) != 1) {
			t.Fatal(transcript)
		}
		if format == "diarized_json" && transcript.Segments[0].Speaker != "A" {
			t.Fatal(transcript)
		}
		if format == "verbose_json" {
			values, ok := server.LastRequest().Body["timestamp_granularities[]"].([]string)
			if !ok || len(values) != 2 || values[0] != "word" || values[1] != "segment" {
				t.Fatal(server.LastRequest())
			}
		}
	}
	video, err := client.CreateVideo(ctx, &VideoCreateRequest{Prompt: "moon", Seconds: "4"})
	if err != nil || video.Status != "queued" {
		t.Fatalf("video: %+v %v", video, err)
	}
	for _, expected := range []string{"queued", "in_progress", "completed"} {
		status, err := client.GetVideo(ctx, video.ID)
		if err != nil || status.Status != expected {
			t.Fatalf("poll: %+v %v", status, err)
		}
	}
	data, err = client.GetVideoContent(ctx, video.ID)
	if err != nil || string(data[4:8]) != "ftyp" {
		t.Fatalf("video bytes: %q %v", data, err)
	}
}

func TestMediaStreamsSentinelAndErrors(t *testing.T) {
	for _, payload := range []string{"data: [DONE]\n\n", "event: image_generation.completed\ndata: {\"b64_json\":\"aGk=\"}\n\n", "data: invalid\n\n", ": comment\n\ndata: \n\ndata: {\"audio\":\"aGk=\"}\n\n"} {
		t.Run(payload, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(payload))
			}))
			defer server.Close()
			client := NewClient(ClientConfig{BaseURL: server.URL, AllowHTTP: true, AllowPrivateIPs: true})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stream, err := client.CreateImageStream(ctx, &ImageGenerationRequest{})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for event := range stream {
				count++
				if strings.Contains(payload, "invalid") && event.Err == nil {
					t.Fatal("missing decode error")
				}
				if strings.Contains(payload, "event:") && event.Type != "image_generation.completed" {
					t.Fatal(event)
				}
			}
			if strings.Contains(payload, "[DONE]") && count != 0 {
				t.Fatal(count)
			}
			speech, err := client.CreateSpeechStream(ctx, &SpeechRequest{})
			if err != nil {
				t.Fatal(err)
			}
			for event := range speech {
				if strings.Contains(payload, "invalid") && event.Err == nil {
					t.Fatal("missing decode error")
				}
			}
		})
	}
}

func TestMediaClientErrors(t *testing.T) {
	client, _ := mediaTestClient(t, testutil.WithErrorResponse(401, map[string]any{"error": map[string]any{"message": "denied"}}))
	input := llms.MediaInput{Data: []byte("audio"), MIMEType: "audio/wav"}
	calls := []func(context.Context) error{
		func(ctx context.Context) error { _, e := client.CreateImage(ctx, &ImageGenerationRequest{}); return e },
		func(ctx context.Context) error {
			_, e := client.CreateImageStream(ctx, &ImageGenerationRequest{})
			return e
		},
		func(ctx context.Context) error {
			_, e := client.EditImage(ctx, &ImageEditRequest{Images: []llms.MediaInput{input}})
			return e
		},
		func(ctx context.Context) error { _, _, e := client.CreateSpeech(ctx, &SpeechRequest{}); return e },
		func(ctx context.Context) error { _, e := client.CreateSpeechStream(ctx, &SpeechRequest{}); return e },
		func(ctx context.Context) error {
			_, e := client.CreateTranscription(ctx, &TranscriptionRequest{File: input})
			return e
		},
		func(ctx context.Context) error { _, e := client.CreateVideo(ctx, &VideoCreateRequest{}); return e },
		func(ctx context.Context) error { _, e := client.GetVideo(ctx, "video_1"); return e },
		func(ctx context.Context) error { _, e := client.GetVideoContent(ctx, "video_1"); return e },
	}
	for i, call := range calls {
		if err := call(context.Background()); err == nil {
			t.Fatalf("call %d accepted failure", i)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i, call := range calls {
		if err := call(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	for _, id := range []string{"", "..", "bad/id", "bad?query"} {
		if _, err := client.GetVideo(context.Background(), id); err == nil {
			t.Fatal(id)
		}
		if _, err := client.GetVideoContent(context.Background(), id); err == nil {
			t.Fatal(id)
		}
	}
	for _, req := range []*ImageEditRequest{{}, {Images: make([]llms.MediaInput, 17)}, {Images: []llms.MediaInput{{}}}, {Images: []llms.MediaInput{input}, Mask: &llms.MediaInput{}}, {ExtraBody: map[string]any{"bad": make(chan int)}}} {
		if _, err := client.EditImage(context.Background(), req); err == nil {
			t.Fatal("accepted invalid edit")
		}
	}
	for _, req := range []*TranscriptionRequest{{}, {File: llms.MediaInput{URL: "https://example.com"}}, {File: llms.MediaInput{Data: make([]byte, 25*1024*1024+1)}}, {File: input, Stream: true}, {File: input, ExtraBody: map[string]any{"bad": make(chan int)}}} {
		if _, err := client.CreateTranscription(context.Background(), req); err == nil {
			t.Fatal("accepted invalid transcription")
		}
	}
}

func TestMediaCustomRoutesAzure(t *testing.T) {
	var seen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		if r.Header.Get("api-key") != "azure-test" || r.URL.Query().Get("api-version") != "version" || !strings.HasPrefix(r.URL.Path, "/openai/v1/custom/") {
			t.Errorf("request: %s %v", r.URL, r.Header)
		}
		if strings.HasSuffix(r.URL.Path, "/content") && r.URL.Query().Get("variant") != "video" {
			t.Error("missing variant")
		}
		_, _ = w.Write([]byte(`{"data":[],"text":"hi","id":"v","status":"queued"}`))
	}))
	defer server.Close()
	client := NewClient(ClientConfig{BaseURL: server.URL + "/openai/v1", APIKey: "azure-test", AzureAPIKey: true, AzureVersion: "version", AllowHTTP: true, AllowPrivateIPs: true, ImagesPath: "/custom/images", ImageEditsPath: "/custom/edits", SpeechPath: "/custom/speech", TranscriptionsPath: "/custom/transcribe", VideosPath: "/custom/videos"})
	ctx := context.Background()
	if _, e := client.CreateImage(ctx, &ImageGenerationRequest{}); e != nil {
		t.Fatal(e)
	}
	if _, e := client.EditImage(ctx, &ImageEditRequest{Images: []llms.MediaInput{{Data: []byte("x")}}}); e != nil {
		t.Fatal(e)
	}
	if _, _, e := client.CreateSpeech(ctx, &SpeechRequest{}); e != nil {
		t.Fatal(e)
	}
	if _, e := client.CreateTranscription(ctx, &TranscriptionRequest{File: llms.MediaInput{Data: []byte("x")}}); e != nil {
		t.Fatal(e)
	}
	if _, e := client.CreateVideo(ctx, &VideoCreateRequest{}); e != nil {
		t.Fatal(e)
	}
	if _, e := client.GetVideo(ctx, "v"); e != nil {
		t.Fatal(e)
	}
	if _, e := client.GetVideoContent(ctx, "v"); e != nil {
		t.Fatal(e)
	}
	if seen != 7 {
		t.Fatal(seen)
	}
}

func TestMediaProviderFlagsAndRoundTrip(t *testing.T) {
	client, _ := mediaTestClient(t)
	config := ProviderConfig{Provider: llms.ProviderOpenAI, DefaultImageModel: "image", DefaultSpeechModel: "speech", DefaultTranscriptionModel: "transcribe", DefaultVideoModel: "video"}
	off := NewBaseProvider(client, config)
	ctx := context.Background()
	input := llms.MediaInput{Data: []byte("audio"), MIMEType: "audio/wav"}
	_, err := off.GenerateImage(ctx, "moon")
	if !errors.Is(err, llms.ErrImageGenerationNotSupported) {
		t.Fatal(err)
	}
	_, err = off.EditImage(ctx, "moon", nil)
	if !errors.Is(err, llms.ErrImageEditNotSupported) {
		t.Fatal(err)
	}
	_, err = off.Synthesize(ctx, "hi")
	if !errors.Is(err, llms.ErrSpeechNotSupported) {
		t.Fatal(err)
	}
	_, err = off.StreamSpeech(ctx, "hi")
	if !errors.Is(err, llms.ErrSpeechStreamNotSupported) {
		t.Fatal(err)
	}
	_, err = off.Transcribe(ctx, input)
	if !errors.Is(err, llms.ErrTranscriptionNotSupported) {
		t.Fatal(err)
	}
	_, err = off.GenerateVideo(ctx, "moon")
	if !errors.Is(err, llms.ErrVideoGenerationNotSupported) {
		t.Fatal(err)
	}
	if _, ok := llms.AsImageGenerator(&off); !ok {
		t.Fatal("interface absent")
	}
	config.Media = MediaCapabilities{Images: true, ImageEdits: true, Speech: true, SpeechStream: true, Transcription: true, Videos: true}
	on := NewBaseProvider(client, config)
	if caps := on.Capabilities(); !caps.ImageGeneration || !caps.VideoGeneration || !caps.Speech || !caps.Transcription {
		t.Fatal(caps)
	}
	if _, err := on.GenerateImage(ctx, "moon"); err != nil {
		t.Fatal(err)
	}
	if _, err := on.EditImage(ctx, "moon", []llms.MediaInput{input}); err != nil {
		t.Fatal(err)
	}
	if _, err := on.Synthesize(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	stream, err := on.StreamSpeech(ctx, "hi")
	if err != nil {
		t.Fatal(err)
	}
	var audio, terminal int
	for chunk := range stream {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Usage != nil {
			if len(chunk.Data) != 0 || chunk.Usage.Unit == "" {
				t.Fatalf("terminal usage chunk = %+v", chunk)
			}
			terminal++
			continue
		}
		audio++
	}
	if audio != 1 || terminal != 1 {
		t.Fatal(audio, terminal)
	}
	if _, err := on.Transcribe(ctx, input); err != nil {
		t.Fatal(err)
	}
	transcript, err := on.Transcribe(ctx, input, llms.WithTranscribeExtra(map[string]any{"response_format": "text"}))
	if err != nil || transcript.Text != "Hi." {
		t.Fatalf("text transcription: %+v %v", transcript, err)
	}
	job, err := on.GenerateVideo(ctx, "moon", llms.WithVideoDuration(4))
	if err != nil {
		t.Fatal(err)
	}
	polling, ok := job.(*llms.PollingVideoJob)
	if !ok {
		t.Fatalf("job %T", job)
	}
	polling.Policy = httpclient.PollPolicy{Initial: time.Millisecond, Max: time.Millisecond}
	result, err := job.Wait(ctx)
	if err != nil || result.Usage.Quantity != 4 || result.Usage.Unit != llms.MediaUnitSecond || string(result.Videos[0].Data[4:8]) != "ftyp" {
		t.Fatalf("result: %+v %v", result, err)
	}
	if !errors.Is(job.Cancel(ctx), llms.ErrJobCancelNotSupported) {
		t.Fatal("cancel supported")
	}
}

func TestMediaProviderValidationAndErrors(t *testing.T) {
	good, _ := mediaTestClient(t)
	bad, _ := mediaTestClient(t, testutil.WithErrorResponse(401, map[string]any{"error": map[string]any{"message": "denied"}}))
	config := ProviderConfig{Provider: llms.ProviderOpenAI, Media: MediaCapabilities{Images: true, ImageEdits: true, Speech: true, SpeechStream: true, Transcription: true, Videos: true}}
	p := NewBaseProvider(good, config)
	ctx := context.Background()
	input := llms.MediaInput{Data: []byte("x"), MIMEType: "image/png"}
	for _, options := range [][]llms.ImageOption{{}, {llms.WithImageCount(11)}, {llms.WithImageExtra(map[string]any{"stream": true})}, {llms.WithImageExtra(map[string]any{"bad": make(chan int)})}} {
		prompt := "moon"
		if len(options) == 0 {
			prompt = " "
		}
		if _, err := p.GenerateImage(ctx, prompt, options...); err == nil {
			t.Fatal("accepted invalid generation")
		}
	}
	for _, text := range []string{"", strings.Repeat("x", 4097)} {
		if _, err := p.Synthesize(ctx, text); err == nil {
			t.Fatal("accepted invalid speech")
		}
		if _, err := p.StreamSpeech(ctx, text); err == nil {
			t.Fatal("accepted invalid speech stream")
		}
	}
	if _, err := p.Synthesize(ctx, "Hi", llms.WithSpeechSpeed(5)); err == nil {
		t.Fatal("accepted speed")
	}
	for _, model := range []string{"tts-1", "tts-1-hd"} {
		if _, err := p.StreamSpeech(ctx, "Hi", llms.WithSpeechModel(model)); !errors.Is(err, llms.ErrSpeechStreamNotSupported) {
			t.Fatal(err)
		}
	}
	if _, err := p.EditImage(ctx, "", nil); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	if _, err := p.EditImage(ctx, "edit", []llms.MediaInput{input}, llms.WithImageExtra(map[string]any{"mask": "bad"})); err == nil {
		t.Fatal("accepted mask")
	}
	if _, err := p.EditImage(ctx, "edit", []llms.MediaInput{input}, llms.WithImageExtra(map[string]any{"bad": make(chan int)})); err == nil {
		t.Fatal("accepted JSON")
	}
	if _, err := p.EditImage(ctx, "edit", []llms.MediaInput{input}, llms.WithImageExtra(map[string]any{"mask": input, "input_fidelity": "high"})); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Transcribe(ctx, input, llms.WithTranscribeExtra(map[string]any{"model": "custom"})); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GenerateVideo(ctx, ""); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	if _, err := p.GenerateVideo(ctx, "moon", llms.WithVideoDuration(5)); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	if _, err := p.GenerateVideo(ctx, "moon", llms.WithVideoFirstFrame(llms.MediaInput{})); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	if _, err := p.GenerateVideo(ctx, "moon", llms.WithVideoResolution("480p")); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	p = NewBaseProvider(bad, config)
	failures := []func() error{
		func() error { _, e := p.GenerateImage(ctx, "moon"); return e },
		func() error { _, e := p.EditImage(ctx, "edit", []llms.MediaInput{input}); return e },
		func() error { _, e := p.Synthesize(ctx, "Hi"); return e },
		func() error { _, e := p.StreamSpeech(ctx, "Hi"); return e },
		func() error { _, e := p.Transcribe(ctx, input); return e },
		func() error { _, e := p.GenerateVideo(ctx, "moon"); return e },
	}
	for _, call := range failures {
		err := call()
		if !errors.Is(err, llms.ErrAuthenticationFailed) {
			t.Fatalf("missing mapped authentication: %v", err)
		}
	}
}

func TestMediaProviderStreamInvalidAudio(t *testing.T) {
	client, _ := mediaTestClient(t, testutil.WithSpeechStreamResponse(map[string]any{"type": "speech.audio.delta", "audio": "!"}))
	p := NewBaseProvider(client, ProviderConfig{Provider: llms.ProviderOpenAI, Media: MediaCapabilities{SpeechStream: true}})
	stream, err := p.StreamSpeech(context.Background(), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for chunk := range stream {
		if chunk.Err == nil {
			t.Fatal("missing decode error")
		}
		count++
	}
	if count != 1 {
		t.Fatal(count)
	}
}

func TestMediaProviderVideoModeration(t *testing.T) {
	state := VideoObject{ID: "v", Status: "failed", Error: &VideoError{Code: "content_policy_violation", Message: "filtered"}}
	client, _ := mediaTestClient(t, testutil.WithVideoResponse(VideoObject{ID: "v", Status: "queued"}, state))
	p := NewBaseProvider(client, ProviderConfig{Provider: llms.ProviderOpenAI, Media: MediaCapabilities{Videos: true}})
	job, err := p.GenerateVideo(context.Background(), "moon")
	if err != nil {
		t.Fatal(err)
	}
	_, err = job.Wait(context.Background())
	var moderation *llms.ModerationError
	if !errors.As(err, &moderation) || !errors.Is(err, llms.ErrContentFiltered) {
		t.Fatal(err)
	}
}

func TestMediaRouteOverrideIsolation(t *testing.T) {
	client, _ := mediaTestClient(t)
	config := ProviderConfig{Media: MediaCapabilities{ImagesPath: "/custom"}}
	p := NewBaseProvider(client, config)
	if p.client.mediaPaths.ImagesPath != "/custom" || client.mediaPaths.ImagesPath != "" {
		t.Fatal("route override mutated shared client")
	}
}

func TestMediaStreamCancellation(t *testing.T) {
	client, server := mediaTestClient(t, testutil.WithSpeechStreamResponse(map[string]any{"type": "speech.audio.delta", "audio": "aGk="}), testutil.WithStreamHeldOpenAfterChunks())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.CreateSpeechStream(ctx, &SpeechRequest{})
	if err != nil {
		t.Fatal(err)
	}
	<-stream
	cancel()
	done := make(chan struct{})
	go func() {
		for event := range stream {
			if event.Err != nil {
				break
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not close")
	}
	select {
	case <-server.StreamRequestClosed():
	case <-time.After(time.Second):
		t.Fatal("request body not closed")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	for _, tc := range []struct{ value, fallback, want string }{
		{"4", "8", "4"}, {"", "8", "8"}, {"", "", ""},
	} {
		if got := firstNonEmpty(tc.value, tc.fallback); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}

func TestTranscriptionCompatibility(t *testing.T) {
	for _, tc := range []struct {
		model string
		opts  []llms.TranscribeOption
		valid bool
	}{
		{"gpt-4o-mini-transcribe", nil, true},
		{"whisper-1", []llms.TranscribeOption{llms.WithTranscribeWordTimestamps(true)}, true},
		{"gpt-4o-transcribe-diarize", []llms.TranscribeOption{llms.WithTranscribeDiarization(true)}, true},
		{"gpt-4o-mini-transcribe", []llms.TranscribeOption{llms.WithTranscribeWordTimestamps(true)}, false},
		{"whisper-1", []llms.TranscribeOption{llms.WithTranscribeDiarization(true)}, false},
		{"gpt-4o-transcribe-diarize", []llms.TranscribeOption{llms.WithTranscribeDiarization(true), llms.WithTranscribeWordTimestamps(true)}, false},
		{"whisper-1", []llms.TranscribeOption{llms.WithTranscribeExtra(map[string]any{"model": "gpt-4o-mini-transcribe", "response_format": "verbose_json"})}, false},
		{"gpt-4o-mini-transcribe", []llms.TranscribeOption{llms.WithTranscribeExtra(map[string]any{"model": "whisper-1"}), llms.WithTranscribeWordTimestamps(true)}, true},
		{"gpt-4o-mini-transcribe", []llms.TranscribeOption{llms.WithTranscribeExtra(map[string]any{"response_format": "diarized_json"})}, false},
	} {
		client, server := mediaTestClient(t)
		p := NewBaseProvider(client, ProviderConfig{Provider: llms.ProviderOpenAI, DefaultTranscriptionModel: tc.model, Media: MediaCapabilities{Transcription: true}})
		_, err := p.Transcribe(context.Background(), llms.MediaInput{Data: []byte("audio"), MIMEType: "audio/wav"}, tc.opts...)
		if tc.valid {
			if err != nil {
				t.Fatalf("%s: %v", tc.model, err)
			}
		} else {
			if !errors.Is(err, llms.ErrInvalidParameters) {
				t.Fatalf("expected validation: %v", err)
			}
			if server.LastRequest().Path != "" {
				t.Fatal("invalid request reached server")
			}
		}
	}
}

func TestMultipartExtrasRejectNonScalars(t *testing.T) {
	for _, value := range []any{map[string]any{"x": 1}, []string{"word"}, []any{map[string]any{"x": 1}}, nil, make(chan int)} {
		_, _, err := multipartMediaFields(&TranscriptionRequest{}, map[string]any{"bad": value})
		if !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("%T: %v", value, err)
		}
	}
	fields, _, err := multipartMediaFields(&TranscriptionRequest{}, map[string]any{"prompt": "words", "temperature": 0.5, "stream": false, "count": 2})
	if err != nil || fields["temperature"] != "0.5" || fields["count"] != "2" || fields["stream"] != "false" {
		t.Fatalf("fields %v: %v", fields, err)
	}
}

func TestMediaStreamUnexpectedEOF(t *testing.T) {
	for _, endpoint := range []string{"image", "speech"} {
		t.Run(endpoint, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if endpoint == "image" {
					_, _ = w.Write([]byte("data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"aGk=\"}\n\n"))
				} else {
					_, _ = w.Write([]byte("data: {\"type\":\"speech.audio.delta\",\"audio\":\"aGk=\"}\n\n"))
				}
			}))
			defer server.Close()
			client := NewClient(ClientConfig{BaseURL: server.URL, AllowHTTP: true, AllowPrivateIPs: true})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var errs []error
			if endpoint == "image" {
				stream, err := client.CreateImageStream(ctx, &ImageGenerationRequest{})
				if err != nil {
					t.Fatal(err)
				}
				for event := range stream {
					errs = append(errs, event.Err)
				}
			} else {
				stream, err := client.CreateSpeechStream(ctx, &SpeechRequest{})
				if err != nil {
					t.Fatal(err)
				}
				for event := range stream {
					errs = append(errs, event.Err)
				}
			}
			if len(errs) != 2 || errs[0] != nil || !errors.Is(errs[1], io.ErrUnexpectedEOF) || !errors.Is(errs[1], llms.ErrStreamInterrupted) {
				t.Fatalf("events: %v", errs)
			}
		})
	}
}

func TestVideoResultDurationAndExpiry(t *testing.T) {
	for _, seconds := range []string{"", "garbage", "0", "-1", "NaN", "+Inf", "4"} {
		t.Run(seconds, func(t *testing.T) {
			obj := VideoObject{ID: "v", Status: "completed", Model: "sora-2", Size: "1280x720", Seconds: seconds, ExpiresAt: 1900000000}
			client, _ := mediaTestClient(t, testutil.WithVideoResponse(obj, obj))
			p := NewBaseProvider(client, ProviderConfig{Provider: llms.ProviderOpenAI, Media: MediaCapabilities{Videos: true}})
			job, err := p.GenerateVideo(context.Background(), "moon")
			if err != nil {
				t.Fatal(err)
			}
			out, err := job.Wait(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if out.Videos[0].ExpiresAt.Unix() != obj.ExpiresAt {
				t.Fatal("expiry not preserved")
			}
			if seconds == "4" {
				if out.Usage.Unit != llms.MediaUnitSecond || out.Usage.Quantity != 4 {
					t.Fatal(out.Usage)
				}
			} else {
				if out.Usage.Unit != "" {
					t.Fatal(out.Usage)
				}
				if _, ok := llms.MediaCost("openai", out.Model, out.Usage); ok {
					t.Fatal("invalid duration priced")
				}
			}
		})
	}
}
