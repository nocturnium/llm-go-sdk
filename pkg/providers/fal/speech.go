package fal

import (
	"context"
	"fmt"
	"math"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// kokoroAmericanVoices are the voice IDs accepted by DefaultSpeechModel.
var kokoroAmericanVoices = map[string]bool{
	"af_heart": true, "af_alloy": true, "af_aoede": true, "af_bella": true, "af_jessica": true, "af_kore": true, "af_nicole": true, "af_nova": true, "af_river": true, "af_sarah": true, "af_sky": true,
	"am_adam": true, "am_echo": true, "am_eric": true, "am_fenrir": true, "am_liam": true, "am_michael": true, "am_onyx": true, "am_puck": true, "am_santa": true,
}

// DefaultVoice is the voice Kokoro American English uses when none is set.
const DefaultVoice = "af_heart"

type speechResult struct {
	Audio struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		FileName    string `json:"file_name"`
		FileSize    int64  `json:"file_size"`
	} `json:"audio"`
}

func speechBody(text, model string, o *llms.SpeechOptions) (map[string]any, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("fal: speech: %w", llms.ErrEmptyText)
	}
	if o.Timestamps {
		return nil, invalid("speech timestamps are unsupported")
	}
	if o.Language != "" || o.Instructions != "" {
		return nil, invalid("Language and Instructions have no mapping; Kokoro languages are separate applications")
	}
	if o.Format.Encoding != "" || o.Format.SampleRate != 0 || o.Format.BitRate != 0 || (o.Format.Container != "" && o.Format.Container != "wav") {
		return nil, invalid("speech Format must be empty or Container wav")
	}
	body := map[string]any{"prompt": text}
	if o.Voice != "" {
		if model == DefaultSpeechModel && !kokoroAmericanVoices[o.Voice] {
			return nil, invalid("unknown Kokoro American English voice " + o.Voice)
		}
		body["voice"] = o.Voice
	}
	if o.Speed != nil {
		if math.IsNaN(*o.Speed) || math.IsInf(*o.Speed, 0) || *o.Speed <= 0 {
			return nil, invalid("speed must be finite and positive")
		}
		body["speed"] = *o.Speed
	}
	if err := mergeExtra(body, o.Extra, "prompt", "voice", "speed"); err != nil {
		return nil, err
	}
	return body, nil
}

// Synthesize queues Kokoro text-to-speech and downloads the WAV result.
// Voice (validated for the default application only), Speed and Extra map
// directly; Format accepts only an empty or wav container. Usage is Unicode
// runes / 1000 (KChar) with Cost nil.
func (c *Client) Synthesize(ctx context.Context, text string, opts ...llms.SpeechOption) (result *llms.SpeechResponse, err error) {
	o := llms.ApplySpeechOptions(opts...)
	if o.Model == "" {
		o.Model = c.options.SpeechModel
	}
	ctx, finish := c.startOperation(ctx, "synthesize", o.Model)
	defer func() { finish(err) }()
	body, err := speechBody(text, o.Model, o)
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
	var res speechResult
	header, err := c.result(ctx, q, &res)
	if err != nil {
		return nil, err
	}
	asset, err := c.fetchAsset(ctx, res.Audio.URL, res.Audio.ContentType)
	if err != nil {
		return nil, err
	}
	out := &llms.SpeechResponse{
		Audio:    asset,
		Format:   llms.AudioFormat{Container: audioContainer(asset.MIMEType)},
		Model:    o.Model,
		Usage:    llms.MediaUsage{Unit: llms.MediaUnitKChar, Quantity: float64(len([]rune(text))) / 1000},
		Metadata: map[string]any{"request_id": q.RequestID},
	}
	if res.Audio.FileName != "" {
		out.Metadata["file_name"] = res.Audio.FileName
	}
	billableUnits(header, out.Metadata)
	return out, nil
}

// audioContainer infers the container from a MIME type, defaulting to wav.
func audioContainer(mime string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0])) {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/ogg":
		return "ogg"
	case "audio/flac":
		return "flac"
	case "audio/mp4", "audio/m4a":
		return "m4a"
	default:
		return "wav"
	}
}

// StreamSpeech is unsupported: the queue API returns whole files only.
func (c *Client) StreamSpeech(context.Context, string, ...llms.SpeechOption) (<-chan llms.AudioChunk, error) {
	return nil, fmt.Errorf("fal: stream speech: %w", llms.ErrSpeechStreamNotSupported)
}
