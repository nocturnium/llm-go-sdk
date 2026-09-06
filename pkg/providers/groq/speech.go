package groq

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

func speechRequest(text string, o *llms.SpeechOptions) (*openaicompat.SpeechRequest, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("groq: %w", llms.ErrEmptyText)
	}
	if utf8.RuneCountInString(text) > 200 || (o.Format.Container != "" && o.Format.Container != "wav") {
		return nil, fmt.Errorf("groq: speech requires at most 200 characters and wav: %w", llms.ErrInvalidParameters)
	}

	if value, supplied := o.Extra["response_format"]; supplied {
		if format, ok := value.(string); !ok || format != "wav" {
			return nil, fmt.Errorf("groq: speech response_format must be wav: %w", llms.ErrInvalidParameters)
		}
	}

	// Groq accepts only the typed speech fields; extras cannot override them.
	req := openaicompat.BuildSpeechRequest(providerConfig.DefaultSpeechModel, text, o)
	req.ResponseFormat = "wav"
	if o.Voice == "" {
		if req.Model != "canopylabs/orpheus-v1-english" {
			return nil, fmt.Errorf("groq: voice is required for model %q: %w", req.Model, llms.ErrInvalidParameters)
		}
		req.Voice = "autumn"
	}
	req.Instructions = ""
	req.Speed = nil
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
