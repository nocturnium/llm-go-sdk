package openai

import (
	"context"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
)

func TestKnownMediaPricing(t *testing.T) {
	// Token-priced transcription is accounted for by the converter, not this table.
	unpriced := map[string]bool{"gpt-4o-transcribe": true, "gpt-4o-mini-transcribe": true, "gpt-transcribe": true, "gpt-4o-transcribe-diarize": true, "dall-e-2": true}
	for id, metadata := range knownModels {
		for _, typ := range metadata.types {
			if typ != llms.ModelTypeImage && typ != llms.ModelTypeAudio && typ != llms.ModelTypeVideo {
				continue
			}
			if _, ok := llms.GetMediaRate("openai", id); !ok && !unpriced[id] {
				t.Errorf("missing price or explicit exemption: %s", id)
			}
		}
	}
	if _, ok := knownModels["dall-e-3"]; ok {
		t.Error("retired model retained")
	}
	for _, tc := range []struct {
		id  string
		typ llms.ModelType
	}{{"sora-2-custom", llms.ModelTypeVideo}, {"gpt-image-2-custom", llms.ModelTypeImage}, {"gpt-4o-transcribe-custom", llms.ModelTypeAudio}, {"gpt-4o-mini-tts-custom", llms.ModelTypeAudio}} {
		types := inferModelTypes(tc.id)
		if len(types) != 1 || types[0] != tc.typ {
			t.Errorf("%s: %v", tc.id, types)
		}
	}
}

func TestOpenAIMediaDefaults(t *testing.T) {
	server := testutil.NewMockOpenAICompatibleServer()
	defer server.Close()
	client, err := New(WithAPIKey("test"), WithBaseURL(server.URL()), WithAllowHTTP(), WithAllowPrivateIPs())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.GenerateImage(ctx, "moon"); err != nil {
		t.Fatal(err)
	}
	if server.LastRequest().Body["model"] != "gpt-image-1.5" {
		t.Fatal(server.LastRequest())
	}
	if _, err := client.EditImage(ctx, "moon", []llms.MediaInput{{Data: []byte("image"), MIMEType: "image/png"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Synthesize(ctx, "Hi."); err != nil {
		t.Fatal(err)
	}
	if server.LastRequest().Body["model"] != "gpt-4o-mini-tts" {
		t.Fatal(server.LastRequest())
	}
	stream, err := client.StreamSpeech(ctx, "Hi.")
	if err != nil {
		t.Fatal(err)
	}
	for chunk := range stream {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
	}
	if _, err := client.Transcribe(ctx, llms.MediaInput{Data: []byte("audio"), MIMEType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	if server.LastRequest().Body["model"] != "gpt-4o-mini-transcribe" {
		t.Fatal(server.LastRequest())
	}
	if _, err := client.GenerateVideo(ctx, "moon"); err != nil {
		t.Fatal(err)
	}
	if server.LastRequest().Body["model"] != "sora-2" {
		t.Fatal(server.LastRequest())
	}
}
