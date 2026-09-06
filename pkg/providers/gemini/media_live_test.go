//go:build integration

package gemini

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
)

func liveGeminiMediaClient(t *testing.T) *Client {
	t.Helper()
	key := testutil.RequireEnvAPIKey(t, llms.EnvGeminiAPIKey)
	c, err := New(WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func skipGeminiFreeQuota(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "free_tier") || strings.Contains(message, "limit: 0") {
			t.Skipf("Gemini media requires paid quota: %v", err)
		}
	}
}
func TestLiveGemini_Synthesize(t *testing.T) {
	c := liveGeminiMediaClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := c.Synthesize(ctx, "Hi there.", llms.WithSpeechModel("gemini-3.1-flash-tts-preview"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Audio.Data) == 0 || out.Format.Container != "pcm" || out.Format.Encoding != "pcm_s16le" || out.Format.SampleRate != 24000 {
		t.Fatalf("expected 24 kHz PCM: %+v", out.Format)
	}
}
func TestLiveGemini_Transcribe(t *testing.T) {
	c := liveGeminiMediaClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	speech, err := c.Synthesize(ctx, "Hi there. This is a short transcription test.", llms.WithSpeechModel("gemini-3.1-flash-tts-preview"))
	if err != nil {
		t.Fatal(err)
	}
	if len(speech.Audio.Data) == 0 || speech.Format.SampleRate != 24000 {
		t.Fatal("expected 24 kHz PCM fixture")
	}
	input := llms.MediaInput{Data: geminiTestWAV(t, speech.Audio.Data), MIMEType: "audio/wav"}
	t.Run("smart", func(t *testing.T) {
		out, err := c.Transcribe(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out.Text) == "" {
			t.Fatal("empty transcript")
		}
	})
	t.Run("diarize_words", func(t *testing.T) {
		out, err := c.Transcribe(ctx, input, llms.WithTranscribeDiarization(true), llms.WithTranscribeWordTimestamps(true))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out.Text) == "" || len(out.Words) == 0 {
			t.Fatal("expected text and word timestamps")
		}
		for _, word := range out.Words {
			if word.Speaker != "" {
				return
			}
		}
		t.Fatal("expected speaker labels")
	})
}
func geminiTestWAV(t *testing.T, pcm []byte) []byte {
	t.Helper()
	if len(pcm) > 10*1024*1024 {
		t.Fatal("unexpectedly large live audio fixture")
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	for _, field := range []any{uint32(36 + len(pcm)), [4]byte{'W', 'A', 'V', 'E'}, [4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(1), uint32(24000), uint32(48000), uint16(2), uint16(16), [4]byte{'d', 'a', 't', 'a'}, uint32(len(pcm))} {
		if err := binary.Write(&out, binary.LittleEndian, field); err != nil {
			t.Fatal(err)
		}
	}
	out.Write(pcm)
	return out.Bytes()
}
func TestLiveGemini_GenerateImage(t *testing.T) {
	c := liveGeminiMediaClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := c.GenerateImage(ctx, "A small blue circle on a white background.", llms.WithImageModel("gemini-3.1-flash-lite-image"), llms.WithImageSize("1K"))
	skipGeminiFreeQuota(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Images) == 0 || len(out.Images[0].Data) == 0 || !strings.HasPrefix(out.Images[0].MIMEType, "image/") {
		t.Fatal("expected inline image")
	}
}
func TestLiveGemini_VideoJobWait(t *testing.T) {
	if os.Getenv("LLM_SDK_LIVE_VIDEO") != "1" {
		t.Skip("set LLM_SDK_LIVE_VIDEO=1 to run paid video test")
	}
	c := liveGeminiMediaClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	job, err := c.GenerateVideo(ctx, "Gentle clouds moving across a blue sky.", llms.WithVideoModel("veo-3.1-lite-generate-preview"), llms.WithVideoDuration(4), llms.WithVideoResolution("720p"))
	skipGeminiFreeQuota(t, err)
	if err != nil {
		t.Fatal(err)
	}
	out, err := job.Wait(ctx)
	skipGeminiFreeQuota(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Videos) == 0 || len(out.Videos[0].Data) < 8 || string(out.Videos[0].Data[4:8]) != "ftyp" {
		t.Fatal("expected MP4 ftyp box")
	}
}
