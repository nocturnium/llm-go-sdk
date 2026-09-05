package togetherai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// Transcribe uploads audio using the provider's multipart route. Options map to
// native fields; unsupported formats and streaming return ErrInvalidParameters.
// Timing is retained when reported; missing duration leaves usage unpriced.
func (c *Client) Transcribe(ctx context.Context, audio llms.MediaInput, opts ...llms.TranscribeOption) (*llms.Transcription, error) {
	return c.transcribe(ctx, "audio/transcriptions", audio, llms.ApplyTranscribeOptions(opts...))
}

func (c *Client) transcribe(ctx context.Context, route string, audio llms.MediaInput, o *llms.TranscribeOptions) (*llms.Transcription, error) {
	if err := audio.Validate(); err != nil {
		return nil, c.mediaError(err)
	}
	if audio.FileID != "" || len(audio.Data) > 80*1024*1024 {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	model := o.Model
	if model == "" {
		model = providerConfig.DefaultTranscriptionModel
	}
	fields := map[string]string{"model": model}
	files := []httpclient.MultipartFile{}
	if audio.URL != "" {
		fields["file"] = audio.URL
	} else {
		extensions := map[string]string{"audio/wav": ".wav", "audio/x-wav": ".wav", "audio/mpeg": ".mp3", "audio/mp3": ".mp3", "audio/mp4": ".m4a", "audio/flac": ".flac", "audio/ogg": ".ogg", "audio/webm": ".webm"}
		if extensions[audio.MIMEType] == "" {
			return nil, c.mediaError(fmt.Errorf("unsupported audio MIME type: %w", llms.ErrInvalidParameters))
		}
		files = append(files, httpclient.MultipartFile{Field: "file", Filename: "audio" + extensions[audio.MIMEType], ContentType: audio.MIMEType, Data: audio.Data})
	}
	if o.Language != "" {
		fields["language"] = o.Language
	}
	if o.Prompt != "" {
		fields["prompt"] = o.Prompt
	}
	format := "verbose_json"
	if o.WordTimestamps {
		format = "verbose_json"
		files = append(files, httpclient.MultipartFile{Field: "timestamp_granularities", Data: []byte("word")})
	}
	if o.Diarize {
		fields["diarize"] = "true"
	}
	if len(o.Keyterms) > 0 {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}

	for k, v := range o.Extra {
		switch value := v.(type) {
		case string, bool, int, float64:
			fields[k] = fmt.Sprint(value)
		case []string:
			for _, item := range value {
				files = append(files, httpclient.MultipartFile{Field: k, Data: []byte(item)})
			}
		default:
			return nil, c.mediaError(fmt.Errorf("invalid multipart extra %q: %w", k, llms.ErrInvalidParameters))
		}
	}
	if f, ok := fields["response_format"]; ok {
		format = f
	}
	if fields["stream"] == "true" || (format != "json" && format != "verbose_json") {
		return nil, c.mediaError(llms.ErrInvalidParameters)
	}
	fields["response_format"] = format
	var raw []byte
	err := c.mediaHTTP.DoMultipart(ctx, http.MethodPost, c.mediaEndpoint(route), fields, files, c.mediaHeaders(), &raw)
	if err != nil {
		return nil, c.mediaError(err)
	}
	var wire struct {
		openaicompat.TranscriptionResponse
		Words []struct {
			Word      string          `json:"word"`
			Start     float64         `json:"start"`
			End       float64         `json:"end"`
			SpeakerID json.RawMessage `json:"speaker_id"`
			Speaker   string          `json:"speaker"`
		} `json:"words"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, c.mediaError(err)
	}
	if wire.Error != nil {
		return nil, c.mediaError(&httpclient.APIError{Code: wire.Error.Code, Message: wire.Error.Message})
	}
	out := openaicompat.ConvertTranscriptionResponse(&wire.TranscriptionResponse, fields["model"])
	for _, w := range wire.Words {
		speaker := w.Speaker
		if speaker == "" && len(w.SpeakerID) > 0 && string(w.SpeakerID) != "null" {
			speaker = strings.Trim(string(w.SpeakerID), "\"")
		}
		out.Words = append(out.Words, llms.TranscriptWord{Word: w.Word, Start: w.Start, End: w.End, Speaker: speaker})
	}

	return out, nil
}
