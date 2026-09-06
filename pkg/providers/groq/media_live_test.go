//go:build integration

package groq

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// TestLiveMedia_Cheapest is opt-in through the provider-specific API key.
// Documentation verification is not a claim that this route was live-tested.
func TestLiveMedia_Cheapest(t *testing.T) {
	key := os.Getenv(llms.EnvGroqAPIKey)
	if key == "" {
		t.Skip("provider-specific API key is not set")
	}
	c, err := New(WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := c.Transcribe(ctx, llms.MediaInput{Data: oneSecondWAV(), MIMEType: "audio/wav"})
	if errors.Is(err, llms.ErrQuotaExceeded) || errors.Is(err, llms.ErrPlanRequired) {
		t.Skipf("media quota/plan unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("nil transcription")
	}
}

// oneSecondWAV embeds one second of silence in a valid 16 kHz, mono PCM WAV.
func oneSecondWAV() []byte {
	const sampleRate = 16000
	data := make([]byte, 44+sampleRate*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], sampleRate)
	binary.LittleEndian.PutUint32(data[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], sampleRate*2)
	return data
}
