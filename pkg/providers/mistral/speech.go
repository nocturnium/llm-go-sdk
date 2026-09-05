package mistral

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

func speechRequest(text string, o *llms.SpeechOptions) (*openaicompat.SpeechRequest, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("mistral: %w", llms.ErrEmptyText)
	}
	switch o.Format.Container {
	case "", "pcm", "wav", "mp3", "flac", "opus":
	default:
		return nil, fmt.Errorf("mistral: unsupported speech format: %w", llms.ErrInvalidParameters)
	}

	req := openaicompat.BuildSpeechRequest(providerConfig.DefaultSpeechModel, text, o)
	req.ExtraBody = make(map[string]any, len(o.Extra))
	for k, v := range o.Extra {
		req.ExtraBody[k] = v
	}
	req.ExtraBody["stream"] = false
	req.Voice = o.Voice
	return req, nil
}

// Synthesize returns audio using the provider's speech model and voice defaults.
// Invalid input or format returns ErrInvalidParameters before sending a request.
func (c *Client) Synthesize(ctx context.Context, text string, opts ...llms.SpeechOption) (*llms.SpeechResponse, error) {
	req, err := speechRequest(text, llms.ApplySpeechOptions(opts...))
	if err != nil {
		return nil, err
	}
	// Mistral returns JSON audio_data and uses voice_id/ref_audio, unlike OpenAI.
	body := map[string]any{}
	for k, v := range req.ExtraBody {
		body[k] = v
	}
	delete(body, "voice")
	body["model"] = req.Model
	body["input"] = req.Input
	body["response_format"] = req.ResponseFormat
	body["stream"] = false
	if req.Voice != "" {
		body["voice_id"] = req.Voice
		delete(body, "ref_audio")
	}
	var wire struct {
		AudioData string `json:"audio_data"`
	}
	if err := c.mediaRequest(ctx, http.MethodPost, c.mediaEndpoint("audio/speech"), body, &wire); err != nil {
		return nil, err
	}
	if wire.AudioData == "" {
		return nil, c.mediaError(fmt.Errorf("missing audio_data"))
	}
	data, err := base64.StdEncoding.DecodeString(wire.AudioData)
	if err != nil {
		return nil, c.mediaError(err)
	}
	contentType := map[string]string{"mp3": "audio/mpeg", "pcm": "audio/L16", "opus": "audio/ogg", "wav": "audio/wav", "flac": "audio/flac"}[req.ResponseFormat]
	out := openaicompat.ConvertSpeechResponse(data, contentType, req, nil)
	out.Usage = llms.MediaUsage{Unit: llms.MediaUnitKChar, Quantity: float64(utf8.RuneCountInString(text)) / 1000}
	return out, nil
}
