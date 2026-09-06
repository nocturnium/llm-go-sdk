package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

func mockClient(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	options := []Option{WithAPIKey("test-key"), WithBaseURL(server.URL + "/api/v1"), WithAllowHTTP(), WithAllowPrivateIPs()}
	options = append(options, opts...)
	c, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestDefaultOptions(t *testing.T) {
	o := defaultOptions()
	if o.Model != DefaultModel || o.BaseURL != defaultBaseURL || o.Timeout != 120*time.Second || o.AllowHTTP || o.AllowPrivateIPs || o.UsageLookup {
		t.Fatalf("defaults: %+v", o)
	}
	c, err := New(WithAPIKey("key"))
	if err != nil {
		t.Fatal(err)
	}
	caps := c.Capabilities()
	if c.Model() != DefaultModel || c.Provider() != llms.ProviderOpenRouter || !caps.ImageGeneration || !caps.VideoGeneration || !caps.Speech || !caps.Transcription || caps.MaxContextTokens != 0 {
		t.Fatal(caps)
	}
	if _, err := c.StreamSpeech(context.Background(), "hi"); !errors.Is(err, llms.ErrSpeechStreamNotSupported) {
		t.Fatal(err)
	}
	if _, err := c.EditImage(context.Background(), "hi", nil); !errors.Is(err, llms.ErrImageEditNotSupported) {
		t.Fatal(err)
	}
}
func TestApplyOptions(t *testing.T) {
	hc := &http.Client{}
	o := apply(WithAPIKey("key"), WithModel("model"), WithBaseURL("https://example.com"), WithHTTPClient(hc), WithTimeout(time.Second), WithEmbeddingModel("embed"), WithAllowHTTP(), WithAllowPrivateIPs(), WithSiteURL("https://app.example"), WithAppName("app"), WithUsageLookup())
	if o.APIKey != "key" || o.Model != "model" || o.HTTPClient != hc || o.Timeout != time.Second || o.EmbeddingModel != "embed" || !o.AllowHTTP || !o.AllowPrivateIPs || o.SiteURL != "https://app.example" || o.AppName != "app" || !o.UsageLookup {
		t.Fatalf("options: %+v", o)
	}
}
func TestNewClientMissingAPIKey(t *testing.T) {
	t.Setenv(llms.EnvOpenRouterAPIKey, "")
	t.Setenv(llms.EnvLLMAPIKey, "")
	if _, err := New(); !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Fatal(err)
	}
}
func TestNewClientWithEnvAPIKey(t *testing.T) {
	t.Setenv(llms.EnvOpenRouterAPIKey, "provider")
	t.Setenv(llms.EnvLLMAPIKey, "generic")
	c, err := New()
	if err != nil || c.options.APIKey != "provider" {
		t.Fatal(err)
	}
	c, err = New(WithAPIKey("explicit"))
	if err != nil || c.options.APIKey != "explicit" {
		t.Fatal(err)
	}
}
func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	t.Setenv(llms.EnvOpenRouterAPIKey, "")
	t.Setenv(llms.EnvLLMAPIKey, "generic")
	c, err := New()
	if err != nil || c.options.APIKey != "generic" {
		t.Fatal(err)
	}
}
func TestNewClientValidation(t *testing.T) {
	for _, base := range []string{"%", "ftp://example.com", "https://user@example.com", "https://example.com?key=x", "https://example.com/#x", "relative"} {
		if _, err := New(WithAPIKey("key"), WithBaseURL(base)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("%s: %v", base, err)
		}
	}
	if _, err := New(WithAPIKey("key"), WithTimeout(-1)); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	if _, err := New(WithAPIKey("key"), WithBaseURL(""), WithHTTPClient(&http.Client{}), WithTimeout(0)); err != nil {
		t.Fatal(err)
	}
}
func TestClientImplementsInterface(t *testing.T) {
	var _ llms.LLM = (*Client)(nil)
	var _ llms.CapableProvider = (*Client)(nil)
}
func TestClientImplementsModelLister(t *testing.T) { var _ llms.ModelLister = (*Client)(nil) }
func TestClientImplementsEmbedder(t *testing.T)    { var _ llms.Embedder = (*Client)(nil) }
func TestRegistry(t *testing.T) {
	for _, cfg := range []llms.Config{{APIKey: "key"}, {APIKey: "key", Model: "model", BaseURL: "https://example.com", HTTPClient: &http.Client{}, Timeout: time.Second, AllowHTTP: true, AllowPrivateIPs: true}} {
		c, err := llms.New("openrouter", cfg)
		if err != nil || c.Provider() != llms.ProviderOpenRouter {
			t.Fatal(err)
		}
	}
}
func TestClient_ChatAndHeaders(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("HTTP-Referer") != "https://app.example" || r.Header.Get("X-Title") != "app" {
			t.Error("wrong route or headers")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != DefaultModel {
			t.Error(body)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
	}, WithSiteURL("https://app.example"), WithAppName("app"))
	text, err := llms.Call(context.Background(), c, "hi")
	if err != nil || text != "hello" {
		t.Fatalf("%s %v", text, err)
	}
}
func TestClient_GenerateImage(t *testing.T) {
	extra := map[string]any{"resolution": "1K", "aspect_ratio": "16:9", "input_references": []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/image"}}}}
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/images" {
			t.Error(r.URL)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["aspect_ratio"] != "16:9" || body["seed"] != float64(0) || body["resolution"] != "1K" || body["input_references"] == nil || body["model"] != DefaultImageModel {
			t.Error(body)
		}
		fmt.Fprint(w, `{"data":[{"b64_json":"aGk=","media_type":"image/jpeg"}],"usage":{"completion_tokens":4175,"cost":0.04,"prompt_tokens":0,"total_tokens":4175}}`)
	})
	out, err := c.GenerateImage(context.Background(), "moon", llms.WithImageAspectRatio("1:1"), llms.WithImageSeed(0), llms.WithImageExtra(extra))
	if err != nil {
		t.Fatal(err)
	}
	if out.Images[0].MIMEType != "image/jpeg" || string(out.Images[0].Data) != "hi" || out.Usage.Cost == nil || *out.Usage.Cost != .04 || out.Usage.Quantity != 4175.0/1e6 || out.Usage.Unit != llms.MediaUnitMTokenOut {
		t.Fatal(out)
	}
	if _, ok := extra["seed"]; ok {
		t.Fatal("mutated extra")
	}
	if _, err := c.GenerateImage(context.Background(), ""); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
}
func TestClient_Synthesize(t *testing.T) {
	for _, mode := range []string{"off", "on", "missing-id", "missing-cost", "lookup-error", "speech-error"} {
		t.Run(mode, func(t *testing.T) {
			lookups := 0
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/audio/speech":
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["model"] != DefaultSpeechModel || body["response_format"] != "mp3" {
						t.Error(body)
					}
					if mode == "speech-error" {
						w.WriteHeader(401)
						fmt.Fprint(w, `{"error":{"message":"denied"}}`)
						return
					}
					if mode != "missing-id" {
						w.Header().Set("X-Generation-Id", "gen+&id")
					}
					w.Header().Set("Content-Type", "audio/mpeg")
					fmt.Fprint(w, "ID3audio")
				case "/api/v1/generation":
					lookups++
					if r.URL.Query().Get("id") != "gen+&id" {
						t.Error(r.URL)
					}
					switch mode {
					case "lookup-error":
						w.WriteHeader(404)
						fmt.Fprint(w, `{"error":{"message":"missing"}}`)
					case "missing-cost":
						fmt.Fprint(w, `{"data":{}}`)
					default:
						fmt.Fprint(w, `{"data":{"total_cost":0.001}}`)
					}
				default:
					t.Error(r.URL)
				}
			}, func(o *options) { o.UsageLookup = mode != "off" })
			out, err := c.Synthesize(context.Background(), "hi")
			if mode == "speech-error" {
				if !errors.Is(err, llms.ErrAuthenticationFailed) || out != nil {
					t.Fatal(out, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "lookup-error" && out.Usage.Cost != nil {
				t.Fatal(out.Usage)
			}
			if mode != "missing-id" && out.Metadata["generation_id"] != "gen+&id" {
				t.Fatal(out.Metadata)
			}

			if string(out.Audio.Data) != "ID3audio" || out.Audio.MIMEType != "audio/mpeg" || out.Format.Container != "mp3" {
				t.Fatal(out)
			}
			if mode == "on" && (out.Usage.Cost == nil || *out.Usage.Cost != .001) {
				t.Fatal(out)
			}
			if (mode == "off" || mode == "missing-id") && (lookups != 0 || out.Usage.Cost != nil) {
				t.Fatal(lookups, out)
			}
		})
	}
	c, _ := New(WithAPIKey("test"), WithUsageLookup())
	for _, text := range []string{"", strings.Repeat("x", 4097)} {
		if _, err := c.Synthesize(context.Background(), text); err == nil {
			t.Fatal("invalid text accepted")
		}
	}
	for _, speed := range []float64{0, 5} {
		if _, err := c.Synthesize(context.Background(), "hi", llms.WithSpeechSpeed(speed)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
}
func TestClient_Transcribe(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/audio/transcriptions" {
			t.Error(r.URL)
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("model") != DefaultTranscriptionModel || len(r.MultipartForm.File["file"]) != 1 {
			t.Error(r.Form)
		}
		if r.FormValue("response_format") == "verbose_json" && r.FormValue("timestamp_granularities[]") != "word" {
			t.Error(r.Form)
		}
		fmt.Fprint(w, `{"text":"hello","words":[{"word":"hello","start":0,"end":1}],"usage":{"seconds":120,"cost":0.012}}`)
	})
	audio := llms.MediaInput{Data: []byte("ID3audio"), MIMEType: "audio/mpeg"}
	for _, opts := range [][]llms.TranscribeOption{nil, {llms.WithTranscribeWordTimestamps(true), llms.WithTranscribeLanguage("en"), llms.WithTranscribePrompt("words"), llms.WithTranscribeExtra(map[string]any{"temperature": 0.5})}} {
		out, err := c.Transcribe(context.Background(), audio, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if out.Text != "hello" || out.Usage.Cost == nil || *out.Usage.Cost != .012 || out.Usage.Quantity != 2 || out.DurationSeconds != 120 || out.Usage.Unit != llms.MediaUnitMinute {
			t.Fatal(out)
		}
	}
}
func TestClient_VideoJob(t *testing.T) {
	polls := 0
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if r.URL.Path != "/api/v1/videos" || body["duration"] != float64(5) || body["resolution"] != "480p" || body["model"] != "minimax/hailuo-3-max" || body["generate_audio"] != false || body["seed"] != float64(42) || len(body["frame_images"].([]any)) != 2 || body["callback_url"] != "https://example.com/callback" || body["input_references"] == nil {
				t.Error(body)
			}
			frames := body["frame_images"].([]any)
			for i, want := range []string{"https://example.com/frame", "data:image/png;base64,ZnJhbWU="} {
				frame := frames[i].(map[string]any)
				kind := []string{"first_frame", "last_frame"}[i]
				if frame["type"] != "image_url" || frame["frame_type"] != kind || frame["image_url"].(map[string]any)["url"] != want {
					t.Error(frame)
				}
			}
			w.WriteHeader(202)
			fmt.Fprint(w, `{"id":"v1","status":"pending","polling_url":"https://untrusted.invalid/steal"}`)
		case r.URL.Path == "/api/v1/videos/v1":
			polls++
			state := "completed"
			if polls == 1 {
				state = "pending"
			}
			if polls == 2 {
				state = "in_progress"
			}
			fmt.Fprintf(w, `{"id":"v1","status":%q,"unsigned_urls":["https://cdn.example/0","https://cdn.example/1"],"usage":{"cost":0.25}}`, state)
		case r.URL.Path == "/api/v1/videos/v1/content":
			if r.URL.Query().Get("index") != "0" && r.URL.Query().Get("index") != "1" {
				t.Error(r.URL)
			}
			fmt.Fprint(w, "0000ftypvideo")
		default:
			t.Error(r.URL)
		}
	})
	job, err := c.GenerateVideo(context.Background(), "moon", llms.WithVideoModel("minimax/hailuo-3-max"), llms.WithVideoDuration(5), llms.WithVideoResolution("480p"), llms.WithVideoAspectRatio("16:9"), llms.WithVideoAudio(false), llms.WithVideoSeed(42), llms.WithVideoFirstFrame(llms.MediaInput{URL: "https://example.com/frame"}), llms.WithVideoLastFrame(llms.MediaInput{Data: []byte("frame"), MIMEType: "image/png"}), llms.WithVideoExtra(map[string]any{"callback_url": "https://example.com/callback", "input_references": []any{}}))
	if err != nil {
		t.Fatal(err)
	}
	if job.ID() != "v1" {
		t.Fatal(job.ID())
	}
	job.(*llms.PollingVideoJob).Policy = httpclient.PollPolicy{Initial: time.Millisecond, Max: time.Millisecond}
	out, err := job.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Videos) != 2 || out.Videos[1].URL != "https://cdn.example/1" || out.Videos[0].MIMEType != "video/mp4" || string(out.Videos[0].Data) != "0000ftypvideo" || out.Usage.Cost == nil || *out.Usage.Cost != .25 || out.Usage.Quantity != 5 || out.Usage.Unit != llms.MediaUnitSecond {
		t.Fatal(out)
	}
	if !errors.Is(job.Cancel(context.Background()), llms.ErrJobCancelNotSupported) {
		t.Fatal("cancel supported")
	}
}
func TestClient_VideoStates(t *testing.T) {
	const videoCancelled = "cancelled" //nolint:misspell // OpenRouter wire spelling.
	for _, tc := range []struct {
		wire, reason string
		state        llms.JobState
		cause        error
	}{
		{"pending", "", llms.JobQueued, nil}, {"in_progress", "", llms.JobRunning, nil}, {"completed", "", llms.JobSucceeded, nil}, {videoCancelled, "", llms.JobCancelled, context.Canceled}, {"expired", "", llms.JobFailed, llms.ErrAssetExpired}, {"failed", `"content_policy_violation"`, llms.JobModerated, llms.ErrContentFiltered}, {"failed", `"safety rejected"`, llms.JobModerated, llms.ErrContentFiltered}, {"failed", `"broken"`, llms.JobFailed, llms.ErrJobFailed}, {"unknown", "", llms.JobFailed, llms.ErrJobFailed},
	} {
		t.Run(tc.wire+tc.reason, func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					fmt.Fprint(w, `{"id":"v1"}`)
					return
				}
				reason := tc.reason
				if reason == "" {
					reason = "null"
				}
				fmt.Fprintf(w, `{"status":%q,"error":%s,"usage":{"cost":0.1}}`, tc.wire, reason)
			})
			job, err := c.GenerateVideo(context.Background(), "moon")
			if err != nil {
				t.Fatal(err)
			}
			status, err := job.Poll(context.Background())
			if err != nil || status.State != tc.state {
				t.Fatal(status, err)
			}
			if tc.cause != nil {
				_, err = job.Wait(context.Background())
				if !errors.Is(err, tc.cause) {
					t.Fatal(err)
				}
			}
			if tc.state == llms.JobModerated {
				var moderation *llms.ModerationError
				if !errors.As(status.Err, &moderation) || !moderation.Charged || moderation.Provider != "openrouter" {
					t.Fatal(status)
				}
			}
		})
	}
}
func TestClient_ErrorsAndCancellation(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"denied"}}`)
	}, WithUsageLookup())
	calls := []func(context.Context) error{
		func(ctx context.Context) error { _, e := llms.Call(ctx, c, "hi"); return e },
		func(ctx context.Context) error { _, e := c.GenerateImage(ctx, "hi"); return e },
		func(ctx context.Context) error { _, e := c.Synthesize(ctx, "hi"); return e },
		func(ctx context.Context) error {
			_, e := c.Transcribe(ctx, llms.MediaInput{Data: []byte("audio"), MIMEType: "audio/mpeg"})
			return e
		},
		func(ctx context.Context) error {
			_, e := c.Transcribe(ctx, llms.MediaInput{Data: []byte("audio"), MIMEType: "audio/mpeg"}, llms.WithTranscribeWordTimestamps(true))
			return e
		},
		func(ctx context.Context) error { _, e := c.GenerateVideo(ctx, "hi"); return e },
		func(ctx context.Context) error { _, e := c.ListModels(ctx); return e },
		func(ctx context.Context) error { _, e := c.ModelInfo(ctx, "model"); return e },
		func(ctx context.Context) error { _, e := c.ListImageModels(ctx); return e },
		func(ctx context.Context) error { _, e := c.ListVideoModels(ctx); return e },
		func(ctx context.Context) error { _, e := c.ListSpeechModels(ctx); return e },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i, call := range calls {
		if err := call(context.Background()); !errors.Is(err, llms.ErrAuthenticationFailed) {
			t.Fatalf("call %d: %v", i, err)
		}
		if err := call(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}
func TestConcurrentCalls(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "speech") {
			w.Header().Set("X-Generation-Id", "id")
			fmt.Fprint(w, "audio")
		} else if strings.HasSuffix(r.URL.Path, "generation") {
			fmt.Fprint(w, `{"data":{"total_cost":0.1}}`)
		} else {
			fmt.Fprint(w, `{"data":[{"id":"model"}]}`)
		}
	}, WithUsageLookup())
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Synthesize(context.Background(), "hi"); err != nil {
				t.Error(err)
			}
			if _, err := c.ListModels(context.Background()); err != nil {
				t.Error(err)
			}
			if _, err := c.ModelInfo(context.Background(), "model"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestClient_SynthesizeVoice(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		for _, voice := range []string{"", "custom-voice"} {
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				got, present := body["voice"]
				if (voice == "" && present) || (voice != "" && got != voice) {
					t.Error(body)
				}
				w.Header().Set("Content-Type", "audio/mpeg")
				fmt.Fprint(w, "ID3audio")
			}, func(o *options) { o.UsageLookup = lookup })
			if _, err := c.Synthesize(context.Background(), "hello", llms.WithSpeechVoice(voice)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestClient_TranscribeFileMetadata(t *testing.T) {
	for mimeType, ext := range map[string]string{"audio/mpeg": "mp3", "audio/mp3": "mp3", "audio/wav": "wav", "audio/mp4": "m4a", "audio/ogg": "ogg", "audio/webm": "webm", "audio/flac": "flac"} {
		t.Run(mimeType, func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1024); err != nil {
					t.Error(err)
					return
				}
				defer r.MultipartForm.RemoveAll()
				files := r.MultipartForm.File["file"]
				if len(files) != 1 {
					t.Error(files)
					return
				}
				if files[0].Filename != "audio."+ext || files[0].Header.Get("Content-Type") != mimeType {
					t.Error(files[0])
				}
				fmt.Fprint(w, `{"text":"Hi","usage":{"total_tokens":10,"input_tokens":7,"output_tokens":3,"cost":0.0000475}}`)
			})
			for _, words := range []bool{false, true} {
				out, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("audio"), MIMEType: mimeType}, llms.WithTranscribeWordTimestamps(words))
				if err != nil {
					t.Fatal(err)
				}
				if out.Usage.Cost == nil || *out.Usage.Cost != .0000475 || out.Usage.Unit != "" {
					t.Fatal(out.Usage)
				}
			}
		})
	}
}

func TestClient_SynthesizeVoiceRequired(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"An explicit voice is required for this TTS provider.","code":400}}`)
	})
	_, err := c.Synthesize(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "An explicit voice is required") {
		t.Fatal(err)
	}
}

func TestGenerationCost(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/generation" || r.URL.Query().Get("id") != "a+&b" {
			t.Error(r.URL)
		}
		fmt.Fprint(w, `{"data":{"total_cost":0.03}}`)
	})
	cost, err := c.GenerationCost(context.Background(), "a+&b")
	if err != nil || cost == nil || *cost != .03 {
		t.Fatal(cost, err)
	}
	if _, err := c.GenerationCost(context.Background(), ""); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}

func TestSpeechExtras(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["provider"] == nil || body["input_references"] == nil || body["model"] != DefaultSpeechModel || body["input"] != "hi" {
			t.Error(body)
		}
		if _, ok := body["voice"]; ok {
			t.Error(body)
		}
		fmt.Fprint(w, "audio")
	})
	_, err := c.Synthesize(context.Background(), "hi", llms.WithSpeechExtra(map[string]any{"provider": map[string]any{"options": map[string]any{}}, "input_references": []any{}, "model": "bad", "input": "bad", "voice": "bad"}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestVideoExtrasAndUnknownDuration(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["size"] != "1280x720" || body["provider"] == nil || body["model"] != DefaultVideoModel || body["prompt"] != "moon" {
				t.Error(body)
			}
			for _, key := range []string{"duration", "negative_prompt", "output_format"} {
				if _, ok := body[key]; ok {
					t.Error(body)
				}
			}
			fmt.Fprint(w, `{"id":"v1"}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "content") {
			fmt.Fprint(w, "video")
			return
		}
		fmt.Fprint(w, `{"status":"completed","unsigned_urls":["https://example.com/v"],"usage":{"cost":0.1}}`)
	})
	job, err := c.GenerateVideo(context.Background(), "moon", llms.WithVideoExtra(map[string]any{"size": "1280x720", "provider": map[string]any{}, "model": "bad", "prompt": "bad", "duration": 99, "negative_prompt": "bad", "output_format": "bad"}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := job.Wait(context.Background())
	if err != nil || out.Usage.Unit != "" || out.Usage.Quantity != 0 || out.Usage.Cost == nil {
		t.Fatal(out, err)
	}
}

func TestVideoModerationStage(t *testing.T) {
	for _, running := range []bool{false, true} {
		polls := 0
		c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				fmt.Fprint(w, `{"id":"v1"}`)
				return
			}
			polls++
			if running && polls == 1 {
				fmt.Fprint(w, `{"status":"in_progress"}`)
				return
			}
			fmt.Fprint(w, `{"status":"failed","error":"safety rejected"}`)
		})
		job, err := c.GenerateVideo(context.Background(), "moon")
		if err != nil {
			t.Fatal(err)
		}
		if running {
			if _, err := job.Poll(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		status, err := job.Poll(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var moderation *llms.ModerationError
		want := llms.ModerationInput
		if running {
			want = llms.ModerationOutput
		}
		if !errors.As(status.Err, &moderation) || moderation.Stage != want {
			t.Fatal(status)
		}
	}
}

func TestTranscriptionMIMERequired(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request sent") })
	for _, mime := range []string{"", "application/octet-stream"} {
		_, err := c.Transcribe(context.Background(), llms.MediaInput{Data: []byte("audio"), MIMEType: mime})
		if !errors.Is(err, llms.ErrInvalidParameters) || !strings.Contains(err.Error(), "audio/mpeg") {
			t.Fatal(err)
		}
	}
}
