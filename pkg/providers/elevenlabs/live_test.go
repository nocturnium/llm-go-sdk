//go:build integration

package elevenlabs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
)

func liveClient(t *testing.T) (*Client, context.Context) {
	t.Helper()
	key := testutil.RequireEnvAPIKey(t, llms.EnvElevenLabsAPIKey)
	c, err := New(WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return c, ctx
}
func liveMP3(t *testing.T, c *Client, ctx context.Context) *llms.SpeechResponse {
	t.Helper()
	out, err := c.Synthesize(ctx, "Hi.", llms.WithSpeechModel("eleven_flash_v2_5"), llms.WithSpeechFormat(llms.AudioFormat{Container: "mp3", SampleRate: 44100, BitRate: 64000}))
	if err != nil {
		t.Fatal(err)
	}
	data := out.Audio.Data
	magic := bytes.HasPrefix(data, []byte("ID3")) || len(data) >= 2 && data[0] == 0xff && (data[1] == 0xfb || data[1] == 0xf3 || data[1] == 0xf2)
	if !magic || out.Usage.Quantity <= 0 {
		t.Fatalf("invalid MP3 or usage: bytes=%d usage=%+v", len(data), out.Usage)
	}
	return out
}
func TestLiveElevenLabs_Synthesize(t *testing.T) { c, ctx := liveClient(t); _ = liveMP3(t, c, ctx) }
func TestLiveElevenLabs_StreamSpeech(t *testing.T) {
	c, ctx := liveClient(t)
	ch, err := c.StreamSpeech(ctx, "Hi.", llms.WithSpeechFormat(llms.AudioFormat{BitRate: 64000}))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if len(chunk.Data) > 0 {
			count++
		}
	}
	if count < 1 {
		t.Fatal("no audio chunks")
	}
}
func TestLiveElevenLabs_SynthesizeWithTimestamps(t *testing.T) {
	c, ctx := liveClient(t)
	out, err := c.Synthesize(ctx, "Hi.", llms.WithSpeechTimestamps(true), llms.WithSpeechFormat(llms.AudioFormat{BitRate: 64000}))
	if err != nil {
		t.Fatal(err)
	}
	if out.Alignment == nil || len(out.Alignment.Chars) == 0 {
		t.Fatal("no alignment")
	}
}
func TestLiveElevenLabs_Transcribe(t *testing.T) {
	c, ctx := liveClient(t)
	// A longer, distinctive sentence keeps Scribe from guessing the wrong
	// language on a sub-second clip.
	speech, err := c.Synthesize(ctx, "The quick brown fox jumps over the lazy dog.", llms.WithSpeechModel("eleven_flash_v2_5"), llms.WithSpeechFormat(llms.AudioFormat{Container: "mp3", SampleRate: 44100, BitRate: 64000}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Transcribe(ctx, llms.MediaInput{Data: speech.Audio.Data, MIMEType: "audio/mpeg"}, llms.WithTranscribeLanguage("en"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out.Text), "fox") || out.Usage.Unit != llms.MediaUnitMinute {
		t.Fatalf("unexpected transcript: %+v", out)
	}
}
func TestLiveElevenLabs_SoundEffect(t *testing.T) {
	c, ctx := liveClient(t)
	out, err := c.Synthesize(ctx, "A short soft click", llms.WithSpeechModel("eleven_text_to_sound_v2"), llms.WithSpeechExtra(map[string]any{"duration_seconds": 0.5}))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Audio.Data) == 0 {
		t.Fatal("no audio")
	}
}
func TestLiveElevenLabs_ListVoices(t *testing.T) {
	c, ctx := liveClient(t)
	voices, err := c.ListVoices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) == 0 || voices[0].VoiceID == "" {
		t.Fatal("no voices")
	}
}
func TestLiveElevenLabs_FlowsImagePlanGate(t *testing.T) {
	c, ctx := liveClient(t)
	out, err := c.GenerateImage(ctx, "A blue circle on a white background")
	if err != nil {
		if !errors.Is(err, llms.ErrPlanRequired) {
			t.Fatal(err)
		}
		t.Skip("Flows requires Pro plan")
	}
	if len(out.Images) == 0 {
		t.Fatal("no image")
	}
	data := out.Images[0].Data
	png := bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
	jpeg := bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff})
	webp := len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	if !png && !jpeg && !webp {
		t.Fatal("invalid image magic")
	}
}
func TestLiveElevenLabs_FlowsVideoPlanGate(t *testing.T) {
	c, ctx := liveClient(t)
	if os.Getenv("LLM_SDK_LIVE_VIDEO") != "1" {
		t.Skip("set LLM_SDK_LIVE_VIDEO=1 to enable paid video generation")
	}
	job, err := c.GenerateVideo(ctx, "Clouds drifting across a blue sky", llms.WithVideoDuration(4), llms.WithVideoResolution("720p"))
	if err != nil {
		if !errors.Is(err, llms.ErrPlanRequired) {
			t.Fatal(err)
		}
		t.Skip("Flows requires Pro plan")
	}
	out, err := job.Wait(ctx)
	if err != nil {
		if errors.Is(err, llms.ErrPlanRequired) {
			t.Skip("Flows requires Pro plan")
		}
		t.Fatal(err)
	}
	if len(out.Videos) == 0 || len(out.Videos[0].Data) == 0 {
		t.Fatal("no video")
	}
}
