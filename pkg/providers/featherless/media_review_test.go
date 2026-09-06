package featherless

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestSpeech_FormatsAndCharacterUsage(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "audio") })
	for _, format := range []string{"mp3", "opus", "aac", "flac", "wav", "pcm"} {
		out, err := c.Synthesize(context.Background(), "hé", llms.WithSpeechFormat(llms.AudioFormat{Container: format}))
		if err != nil {
			t.Fatal(err)
		}
		if out.Usage.Unit != llms.MediaUnitKChar || out.Usage.Quantity != .002 {
			t.Error(out.Usage)
		}
	}
}
func TestStreamSpeech_ReportedCharacters(t *testing.T) {
	for _, usage := range []string{`{"input_characters":25}`, `{"input_characters":0}`, `{}`, `{"input_characters":-1}`} {
		c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			b := mediaJSON(t, r)
			if b["stream_format"] != "sse" {
				t.Error(b)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: speech.audio.delta\ndata: {\"audio\":\"aGk=\"}\n\nevent: speech.audio.done\ndata: {\"usage\":%s}\n\n", usage)
		})
		var got *llms.MediaUsage
		WithSpeechUsageHandler(func(model string, u llms.MediaUsage) {
			if model != providerConfig.DefaultSpeechModel {
				t.Error(model)
			}
			got = &u
		})(c.options)
		ch, err := c.StreamSpeech(context.Background(), "hi")
		if err != nil {
			t.Fatal(err)
		}
		for chunk := range ch {
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
		}
		switch usage {
		case `{"input_characters":25}`:
			if got == nil || got.Unit != llms.MediaUnitKChar || got.Quantity != .025 {
				t.Errorf("%+v", got)
			}
		case `{"input_characters":0}`:
			if got == nil || got.Quantity != 0 {
				t.Errorf("%+v", got)
			}
		default:
			if got != nil {
				t.Errorf("%+v", got)
			}
		}
	}
}
