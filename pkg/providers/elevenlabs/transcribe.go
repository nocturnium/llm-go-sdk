package elevenlabs

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

var audioExtensions = map[string]string{"audio/mpeg": "mp3", "audio/mp3": "mp3", "audio/wav": "wav", "audio/x-wav": "wav", "audio/mp4": "m4a", "audio/m4a": "m4a", "audio/ogg": "ogg", "audio/webm": "webm", "audio/flac": "flac", "video/mp4": "mp4", "video/webm": "webm"}

const supportedAudio = "audio/mpeg, audio/mp3, audio/wav, audio/x-wav, audio/mp4, audio/m4a, audio/ogg, audio/webm, audio/flac, video/mp4, video/webm"

func transcriptionInput(audio llms.MediaInput) (map[string]string, []httpclient.MultipartFile, error) {
	if err := audio.Validate(); err != nil {
		return nil, nil, WrapError("transcribe", err)
	}
	fields := map[string]string{}
	if audio.FileID != "" {
		return nil, nil, invalid("transcription does not accept FileID")
	}
	if audio.URL != "" {
		u, err := url.Parse(audio.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return nil, nil, invalid("source_url requires an HTTPS URL")
		}
		// source_url replaces the deprecated cloud_storage_url field.
		fields["source_url"] = audio.URL
		return fields, nil, nil
	}
	ext, ok := audioExtensions[audio.MIMEType]
	if !ok {
		return nil, nil, invalid("unsupported MIME type; supported: " + supportedAudio)
	}
	return fields, []httpclient.MultipartFile{{Field: "file", Filename: "audio." + ext, ContentType: audio.MIMEType, Data: audio.Data}}, nil
}
func transcriptionFields(fields map[string]string, o *llms.TranscribeOptions) error {
	fields["model_id"] = o.Model
	fields["diarize"] = strconv.FormatBool(o.Diarize)
	fields["timestamps_granularity"] = "word"
	if o.Language != "" {
		fields["language_code"] = o.Language
	}
	if v, ok := o.Extra["num_speakers"]; ok {
		n, valid := number(v)
		if !valid || n < 1 || n > 32 || math.Trunc(n) != n {
			return invalid("num_speakers must be an integer in [1,32]")
		}
		fields["num_speakers"] = strconv.Itoa(int(n))
	}
	if v, ok := o.Extra["timestamps_granularity"]; ok {
		s, valid := v.(string)
		if !valid || (s != "word" && s != "character" && s != "none") {
			return invalid("timestamps_granularity must be word, character or none")
		}
		fields["timestamps_granularity"] = s
	}
	if o.WordTimestamps {
		fields["timestamps_granularity"] = "word"
	}
	if v, ok := o.Extra["tag_audio_events"]; ok {
		b, valid := v.(bool)
		if !valid {
			return invalid("tag_audio_events must be boolean")
		}
		fields["tag_audio_events"] = strconv.FormatBool(b)
	}
	return nil
}

// Transcribe uploads inline audio or an HTTPS source_url to Scribe.
// FileID and unsupported MIME types return ErrInvalidParameters. Duration and
// minute usage prefer audio_duration_secs, falling back to max(word.End) only
// when it is zero. Without either duration source, the usage Unit is empty.
func (c *Client) Transcribe(ctx context.Context, audio llms.MediaInput, opts ...llms.TranscribeOption) (result *llms.Transcription, err error) {
	o := llms.ApplyTranscribeOptions(opts...)
	if o.Model == "" {
		o.Model = c.options.TranscriptionModel
	}
	ctx, finish := c.startOperation(ctx, "transcribe", o.Model)
	defer func() { finish(err) }()
	fields, files, err := transcriptionInput(audio)
	if err != nil {
		return nil, err
	}
	if err = transcriptionFields(fields, o); err != nil {
		return nil, err
	}
	for _, term := range o.Keyterms {
		if term == "" {
			return nil, invalid("keyterms must be nonempty")
		}
		files = append(files, httpclient.MultipartFile{Field: "keyterms", Data: []byte(term)})
	}
	var response struct {
		AudioDurationSecs float64 `json:"audio_duration_secs"`
		Language          string  `json:"language_code"`
		Probability       float64 `json:"language_probability"`
		Text              string  `json:"text"`
		ID                string  `json:"transcription_id"`
		Words             []struct {
			Text    string  `json:"text"`
			Type    string  `json:"type"`
			Start   float64 `json:"start"`
			End     float64 `json:"end"`
			Speaker string  `json:"speaker_id"`
		} `json:"words"`
	}
	if err = c.transport.DoMultipart(ctx, http.MethodPost, c.endpoint("speech-to-text", nil), fields, files, c.headers, &response); err != nil {
		return nil, WrapError("transcribe", err)
	}
	out := &llms.Transcription{Model: o.Model, Text: response.Text, Language: response.Language, Words: []llms.TranscriptWord{}, Metadata: map[string]any{"transcription_id": response.ID, "language_probability": response.Probability}}
	for _, word := range response.Words {
		if word.Start < 0 || word.End < word.Start {
			return nil, invalid(fmt.Sprintf("invalid word timing for %q", word.Text))
		}
		if response.AudioDurationSecs == 0 {
			out.DurationSeconds = math.Max(out.DurationSeconds, word.End)
		}
		if word.Type != "word" {
			continue
		}
		out.Words = append(out.Words, llms.TranscriptWord{Word: word.Text, Start: word.Start, End: word.End, Speaker: word.Speaker})
	}
	if response.AudioDurationSecs < 0 {
		return nil, invalid("negative audio_duration_secs")
	}
	if response.AudioDurationSecs != 0 {
		out.DurationSeconds = response.AudioDurationSecs
	}
	if response.AudioDurationSecs > 0 || len(response.Words) > 0 {
		out.Usage = llms.MediaUsage{Unit: llms.MediaUnitMinute, Quantity: out.DurationSeconds / 60}
	}
	return out, nil
}
