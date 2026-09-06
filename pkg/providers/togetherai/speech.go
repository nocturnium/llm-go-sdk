package togetherai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

func speechRequest(text string, o *llms.SpeechOptions) (*openaicompat.SpeechRequest, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("togetherai: %w", llms.ErrEmptyText)
	}
	switch o.Format.Container {
	case "", "wav", "mp3", "raw":
	default:
		return nil, fmt.Errorf("togetherai: unsupported speech format: %w", llms.ErrInvalidParameters)
	}

	req := openaicompat.BuildSpeechRequest(providerConfig.DefaultSpeechModel, text, o)
	req.ExtraBody = make(map[string]any, len(o.Extra))
	for k, v := range o.Extra {
		req.ExtraBody[k] = v
	}
	req.ExtraBody["stream"] = false
	req.Voice = o.Voice
	if req.Voice == "" {
		req.Voice = "af_bella"
	}
	if o.Language != "" {
		req.ExtraBody["language"] = o.Language
	}
	req.Instructions = ""
	req.Speed = nil
	if o.Format.SampleRate > 0 {
		req.ExtraBody["sample_rate"] = o.Format.SampleRate
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
		return nil, c.mediaError(err)
	}
	out := openaicompat.ConvertSpeechResponse(data, contentType, req, nil)
	out.Usage = llms.MediaUsage{Unit: llms.MediaUnitKChar, Quantity: float64(utf8.RuneCountInString(text)) / 1000}
	return out, nil
}

// StreamSpeech decodes Together audio.tts.chunk SSE frames until [DONE].
// Only raw output supports SSE; other containers return ErrInvalidParameters.
// Drain the channel or cancel ctx to release the connection. [DONE] is followed
// by a Data-less chunk carrying the same per-character usage Synthesize reports.
func (c *Client) StreamSpeech(ctx context.Context, text string, opts ...llms.SpeechOption) (<-chan llms.AudioChunk, error) {
	o := llms.ApplySpeechOptions(opts...)
	if o.Format.Container != "" && o.Format.Container != "raw" {
		return nil, c.mediaError(fmt.Errorf("streaming speech requires raw response_format: %w", llms.ErrInvalidParameters))
	}
	o.Format.Container = "raw"
	req, err := speechRequest(text, o)
	if err != nil {
		return nil, err
	}
	req.ExtraBody["stream"] = true
	body, err := c.mediaHTTP.DoStream(ctx, httpclient.Request{Method: http.MethodPost, URL: c.mediaEndpoint("audio/speech"), Headers: c.mediaHeaders(), Body: req})
	if err != nil {
		return nil, c.mediaError(err)
	}
	chunks := make(chan llms.AudioChunk, 8)
	go func() {
		defer close(chunks)
		reader := httpclient.NewSSEReader(body)
		defer func() { _ = reader.Close() }()
		for {
			event, err := reader.Read()
			var data []byte
			if err == nil {
				if event.Data == "" {
					continue
				}
				if event.Data == "[DONE]" {
					usage := llms.MediaUsage{Unit: llms.MediaUnitKChar, Quantity: float64(utf8.RuneCountInString(text)) / 1000}
					select {
					case chunks <- llms.AudioChunk{Usage: &usage}:
					case <-ctx.Done():
					}
					return
				}
				var frame struct {
					Object string `json:"object"`
					B64    string `json:"b64"`
				}
				err = json.Unmarshal([]byte(event.Data), &frame)
				if err == nil {
					if frame.Object != "audio.tts.chunk" {
						err = fmt.Errorf("unexpected speech event %q", frame.Object)
					} else {
						data, err = base64.StdEncoding.DecodeString(frame.B64)
					}
				}
			} else if errors.Is(err, io.EOF) {
				err = llms.ErrStreamInterrupted
			}
			if ctx.Err() != nil {
				return
			}
			select {
			case chunks <- llms.AudioChunk{Data: data, Err: c.mediaError(err)}:
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
