package elevenlabs

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestOutputFormat(t *testing.T) {
	// Independent wire list pins all supported formats and uses bits/second inputs.
	names := []string{"mp3_22050_32", "mp3_24000_48", "mp3_44100_32", "mp3_44100_64", "mp3_44100_96", "mp3_44100_128", "mp3_44100_192", "opus_48000_32", "opus_48000_64", "opus_48000_96", "opus_48000_128", "opus_48000_192", "pcm_8000", "pcm_16000", "pcm_22050", "pcm_24000", "pcm_32000", "pcm_44100", "pcm_48000", "ulaw_8000", "alaw_8000", "wav_8000", "wav_16000", "wav_22050", "wav_24000", "wav_32000", "wav_44100", "wav_48000"}
	if len(names) != 28 || len(supportedFormats) != 28 {
		t.Fatal("expected 28 formats")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			parts := strings.Split(name, "_")
			rate, _ := strconv.Atoi(parts[1])
			format := llms.AudioFormat{Container: parts[0], SampleRate: rate}
			if len(parts) == 3 {
				bits, _ := strconv.Atoi(parts[2])
				format.BitRate = bits * 1000
			}
			got, normalized, err := outputFormat(format)
			if err != nil || got != name || normalized != format {
				t.Fatalf("%s %+v %v", got, normalized, err)
			}
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("output_format") != name {
					t.Error(r.URL)
				}
				_, _ = w.Write([]byte("audio"))
			})
			if _, err = c.Synthesize(context.Background(), "Hi.", llms.WithSpeechFormat(format)); err != nil {
				t.Fatal(err)
			}
		})
	}
	got, format, err := outputFormat(llms.AudioFormat{})
	if err != nil || got != "mp3_44100_128" || format.BitRate != 128000 {
		t.Fatal(got, format, err)
	}
	for _, bad := range []llms.AudioFormat{{Container: "flac"}, {SampleRate: 123}, {BitRate: 12345}, {BitRate: 640}, {Container: "pcm", BitRate: 128000}, {Encoding: "unknown"}} {
		if _, _, err = outputFormat(bad); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Error(err)
		}
	}
}
func TestSynthesize_CharacterCost(t *testing.T) {
	for _, tc := range []struct {
		header  string
		credits int64
		present bool
	}{{"2000", 2000, true}, {"", 0, false}, {"bad", 0, false}, {"-1", 0, false}, {"0", 0, true}} {
		t.Run(tc.header, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/text-to-speech/voice" || r.Method != "POST" {
					t.Error(r.URL)
				}
				body := requestBody(t, r)
				settings, ok := body["voice_settings"].(map[string]any)
				if !ok || settings["speed"] != 1.2 || body["model_id"] != "eleven_v3" || body["language_code"] != "en" || body["seed"] != float64(42) || body["previous_text"] != "before" || body["next_text"] != "after" || body["apply_text_normalization"] != "auto" {
					t.Errorf("body %+v", body)
				}
				w.Header().Set("character-cost", tc.header)
				w.Header().Set("Content-Type", "audio/mpeg")
				_, _ = w.Write([]byte("ID3"))
			})
			out, err := c.Synthesize(context.Background(), "hé🙂", llms.WithSpeechVoice("voice"), llms.WithSpeechModel("eleven_v3"), llms.WithSpeechLanguage("en"), llms.WithSpeechSpeed(1.2), llms.WithSpeechExtra(map[string]any{"seed": 42, "previous_text": "before", "next_text": "after", "apply_text_normalization": "auto"}))
			if err != nil || out.Usage.Quantity != 0.003 || out.Usage.Unit != llms.MediaUnitKChar || out.Audio.MIMEType != "audio/mpeg" || string(out.Audio.Data) != "ID3" {
				t.Fatalf("%+v %v", out, err)
			}
			credits, present := out.Metadata["character_cost"]
			if present != tc.present || present && credits != tc.credits {
				t.Fatalf("credit metadata: %#v", out.Metadata)
			}
		})
	}
}
func TestSynthesize_Timestamps(t *testing.T) {
	for _, normalizedOnly := range []bool{false, true} {
		t.Run(strconv.FormatBool(normalizedOnly), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/with-timestamps") {
					t.Error(r.URL)
				}
				alignment := map[string]any{"characters": []string{"H", "i"}, "character_start_times_seconds": []float64{0, 0.1236}, "character_end_times_seconds": []float64{0.1236, 0.3}}
				body := map[string]any{"audio_base64": "SUQz", "normalized_alignment": alignment}
				if !normalizedOnly {
					body["alignment"] = alignment
				}
				w.Header().Set("character-cost", "10")
				writeJSON(t, w, body)
			})
			out, err := c.Synthesize(context.Background(), "Hi", llms.WithSpeechTimestamps(true))
			if err != nil || string(out.Audio.Data) != "ID3" || out.Alignment.StartMS[1] != 124 || out.Alignment.EndMS[1] != 300 || out.Audio.MIMEType != "audio/mpeg" || out.Usage.Quantity != 0.002 || out.Metadata["character_cost"] != int64(10) || out.Metadata["normalized_alignment"] == nil {
				t.Fatalf("%+v %v", out, err)
			}
		})
	}
}
func TestSynthesize_TimestampErrors(t *testing.T) {
	for _, body := range []string{"{", `{"audio_base64":"!!!"}`, `{"audio_base64":"SUQz","alignment":{"characters":["x"]}}`, `{"audio_base64":"SUQz","normalized_alignment":{"characters":["x"]}}`} {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		if _, err := c.Synthesize(context.Background(), "Hi", llms.WithSpeechTimestamps(true)); err == nil {
			t.Error(body)
		}
	}
	if a, err := convertAlignment(nil); err != nil || a != nil {
		t.Fatal(a, err)
	}
	if audioMIME("pcm") != "application/octet-stream" || audioMIME("wav") != "audio/wav" || audioMIME("opus") != "audio/ogg" {
		t.Fatal("MIME")
	}
}
func TestSynthesize_Routing(t *testing.T) {
	for _, tc := range []struct {
		model, route, key string
		extra             map[string]any
		quantity          float64
	}{{"eleven_text_to_sound_v2", "sound-generation", "text", map[string]any{"duration_seconds": 0.5, "prompt_influence": 0.2, "loop": true}, 0.5 / 60}, {"music_v2", "music", "prompt", map[string]any{"music_length_ms": 3000, "force_instrumental": true}, 0.05}, {"music_v1", "music", "prompt", nil, 10000.0 / 60000}, {"eleven_text_to_sound_v2", "sound-generation", "text", nil, 0}} {
		t.Run(tc.model+tc.route+strconv.FormatFloat(tc.quantity, 'g', -1, 64), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/"+tc.route {
					t.Error(r.URL)
				}
				body := requestBody(t, r)
				if body[tc.key] != "rain" || body["model_id"] != tc.model {
					t.Error(body)
				}
				for k, v := range tc.extra {
					if k == "music_length_ms" {
						v = float64(3000)
					}
					if body[k] != v {
						t.Errorf("%s: %v != %v", k, body[k], v)
					}
				}
				_, _ = w.Write([]byte("audio"))
			})
			out, err := c.Synthesize(context.Background(), "rain", llms.WithSpeechModel(tc.model), llms.WithSpeechExtra(tc.extra))
			if err != nil || out.Usage.Quantity != tc.quantity {
				t.Fatalf("%+v %v", out, err)
			}
			if tc.quantity > 0 && out.Usage.Unit != llms.MediaUnitMinute {
				t.Fatal(out.Usage)
			}
		})
	}
}
func TestSynthesize_Validation(t *testing.T) {
	c, _ := New(WithAPIKey("test"))
	for _, tc := range []struct {
		text string
		opts []llms.SpeechOption
	}{{" ", nil}, {"Hi", []llms.SpeechOption{llms.WithSpeechVoice("a/b")}}, {strings.Repeat("a", 5001), []llms.SpeechOption{llms.WithSpeechModel("eleven_v3")}}, {"Hi", []llms.SpeechOption{llms.WithSpeechSpeed(-1)}}, {"Hi", []llms.SpeechOption{llms.WithSpeechFormat(llms.AudioFormat{Container: "bad"})}}} {
		if _, err := c.Synthesize(context.Background(), tc.text, tc.opts...); err == nil {
			t.Fatal("accepted invalid speech")
		}
	}
	for _, tc := range []struct {
		model string
		extra map[string]any
	}{{"music_v2", map[string]any{"music_length_ms": 2}}, {"music_v2", map[string]any{"music_length_ms": 3000.5}}, {"music_v2", map[string]any{"force_instrumental": "true"}}, {"eleven_text_to_sound_v2", map[string]any{"duration_seconds": 31}}, {"eleven_text_to_sound_v2", map[string]any{"prompt_influence": 2}}, {"eleven_text_to_sound_v2", map[string]any{"loop": "true"}}} {
		if _, err := c.Synthesize(context.Background(), "Hi", llms.WithSpeechModel(tc.model), llms.WithSpeechExtra(tc.extra)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	if _, err := c.Synthesize(context.Background(), "Hi", llms.WithSpeechModel("music_v2"), llms.WithSpeechTimestamps(true)); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	for _, v := range []any{int(1), int64(1), float32(1), float64(1), json.Number("1")} {
		if n, ok := number(v); !ok || n != 1 {
			t.Fatal(v)
		}
	}
	for _, v := range []any{"1", math.NaN(), math.Inf(1), json.Number("bad")} {
		if _, ok := number(v); ok {
			t.Fatal(v)
		}
	}
}
func TestStreamSpeech(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/stream") || r.URL.Query().Get("output_format") != "mp3_44100_128" {
			t.Error(r.URL)
		}
		_ = requestBody(t, r)
		_, _ = w.Write([]byte("ID3"))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("audio"))
	})
	chunks, err := c.StreamSpeech(context.Background(), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	count := 0
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		data = append(data, chunk.Data...)
		count++
	}
	if count < 1 || string(data) != "ID3audio" {
		t.Fatal(count, string(data))
	}
}
func TestStreamSpeech_Cancel(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("audio"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chunks, err := c.StreamSpeech(ctx, "Hi")
	if err != nil {
		t.Fatal(err)
	}
	first := <-chunks
	if len(first.Data) == 0 {
		t.Fatal(first)
	}
	cancel()
	terminal := false
	timer := time.After(2 * time.Second)
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				if !terminal {
					t.Fatal("missing terminal cancellation")
				}
				return
			}
			if errors.Is(chunk.Err, context.Canceled) {
				terminal = true
			}
		case <-timer:
			t.Fatal("channel did not close")
		}
	}
}
func TestStreamSpeech_Errors(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(402)
		_, _ = w.Write([]byte(`{"detail":"Pro required"}`))
	})
	if _, err := c.StreamSpeech(context.Background(), "Hi"); !errors.Is(err, llms.ErrPlanRequired) {
		t.Fatal(err)
	}
	if _, err := c.StreamSpeech(context.Background(), ""); !errors.Is(err, llms.ErrEmptyText) {
		t.Fatal(err)
	}
	for _, opts := range [][]llms.SpeechOption{{llms.WithSpeechModel("music_v2")}, {llms.WithSpeechTimestamps(true)}} {
		if _, err := c.StreamSpeech(context.Background(), "Hi", opts...); !errors.Is(err, llms.ErrSpeechStreamNotSupported) {
			t.Fatal(err)
		}
	}
	c = testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("short"))
	})
	ch, err := c.StreamSpeech(context.Background(), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for chunk := range ch {
		failed = failed || chunk.Err != nil
	}
	if !failed {
		t.Fatal("truncated stream succeeded")
	}
}
func TestSynthesizeDialogue(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/text-to-dialogue" { //nolint:misspell // ElevenLabs wire route.
			t.Error(r.URL)
		}
		body := requestBody(t, r)
		lines, ok := body["inputs"].([]any)
		settings, settingsOK := body["voice_settings"].(map[string]any)
		if !settingsOK || settings["speed"] != 1.2 || body["language_code"] != "en" || body["text"] != nil || body["native_extension"] != true {
			t.Error(body)
		}
		if !ok || len(lines) != 2 || body["model_id"] != "eleven_v3" {
			t.Error(body)
		}
		_, _ = w.Write([]byte("dialog"))
	})
	out, err := c.SynthesizeDialogue(context.Background(), []DialogueLine{{Text: "Hi", VoiceID: "a"}, {Text: "Yo", VoiceID: "b"}}, llms.WithSpeechSpeed(1.2), llms.WithSpeechLanguage("en"), llms.WithSpeechExtra(map[string]any{"native_extension": true, "inputs": "ignored"}))
	if err != nil || out.Model != "eleven_v3" || out.Usage.Quantity != 0.004 || string(out.Audio.Data) != "dialog" {
		t.Fatal(out, err)
	}
	for _, lines := range [][]DialogueLine{nil, {{Text: "", VoiceID: "a"}}, {{Text: "Hi", VoiceID: "../x"}}} {
		if _, err = c.SynthesizeDialogue(context.Background(), lines); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	for _, opt := range []llms.SpeechOption{llms.WithSpeechModel("music_v2"), llms.WithSpeechTimestamps(true), llms.WithSpeechFormat(llms.AudioFormat{Container: "bad"})} {
		if _, err = c.SynthesizeDialogue(context.Background(), []DialogueLine{{Text: "Hi", VoiceID: "a"}}, opt); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
}

func TestSpeechExtra_Passthrough(t *testing.T) {
	for _, model := range []string{"eleven_flash_v2_5", "eleven_text_to_sound_v2", "music_v2"} {
		t.Run(model, func(t *testing.T) {
			extra := map[string]any{"native_extension": true, "model_id": "wrong", "text": "wrong", "prompt": "wrong", "voice_settings": map[string]any{"speed": -1}, "language_code": "wrong"}
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				body := requestBody(t, r)
				if body["native_extension"] != true || body["model_id"] != model {
					t.Error(body)
				}
				if model == "music_v2" {
					if body["prompt"] != "Hi" || body["text"] != nil {
						t.Error(body)
					}
				} else if body["text"] != "Hi" || body["prompt"] != nil {
					t.Error(body)
				}
				if model == "eleven_flash_v2_5" {
					settings, ok := body["voice_settings"].(map[string]any)
					if !ok || settings["speed"] != 1.2 || body["language_code"] != "en" {
						t.Error(body)
					}
				}
				_, _ = w.Write([]byte("audio"))
			})
			_, err := c.Synthesize(context.Background(), "Hi", llms.WithSpeechModel(model), llms.WithSpeechSpeed(1.2), llms.WithSpeechLanguage("en"), llms.WithSpeechExtra(extra))
			if err != nil {
				t.Fatal(err)
			}
			if extra["model_id"] != "wrong" || extra["text"] != "wrong" {
				t.Fatal("caller extras mutated")
			}
		})
	}
}
