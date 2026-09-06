//go:build integration

package fal

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
)

func liveClient(t *testing.T) (*Client, context.Context) {
	t.Helper()
	key := testutil.RequireEnvAPIKey(t, llms.EnvFalAPIKey)
	c, err := New(WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return c, ctx
}
func liveSpeech(t *testing.T, c *Client, ctx context.Context) *llms.SpeechResponse {
	t.Helper()
	out, err := c.Synthesize(ctx, "The quick brown fox jumps over the lazy dog.", llms.WithSpeechVoice("af_heart"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out.Audio.Data, []byte("RIFF")) || out.Format.Container != "wav" || out.Usage.Unit != llms.MediaUnitKChar || out.Usage.Quantity <= 0 || out.Usage.Cost != nil {
		t.Fatalf("invalid WAV or usage: bytes=%d format=%+v usage=%+v", len(out.Audio.Data), out.Format, out.Usage)
	}
	return out
}
func TestLiveFal_GenerateImage(t *testing.T) {
	c, ctx := liveClient(t)
	out, err := c.GenerateImage(ctx, "a lighthouse at dawn, watercolor", llms.WithImageAspectRatio("1:1"), llms.WithImageFormat("png"), llms.WithImageExtra(map[string]any{"num_inference_steps": 2}))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Images) != 1 || !bytes.HasPrefix(out.Images[0].Data, []byte("\x89PNG")) || out.Images[0].URL == "" || out.Usage.Unit != llms.MediaUnitMegapixel || out.Usage.Quantity <= 0 || out.Usage.Cost != nil {
		t.Fatalf("image: count=%d bytes=%d usage=%+v", len(out.Images), len(out.Images[0].Data), out.Usage)
	}
	if out.Metadata["request_id"] == "" {
		t.Fatalf("metadata: %v", out.Metadata)
	}
}
func TestLiveFal_Synthesize(t *testing.T) { c, ctx := liveClient(t); _ = liveSpeech(t, c, ctx) }
func TestLiveFal_Transcribe(t *testing.T) {
	c, ctx := liveClient(t)
	speech := liveSpeech(t, c, ctx)
	out, err := c.Transcribe(ctx, llms.MediaInput{Data: speech.Audio.Data, MIMEType: "audio/wav"}, llms.WithTranscribeLanguage("en"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(out.Text), "fox") || out.Language != "en" || len(out.Segments) == 0 || out.Usage.Unit != llms.MediaUnitMinute || out.Usage.Quantity <= 0 {
		t.Fatalf("transcription: %+v", out)
	}
}
func TestLiveFal_VideoJobWait(t *testing.T) {
	if os.Getenv("LLM_SDK_LIVE_VIDEO") != "1" {
		t.Skip("set LLM_SDK_LIVE_VIDEO=1 to run the paid video generation test")
	}
	c, ctx := liveClient(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	job, err := c.GenerateVideo(ctx, "ocean waves rolling onto a sandy beach at sunset", llms.WithVideoDuration(6))
	if err != nil {
		t.Fatal(err)
	}
	if job.ID() == "" {
		t.Fatal("empty job ID")
	}
	out, err := job.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Videos) != 1 || len(out.Videos[0].Data) == 0 || !strings.HasPrefix(out.Videos[0].MIMEType, "video/") || out.Usage.Unit != llms.MediaUnitSecond || out.Usage.Quantity != 6 {
		t.Fatalf("video: %+v usage=%+v", out.Videos, out.Usage)
	}
}
