package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/geminiapi"
)

func mediaTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Error("missing API key")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	c, err := New(WithAPIKey("test-key"), WithBaseURL(server.URL+"/v1beta"), WithAllowHTTP(), WithAllowPrivateIPs(), WithPollPolicy(PollPolicy{Initial: time.Millisecond, Max: time.Millisecond, Timeout: time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func readMediaRequest(t *testing.T, r *http.Request, out any) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Errorf("method = %s", r.Method)
	}
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		t.Error(err)
	}
}
func writeMediaJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}
func inlineResponse(mimeType string) *geminiapi.GenerateContentResponse {
	return &geminiapi.GenerateContentResponse{Candidates: []geminiapi.Candidate{{Content: &geminiapi.Content{Parts: []geminiapi.Part{{Text: "ignored"}, {InlineData: &geminiapi.InlineData{MimeType: mimeType, Data: base64.StdEncoding.EncodeToString([]byte("media"))}}}}}}, UsageMetadata: &geminiapi.UsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 200, TotalTokenCount: 300}}
}
func assertCost(t *testing.T, usage llms.MediaUsage, want float64) {
	t.Helper()
	if usage.Cost == nil || math.Abs(*usage.Cost-want) > 1e-10 {
		t.Fatalf("usage = %+v, want cost %.10f", usage, want)
	}
}
func TestClient_GenerateImage_Media(t *testing.T) {
	for _, tc := range []struct {
		model, size, aspect string
		cost                float64
	}{
		{"", "", "", .067}, {"gemini-3.1-flash-image", "512", "16:9", .045},
		{"gemini-3.1-flash-image", "2K", "9:16", .101}, {"gemini-3.1-flash-image", "4K", "1:1", .151},
		{"gemini-2.5-flash-image", "1K", "1:1", .039}, {"gemini-3.1-flash-lite-image", "1K", "1:1", .0336},
		{"gemini-3-pro-image", "1K", "1:1", .134}, {"gemini-3-pro-image", "2K", "1:1", .134}, {"gemini-3-pro-image", "4K", "1:1", .24},
		{"gemini-3.1-flash-lite-image", "4K", "1:1", -1}, {"custom", "1K", "1:1", -1}, {"gemini-3.1-flash-image", "4k", "1:1", .151},
	} {
		t.Run(tc.model+tc.size, func(t *testing.T) {
			c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				var req geminiapi.GenerateContentRequest
				readMediaRequest(t, r, &req)
				size, aspect := strings.ToUpper(tc.size), tc.aspect
				if size == "" {
					size = "1K"
				}
				if aspect == "" {
					aspect = "1:1"
				}
				if !strings.HasSuffix(r.URL.Path, ":generateContent") || req.GenerationConfig.ImageConfig.ImageSize != size || req.GenerationConfig.ImageConfig.AspectRatio != aspect || !reflect.DeepEqual(req.GenerationConfig.ResponseModalities, []string{"IMAGE"}) {
					t.Errorf("request = %+v", req)
				}
				if req.Contents[0].Parts[0].Text != "draw" {
					t.Error("missing prompt")
				}
				writeMediaJSON(t, w, inlineResponse("image/png"))
			})
			out, err := c.GenerateImage(context.Background(), "draw", llms.WithImageModel(tc.model), llms.WithImageSize(tc.size), llms.WithImageAspectRatio(tc.aspect))
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Images) != 1 || string(out.Images[0].Data) != "media" || out.Images[0].MIMEType != "image/png" || out.Usage.Quantity != 1 {
				t.Fatalf("response = %+v", out)
			}
			if tc.cost < 0 {
				if out.Usage.Cost != nil || out.Usage.Unit != "" {
					t.Fatal("unlisted price must remain unknown")
				}
			} else {
				assertCost(t, out.Usage, tc.cost)
			}
		})
	}
}
func TestClient_EditImage_Media(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req geminiapi.GenerateContentRequest
		readMediaRequest(t, r, &req)
		parts := req.Contents[0].Parts
		if len(parts) != 3 || parts[1].InlineData.Data != "b25l" || parts[2].InlineData.MimeType != "image/jpeg" {
			t.Errorf("parts = %+v", parts)
		}
		writeMediaJSON(t, w, inlineResponse("image/png"))
	})
	_, err := c.EditImage(context.Background(), "edit", []llms.MediaInput{{Data: []byte("one"), MIMEType: "image/png"}, {Data: []byte("two"), MIMEType: "image/jpeg"}})
	if err != nil {
		t.Fatal(err)
	}
}
func TestClient_VideoJob_Media(t *testing.T) {
	var polls atomic.Int32
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ":predictLongRunning"):
			var req geminiapi.PredictLongRunningRequest
			readMediaRequest(t, r, &req)
			p := req.Parameters
			if p.DurationSeconds != 4 || p.Resolution != "1080p" || p.AspectRatio != "9:16" || p.NumberOfVideos != 1 || p.Seed == nil || *p.Seed != 42 || p.NegativePrompt != "blur" || p.PersonGeneration != "allow_adult" {
				t.Errorf("parameters = %+v", p)
			}
			inst := req.Instances[0]
			if inst.Image == nil || inst.LastFrame == nil || len(inst.ReferenceImages) != 1 || inst.Image.BytesBase64Encoded != "aW1hZ2U=" {
				t.Errorf("instance = %+v", inst)
			}
			writeMediaJSON(t, w, map[string]any{"name": "models/veo/operations/job"})
		case strings.Contains(r.URL.Path, "/operations/"):
			if r.Method != http.MethodGet {
				t.Error("poll must GET")
			}
			if polls.Add(1) == 1 {
				writeMediaJSON(t, w, map[string]any{"done": false})
				return
			}
			writeMediaJSON(t, w, videoOperation("http://"+r.Host+"/file"))
		case r.URL.Path == "/file":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("mp4"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	input := llms.MediaInput{Data: []byte("image"), MIMEType: "image/png"}
	job, err := c.GenerateVideo(context.Background(), "clouds", llms.WithVideoModel("veo-3.1-lite-generate-preview"), llms.WithVideoDuration(4), llms.WithVideoResolution("1080p"), llms.WithVideoAspectRatio("9:16"), llms.WithVideoSeed(42), llms.WithVideoNegativePrompt("blur"), llms.WithVideoFirstFrame(input), llms.WithVideoLastFrame(input), llms.WithVideoReferenceImages([]llms.MediaInput{input}), llms.WithVideoExtra(map[string]any{"personGeneration": "allow_adult"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := job.(*llms.PollingVideoJob); !ok || job.ID() != "models/veo/operations/job" {
		t.Fatal("wrong job")
	}
	out, err := job.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if polls.Load() != 2 || len(out.Videos) != 1 || string(out.Videos[0].Data) != "mp4" || out.Videos[0].MIMEType != "video/mp4" || out.Videos[0].URL == "" || time.Until(out.Videos[0].ExpiresAt) < 46*time.Hour {
		t.Fatalf("result = %+v", out)
	}
	if out.Usage.Unit != llms.MediaUnitSecond || out.Usage.Quantity != 4 {
		t.Fatalf("usage = %+v", out.Usage)
	}
	assertCost(t, out.Usage, .32)
	if err := job.Cancel(context.Background()); !errors.Is(err, llms.ErrJobCancelNotSupported) {
		t.Fatal(err)
	}
}
func videoOperation(uri string) *geminiapi.Operation {
	return &geminiapi.Operation{Done: true, Response: &geminiapi.VideoOperationResponse{GenerateVideoResponse: geminiapi.GeneratedVideoResponse{GeneratedSamples: []geminiapi.GeneratedVideoSample{{Video: geminiapi.VideoFile{URI: uri}}}}}}
}
func TestClient_VideoJob_Failures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		op    *geminiapi.Operation
		state llms.JobState
		want  error
	}{
		{"operation", &geminiapi.Operation{Done: true, Error: &geminiapi.OperationError{Code: 13, Status: "INTERNAL", Message: "failed"}}, llms.JobFailed, llms.ErrJobFailed},
		{"filtered", &geminiapi.Operation{Done: true, Response: &geminiapi.VideoOperationResponse{GenerateVideoResponse: geminiapi.GeneratedVideoResponse{RAIMediaFilteredCount: 1, RAIMediaFilteredReasons: []string{"policy"}}}}, llms.JobModerated, llms.ErrContentFiltered},
		{"missing", &geminiapi.Operation{Done: true}, llms.JobFailed, llms.ErrJobFailed},
		{"empty", &geminiapi.Operation{Done: true, Response: &geminiapi.VideoOperationResponse{}}, llms.JobFailed, llms.ErrJobFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					writeMediaJSON(t, w, map[string]any{"name": "models/veo/operations/job"})
					return
				}
				writeMediaJSON(t, w, tc.op)
			})
			job, err := c.GenerateVideo(context.Background(), "clouds")
			if err != nil {
				t.Fatal(err)
			}
			status, err := job.Poll(context.Background())
			if err != nil || status.State != tc.state {
				t.Fatalf("status = %+v, %v", status, err)
			}
			_, err = job.Wait(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if tc.state == llms.JobModerated {
				var mod *llms.ModerationError
				if !errors.As(err, &mod) || mod.Stage != llms.ModerationOutput || mod.Provider != "gemini" || !reflect.DeepEqual(mod.Reasons, []string{"policy"}) {
					t.Fatalf("moderation = %+v", mod)
				}
			}
		})
	}
}
func TestClient_Synthesize_Media(t *testing.T) {
	for _, tc := range []struct {
		name, instructions, voice, mime, model string
		rate                                   int
		cost                                   float64
	}{
		{"default", "", "", "audio/l16; rate=24000; channels=1", "", 24000, .0041},
		{"instructions", "Whisper", "Kore", "audio/l16; rate=22050", "gemini-2.5-flash-preview-tts", 22050, .00205},
		{"no rate", "", "Kore", "audio/l16", "gemini-2.5-pro-preview-tts", 24000, .0041},
		{"unknown", "", "Kore", "audio/l16", "custom", 24000, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				var req geminiapi.GenerateContentRequest
				readMediaRequest(t, r, &req)
				want := "Say: Hi there."
				if tc.instructions != "" {
					want = tc.instructions + ": Hi there."
				}
				if req.Contents[0].Parts[0].Text != want || req.GenerationConfig.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName != "Kore" || !reflect.DeepEqual(req.GenerationConfig.ResponseModalities, []string{"AUDIO"}) {
					t.Errorf("request = %+v", req)
				}
				writeMediaJSON(t, w, inlineResponse(tc.mime))
			})
			out, err := c.Synthesize(context.Background(), "Hi there.", llms.WithSpeechInstructions(tc.instructions), llms.WithSpeechVoice(tc.voice), llms.WithSpeechModel(tc.model), llms.WithSpeechFormat(llms.AudioFormat{Container: "pcm"}))
			if err != nil {
				t.Fatal(err)
			}
			if string(out.Audio.Data) != "media" || out.Format != (llms.AudioFormat{Container: "pcm", Encoding: "pcm_s16le", SampleRate: tc.rate}) || out.Usage.Unit != llms.MediaUnitMTokenOut || out.Usage.Quantity != .0002 {
				t.Fatalf("response = %+v", out)
			}
			if tc.cost < 0 {
				if out.Usage.Cost != nil {
					t.Fatal("unknown model priced")
				}
			} else {
				assertCost(t, out.Usage, tc.cost)
			}
		})
	}
}
func TestClient_Synthesize_MultiSpeaker(t *testing.T) {
	multi := map[string]any{"speakerVoiceConfigs": []any{map[string]any{"speaker": "A", "voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": "Kore"}}}}}
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req geminiapi.GenerateContentRequest
		readMediaRequest(t, r, &req)
		cfg := req.GenerationConfig.SpeechConfig
		if cfg.VoiceConfig != nil || cfg.MultiSpeakerVoiceConfig.SpeakerVoiceConfigs[0].Speaker != "A" {
			t.Errorf("config = %+v", cfg)
		}
		resp := inlineResponse("audio/l16")
		resp.UsageMetadata = nil
		writeMediaJSON(t, w, resp)
	})
	out, err := c.Synthesize(context.Background(), "A: Hi", llms.WithSpeechExtra(map[string]any{"multiSpeakerVoiceConfig": multi}))
	if err != nil || out.Usage.Cost != nil {
		t.Fatalf("result = %+v, %v", out, err)
	}
}
func transcriptResponse() *geminiapi.Interaction {
	return &geminiapi.Interaction{ID: "interaction", Status: "completed", Usage: &geminiapi.InteractionUsage{TotalInputTokens: 363, TotalOutputTokens: 0, InputTokensByModality: []geminiapi.InteractionModalityTokens{{Modality: "audio", Tokens: 362}, {Modality: "text", Tokens: 1}}, ModelInvocationTokenCounts: []geminiapi.ModelInvocationTokenCount{{CandidatesTokensDetails: []geminiapi.InteractionModalityTokens{{Modality: "text", Tokens: 38}}}}}, Steps: []geminiapi.InteractionStep{
		{Type: "other", Content: []geminiapi.InteractionContent{{Type: "text", Text: "ignored"}}},
		{Type: "model_output", Content: []geminiapi.InteractionContent{{Type: "audio", Text: "ignored"}, {Type: "text", Text: "Hi ", Annotations: []geminiapi.WordAnnotation{{Type: "other"}, {Type: "word_info", Text: "Hi", Speaker: "spk:0", StartOffset: "0.100s", EndOffset: "0.600s"}}}, {Type: "text", Text: "there.", Annotations: []geminiapi.WordAnnotation{{Type: "word_info", Text: "there.", Speaker: "spk:0", StartOffset: "0.700s", EndOffset: "1.200s"}}}}},
	}}
}
func TestClient_Transcribe_Media(t *testing.T) {
	for _, tc := range []struct {
		name         string
		opts         []llms.TranscribeOption
		mode         any
		remote, poll bool
	}{
		{name: "smart", opts: []llms.TranscribeOption{llms.WithTranscribeLanguage("en"), llms.WithTranscribeKeyterms([]string{"Codex"})}, mode: "smart"},
		{name: "diarize", opts: []llms.TranscribeOption{llms.WithTranscribeDiarization(true)}, mode: map[string]any{"type": "verbatim", "diarization_mode": "speaker"}},
		{name: "words", opts: []llms.TranscribeOption{llms.WithTranscribeWordTimestamps(true)}, mode: map[string]any{"type": "verbatim", "timestamp_granularities": []any{"word"}}},
		{name: "both polling", opts: []llms.TranscribeOption{llms.WithTranscribeDiarization(true), llms.WithTranscribeWordTimestamps(true)}, mode: map[string]any{"type": "verbatim", "diarization_mode": "speaker", "timestamp_granularities": []any{"word"}}, poll: true},
		{name: "verbatim URL", opts: []llms.TranscribeOption{llms.WithTranscribeExtra(map[string]any{"mode": "verbatim"})}, mode: map[string]any{"type": "verbatim"}, remote: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gets atomic.Int32
			c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					if r.URL.Path != "/v1beta/interactions/interaction" {
						t.Errorf("path = %s", r.URL.Path)
					}
					if gets.Add(1) == 1 {
						writeMediaJSON(t, w, map[string]any{"id": "interaction", "status": "in_progress"})
						return
					}
					writeMediaJSON(t, w, transcriptResponse())
					return
				}
				var req geminiapi.InteractionRequest
				readMediaRequest(t, r, &req)
				if r.URL.Path != "/v1beta/interactions" || req.Model != "gemini-3.5-transcribe" || len(req.Input) != 1 || req.Input[0].Type != "audio" || req.Input[0].MIMEType != "audio/wav" || !reflect.DeepEqual(req.GenerationConfig.TranscriptionConfig.Mode, tc.mode) {
					t.Errorf("request = %+v", req)
				}
				if tc.remote {
					if req.Input[0].URI != "https://example.com/audio.wav" || req.Input[0].Data != "" {
						t.Error("missing URI")
					}
				} else if req.Input[0].Data != "d2F2" || req.Input[0].URI != "" {
					t.Error("missing inline audio")
				}
				if tc.name == "smart" && (!reflect.DeepEqual(req.GenerationConfig.TranscriptionConfig.LanguageCodes, []string{"en"}) || !reflect.DeepEqual(req.GenerationConfig.TranscriptionConfig.CustomVocabulary, []string{"Codex"})) {
					t.Error("missing language/vocabulary")
				}
				if tc.poll {
					writeMediaJSON(t, w, map[string]any{"id": "interaction", "status": "in_progress"})
					return
				}
				response := transcriptResponse()
				if tc.name == "smart" {
					for i := range response.Steps {
						for j := range response.Steps[i].Content {
							response.Steps[i].Content[j].Annotations = nil
						}
					}
				}
				writeMediaJSON(t, w, response)
			})
			input := llms.MediaInput{Data: []byte("wav"), MIMEType: "audio/wav"}
			if tc.remote {
				input.Data = nil
				input.URL = "https://example.com/audio.wav"
			}
			out, err := c.Transcribe(context.Background(), input, tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if out.Text != "Hi there." || out.Language != "" || out.Usage.Unit != llms.MediaUnitMinute || math.Abs(out.Usage.Quantity-362.0/25/60) > 1e-12 {
				t.Fatalf("response = %+v", out)
			}
			if tc.name == "smart" {
				if len(out.Words) != 0 || out.DurationSeconds != 0 {
					t.Fatal("smart fixture must have no annotations")
				}
			} else if out.DurationSeconds != 1.2 || len(out.Words) != 2 || out.Words[0].Start != .1 || out.Words[0].Speaker != "spk:0" {
				t.Fatalf("words = %+v", out)
			}
			assertCost(t, out.Usage, 362*2.0/1e6+38*12.0/1e6)
			if tc.poll && gets.Load() != 2 {
				t.Fatal("did not poll")
			}
		})
	}
}

func TestClient_MediaValidation(t *testing.T) {
	c := mediaTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("validation must not send request") })
	ctx := context.Background()
	for _, o := range []llms.ImageOption{llms.WithImageSize("bad"), llms.WithImageCount(2), llms.WithImageCount(-1), llms.WithImageAspectRatio("bad"), llms.WithImageAspectRatio("0:1"), llms.WithImageExtra(map[string]any{"bad": true})} {
		if _, err := c.GenerateImage(ctx, "draw", o); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("image validation: %v", err)
		}
	}
	if _, err := c.GenerateImage(ctx, " "); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	if _, err := c.EditImage(ctx, "edit", nil); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	for _, input := range []llms.MediaInput{{}, {URL: "https://example.com/a", MIMEType: "image/png"}, {Data: []byte("x"), MIMEType: "audio/wav"}} {
		if _, err := c.EditImage(ctx, "edit", []llms.MediaInput{input}); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	for _, o := range []llms.VideoOption{llms.WithVideoDuration(5), llms.WithVideoResolution("bad"), llms.WithVideoAspectRatio("1:1"), llms.WithVideoReferenceImages(make([]llms.MediaInput, 4)), llms.WithVideoAudio(false), llms.WithVideoFirstFrame(llms.MediaInput{}), llms.WithVideoReferenceImages([]llms.MediaInput{{}}), llms.WithVideoExtra(map[string]any{"bad": true}), llms.WithVideoExtra(map[string]any{"personGeneration": 1})} {
		if _, err := c.GenerateVideo(ctx, "clouds", o); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("video validation: %v", err)
		}
	}
	if _, err := c.GenerateVideo(ctx, ""); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	for _, format := range []llms.AudioFormat{{Container: "mp3"}, {Container: "wav"}, {Encoding: "float"}, {SampleRate: 8000}, {BitRate: 128000}} {
		if _, err := c.Synthesize(ctx, "hello", llms.WithSpeechFormat(format)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	if _, err := c.Synthesize(ctx, ""); !errors.Is(err, llms.ErrEmptyText) {
		t.Fatal(err)
	}
	if _, err := c.StreamSpeech(ctx, "hello"); !errors.Is(err, llms.ErrSpeechStreamNotSupported) {
		t.Fatal(err)
	}
	for _, extra := range []map[string]any{{"bad": true}, {"multiSpeakerVoiceConfig": func() {}}, {"multiSpeakerVoiceConfig": "bad"}, {"multiSpeakerVoiceConfig": map[string]any{}}, {"multiSpeakerVoiceConfig": map[string]any{"speakerVoiceConfigs": []any{map[string]any{"speaker": "A"}}}}} {
		if _, err := c.Synthesize(ctx, "hello", llms.WithSpeechExtra(extra)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("speech extra: %v", err)
		}
	}
	for _, input := range []llms.MediaInput{{}, {FileID: "file", MIMEType: "audio/wav"}, {Data: []byte("x"), MIMEType: "image/png"}} {
		if _, err := c.Transcribe(ctx, input); err == nil {
			t.Fatal("expected invalid audio")
		}
	}
	input := llms.MediaInput{Data: []byte("wav"), MIMEType: "audio/wav"}
	for _, opts := range [][]llms.TranscribeOption{
		{llms.WithTranscribeKeyterms([]string{"x"}), llms.WithTranscribeDiarization(true)},
		{llms.WithTranscribeKeyterms([]string{"x"}), llms.WithTranscribeWordTimestamps(true)},
		{llms.WithTranscribeDiarization(true), llms.WithTranscribeExtra(map[string]any{"mode": "smart"})},
		{llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeExtra(map[string]any{"mode": "smart"})},
		{llms.WithTranscribeExtra(map[string]any{"bad": true})},
		{llms.WithTranscribeExtra(map[string]any{"mode": "bad"})},
		{llms.WithTranscribeExtra(map[string]any{"mode": 123})},
		{llms.WithTranscribeExtra(map[string]any{"mode": map[string]any{"type": "smart"}})},
		{llms.WithTranscribeExtra(map[string]any{"mode": map[string]any{"type": "verbatim", "diarization_mode": "bad"}})},
		{llms.WithTranscribeExtra(map[string]any{"mode": map[string]any{"type": "verbatim", "timestamp_granularities": []string{"sentence"}}})},
		{llms.WithTranscribeKeyterms([]string{"x"}), llms.WithTranscribeExtra(map[string]any{"mode": map[string]any{"type": "verbatim", "diarization_mode": "speaker"}})},
	} {
		if _, err := c.Transcribe(ctx, input, opts...); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("transcribe validation: %v", err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.GenerateImage(canceled, "draw"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.GenerateVideo(canceled, "clouds"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Synthesize(canceled, "hi"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Transcribe(canceled, input); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestClient_MediaAPIErrors(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"free_tier limit: 0","status":"RESOURCE_EXHAUSTED"}}`))
	})
	ctx := context.Background()
	_, imageErr := c.GenerateImage(ctx, "draw")
	_, videoErr := c.GenerateVideo(ctx, "clouds")
	_, speechErr := c.Synthesize(ctx, "hi")
	_, transcribeErr := c.Transcribe(ctx, llms.MediaInput{Data: []byte("wav"), MIMEType: "audio/wav"})
	for _, err := range []error{imageErr, videoErr, speechErr, transcribeErr} {
		if !errors.Is(err, llms.ErrRateLimited) || llms.ProviderFromError(err) != llms.ProviderGemini || !strings.Contains(err.Error(), "free_tier") {
			t.Fatal(err)
		}
	}
}
func TestClient_MediaMalformedResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp *geminiapi.GenerateContentResponse
	}{
		{"no candidate", &geminiapi.GenerateContentResponse{}},
		{"no content", &geminiapi.GenerateContentResponse{Candidates: []geminiapi.Candidate{{}}}},
		{"filtered", &geminiapi.GenerateContentResponse{Candidates: []geminiapi.Candidate{{FinishReason: "SAFETY"}}}},
		{"bad base64", &geminiapi.GenerateContentResponse{Candidates: []geminiapi.Candidate{{Content: &geminiapi.Content{Parts: []geminiapi.Part{{InlineData: &geminiapi.InlineData{MimeType: "image/png", Data: "%%%"}}}}}}}},
		{"empty inline", &geminiapi.GenerateContentResponse{Candidates: []geminiapi.Candidate{{Content: &geminiapi.Content{Parts: []geminiapi.Part{{InlineData: &geminiapi.InlineData{MimeType: "image/png"}}}}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mediaTestClient(t, func(w http.ResponseWriter, _ *http.Request) { writeMediaJSON(t, w, tc.resp) })
			if _, err := c.GenerateImage(context.Background(), "draw"); err == nil {
				t.Fatal("expected malformed response error")
			}
		})
	}
	for _, value := range []string{"audio/mp3", "audio/l16; rate=oops", "audio/l16; channels=2", "audio/l16; invalid"} {
		t.Run(value, func(t *testing.T) {
			c := mediaTestClient(t, func(w http.ResponseWriter, _ *http.Request) { writeMediaJSON(t, w, inlineResponse(value)) })
			if _, err := c.Synthesize(context.Background(), "hi"); err == nil {
				t.Fatal("expected encoding error")
			}
		})
	}
	c := mediaTestClient(t, func(w http.ResponseWriter, _ *http.Request) { writeMediaJSON(t, w, inlineResponse("image/png")) })
	if _, err := c.Synthesize(context.Background(), "hi"); err == nil {
		t.Fatal("expected missing audio")
	}
}
func TestClient_Transcribe_EdgeCases(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		getError     bool
	}{
		{"failed", "failed", false}, {"canceled", "cancelled", false}, {"poll error", "in_progress", true}, //nolint:misspell // Gemini wire status.
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && tc.getError {
					w.WriteHeader(400)
					return
				}
				writeMediaJSON(t, w, map[string]any{"id": "interaction", "status": tc.status})
			})
			if _, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("wav"), MIMEType: "audio/wav"}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	for _, offset := range []string{"bad", "NaN", "+Inf", "-1s"} {
		if _, err := parseOffset(offset); err == nil {
			t.Fatalf("accepted offset %q", offset)
		}
	}
	for _, reverse := range []bool{false, true} {
		resp := transcriptResponse()
		if reverse {
			resp.Steps[1].Content[1].Annotations[1].EndOffset = "0.01s"
		} else {
			resp.Steps[1].Content[1].Annotations[1].StartOffset = "bad"
		}
		if _, err := convertTranscription(resp, "gemini-3.5-transcribe"); err == nil {
			t.Fatal("accepted invalid offsets")
		}
	}
	resp := transcriptResponse()
	resp.Steps[1].Content[1].Annotations[1].EndOffset = "bad"
	if _, err := convertTranscription(resp, "gemini-3.5-transcribe"); err == nil {
		t.Fatal("accepted bad end")
	}
	resp = transcriptResponse()
	resp.Usage = nil
	resp.Steps = nil
	out, err := convertTranscription(resp, "custom")
	if err != nil || out.Usage.Cost != nil || out.DurationSeconds != 0 {
		t.Fatalf("result = %+v, %v", out, err)
	}
	out, err = convertTranscription(transcriptResponse(), "custom")
	if err != nil || out.Usage.Cost != nil {
		t.Fatalf("result = %+v, %v", out, err)
	}
	for _, extra := range []map[string]any{{"mode": "smart"}, {"mode": map[string]any{"type": "verbatim"}}} {
		o := llms.ApplyTranscribeOptions(llms.WithTranscribeExtra(extra))
		if _, err := transcriptionConfig(o); err != nil {
			t.Fatal(err)
		}
	}
	o := llms.ApplyTranscribeOptions(llms.WithTranscribeDiarization(true), llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeExtra(map[string]any{"mode": map[string]any{"type": "verbatim"}}))
	config, err := transcriptionConfig(o)
	if err != nil {
		t.Fatal(err)
	}
	mode, ok := config.Mode.(geminiapi.VerbatimMode)
	if !ok || mode.DiarizationMode != "speaker" || !reflect.DeepEqual(mode.TimestampGranularities, []string{"word"}) {
		t.Fatalf("mode = %+v", config.Mode)
	}
}
func TestMediaOptionsAndMetadata(t *testing.T) {
	policy := DefaultPollPolicy()
	o := apply(WithImageModel("i"), WithVideoModel("v"), WithSpeechModel("s"), WithSpeechVoice("voice"), WithTranscriptionModel("t"), WithPollPolicy(policy))
	if o.ImageModel != "i" || o.VideoModel != "v" || o.SpeechModel != "s" || o.SpeechVoice != "voice" || o.TranscriptionModel != "t" || o.PollPolicy != policy {
		t.Fatalf("options = %+v", o)
	}
	c, err := New(WithAPIKey("test"))
	if err != nil {
		t.Fatal(err)
	}
	caps := c.Capabilities()
	if !caps.ImageGeneration || !caps.VideoGeneration || !caps.Speech || !caps.Transcription {
		t.Fatal("missing capability")
	}
	for _, group := range []struct {
		ids []string
		typ llms.ModelType
	}{
		{[]string{"gemini-2.5-flash-image", "gemini-3.1-flash-image", "gemini-3.1-flash-lite-image", "gemini-3-pro-image"}, llms.ModelTypeImage},
		{[]string{"veo-3.1-generate-preview", "veo-3.1-fast-generate-preview", "veo-3.1-lite-generate-preview"}, llms.ModelTypeVideo},
		{[]string{"gemini-3.1-flash-tts-preview", "gemini-2.5-flash-preview-tts", "gemini-2.5-pro-preview-tts", "gemini-3.5-transcribe"}, llms.ModelTypeAudio},
	} {
		for _, id := range group.ids {
			out := convertGeminiModel(&geminiapi.ModelInfo{Name: "models/" + id})
			if len(out.Types) != 1 || out.Types[0] != group.typ || out.DisplayName == "" {
				t.Fatalf("metadata = %+v", out)
			}
			out.Types[0] = llms.ModelTypeChat
			if knownModels[id].types[0] != group.typ {
				t.Fatal("aliased model types")
			}
		}
	}
	for model, variants := range videoPrices {
		for resolution, rate := range variants {
			u := pricedUsage(llms.MediaUnitSecond, 4, videoPrices, model, resolution)
			assertCost(t, u, rate*4)
		}
	}
	for model, prices := range imagePrices {
		rate, ok := llms.GetMediaRate("gemini", model)
		if !ok || rate.Unit != llms.MediaUnitImage || rate.USD != prices["1K"] {
			t.Fatal("image price drift")
		}
	}
	for model, prices := range videoPrices {
		rate, ok := llms.GetMediaRate("gemini", model)
		if !ok || rate.Unit != llms.MediaUnitSecond || rate.USD != prices["720p"] {
			t.Fatal("video price drift")
		}
	}
	for model, prices := range speechPrices {
		rate, ok := llms.GetMediaRate("gemini", model)
		if !ok || rate.Unit != llms.MediaUnitMTokenOut || rate.USD != prices[1] {
			t.Fatal("speech price drift")
		}
	}
	if mediaModel("models/gemini-3.1-flash-image", "") != "gemini-3.1-flash-image" {
		t.Fatal("model prefix")
	}
}

func TestClient_ImageModeration(t *testing.T) {
	for _, reason := range []string{"IMAGE_SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII"} {
		t.Run(reason, func(t *testing.T) {
			c := mediaTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				writeMediaJSON(t, w, map[string]any{"candidates": []any{map[string]any{"finishReason": reason}}})
			})
			_, err := c.GenerateImage(context.Background(), "draw")
			var mod *llms.ModerationError
			if !errors.As(err, &mod) || mod.Stage != llms.ModerationOutput || !reflect.DeepEqual(mod.Reasons, []string{reason}) {
				t.Fatalf("moderation = %v", err)
			}
		})
	}
	for _, blocked := range []bool{false, true} {
		c := mediaTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			if blocked {
				_, _ = w.Write([]byte(`{"candidates":[],"promptFeedback":{"blockReason":"BLOCKLIST","safetyRatings":[{"category":"test","probability":"HIGH"}]}}`))
			} else {
				_, _ = w.Write([]byte(`{"candidates":[]}`))
			}
		})
		_, err := c.GenerateImage(context.Background(), "draw")
		if blocked {
			var mod *llms.ModerationError
			if !errors.As(err, &mod) || mod.Stage != llms.ModerationInput || !reflect.DeepEqual(mod.Reasons, []string{"BLOCKLIST"}) {
				t.Fatal(err)
			}
		} else if !errors.Is(err, llms.ErrIncompleteResponse) {
			t.Fatal(err)
		}
	}
}
func TestTranscription_InvocationAccounting(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		output      int
	}{
		{"live", `"model_invocation_token_counts":[{"candidates_tokens_details":[{"modality":"text","tokens":38}]}]`, 38},
		{"multiple", `"model_invocation_token_counts":[{"candidates_tokens_details":[{"modality":"audio","tokens":500},{"modality":"text","tokens":20}]},{"candidates_tokens_details":[{"modality":"text","tokens":18}]}]`, 38},
		{"empty", `"model_invocation_token_counts":[]`, 0},
		{"absent", `"total_input_tokens":363`, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var response geminiapi.Interaction
			raw := `{"usage":{"total_output_tokens":9,"input_tokens_by_modality":[{"modality":"audio","tokens":362},{"modality":"text","tokens":1}],` + tc.usage + `}}`
			if err := json.Unmarshal([]byte(raw), &response); err != nil {
				t.Fatal(err)
			}
			out, err := convertTranscription(&response, "gemini-3.5-transcribe")
			if err != nil {
				t.Fatal(err)
			}
			assertCost(t, out.Usage, 362*2.0/1e6+float64(tc.output)*12/1e6)
		})
	}
}
func TestPricedUsage_UnknownVariant(t *testing.T) {
	// A card missing 4k must not fall back to the root 720p rate. The actual
	// verified 4k card remains priced when supplied.
	card := map[string]map[string]float64{"veo-3.1-generate-preview": {"720p": .4}}
	usage := pricedUsage(llms.MediaUnitSecond, 4, card, "veo-3.1-generate-preview", "4k")
	if usage.Unit != "" || usage.Cost != nil {
		t.Fatalf("usage = %+v", usage)
	}
	if _, ok := llms.MediaCost("gemini", "veo-3.1-generate-preview", usage); ok {
		t.Fatal("used base rate for missing variant")
	}
	tracker := llms.NewCostTracker(nil)
	tracker.RecordMedia("gemini", "veo-3.1-generate-preview", usage)
	if tracker.MediaTotals()["gemini:veo-3.1-generate-preview:"].Unpriced != 1 {
		t.Fatal("not recorded as unpriced")
	}
}
func TestClient_Transcribe_UnknownStatusAndAudioPolicy(t *testing.T) {
	var gets atomic.Int32
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req geminiapi.InteractionRequest
			readMediaRequest(t, r, &req)
			if req.Input[0].URI != "http://127.0.0.1/audio" {
				t.Error("missing caller URI")
			}
			writeMediaJSON(t, w, map[string]any{"id": "interaction", "status": "queued"})
			return
		}
		if gets.Add(1) == 1 {
			writeMediaJSON(t, w, map[string]any{"id": "interaction", "status": "future_status"})
			return
		}
		writeMediaJSON(t, w, transcriptResponse())
	})
	out, err := c.Transcribe(context.Background(), llms.MediaInput{URL: "http://127.0.0.1/audio", MIMEType: "audio/wav"}, llms.WithTranscribeLanguage("en"))
	if err != nil || gets.Load() != 2 || out.Language != "" {
		t.Fatalf("result = %+v, %v", out, err)
	}
	for _, opts := range [][]Option{{}, {WithAllowHTTP()}, {WithAllowPrivateIPs()}} {
		opts = append(opts, WithAPIKey("test"))
		client, e := New(opts...)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = client.Transcribe(context.Background(), llms.MediaInput{URL: "http://127.0.0.1/audio", MIMEType: "audio/wav"}); e == nil {
			t.Fatal("lost independent audio policy")
		}
	}
	for _, status := range []string{"completed", "queued"} {
		response := &geminiapi.Interaction{Status: status, Error: &geminiapi.InteractionError{Message: "failure"}}
		if _, err := interactionComplete(response); !errors.Is(err, llms.ErrJobFailed) {
			t.Fatal(err)
		}
	}
}
func TestClient_VideoJob_ConcurrentWait(t *testing.T) {
	var polls atomic.Int32
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writeMediaJSON(t, w, map[string]any{"name": "models/veo/operations/job"})
			return
		}
		if strings.Contains(r.URL.Path, "operations") {
			polls.Add(1)
			writeMediaJSON(t, w, videoOperation("http://"+r.Host+"/file"))
			return
		}
		_, _ = w.Write([]byte("mp4"))
	})
	job, err := c.GenerateVideo(context.Background(), "clouds", llms.WithVideoResolution("4K"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := job.Wait(context.Background()); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if polls.Load() != 1 {
		t.Fatalf("terminal operation fetched %d times", polls.Load())
	}
}
