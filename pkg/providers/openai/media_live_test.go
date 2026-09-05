//go:build integration

package openai

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
)

func liveMediaClient(t *testing.T) *Client {
	t.Helper()
	key := testutil.RequireEnvAPIKey(t, llms.EnvOpenAIAPIKey)
	client, err := New(WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestLiveOpenAI_GenerateImage(t *testing.T) {
	client := liveMediaClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	response, err := client.GenerateImage(ctx, "A small blue circle on a white background.", llms.WithImageModel("gpt-image-1-mini"), llms.WithImageQuality("low"), llms.WithImageSize("1024x1024"))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Images) == 0 || !bytes.HasPrefix(response.Images[0].Data, []byte("\x89PNG\r\n\x1a\n")) || response.Usage.Quantity <= 0 {
		t.Fatal("expected PNG image and output-token usage")
	}
}

func TestLiveOpenAI_Synthesize(t *testing.T) {
	client := liveMediaClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	response, err := client.Synthesize(ctx, "Hi.", llms.WithSpeechModel("gpt-4o-mini-tts"), llms.WithSpeechFormat(llms.AudioFormat{Container: "wav"}))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(response.Audio.Data, []byte("RIFF")) {
		t.Fatal("expected WAV RIFF header")
	}
}

func TestLiveOpenAI_StreamSpeech(t *testing.T) {
	client := liveMediaClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	chunks, err := client.StreamSpeech(ctx, "Hi.", llms.WithSpeechModel("gpt-4o-mini-tts"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				if count == 0 {
					t.Fatal("expected audio chunks")
				}
				return
			}
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
			if len(chunk.Data) > 0 {
				count++
			}
		case <-ctx.Done():
			t.Fatal("speech channel failed to close before timeout")
		}
	}
}

func TestLiveOpenAI_Transcribe(t *testing.T) {
	client := liveMediaClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var wav []byte
	if !t.Run("synthesize fixture", func(t *testing.T) {
		response, err := client.Synthesize(ctx, "Hi.", llms.WithSpeechFormat(llms.AudioFormat{Container: "wav"}))
		if err != nil {
			t.Fatal(err)
		}
		wav = response.Audio.Data
		if !bytes.HasPrefix(wav, []byte("RIFF")) {
			t.Fatal("expected WAV")
		}
	}) {
		return
	}
	t.Run("whisper_verbose_words", func(t *testing.T) {
		response, err := client.Transcribe(ctx, llms.MediaInput{Data: wav, MIMEType: "audio/wav"}, llms.WithTranscribeModel("whisper-1"), llms.WithTranscribeWordTimestamps(true))
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Words) == 0 || response.Usage.Unit != llms.MediaUnitMinute {
			t.Fatalf("expected words and minute usage: %+v", response)
		}
	})
	t.Run("diarize", func(t *testing.T) {
		response, err := client.Transcribe(ctx, llms.MediaInput{Data: wav, MIMEType: "audio/wav"}, llms.WithTranscribeModel("gpt-4o-transcribe-diarize"), llms.WithTranscribeDiarization(true))
		if err != nil {
			t.Fatal(err)
		}
		for _, segment := range response.Segments {
			if segment.Speaker != "" {
				return
			}
		}
		t.Fatal("expected a speaker in diarized transcription")
	})
	t.Run("default_json_usage", func(t *testing.T) {
		response, err := client.Transcribe(ctx, llms.MediaInput{Data: wav, MIMEType: "audio/wav"}, llms.WithTranscribeModel("gpt-4o-mini-transcribe"))
		if err != nil {
			t.Fatal(err)
		}
		if response.Text == "" || response.Usage.Cost == nil {
			t.Fatalf("expected text and token cost: %+v", response)
		}
	})
}

func TestLiveOpenAI_VideoJobWait(t *testing.T) {
	if os.Getenv("LLM_SDK_LIVE_VIDEO") != "1" {
		t.Skip("set LLM_SDK_LIVE_VIDEO=1 to run paid video test")
	}
	client := liveMediaClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	job, err := client.GenerateVideo(ctx, "Gentle clouds moving across a blue sky.", llms.WithVideoModel("sora-2"), llms.WithVideoDuration(4), llms.WithVideoResolution("720x1280"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := job.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Videos) == 0 || len(response.Videos[0].Data) < 8 || string(response.Videos[0].Data[4:8]) != "ftyp" {
		t.Fatal("expected MP4 ftyp box")
	}
}
