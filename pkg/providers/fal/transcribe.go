package fal

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"net/url"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// maxInlineAudio bounds Data inputs, which are sent as base64 data URIs.
const maxInlineAudio = 25 * 1024 * 1024

type transcriptionResult struct {
	Text   string `json:"text"`
	Chunks []struct {
		Timestamp []*float64 `json:"timestamp"`
		Text      string     `json:"text"`
		Speaker   string     `json:"speaker"`
	} `json:"chunks"`
	InferredLanguages   []string `json:"inferred_languages"`
	DiarizationSegments []struct {
		Timestamp []*float64 `json:"timestamp"`
		Speaker   string     `json:"speaker"`
	} `json:"diarization_segments"`
}

// audioURL converts a MediaInput into the audio_url field: an https URL or a
// base64 data URI (Data capped at 25 MB). FileID is rejected.
func audioURL(audio llms.MediaInput) (string, error) {
	if err := audio.Validate(); err != nil {
		return "", fmt.Errorf("fal: transcribe: %w", err)
	}
	if audio.FileID != "" {
		return "", invalid("transcription does not accept FileID")
	}
	if audio.URL != "" {
		u, err := url.Parse(audio.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return "", invalid("audio URL must be HTTPS")
		}
		return audio.URL, nil
	}
	if len(audio.Data) > maxInlineAudio {
		return "", invalid("inline audio exceeds 25 MB")
	}
	mime := strings.TrimSpace(audio.MIMEType)
	if mime == "" || !strings.HasPrefix(mime, "audio/") && !strings.HasPrefix(mime, "video/") {
		return "", invalid("inline audio requires an audio/* or video/* MIMEType")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(audio.Data), nil
}
func transcribeBody(audio llms.MediaInput, task string, o *llms.TranscribeOptions) (map[string]any, error) {
	source, err := audioURL(audio)
	if err != nil {
		return nil, err
	}
	if len(o.Keyterms) > 0 {
		return nil, invalid("Keyterms have no whisper mapping")
	}
	body := map[string]any{"audio_url": source, "task": task}
	if o.Language != "" {
		body["language"] = o.Language
	}
	if o.Prompt != "" {
		body["prompt"] = o.Prompt
	}
	if o.Diarize {
		body["diarize"] = true
	}
	if o.WordTimestamps {
		body["chunk_level"] = "word"
	}
	if err = mergeExtra(body, o.Extra, "audio_url", "task", "language", "diarize", "chunk_level", "prompt"); err != nil {
		return nil, err
	}
	if v, ok := body["num_speakers"]; ok {
		n, valid := number(v)
		if !valid || n < 1 || math.Trunc(n) != n {
			return nil, invalid("num_speakers must be a positive integer")
		}
	}
	return body, nil
}

// Transcribe queues Whisper speech-to-text. Inputs are HTTPS URLs or inline Data
// (sent as a data URI, at most 25 MB); FileID is rejected. Language, Prompt,
// Diarize (plus Extra num_speakers) and WordTimestamps (chunk_level word) map
// directly. Usage is minutes from the last chunk end when chunks exist; otherwise
// the unit is empty. Cost stays nil.
func (c *Client) Transcribe(ctx context.Context, audio llms.MediaInput, opts ...llms.TranscribeOption) (*llms.Transcription, error) {
	return c.transcribe(ctx, audio, "transcribe", opts...)
}

// Translate transcribes audio into English by setting task translate.
func (c *Client) Translate(ctx context.Context, audio llms.MediaInput, opts ...llms.TranscribeOption) (*llms.Transcription, error) {
	return c.transcribe(ctx, audio, "translate", opts...)
}
func (c *Client) transcribe(ctx context.Context, audio llms.MediaInput, task string, opts ...llms.TranscribeOption) (result *llms.Transcription, err error) {
	o := llms.ApplyTranscribeOptions(opts...)
	if o.Model == "" {
		o.Model = c.options.TranscriptionModel
	}
	ctx, finish := c.startOperation(ctx, task, o.Model)
	defer func() { finish(err) }()
	body, err := transcribeBody(audio, task, o)
	if err != nil {
		return nil, err
	}
	q, err := c.submit(ctx, o.Model, body)
	if err != nil {
		return nil, err
	}
	if err = c.await(ctx, q); err != nil {
		return nil, err
	}
	var res transcriptionResult
	header, err := c.result(ctx, q, &res)
	if err != nil {
		return nil, err
	}
	words := body["chunk_level"] == "word"
	out := &llms.Transcription{Model: o.Model, Text: res.Text, Segments: []llms.TranscriptSegment{}, Words: []llms.TranscriptWord{}, Metadata: map[string]any{"request_id": q.RequestID, "task": task}}
	if len(res.InferredLanguages) > 0 {
		out.Language = res.InferredLanguages[0]
		out.Metadata["inferred_languages"] = res.InferredLanguages
	}
	billableUnits(header, out.Metadata)
	var end float64
	skipped := 0
	for _, chunk := range res.Chunks {
		start, stop := timestamp(chunk.Timestamp)
		if start < 0 || stop < start {
			// A provider-side timing defect: keep the text and the well-formed chunks.
			skipped++
			continue
		}
		end = math.Max(end, stop)
		text := strings.TrimSpace(chunk.Text)
		if words {
			out.Words = append(out.Words, llms.TranscriptWord{Word: text, Start: start, End: stop, Speaker: chunk.Speaker})
			continue
		}
		out.Segments = append(out.Segments, llms.TranscriptSegment{Text: text, Start: start, End: stop, Speaker: chunk.Speaker})
	}
	if len(res.DiarizationSegments) > 0 {
		segments := make([]map[string]any, 0, len(res.DiarizationSegments))
		for _, segment := range res.DiarizationSegments {
			start, stop := timestamp(segment.Timestamp)
			segments = append(segments, map[string]any{"start": start, "end": stop, "speaker": segment.Speaker})
		}
		out.Metadata["diarization_segments"] = segments
	}
	if skipped > 0 {
		out.Metadata["skipped_chunks"] = skipped
	}
	if len(res.Chunks) > skipped {
		out.DurationSeconds = end
		out.Usage = llms.MediaUsage{Unit: llms.MediaUnitMinute, Quantity: end / 60}
	}
	return out, nil
}

// timestamp reads a [start, end] pair; a null end (whisper's final chunk) reuses start.
func timestamp(pair []*float64) (float64, float64) {
	var start, end float64
	if len(pair) > 0 && pair[0] != nil {
		start = *pair[0]
	}
	end = start
	if len(pair) > 1 && pair[1] != nil {
		end = *pair[1]
	}
	return start, end
}
