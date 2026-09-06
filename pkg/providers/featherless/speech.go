package featherless

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

func speechRequest(text string, o *llms.SpeechOptions) (*openaicompat.SpeechRequest, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("featherless: %w", llms.ErrEmptyText)
	}
	switch o.Format.Container {
	case "", "mp3", "opus", "aac", "flac", "wav", "pcm":
	default:
		return nil, fmt.Errorf("featherless: unsupported speech format: %w", llms.ErrInvalidParameters)
	}
	if o.Speed != nil && (math.IsNaN(*o.Speed) || *o.Speed < 0.25 || *o.Speed > 4) {
		return nil, fmt.Errorf("featherless: invalid speed: %w", llms.ErrInvalidParameters)
	}

	req := openaicompat.BuildSpeechRequest(providerConfig.DefaultSpeechModel, text, o)
	req.ExtraBody = make(map[string]any, len(o.Extra))
	for k, v := range o.Extra {
		req.ExtraBody[k] = v
	}
	req.ExtraBody["stream"] = false
	if o.Voice == "" {
		req.Voice = "af_bella"
	}
	return req, nil
}

// Synthesize returns audio using the provider's speech model and voice defaults.
// Invalid input or format returns ErrInvalidParameters before sending a request.
func (c *Client) Synthesize(ctx context.Context, text string, opts ...llms.SpeechOption) (*llms.SpeechResponse, error) {
	req, err := speechRequest(text, llms.ApplySpeechOptions(opts...))
	if err != nil {
		return nil, err
	}
	data, contentType, err := c.BaseProvider.Client().CreateSpeech(ctx, req)
	if err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "synthesize", err)
	}
	out := openaicompat.ConvertSpeechResponse(data, contentType, req, nil)
	out.Usage = llms.MediaUsage{Unit: llms.MediaUnitKChar, Quantity: float64(utf8.RuneCountInString(text)) / 1000}

	return out, nil
}

// StreamSpeech decodes OpenAI-shaped SSE speech.audio.delta events.
// Drain the channel or cancel ctx; transport and decoding errors are terminal chunks.
// A successful stream ends with a Data-less chunk whose Usage is the reported
// input_characters (KChar), or the request's Unicode character count when the
// done event carries none; WithSpeechUsageHandler still receives reported usage.
func (c *Client) StreamSpeech(ctx context.Context, text string, opts ...llms.SpeechOption) (<-chan llms.AudioChunk, error) {
	req, err := speechRequest(text, llms.ApplySpeechOptions(opts...))
	if err != nil {
		return nil, err
	}
	req.ExtraBody["stream"] = true
	req.StreamFormat = "sse"
	streamCtx, cancel := context.WithCancel(ctx)
	events, err := c.BaseProvider.Client().CreateSpeechStream(streamCtx, req)
	if err != nil {
		cancel()
		return nil, openaicompat.WrapError(c.Provider(), "stream speech", err)
	}
	chunks := make(chan llms.AudioChunk, 8)
	go func() {
		defer close(chunks)
		defer cancel()
		for event := range events {
			if event.Type == "speech.audio.done" && event.Err == nil {
				usage := openaicompat.SpeechStreamUsage(event.Usage)
				if usage != nil && c.options.SpeechUsageHandler != nil {
					c.options.SpeechUsageHandler(req.Model, *usage)
				}
				if usage == nil {
					usage = &llms.MediaUsage{Unit: llms.MediaUnitKChar, Quantity: float64(utf8.RuneCountInString(text)) / 1000}
				}
				select {
				case chunks <- llms.AudioChunk{Usage: usage}:
				case <-ctx.Done():
				}
				return
			}
			data, err := base64.StdEncoding.DecodeString(event.Audio)
			if event.Err != nil {
				err = event.Err
			}
			chunk := llms.AudioChunk{Data: data, Err: openaicompat.WrapError(c.Provider(), "stream speech", err)}
			select {
			case chunks <- chunk:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return chunks, nil
}
