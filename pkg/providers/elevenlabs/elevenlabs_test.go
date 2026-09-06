package elevenlabs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") && r.Header.Get("xi-api-key") != "test-key" {
			t.Error("missing xi-api-key")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	base := []Option{WithAPIKey("test-key"), WithBaseURL(server.URL), WithAllowHTTP(), WithAllowPrivateIPs(), WithPollPolicy(PollPolicy{Initial: time.Millisecond, Max: time.Millisecond})}
	c, err := New(base...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}
func requestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	return body
}
func TestDefaultOptions(t *testing.T) {
	o := defaultOptions()
	if o.Model != "eleven_flash_v2_5" || o.Voice != "21m00Tcm4TlvDq8ikWAM" || o.TranscriptionModel != "scribe_v2" || o.ImageModel != "gemini-3.1-flash-lite-image" || o.VideoModel != "veo-3.1-fast-generate-001" || o.AllowHTTP || o.AllowPrivateIPs || o.Timeout != 120*time.Second {
		t.Fatalf("defaults: %+v", o)
	}
}
func TestApplyOptions(t *testing.T) {
	value := 0.5
	speed := 1.1
	boost := true
	settings := VoiceSettings{Stability: &value, SimilarityBoost: &value, Style: &value, Speed: &speed, UseSpeakerBoost: &boost}
	option := WithVoiceSettings(settings)
	value = 0.9
	hc := &http.Client{}
	p := PollPolicy{Timeout: time.Minute}
	o := apply(WithAPIKey("key"), WithModel("speech"), WithVoice("voice"), WithImageModel("image"), WithVideoModel("video"), WithTranscriptionModel("transcribe"), WithBaseURL("https://example.com"), WithTimeout(time.Second), WithHTTPClient(hc), WithPollPolicy(p), WithAllowHTTP(), WithAllowPrivateIPs(), option)
	if o.APIKey != "key" || o.Model != "speech" || o.Voice != "voice" || o.ImageModel != "image" || o.VideoModel != "video" || o.TranscriptionModel != "transcribe" || o.Timeout != time.Second || o.HTTPClient != hc || o.PollPolicy != p || !o.AllowHTTP || !o.AllowPrivateIPs || *o.VoiceSettings.Stability != 0.5 {
		t.Fatalf("options: %+v", o)
	}
	c, err := New(WithAPIKey("key"), WithTimeout(0))
	if err != nil || c.options.Timeout <= 0 {
		t.Fatalf("timeout: %v", err)
	}
}
func TestNewClientMissingAPIKey(t *testing.T) {
	t.Setenv(llms.EnvElevenLabsAPIKey, "")
	t.Setenv(llms.EnvLLMAPIKey, "")
	if _, err := New(); !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Fatal(err)
	}
}
func TestNewClientWithEnvAPIKey(t *testing.T) {
	t.Setenv(llms.EnvElevenLabsAPIKey, "specific")
	t.Setenv(llms.EnvLLMAPIKey, "generic")
	c, err := New()
	if err != nil || c.headers["xi-api-key"] != "specific" {
		t.Fatal(err)
	}
	c, err = New(WithAPIKey("explicit"))
	if err != nil || c.headers["xi-api-key"] != "explicit" {
		t.Fatal(err)
	}
}
func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	t.Setenv(llms.EnvElevenLabsAPIKey, "")
	t.Setenv(llms.EnvLLMAPIKey, "generic")
	c, err := New()
	if err != nil || c.headers["xi-api-key"] != "generic" {
		t.Fatal(err)
	}
}
func TestClientImplementsInterface(t *testing.T) {
	c, err := New(WithAPIKey("test"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := any(c).(llms.LLM); ok {
		t.Fatal("media-only provider implements chat")
	}
	if !llms.SupportsSpeech(c) || !llms.SupportsTranscription(c) || !llms.SupportsImageEdit(c) || !llms.SupportsImageGeneration(c) || !llms.SupportsVideoGeneration(c) || !llms.SupportsModelListing(c) {
		t.Fatal("missing interface")
	}
	if c.Provider() != llms.ProviderElevenLabs || c.Model() != defaultOptions().Model || c.Capabilities() != (llms.Capabilities{Speech: true, Transcription: true, ImageGeneration: true, VideoGeneration: true}) {
		t.Fatal("identity/capabilities")
	}
}
func TestNewClient_Validation(t *testing.T) {
	for _, base := range []string{"%", "http://", "ftp://example.com", "https://user:pass@example.com", "https://example.com?key=x", "https://example.com#x"} {
		if _, err := New(WithAPIKey("test"), WithBaseURL(base)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("%s: %v", base, err)
		}
	}
	for _, voice := range []string{"", ".", "..", "...", "a/b", "a\\b", "a?b", "a%b", "a#b", "a\n", strings.Repeat("a", 257)} {
		if _, err := New(WithAPIKey("test"), WithVoice(voice)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("voice %q: %v", voice, err)
		}
	}
	n := -1.0
	if _, err := New(WithAPIKey("test"), WithVoiceSettings(VoiceSettings{Stability: &n})); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}
func TestWrapError(t *testing.T) {
	if WrapError("op", nil) != nil {
		t.Fatal("nil")
	}
	original := errors.New("original")
	if !errors.Is(WrapError("op", original), original) {
		t.Fatal("lost cause")
	}
	for _, tc := range []struct {
		code    int
		body    string
		target  error
		message string
	}{
		{401, `{"detail":{"status":"invalid_api_key","message":"bad key"}}`, llms.ErrAuthenticationFailed, "bad key"},
		{402, `{"detail":{"type":"payment_required","code":"paid_plan_required","message":"This endpoint requires a Pro plan or above."}}`, llms.ErrPlanRequired, "Pro plan"},
		{422, `{"detail":"invalid parameters"}`, llms.ErrInvalidParameters, "invalid parameters"},
		{429, `{"detail":{"status":"rate_limited","message":"slow down"}}`, llms.ErrRateLimited, "slow down"},
		{404, `{"detail":"missing"}`, llms.ErrModelNotFound, "missing"},
		{503, `{"detail":"unavailable"}`, llms.ErrServiceUnavailable, "unavailable"},
		{418, `teapot`, nil, "teapot"},
		{422, `{"detail":null}`, llms.ErrInvalidParameters, `{"detail":null}`},
	} {
		t.Run(tc.message, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.code); _, _ = w.Write([]byte(tc.body)) })
			_, err := c.ListVoices(context.Background())
			var original *httpclient.APIError
			if err == nil || !strings.Contains(err.Error(), tc.message) || !errors.As(err, &original) || (tc.target != nil && !errors.Is(err, tc.target)) {
				t.Fatalf("error: %v", err)
			}
		})
	}
}
func TestListVoices(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/voices" {
			t.Error(r.URL)
		}
		writeJSON(t, w, map[string]any{"voices": []Voice{{VoiceID: "rachel", Name: "Rachel", Category: "premade", Labels: map[string]string{"accent": "american"}}}})
	})
	voices, err := c.ListVoices(context.Background())
	if err != nil || len(voices) != 1 || voices[0].Labels["accent"] != "american" {
		t.Fatalf("%+v %v", voices, err)
	}
}
func TestListVoices_Empty(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { writeJSON(t, w, map[string]any{}) })
	voices, err := c.ListVoices(context.Background())
	if err != nil || voices == nil || len(voices) != 0 {
		t.Fatal(voices, err)
	}
}
func TestCachedModelsMetadata(t *testing.T) {
	for _, m := range knownModels {
		if m.ID == "" || m.DisplayName == "" || m.Provider != llms.ProviderElevenLabs || m.Organization == "" || !m.FromCache || len(m.Types) != 1 || m.DeprecatedAt != nil {
			t.Fatal(m)
		}
	}
}
func TestListModels(t *testing.T) {
	c, _ := New(WithAPIKey("test"))
	ctx := context.Background()
	all, err := c.ListModels(ctx)
	if err != nil || len(all.Models) != 25 {
		t.Fatal(all, err)
	}
	first, err := c.ListModels(ctx, llms.WithModelTypes(llms.ModelTypeImage), llms.WithModelLimit(3))
	if err != nil || len(first.Models) != 3 || !first.HasMore {
		t.Fatal(first, err)
	}
	second, err := c.ListModels(ctx, llms.WithModelTypes(llms.ModelTypeImage), llms.WithModelCursor(first.NextCursor))
	if err != nil || len(second.Models) != 5 || second.HasMore {
		t.Fatal(second, err)
	}
	none, err := c.ListModels(ctx, llms.WithModelTypes(llms.ModelTypeChat))
	if err != nil || none.Models == nil || len(none.Models) != 0 {
		t.Fatal(none, err)
	}
	for _, opt := range []llms.ListModelsOption{llms.WithModelLimit(-1), llms.WithModelCursor("missing")} {
		if _, err = c.ListModels(ctx, opt); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	all.Models[0].Types[0] = llms.ModelTypeChat
	fresh, _ := c.ListModels(ctx)
	if fresh.Models[0].Types[0] != llms.ModelTypeAudio {
		t.Fatal("alias")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = c.ListModels(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestModelInfo(t *testing.T) {
	c, _ := New(WithAPIKey("test"))
	a, err := c.ModelInfo(context.Background(), "ELEVEN_V3")
	if err != nil || a.ID != "eleven_v3" {
		t.Fatal(a, err)
	}
	a.Types[0] = llms.ModelTypeChat
	b, _ := c.ModelInfo(context.Background(), "eleven_v3")
	if b.Types[0] != llms.ModelTypeAudio {
		t.Fatal("alias")
	}
	if _, err = c.ModelInfo(context.Background(), "unknown"); !errors.Is(err, llms.ErrModelNotFound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.ModelInfo(ctx, "eleven_v3"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestConcurrentListModels(t *testing.T) {
	c, _ := New(WithAPIKey("test"))
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			all, err := c.ListModels(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			all.Models[0].Types[0] = llms.ModelTypeChat
		}()
	}
	wg.Wait()
}
func TestConcurrentModelInfo(t *testing.T) {
	c, _ := New(WithAPIKey("test"))
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := c.ModelInfo(context.Background(), "eleven_v3")
			if err != nil {
				t.Error(err)
				return
			}
			m.Types[0] = llms.ModelTypeChat
		}()
	}
	wg.Wait()
}
func TestKnownMediaPricing(t *testing.T) {
	expected := map[string]llms.MediaRate{"eleven_turbo_v2_5": {Unit: llms.MediaUnitKChar, USD: 0.05}, "eleven_turbo_v2": {Unit: llms.MediaUnitKChar, USD: 0.05}, "eleven_flash_v2": {Unit: llms.MediaUnitKChar, USD: 0.05}, "eleven_v3": {Unit: llms.MediaUnitKChar, USD: 0.1}, "eleven_multilingual_v2": {Unit: llms.MediaUnitKChar, USD: 0.1}, "eleven_v3_conversational": {Unit: llms.MediaUnitKChar, USD: 0.05}, "eleven_flash_v2_5": {Unit: llms.MediaUnitKChar, USD: 0.05}, "scribe_v2": {Unit: llms.MediaUnitMinute, USD: 0.0036667}, "eleven_text_to_sound_v2": {Unit: llms.MediaUnitMinute, USD: 0.12}, "music_v2": {Unit: llms.MediaUnitMinute, USD: 0.15}}
	for _, m := range knownModels {
		rate, priced := llms.GetMediaRate("elevenlabs", m.ID)
		want, expectedPrice := expected[m.ID]
		if priced != expectedPrice || !reflect.DeepEqual(rate, want) {
			t.Errorf("%s: %+v", m.ID, rate)
		}
		if !priced && m.Types[0] == llms.ModelTypeAudio && m.ID != "music_v1" {
			t.Errorf("missing price or explicit exemption: %s", m.ID)
		}
	}
	// Flows are credit-priced; music_v1 has no verified rate. Never guess either.
	for id := range expected {
		found := false
		for _, m := range knownModels {
			if m.ID == id {
				found = true
			}
		}
		if !found {
			t.Errorf("priced model absent: %s", id)
		}
	}
}

func TestModelInfo_UpstreamOrganization(t *testing.T) {
	c, _ := New(WithAPIKey("test"))
	for _, tc := range []struct{ id, org string }{
		{"veo-3.1-fast-generate-001", "Google"}, {"gpt-image-1.5", "OpenAI"},
		{"bytedance-seedream-5-lite", "ByteDance"}, {"bytedance-seedance-v2", "ByteDance"},
		{"eleven_turbo_v2_5", "ElevenLabs"}, {"eleven_turbo_v2", "ElevenLabs"}, {"eleven_flash_v2", "ElevenLabs"},
	} {
		info, err := c.ModelInfo(context.Background(), tc.id)
		if err != nil || info.Organization != tc.org {
			t.Fatalf("%s: %+v %v", tc.id, info, err)
		}
	}
}
