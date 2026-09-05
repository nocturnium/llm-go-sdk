package openrouter

import (
	"context"
	"fmt"
	"net/http"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// Transcribe uploads at most 25 MB of inline audio with an audio filename and
// content type. JSON and verbose_json use the compatible multipart wire without
// OpenAI's unprefixed model allowlist. Only json and verbose_json are supported; diarization and
// streaming return ErrInvalidParameters. Provider costs are preserved.
func (c *Client) Transcribe(ctx context.Context, audio llms.MediaInput, opts ...llms.TranscribeOption) (*llms.Transcription, error) {
	o := llms.ApplyTranscribeOptions(opts...)
	format := "json"
	if o.WordTimestamps {
		format = "verbose_json"
	}
	if value, ok := o.Extra["response_format"]; ok {
		format, _ = value.(string)
	}
	if o.Diarize || (format != "json" && format != "verbose_json") {
		return nil, fmt.Errorf("openrouter: unsupported transcription format: %w", llms.ErrInvalidParameters)
	}
	if err := audio.Validate(); err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "transcribe", err)
	}
	if len(audio.Data) == 0 || len(audio.Data) > 25*1024*1024 {
		return nil, fmt.Errorf("openrouter: transcription requires inline audio up to 25 MB: %w", llms.ErrInvalidParameters)
	}
	req := openaicompat.BuildTranscriptionRequest(DefaultTranscriptionModel, audio, o)
	fields := map[string]string{"model": req.Model, "response_format": format}
	if req.Language != "" {
		fields["language"] = req.Language
	}
	if req.Prompt != "" {
		fields["prompt"] = req.Prompt
	}
	for k, v := range o.Extra {
		switch v.(type) {
		case string, bool, int, float64:
			fields[k] = fmt.Sprint(v)
		default:
			return nil, fmt.Errorf("openrouter: transcription Extra must be scalar: %w", llms.ErrInvalidParameters)
		}
	}
	if fields["stream"] == "true" {
		return nil, fmt.Errorf("openrouter: streaming transcription unsupported: %w", llms.ErrInvalidParameters)
	}
	extensions := map[string]string{"audio/mpeg": ".mp3", "audio/mp3": ".mp3", "audio/wav": ".wav", "audio/mp4": ".m4a", "audio/flac": ".flac", "audio/ogg": ".ogg", "audio/webm": ".webm"}
	if extensions[audio.MIMEType] == "" {
		return nil, fmt.Errorf("openrouter: supported audio MIME types: audio/mpeg, audio/mp3, audio/wav, audio/mp4, audio/flac, audio/ogg, audio/webm: %w", llms.ErrInvalidParameters)
	}
	files := []httpclient.MultipartFile{{Field: "file", Filename: "audio" + extensions[audio.MIMEType], ContentType: audio.MIMEType, Data: audio.Data}}
	if o.WordTimestamps {
		files = append(files, httpclient.MultipartFile{Field: "timestamp_granularities[]", Data: []byte("word")})
	}
	var response openaicompat.TranscriptionResponse
	err := c.transport.DoMultipart(ctx, http.MethodPost, c.endpoint("audio/transcriptions", nil), fields, files, c.headers, &response)
	if err != nil {
		return nil, openaicompat.WrapError(c.Provider(), "transcribe", err)
	}
	return openaicompat.ConvertTranscriptionResponse(&response, fields["model"]), nil
}
