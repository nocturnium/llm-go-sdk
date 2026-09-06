// Example: Media Generation
//
// This example demonstrates the provider-agnostic media interfaces:
// text-to-image, text-to-speech, speech-to-text, and (optionally) a
// text-to-video job. It uses OpenAI for image and video, and prefers
// ElevenLabs for speech when ELEVENLABS_API_KEY is set, falling back to
// OpenAI otherwise.
//
// Run with: go run ./examples/media
// Set LLM_SDK_LIVE_VIDEO=1 to also run the paid video job.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/elevenlabs"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if os.Getenv(llms.EnvOpenAIAPIKey) == "" {
		log.Fatal("Set OPENAI_API_KEY to run this example")
	}
	oai, err := openai.New()
	if err != nil {
		fatal("client", err)
	}

	tracker := llms.NewCostTracker()

	// 1. Text-to-image. Every provider that supports images satisfies
	// llms.ImageGenerator; check at runtime with llms.AsImageGenerator.
	if gen, ok := llms.AsImageGenerator(oai); ok {
		img, err := gen.GenerateImage(ctx, "a watercolor lighthouse at dawn",
			llms.WithImageModel("gpt-image-1-mini"),
			llms.WithImageQuality("low"),
			llms.WithImageSize("1024x1024"),
		)
		if err != nil {
			fatal("image", err)
		}
		tracker.RecordMedia(string(oai.Provider()), img.Model, img.Usage)
		writeFile("lighthouse.png", img.Images[0].Data)
	}

	// 2. Text-to-speech. Pick the speech provider by what is configured.
	var speaker llms.SpeechSynthesizer = oai
	speechModel := "gpt-4o-mini-tts"
	providerName := string(oai.Provider())
	if os.Getenv(llms.EnvElevenLabsAPIKey) != "" {
		el, err := elevenlabs.New()
		if err != nil {
			fatal("client", err)
		}
		speaker, speechModel, providerName = el, "eleven_flash_v2_5", string(el.Provider())
	}
	speech, err := speaker.Synthesize(ctx, "Hello from the media example.",
		llms.WithSpeechModel(speechModel),
		llms.WithSpeechFormat(llms.AudioFormat{Container: "mp3"}),
	)
	if err != nil {
		fatal("speech", err)
	}
	tracker.RecordMedia(providerName, speech.Model, speech.Usage)
	writeFile("hello.mp3", speech.Audio.Data)

	// 3. Speech-to-text, feeding the audio we just produced.
	transcript, err := oai.Transcribe(ctx, llms.MediaInput{Data: speech.Audio.Data, MIMEType: "audio/mpeg"})
	if err != nil {
		fatal("transcribe", err)
	}
	tracker.RecordMedia(string(oai.Provider()), transcript.Model, transcript.Usage)
	fmt.Printf("Transcript: %q\n", transcript.Text)

	// 4. Text-to-video is asynchronous: GenerateVideo returns a job, and
	// Wait polls until it reaches a terminal state.
	if os.Getenv("LLM_SDK_LIVE_VIDEO") == "1" {
		job, err := oai.GenerateVideo(ctx, "a paper boat drifting on a pond",
			llms.WithVideoModel("sora-2"),
			llms.WithVideoDuration(4),
			llms.WithVideoResolution("720p"),
		)
		if err != nil {
			fatal("video", err)
		}
		video, err := job.Wait(ctx)
		if err != nil {
			fatal("video wait", err)
		}
		tracker.RecordMedia(string(oai.Provider()), video.Model, video.Usage)
		writeFile("boat.mp4", video.Videos[0].Data)
	}

	fmt.Printf("Total media cost: $%.4f\n", tracker.GetTotalCost())
	for key, total := range tracker.MediaTotals() {
		fmt.Printf("  %-40s %8.4f %-10s $%.4f (unpriced: %d)\n", key, total.Quantity, total.Unit, total.Cost, total.Unpriced)
	}
}

func writeFile(name string, data []byte) {
	if err := os.WriteFile(name, data, 0o600); err != nil {
		fatal("write "+name, err)
	}
	fmt.Printf("Wrote %s (%d bytes)\n", name, len(data))
}

// fatal logs a provider error and exits. Provider errors can echo request
// or environment-derived text, so newlines are stripped before logging to
// keep each entry on one line (and to satisfy CodeQL's log-injection check).
func fatal(op string, err error) {
	log.Fatalf("%s: %s", op, strings.ReplaceAll(err.Error(), "\n", " "))
}
