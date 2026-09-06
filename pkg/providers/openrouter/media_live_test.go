//go:build integration

package openrouter

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	key := testutil.RequireEnvAPIKey(t, llms.EnvOpenRouterAPIKey)
	client, err := New(WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	return client
}
func TestLiveOpenRouter_Chat(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	text, err := llms.Call(ctx, c, "Reply with hello.", llms.WithMaxTokens(64))
	if err != nil {
		t.Fatal(err)
	}
	if text == "" {
		t.Fatal("empty completion")
	}
}
func TestLiveOpenRouter_Stream(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	stream, err := c.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "Reply with hello."}}, llms.WithMaxTokens(64))
	if err != nil {
		t.Fatal(err)
	}
	done := false
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		done = done || chunk.Done
	}
	if !done {
		t.Fatal("missing terminal chunk")
	}
}
func TestLiveOpenRouter_ErrorHandling(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := llms.Call(ctx, c, "hello"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestLiveOpenRouter_GenerateImage(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := c.GenerateImage(ctx, "A small blue circle on white.", llms.WithImageModel(DefaultImageModel), llms.WithImageExtra(map[string]any{"resolution": "1K"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Images) == 0 {
		t.Fatal("no image")
	}
	data := out.Images[0].Data
	if !(bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) || bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff})) || out.Usage.Cost == nil {
		t.Fatal("expected PNG/JPEG and reported cost")
	}
}
func synthesizeFixture(t *testing.T, ctx context.Context, c *Client) []byte {
	t.Helper()
	out, err := c.Synthesize(ctx, "Hello, this is a short speech test.")
	if err != nil {
		t.Fatal(err)
	}
	data := out.Audio.Data
	if out.Audio.MIMEType != "audio/mpeg" {
		t.Fatalf("MIME type: %s", out.Audio.MIMEType)
	}
	// Usage is a value struct, present even when binary speech has no reported cost.
	if out.Model != DefaultSpeechModel {
		t.Fatal(out.Model)
	}
	if !bytes.HasPrefix(data, []byte("ID3")) && !(len(data) > 1 && data[0] == 0xff && (data[1] == 0xfb || data[1] == 0xf3)) {
		t.Fatal("expected MP3 bytes")
	}
	return data
}
func TestLiveOpenRouter_Synthesize(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	synthesizeFixture(t, ctx, c)
}
func TestLiveOpenRouter_Transcribe(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	data := synthesizeFixture(t, ctx, c)
	out, err := c.Transcribe(ctx, llms.MediaInput{Data: data, MIMEType: "audio/mpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text == "" || out.Usage.Cost == nil || out.Usage.Unit != llms.MediaUnitMinute {
		t.Fatal("expected text and reported cost")
	}
	t.Run("gpt4o_tokens", func(t *testing.T) {
		// /models did not list openai/gpt-4o-mini-transcribe on 2026-09-05.
		out, err := c.Transcribe(ctx, llms.MediaInput{Data: data, MIMEType: "audio/mpeg"}, llms.WithTranscribeModel("openai/gpt-4o-transcribe"))
		if err != nil {
			t.Fatal(err)
		}
		if out.Text == "" || out.Usage.Cost == nil {
			t.Fatal("expected text and cost")
		}
	})
}
func TestLiveOpenRouter_ListSpeechModels(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := c.ListSpeechModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		if model.ID == DefaultSpeechModel {
			return
		}
	}
	t.Fatal("default speech model missing from discovery")
}
func TestLiveOpenRouter_ListImageModels(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := c.ListImageModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("empty discovery")
	}
}
func TestLiveOpenRouter_VideoJobWait(t *testing.T) {
	if os.Getenv("LLM_SDK_LIVE_VIDEO") != "1" {
		t.Skip("set LLM_SDK_LIVE_VIDEO=1 for paid video test")
	}
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	// Lowest minimum clip cost among explicitly per-second priced 480p entries
	// in discovery on 2026-09-05; token-priced entries are not directly comparable.
	job, err := c.GenerateVideo(ctx, "Gentle clouds moving in a blue sky.", llms.WithVideoModel("x-ai/grok-imagine-video"), llms.WithVideoDuration(1), llms.WithVideoResolution("480p"), llms.WithVideoAudio(false))
	if err != nil {
		t.Fatal(err)
	}
	out, err := job.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Videos) == 0 || len(out.Videos[0].Data) < 8 || string(out.Videos[0].Data[4:8]) != "ftyp" || out.Usage.Cost == nil {
		t.Fatal("expected MP4 and reported cost")
	}
}
