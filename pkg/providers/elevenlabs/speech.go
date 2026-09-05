package elevenlabs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

var supportedFormats = []string{
	"mp3_22050_32", "mp3_24000_48", "mp3_44100_32", "mp3_44100_64", "mp3_44100_96", "mp3_44100_128", "mp3_44100_192",
	"opus_48000_32", "opus_48000_64", "opus_48000_96", "opus_48000_128", "opus_48000_192",
	"pcm_8000", "pcm_16000", "pcm_22050", "pcm_24000", "pcm_32000", "pcm_44100", "pcm_48000", "ulaw_8000", "alaw_8000", "wav_8000", "wav_16000", "wav_22050", "wav_24000", "wav_32000", "wav_44100", "wav_48000",
}

func outputFormat(f llms.AudioFormat) (string, llms.AudioFormat, error) {
	if f.Container == "" {
		f.Container = "mp3"
	}
	if f.SampleRate == 0 {
		f.SampleRate = 44100
	}
	name := f.Container + "_" + strconv.Itoa(f.SampleRate)
	if f.Container == "mp3" || f.Container == "opus" {
		if f.BitRate == 0 {
			f.BitRate = 128000
		}
		if f.BitRate%1000 != 0 {
			return "", f, invalid("unsupported output format; supported: " + strings.Join(supportedFormats, ", "))
		}
		name += "_" + strconv.Itoa(f.BitRate/1000)
	} else if f.BitRate != 0 {
		return "", f, invalid("unsupported output format; supported: " + strings.Join(supportedFormats, ", "))
	}
	if f.Encoding != "" {
		return "", f, invalid("Encoding is unsupported; select Container, SampleRate and BitRate")
	}
	for _, supported := range supportedFormats {
		if name == supported {
			return name, f, nil
		}
	}
	return "", f, invalid("unsupported output format; supported: " + strings.Join(supportedFormats, ", "))
}
func validateSettings(s VoiceSettings) error {
	for _, v := range []*float64{s.Stability, s.SimilarityBoost, s.Style} {
		if v != nil && (math.IsNaN(*v) || *v < 0 || *v > 1) {
			return invalid("voice settings must be in [0,1]")
		}
	}
	if s.Speed != nil && (math.IsNaN(*s.Speed) || math.IsInf(*s.Speed, 0) || *s.Speed <= 0) {
		return invalid("speed must be finite and positive")
	}
	return nil
}
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case float32:
		return number(float64(n))
	case json.Number:
		f, e := n.Float64()
		return f, e == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	default:
		return 0, false
	}
}
func rangeExtra(body map[string]any, key string, minValue, maxValue float64) (float64, error) {
	v, exists := body[key]
	if !exists {
		return 0, nil
	}
	n, ok := number(v)
	if !ok || n < minValue || n > maxValue {
		return 0, invalid(fmt.Sprintf("%s must be in [%g,%g]", key, minValue, maxValue))
	}
	return n, nil
}
func validateBools(body map[string]any, keys ...string) error {
	for _, key := range keys {
		if v, ok := body[key]; ok {
			if _, valid := v.(bool); !valid {
				return invalid(key + " must be boolean")
			}
		}
	}
	return nil
}

type speechRequest struct {
	route, model, formatName string
	format                   llms.AudioFormat
	body                     map[string]any
	usage                    llms.MediaUsage
	timestamps               bool
}

func (c *Client) speechRequest(text string, o *llms.SpeechOptions) (*speechRequest, error) {
	if strings.TrimSpace(text) == "" {
		return nil, WrapError("speech", llms.ErrEmptyText)
	}
	name, format, err := outputFormat(o.Format)
	if err != nil {
		return nil, err
	}
	model := o.Model
	if model == "" {
		model = c.options.Model
	}
	r := &speechRequest{model: model, formatName: name, format: format, body: map[string]any{}, timestamps: o.Timestamps}
	mergeExtra(r.body, o.Extra)
	// Typed fields determine routing, validation and accounting. Extras cannot replace them.
	r.body["model_id"] = model
	delete(r.body, "text")
	delete(r.body, "prompt")
	delete(r.body, "voice_settings")
	delete(r.body, "language_code")
	switch {
	case strings.HasPrefix(model, "eleven_text_to_sound"):
		r.route = "sound-generation"
		r.body["text"] = text
		duration, e := rangeExtra(r.body, "duration_seconds", 0.5, 30)
		if e != nil {
			return nil, e
		}
		if _, e = rangeExtra(r.body, "prompt_influence", 0, 1); e != nil {
			return nil, e
		}
		if e = validateBools(r.body, "loop"); e != nil {
			return nil, e
		}
		if duration > 0 {
			r.usage = llms.MediaUsage{Unit: llms.MediaUnitMinute, Quantity: duration / 60}
		}
	case strings.HasPrefix(model, "music_v"):
		r.route = "music"
		r.body["prompt"] = text
		if _, set := r.body["music_length_ms"]; !set {
			r.body["music_length_ms"] = 10000
		}
		duration, e := rangeExtra(r.body, "music_length_ms", 3000, 600000)
		if e != nil {
			return nil, e
		}
		if math.Trunc(duration) != duration {
			return nil, invalid("music_length_ms must be an integer")
		}
		if e = validateBools(r.body, "force_instrumental"); e != nil {
			return nil, e
		}
		r.usage = llms.MediaUsage{Unit: llms.MediaUnitMinute, Quantity: duration / 60000}
	default:
		voice := o.Voice
		if voice == "" {
			voice = c.options.Voice
		}
		if err = validateID(voice); err != nil {
			return nil, err
		}
		limit := map[string]int{"eleven_v3": 5000, "eleven_multilingual_v2": 10000, "eleven_flash_v2_5": 40000}[model]
		if limit > 0 && len([]rune(text)) > limit {
			return nil, invalid("text exceeds model character limit")
		}
		r.route = "text-to-speech/" + voice
		r.body["text"] = text
		settings := cloneSettings(c.options.VoiceSettings)
		if o.Speed != nil {
			settings.Speed = copyValue(o.Speed)
		}
		if err = validateSettings(settings); err != nil {
			return nil, err
		}
		r.body["voice_settings"] = settings
		if o.Language != "" {
			r.body["language_code"] = o.Language
		}
		r.usage = llms.MediaUsage{Unit: llms.MediaUnitKChar, Quantity: float64(len([]rune(text))) / 1000}
	}
	if r.timestamps && !strings.HasPrefix(r.route, "text-to-speech/") {
		return nil, invalid("timestamps require a text-to-speech model")
	}
	return r, nil
}

// Synthesize generates TTS, sound effects or music according to the resolved model.
// Timestamps select the TTS with-timestamps endpoint. Invalid options return
// ErrInvalidParameters; provider failures preserve their standard error sentinel.
func (c *Client) Synthesize(ctx context.Context, text string, opts ...llms.SpeechOption) (*llms.SpeechResponse, error) {
	r, err := c.speechRequest(text, llms.ApplySpeechOptions(opts...))
	if err != nil {
		return nil, err
	}
	return c.synthesize(ctx, r)
}
func (c *Client) synthesize(ctx context.Context, r *speechRequest) (result *llms.SpeechResponse, err error) {
	ctx, finish := c.startOperation(ctx, "synthesize", r.model)
	defer func() { finish(err) }()
	route := r.route
	if r.timestamps {
		route += "/with-timestamps"
	}
	raw, err := c.transport.DoBinary(ctx, http.MethodPost, c.endpoint(route, url.Values{"output_format": {r.formatName}}), r.body, c.headers)
	if err != nil {
		return nil, WrapError("synthesize", err)
	}
	out := &llms.SpeechResponse{Audio: llms.MediaAsset{Data: raw.Data, MIMEType: raw.ContentType}, Format: r.format, Model: r.model, Usage: r.usage, Metadata: map[string]any{}}
	// character-cost is a plan-scaled credit counter, not a character count.
	if credits, e := strconv.ParseInt(raw.Header.Get("character-cost"), 10, 64); e == nil && credits >= 0 {
		out.Metadata["character_cost"] = credits
	}
	if !r.timestamps {
		return out, nil
	}
	var timed struct {
		Audio      string         `json:"audio_base64"`
		Alignment  *wireAlignment `json:"alignment"`
		Normalized *wireAlignment `json:"normalized_alignment"`
	}
	if err = json.Unmarshal(raw.Data, &timed); err != nil {
		return nil, WrapError("timestamps", err)
	}
	out.Audio.Data, err = base64.StdEncoding.DecodeString(timed.Audio)
	if err != nil {
		return nil, WrapError("timestamps audio", err)
	}
	out.Audio.MIMEType = audioMIME(r.format.Container)
	alignment := timed.Alignment
	if alignment == nil {
		alignment = timed.Normalized
	}
	out.Alignment, err = convertAlignment(alignment)
	if err != nil {
		return nil, err
	}
	if timed.Normalized != nil {
		normalized, e := convertAlignment(timed.Normalized)
		if e != nil {
			return nil, e
		}
		out.Metadata["normalized_alignment"] = normalized
	}
	return out, nil
}
func audioMIME(container string) string {
	switch container {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

type wireAlignment struct {
	Chars []string  `json:"characters"`
	Start []float64 `json:"character_start_times_seconds"`
	End   []float64 `json:"character_end_times_seconds"`
}

func convertAlignment(a *wireAlignment) (*llms.Alignment, error) {
	if a == nil {
		return nil, nil
	}
	if len(a.Chars) != len(a.Start) || len(a.Chars) != len(a.End) {
		return nil, invalid("mismatched alignment lengths")
	}
	out := &llms.Alignment{Chars: a.Chars, StartMS: make([]int, len(a.Chars)), EndMS: make([]int, len(a.Chars))}
	for i := range a.Chars {
		out.StartMS[i] = int(math.Round(a.Start[i] * 1000))
		out.EndMS[i] = int(math.Round(a.End[i] * 1000))
	}
	return out, nil
}

// StreamSpeech streams raw TTS audio. Drain the channel or cancel ctx. Failures
// after setup produce a terminal Err chunk. If the buffer is full, the error path
// evicts one buffered audio chunk to guarantee terminal error delivery without blocking.
// SFX/music and timestamps are unsupported.
func (c *Client) StreamSpeech(ctx context.Context, text string, opts ...llms.SpeechOption) (<-chan llms.AudioChunk, error) {
	r, err := c.speechRequest(text, llms.ApplySpeechOptions(opts...))
	if err != nil {
		return nil, err
	}
	if r.timestamps || !strings.HasPrefix(r.route, "text-to-speech/") {
		return nil, WrapError("stream speech", llms.ErrSpeechStreamNotSupported)
	}
	// DoStream deliberately removes the unary HTTP timeout; bound this stream explicitly.
	ctx, finish := c.startOperation(ctx, "stream speech", r.model)
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	stream, err := c.transport.DoStream(ctx, httpclient.Request{Method: http.MethodPost, URL: c.endpoint(r.route+"/stream", url.Values{"output_format": {r.formatName}}), Body: r.body, Headers: c.headers})
	if err != nil {
		cancel()
		finish(err)
		return nil, WrapError("stream speech", err)
	}
	chunks := make(chan llms.AudioChunk, 8)
	go func() {
		var streamErr error
		defer func() { finish(streamErr) }()
		defer close(chunks)
		defer cancel()
		defer func() { _ = stream.Close() }()
		stop := context.AfterFunc(ctx, func() { _ = stream.Close() })
		defer stop()
		terminal := func(err error) {
			streamErr = err
			chunk := llms.AudioChunk{Err: WrapError("stream speech", err)}
			select {
			case chunks <- chunk:
			default:
				select {
				case <-chunks:
				default:
				}
				chunks <- chunk
			}
		}
		buffer := make([]byte, 32*1024)
		for {
			n, readErr := stream.Read(buffer)
			if ctx.Err() != nil {
				terminal(ctx.Err())
				return
			}
			if n > 0 {
				data := append([]byte(nil), buffer[:n]...)
				select {
				case chunks <- llms.AudioChunk{Data: data}:
				case <-ctx.Done():
					terminal(ctx.Err())
					return
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					terminal(readErr)
				}
				return
			}
		}
	}()
	return chunks, nil
}

// DialogueLine is one utterance with an explicit voice ID.
type DialogueLine struct {
	Text    string `json:"text"`
	VoiceID string `json:"voice_id"`
}

// SynthesizeDialogue generates audio from ordered utterances. Empty lines or
// invalid voice IDs return ErrInvalidParameters. Timestamps are unsupported.
// The default and only supported model is eleven_v3, independently of WithModel.
// Voice settings, language and additional speech extras are retained.
func (c *Client) SynthesizeDialogue(ctx context.Context, lines []DialogueLine, opts ...llms.SpeechOption) (*llms.SpeechResponse, error) {
	if len(lines) == 0 {
		return nil, invalid("dialog requires lines")
	}
	var text strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line.Text) == "" {
			return nil, invalid("dialog text is empty")
		}
		if err := validateID(line.VoiceID); err != nil {
			return nil, err
		}
		text.WriteString(line.Text)
	}
	o := llms.ApplySpeechOptions(opts...)
	if o.Model == "" {
		o.Model = "eleven_v3"
	}
	if o.Model != "eleven_v3" {
		return nil, invalid("dialog requires eleven_v3")
	}
	r, err := c.speechRequest(text.String(), o)
	if err != nil {
		return nil, err
	}
	if r.timestamps || !strings.HasPrefix(r.route, "text-to-speech/") {
		return nil, invalid("dialog requires TTS without timestamps")
	}
	r.route = "text-to-dialogue" //nolint:misspell // ElevenLabs wire route.
	delete(r.body, "text")
	r.body["inputs"] = lines
	return c.synthesize(ctx, r)
}

// Voice describes a voice returned by ListVoices.
type Voice struct {
	VoiceID  string            `json:"voice_id"`
	Name     string            `json:"name"`
	Category string            `json:"category"`
	Labels   map[string]string `json:"labels"`
}

// ListVoices returns accessible voices, or a context/provider error.
func (c *Client) ListVoices(ctx context.Context) ([]Voice, error) {
	var result struct {
		Voices []Voice `json:"voices"`
	}
	if err := c.request(ctx, http.MethodGet, "voices", nil, &result); err != nil {
		return nil, err
	}
	if result.Voices == nil {
		result.Voices = []Voice{}
	}
	return result.Voices, nil
}
