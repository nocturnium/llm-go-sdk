package featherless

import (
	"context"
	"errors"
	"math"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
)

func TestStreamSpeech(t *testing.T) {
	for _, audio := range []string{"UklGRg==", "%%%"} {
		t.Run(audio, func(t *testing.T) {
			server := testutil.NewMockOpenAICompatibleServer(testutil.WithSpeechStreamResponse(map[string]any{"type": "speech.audio.delta", "audio": audio}, map[string]any{"type": "speech.audio.done"}))
			defer server.Close()
			c, err := New(WithAPIKey("test"), WithBaseURL(server.URL()), WithAllowHTTP(), WithAllowPrivateIPs())
			if err != nil {
				t.Fatal(err)
			}
			chunks, err := c.StreamSpeech(context.Background(), "hello", llms.WithSpeechVoice("custom"), llms.WithSpeechInstructions("quiet"), llms.WithSpeechSpeed(1), llms.WithSpeechExtra(map[string]any{"stream": false}))
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			var usage *llms.MediaUsage
			for chunk := range chunks {
				if chunk.Usage != nil {
					usage = chunk.Usage
					continue
				}
				count++
				if audio == "%%%" {
					if chunk.Err == nil {
						t.Error("bad base64 accepted")
					}
				} else if string(chunk.Data) != "RIFF" || chunk.Err != nil {
					t.Errorf("%+v", chunk)
				}
			}
			if count != 1 {
				t.Fatal(count)
			}
			if audio == "%%%" && usage != nil {
				t.Fatal("failed stream must not report usage")
			}
			if audio != "%%%" && (usage == nil || usage.Unit != llms.MediaUnitKChar || usage.Quantity != .005) {
				t.Fatalf("usage = %+v", usage)
			}
			b := server.LastRequest().Body
			if b["stream"] != true || b["stream_format"] != "sse" || b["voice"] != "custom" || b["instructions"] != "quiet" {
				t.Error(b)
			}
		})
	}
}

func TestStreamSpeech_Errors(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(402) })
	if _, err := c.StreamSpeech(context.Background(), "hello"); !errors.Is(err, llms.ErrPlanRequired) {
		t.Fatal(err)
	}
	if _, err := c.StreamSpeech(context.Background(), ""); !errors.Is(err, llms.ErrEmptyText) {
		t.Fatal(err)
	}
	for _, speed := range []float64{math.NaN(), .1, 5} {
		if _, err := c.Synthesize(context.Background(), "hello", llms.WithSpeechSpeed(speed)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Error(err)
		}
	}
}

func TestStreamSpeech_Truncated(t *testing.T) {
	c := mediaTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/event-stream") })
	chunks, err := c.StreamSpeech(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for chunk := range chunks {
		count++
		if !errors.Is(chunk.Err, llms.ErrStreamInterrupted) {
			t.Error(chunk.Err)
		}
	}
	if count != 1 {
		t.Fatal(count)
	}
}
