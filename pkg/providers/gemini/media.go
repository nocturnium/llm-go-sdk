package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/geminiapi"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

var (
	_ llms.ImageGenerator    = (*Client)(nil)
	_ llms.ImageEditor       = (*Client)(nil)
	_ llms.VideoGenerator    = (*Client)(nil)
	_ llms.SpeechSynthesizer = (*Client)(nil)
	_ llms.Transcriber       = (*Client)(nil)
	_ llms.CapableProvider   = (*Client)(nil)
)

// Published USD rates as verified 2026-09-05. Missing size/resolution entries
// deliberately remain unpriced; root MediaPricing contains only 1K/720p bases.
var imagePrices = map[string]map[string]float64{
	"gemini-2.5-flash-image":      {"1K": 0.039},
	"gemini-3.1-flash-image":      {"512": 0.045, "1K": 0.067, "2K": 0.101, "4K": 0.151},
	"gemini-3.1-flash-lite-image": {"1K": 0.0336},
	"gemini-3-pro-image":          {"1K": 0.134, "2K": 0.134, "4K": 0.24},
}
var videoPrices = map[string]map[string]float64{
	"veo-3.1-generate-preview":      {"720p": 0.40, "1080p": 0.40, "4k": 0.60},
	"veo-3.1-fast-generate-preview": {"720p": 0.10, "1080p": 0.12, "4k": 0.30},
	"veo-3.1-lite-generate-preview": {"720p": 0.05, "1080p": 0.08},
}
var speechPrices = map[string][2]float64{
	"gemini-3.1-flash-tts-preview": {1, 20},
	"gemini-2.5-flash-preview-tts": {0.5, 10},
	"gemini-2.5-pro-preview-tts":   {1, 20},
}

// Transcription prices are USD per million audio-input/text-output tokens.
var transcriptionPrices = map[string][2]float64{"gemini-3.5-transcribe": {2, 12}}

func invalidMedia(message string) error {
	return fmt.Errorf("gemini: %s: %w", message, llms.ErrInvalidParameters)
}
func mediaModel(model, fallback string) string {
	if model == "" {
		model = fallback
	}
	return strings.TrimPrefix(model, "models/")
}
func startMedia(ctx context.Context, operation, model string) (context.Context, func(error)) {
	ctx, span := otel.Tracer("llms").Start(ctx, "gemini."+operation)
	span.SetAttributes(attribute.String("llm.provider", "gemini"), attribute.String("llm.model", model), attribute.String("llm.operation", operation))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}
func pricedUsage(unit llms.MediaUnit, quantity float64, table map[string]map[string]float64, model, variant string) llms.MediaUsage {
	// Unit is withheld when the model/variant has no verified rate: MediaCost
	// falls back to the per-model registry rate whenever the unit matches, and
	// that would bill an unpriced variant (e.g. 4k) at the base (720p) rate.
	// Leaving the unit empty keeps such usage recorded as Unpriced.
	u := llms.MediaUsage{Quantity: quantity}
	if rate, ok := table[model][variant]; ok {
		cost := rate * quantity
		u.Unit = unit
		u.Cost = &cost
	}
	return u
}
func inlineImage(input llms.MediaInput) (*geminiapi.InlineData, error) {
	if err := input.Validate(); err != nil {
		return nil, geminiapi.WrapError("image input", err)
	}
	if len(input.Data) == 0 || !strings.HasPrefix(input.MIMEType, "image/") {
		return nil, invalidMedia("images require inline Data and image MIMEType")
	}
	return &geminiapi.InlineData{MimeType: input.MIMEType, Data: base64.StdEncoding.EncodeToString(input.Data)}, nil
}

// GenerateImage returns native inline images. Size defaults to 1K and AspectRatio
// to 1:1. Invalid sizes/counts return ErrInvalidParameters; API errors retain
// Gemini context. Image/video access can be gated by paid quota.
func (c *Client) GenerateImage(ctx context.Context, prompt string, opts ...llms.ImageOption) (*llms.ImageResponse, error) {
	return c.generateImage(ctx, prompt, nil, opts...)
}

// EditImage uses inline Data references (image MIMEType required). Empty images,
// URL and FileID inputs return ErrInvalidParameters. Other behavior matches GenerateImage.
func (c *Client) EditImage(ctx context.Context, prompt string, images []llms.MediaInput, opts ...llms.ImageOption) (*llms.ImageResponse, error) {
	if len(images) == 0 {
		return nil, invalidMedia("editing requires images")
	}
	return c.generateImage(ctx, prompt, images, opts...)
}
func (c *Client) generateImage(ctx context.Context, prompt string, images []llms.MediaInput, opts ...llms.ImageOption) (out *llms.ImageResponse, err error) {
	o := llms.ApplyImageOptions(opts...)
	model := mediaModel(o.Model, c.options.ImageModel)
	ctx, finish := startMedia(ctx, "generate image", model)
	defer func() { finish(err) }()
	if err := ctx.Err(); err != nil {
		return nil, geminiapi.WrapError("image", err)
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, geminiapi.WrapError("image", llms.ErrEmptyPrompt)
	}
	if o.N < 0 || o.N > 1 {
		return nil, invalidMedia("native images support one candidate per request")
	}
	if o.Size == "" {
		o.Size = "1K"
	}
	o.Size = strings.ToUpper(o.Size)
	switch o.Size {
	case "512", "1K", "2K", "4K":
	default:
		return nil, invalidMedia("image size must be 512, 1K, 2K or 4K")
	}
	if o.AspectRatio == "" {
		o.AspectRatio = "1:1"
	}
	// Aspect ratios vary by model; validate the ratio shape and let Gemini select
	// supported ratios rather than imposing an incomplete model-wide allowlist.
	if !validAspectRatio(o.AspectRatio) {
		return nil, invalidMedia("aspect ratio must be positive integers separated by ':'")
	}
	if len(o.Extra) != 0 {
		return nil, invalidMedia("image extras have no verified mapping")
	}
	parts := []geminiapi.Part{{Text: prompt}}
	for _, input := range images {
		data, e := inlineImage(input)
		if e != nil {
			return nil, e
		}
		parts = append(parts, geminiapi.Part{InlineData: data})
	}
	req := &geminiapi.GenerateContentRequest{Contents: []geminiapi.Content{{Parts: parts}}, GenerationConfig: &geminiapi.GenerationConfig{ResponseModalities: []string{"IMAGE"}, ImageConfig: &geminiapi.ImageConfig{AspectRatio: o.AspectRatio, ImageSize: o.Size}}}
	resp, err := c.client.GenerateContent(ctx, model, req)
	if err != nil {
		return nil, geminiapi.WrapError("image", err)
	}
	assets, err := inlineAssets(resp, "image/")
	if err != nil {
		return nil, err
	}
	return &llms.ImageResponse{Images: assets, Model: model, Usage: pricedUsage(llms.MediaUnitImage, float64(len(assets)), imagePrices, model, o.Size)}, nil
}
func validAspectRatio(ratio string) bool {
	parts := strings.Split(ratio, ":")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return false
		}
	}
	return true
}
func inlineAssets(resp *geminiapi.GenerateContentResponse, prefix string) ([]llms.MediaAsset, error) {
	if len(resp.Candidates) == 0 {
		if feedback := resp.PromptFeedback; feedback != nil && feedback.BlockReason != "" {
			return nil, &llms.ModerationError{Provider: "gemini", Stage: llms.ModerationInput, Reasons: []string{feedback.BlockReason}}
		}
		return nil, geminiapi.WrapError("missing media candidate", llms.ErrIncompleteResponse)
	}
	candidate := resp.Candidates[0]
	if geminiapi.GetFinishReason(candidate.FinishReason) == "content_filter" {
		return nil, &llms.ModerationError{Provider: "gemini", Stage: llms.ModerationOutput, Reasons: []string{candidate.FinishReason}}
	}
	assets := make([]llms.MediaAsset, 0)
	if candidate.Content != nil {
		for _, part := range candidate.Content.Parts {
			if part.InlineData == nil || !strings.HasPrefix(part.InlineData.MimeType, prefix) {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
			if err != nil {
				return nil, geminiapi.WrapError("decode media", err)
			}
			if len(data) == 0 {
				continue
			}
			assets = append(assets, llms.MediaAsset{Data: data, MIMEType: part.InlineData.MimeType})
		}
	}
	if len(assets) == 0 {
		return nil, geminiapi.WrapError("missing inline media", llms.ErrIncompleteResponse)
	}
	return assets, nil
}

func videoRequest(prompt string, o *llms.VideoOptions) (*geminiapi.PredictLongRunningRequest, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, geminiapi.WrapError("video", llms.ErrEmptyPrompt)
	}
	if o.DurationSeconds == 0 {
		o.DurationSeconds = 8
	}
	if o.Resolution == "" {
		o.Resolution = "720p"
	}
	o.Resolution = strings.ToLower(o.Resolution)
	if o.AspectRatio == "" {
		o.AspectRatio = "16:9"
	}
	if o.DurationSeconds != 4 && o.DurationSeconds != 6 && o.DurationSeconds != 8 {
		return nil, invalidMedia("Veo duration must be 4, 6 or 8 seconds")
	}
	if o.Resolution != "720p" && o.Resolution != "1080p" && o.Resolution != "4k" {
		return nil, invalidMedia("Veo resolution must be 720p, 1080p or 4k")
	}
	if o.AspectRatio != "16:9" && o.AspectRatio != "9:16" {
		return nil, invalidMedia("Veo aspect ratio must be 16:9 or 9:16")
	}
	if len(o.ReferenceImages) > 3 {
		return nil, invalidMedia("Veo accepts at most three reference images")
	}
	if o.Audio != nil && !*o.Audio {
		return nil, invalidMedia("Veo audio disabling has no verified mapping")
	}
	instance := geminiapi.VideoInstance{Prompt: prompt}
	for i, input := range []*llms.MediaInput{o.FirstFrame, o.LastFrame} {
		if input == nil {
			continue
		}
		data, err := inlineImage(*input)
		if err != nil {
			return nil, err
		}
		ref := &geminiapi.VideoImage{BytesBase64Encoded: data.Data, MIMEType: data.MimeType}
		if i == 0 {
			instance.Image = ref
		} else {
			instance.LastFrame = ref
		}
	}
	for _, input := range o.ReferenceImages {
		data, err := inlineImage(input)
		if err != nil {
			return nil, err
		}
		instance.ReferenceImages = append(instance.ReferenceImages, geminiapi.VideoReferenceImage{Image: geminiapi.VideoImage{BytesBase64Encoded: data.Data, MIMEType: data.MimeType}})
	}
	params := geminiapi.VideoParameters{AspectRatio: o.AspectRatio, Resolution: o.Resolution, DurationSeconds: o.DurationSeconds, NumberOfVideos: 1, Seed: o.Seed, NegativePrompt: o.NegativePrompt}
	for key, value := range o.Extra {
		if key != "personGeneration" {
			return nil, invalidMedia("only personGeneration is supported in video extras")
		}
		person, ok := value.(string)
		if !ok {
			return nil, invalidMedia("personGeneration must be a string")
		}
		params.PersonGeneration = person
	}
	return &geminiapi.PredictLongRunningRequest{Instances: []geminiapi.VideoInstance{instance}, Parameters: params}, nil
}
func operationStatus(op *geminiapi.Operation) *llms.JobStatus {
	if op.Error != nil {
		return &llms.JobStatus{State: llms.JobFailed, Err: fmt.Errorf("gemini: operation %d %s: %s: %w", op.Error.Code, op.Error.Status, op.Error.Message, llms.ErrJobFailed)}
	}
	if !op.Done {
		return &llms.JobStatus{State: llms.JobRunning}
	}
	if op.Response != nil {
		result := op.Response.GenerateVideoResponse
		if len(result.GeneratedSamples) > 0 {
			return &llms.JobStatus{State: llms.JobSucceeded}
		}
		if result.RAIMediaFilteredCount > 0 {
			return &llms.JobStatus{State: llms.JobModerated, Err: &llms.ModerationError{Provider: "gemini", Stage: llms.ModerationOutput, Reasons: result.RAIMediaFilteredReasons}}
		}
	}
	return &llms.JobStatus{State: llms.JobFailed, Err: geminiapi.WrapError("operation missing video result", llms.ErrIncompleteResponse)}
}

// GenerateVideo returns a PollingVideoJob for Veo. Defaults are 8 seconds, 720p,
// 16:9 with audio. Wait downloads MP4 with the Gemini key and a conservative 47h
// expiry. Cancel is unsupported; use a bounded context or WithPollPolicy.
// Invalid options return ErrInvalidParameters; terminal failures match ErrJobFailed
// or return a ModerationError for output filtering.
func (c *Client) GenerateVideo(ctx context.Context, prompt string, opts ...llms.VideoOption) (job llms.VideoJob, err error) {
	o := llms.ApplyVideoOptions(opts...)
	model := mediaModel(o.Model, c.options.VideoModel)
	ctx, finish := startMedia(ctx, "generate video", model)
	defer func() { finish(err) }()
	if err := ctx.Err(); err != nil {
		return nil, geminiapi.WrapError("video", err)
	}
	req, err := videoRequest(prompt, o)
	if err != nil {
		return nil, err
	}
	op, err := c.client.PredictLongRunning(ctx, model, req)
	if err != nil {
		return nil, err
	}
	name := op.Name
	// Serialize polls and retain the first terminal response for concurrent Wait calls.
	var mu sync.Mutex
	var terminal *geminiapi.Operation
	handle := &llms.PollingVideoJob{JobID: name, Policy: httpclient.PollPolicy(c.options.PollPolicy)}
	handle.PollFn = func(ctx context.Context) (*llms.JobStatus, error) {
		mu.Lock()
		defer mu.Unlock()
		if terminal != nil {
			return operationStatus(terminal), nil
		}
		current, e := c.client.GetOperation(ctx, name)
		if e != nil {
			return nil, e
		}
		status := operationStatus(current)
		if status.State.Terminal() {
			terminal = current
		}
		return status, nil
	}
	handle.ResultFn = func(ctx context.Context) (*llms.VideoResponse, error) {
		mu.Lock()
		current := terminal
		mu.Unlock()
		if current == nil {
			return nil, geminiapi.WrapError("video result unavailable", llms.ErrJobFailed)
		}
		status := operationStatus(current)
		if status.Err != nil {
			return nil, status.Err
		}
		if status.State != llms.JobSucceeded {
			return nil, geminiapi.WrapError("video result unavailable", llms.ErrJobFailed)
		}
		out := &llms.VideoResponse{Model: model, Videos: make([]llms.MediaAsset, 0)}
		for _, sample := range current.Response.GenerateVideoResponse.GeneratedSamples {
			data, e := c.client.DownloadFile(ctx, sample.Video.URI)
			if e != nil {
				return nil, e
			}
			out.Videos = append(out.Videos, llms.MediaAsset{Data: data, URL: sample.Video.URI, MIMEType: "video/mp4", ExpiresAt: time.Now().Add(47 * time.Hour)})
		}
		out.Usage = pricedUsage(llms.MediaUnitSecond, float64(req.Parameters.DurationSeconds)*float64(len(out.Videos)), videoPrices, model, req.Parameters.Resolution)
		return out, nil
	}
	return handle, nil
}

// Synthesize returns raw PCM s16le, normally 24 kHz mono. Only Container "pcm"
// (or unset) is accepted; other containers return ErrInvalidParameters. Without
// Instructions the prompt is "Say: <text>"; otherwise it is "<Instructions>: <text>".
// Speech errors retain Gemini context. Usage cost includes input and output tokens.
func (c *Client) Synthesize(ctx context.Context, text string, opts ...llms.SpeechOption) (out *llms.SpeechResponse, err error) {
	o := llms.ApplySpeechOptions(opts...)
	model := mediaModel(o.Model, c.options.SpeechModel)
	ctx, finish := startMedia(ctx, "synthesize", model)
	defer func() { finish(err) }()
	if err := ctx.Err(); err != nil {
		return nil, geminiapi.WrapError("speech", err)
	}
	if strings.TrimSpace(text) == "" {
		return nil, geminiapi.WrapError("speech", llms.ErrEmptyText)
	}
	if o.Format.Container != "" && o.Format.Container != "pcm" {
		return nil, invalidMedia("Gemini TTS supports only PCM")
	}
	if (o.Format.Encoding != "" && o.Format.Encoding != "pcm_s16le") || (o.Format.SampleRate != 0 && o.Format.SampleRate != 24000) || o.Format.BitRate != 0 {
		return nil, invalidMedia("Gemini TTS emits pcm_s16le at 24000 Hz without a bitrate selector")
	}
	voice := o.Voice
	if voice == "" {
		voice = c.options.SpeechVoice
	}
	config, err := speechConfig(voice, o.Extra)
	if err != nil {
		return nil, err
	}
	instructions := o.Instructions
	if instructions == "" {
		instructions = "Say"
	}
	req := &geminiapi.GenerateContentRequest{Contents: []geminiapi.Content{{Parts: []geminiapi.Part{{Text: instructions + ": " + text}}}}, GenerationConfig: &geminiapi.GenerationConfig{ResponseModalities: []string{"AUDIO"}, SpeechConfig: config}}
	resp, err := c.client.GenerateContent(ctx, model, req)
	if err != nil {
		return nil, geminiapi.WrapError("speech", err)
	}
	assets, err := inlineAssets(resp, "audio/")
	if err != nil {
		return nil, err
	}
	format, err := pcmFormat(assets[0].MIMEType)
	if err != nil {
		return nil, err
	}
	out = &llms.SpeechResponse{Audio: assets[0], Format: format, Model: model}
	if usage := resp.UsageMetadata; usage != nil {
		out.Usage = llms.MediaUsage{Unit: llms.MediaUnitMTokenOut, Quantity: float64(usage.CandidatesTokenCount) / 1e6}
		if rates, ok := speechPrices[model]; ok {
			cost := (float64(usage.PromptTokenCount)*rates[0] + float64(usage.CandidatesTokenCount)*rates[1]) / 1e6
			out.Usage.Cost = &cost
		}
	}
	return out, nil
}
func speechConfig(voice string, extra map[string]any) (*geminiapi.SpeechConfig, error) {
	config := &geminiapi.SpeechConfig{VoiceConfig: &geminiapi.VoiceConfig{PrebuiltVoiceConfig: geminiapi.PrebuiltVoiceConfig{VoiceName: voice}}}
	for key, value := range extra {
		if key != "multiSpeakerVoiceConfig" {
			return nil, invalidMedia("only multiSpeakerVoiceConfig is supported in speech extras")
		}
		var multi geminiapi.MultiSpeakerVoiceConfig
		if err := decodeMediaExtra(value, &multi); err != nil {
			return nil, err
		}
		if len(multi.SpeakerVoiceConfigs) == 0 {
			return nil, invalidMedia("speaker voices cannot be empty")
		}
		for _, speaker := range multi.SpeakerVoiceConfigs {
			if speaker.Speaker == "" || speaker.VoiceConfig.PrebuiltVoiceConfig.VoiceName == "" {
				return nil, invalidMedia("speaker and voiceName are required")
			}
		}
		config.VoiceConfig = nil
		config.MultiSpeakerVoiceConfig = &multi
	}
	return config, nil
}
func decodeMediaExtra(value, out any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return invalidMedia("invalid media extra JSON")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return invalidMedia("invalid media extra shape")
	}
	return nil
}
func pcmFormat(value string) (llms.AudioFormat, error) {
	format := llms.AudioFormat{Container: "pcm", Encoding: "pcm_s16le", SampleRate: 24000}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil {
		return format, geminiapi.WrapError("speech MIME type", err)
	}
	if mediaType != "audio/l16" {
		return format, geminiapi.WrapError("unexpected speech encoding", llms.ErrIncompleteResponse)
	}
	if rate, ok := params["rate"]; ok {
		n, err := strconv.Atoi(rate)
		if err != nil || n <= 0 {
			return format, geminiapi.WrapError("invalid PCM rate", llms.ErrIncompleteResponse)
		}
		format.SampleRate = n
	}
	if channels, ok := params["channels"]; ok && channels != "1" {
		return format, geminiapi.WrapError("expected mono PCM", llms.ErrIncompleteResponse)
	}
	return format, nil
}

// StreamSpeech returns ErrSpeechStreamNotSupported; native streaming TTS is not implemented.
func (c *Client) StreamSpeech(_ context.Context, _ string, _ ...llms.SpeechOption) (<-chan llms.AudioChunk, error) {
	return nil, geminiapi.WrapError("stream speech", llms.ErrSpeechStreamNotSupported)
}

func transcriptionConfig(o *llms.TranscribeOptions) (geminiapi.TranscriptionConfig, error) {
	config := geminiapi.TranscriptionConfig{Mode: "smart", CustomVocabulary: append([]string(nil), o.Keyterms...)}
	if o.Language != "" {
		config.LanguageCodes = []string{o.Language}
	}
	mode := geminiapi.VerbatimMode{Type: "verbatim"}
	if o.Diarize {
		mode.DiarizationMode = "speaker"
	}
	if o.WordTimestamps {
		mode.TimestampGranularities = []string{"word"}
	}
	if o.Diarize || o.WordTimestamps {
		config.Mode = mode
	}
	for key, value := range o.Extra {
		if key != "mode" {
			return config, invalidMedia("only mode is supported in transcription extras")
		}
		if text, ok := value.(string); ok {
			switch text {
			case "smart":
				if o.Diarize || o.WordTimestamps {
					return config, invalidMedia("smart mode cannot use diarization or timestamps")
				}
				config.Mode = "smart"
			case "verbatim":
				config.Mode = mode
			default:
				return config, invalidMedia("transcription mode must be smart or verbatim")
			}
		} else {
			var explicit geminiapi.VerbatimMode
			if err := decodeMediaExtra(value, &explicit); err != nil {
				return config, err
			}
			if explicit.Type != "verbatim" {
				return config, invalidMedia("object mode must be verbatim")
			}
			if explicit.DiarizationMode == "" {
				explicit.DiarizationMode = mode.DiarizationMode
			}
			if len(explicit.TimestampGranularities) == 0 {
				explicit.TimestampGranularities = mode.TimestampGranularities
			}
			config.Mode = explicit
		}
	}
	if verbatim, ok := config.Mode.(geminiapi.VerbatimMode); ok {
		if verbatim.DiarizationMode != "" && verbatim.DiarizationMode != "speaker" {
			return config, invalidMedia("diarization_mode must be speaker")
		}
		for _, granularity := range verbatim.TimestampGranularities {
			if granularity != "word" {
				return config, invalidMedia("timestamp granularity must be word")
			}
		}
		if len(config.CustomVocabulary) > 0 && (verbatim.DiarizationMode != "" || len(verbatim.TimestampGranularities) > 0) {
			return config, invalidMedia("custom vocabulary cannot use diarization or timestamps")
		}
	}
	return config, nil
}

// Transcribe sends inline audio or an audio URL to Interactions. Smart mode is
// default; Diarize/WordTimestamps select verbatim. Keyterms cannot combine with
// either, and explicit smart mode conflicts return ErrInvalidParameters before I/O.
// In-progress results poll under WithPollPolicy. Minute usage is derived from
// audio input tokens / 25 tokens per second / 60; cost uses audio-input and text-output tokens.
func (c *Client) Transcribe(ctx context.Context, audio llms.MediaInput, opts ...llms.TranscribeOption) (out *llms.Transcription, err error) {
	o := llms.ApplyTranscribeOptions(opts...)
	model := mediaModel(o.Model, c.options.TranscriptionModel)
	ctx, finish := startMedia(ctx, "transcribe", model)
	defer func() { finish(err) }()
	if err := ctx.Err(); err != nil {
		return nil, geminiapi.WrapError("transcribe", err)
	}
	if err := audio.Validate(); err != nil {
		return nil, geminiapi.WrapError("transcribe input", err)
	}
	if audio.FileID != "" || !strings.HasPrefix(audio.MIMEType, "audio/") {
		return nil, invalidMedia("transcription requires Data or URL with an audio MIMEType")
	}
	if audio.URL != "" {
		if err := c.client.ValidateMediaURL(audio.URL); err != nil {
			return nil, geminiapi.WrapError("audio URL", err)
		}
	}
	config, err := transcriptionConfig(o)
	if err != nil {
		return nil, err
	}
	input := geminiapi.InteractionInput{Type: "audio", MIMEType: audio.MIMEType, URI: audio.URL}
	if len(audio.Data) > 0 {
		input.Data = base64.StdEncoding.EncodeToString(audio.Data)
	}
	req := &geminiapi.InteractionRequest{Model: model, Input: []geminiapi.InteractionInput{input}, GenerationConfig: geminiapi.InteractionGenerationConfig{TranscriptionConfig: config}}
	response, err := c.client.CreateInteraction(ctx, req)
	if err != nil {
		return nil, err
	}
	id := response.ID
	err = httpclient.Poll(ctx, httpclient.PollPolicy(c.options.PollPolicy), func(ctx context.Context) (bool, error) {
		done, e := interactionComplete(response)
		if done || e != nil {
			return done, e
		}
		response, e = c.client.GetInteraction(ctx, id)
		if e != nil {
			return false, e
		}
		return interactionComplete(response)
	})
	if err != nil {
		return nil, geminiapi.WrapError("transcription polling", err)
	}
	return convertTranscription(response, model)
}
func convertTranscription(response *geminiapi.Interaction, model string) (*llms.Transcription, error) {
	out := &llms.Transcription{Model: model, Words: make([]llms.TranscriptWord, 0)}
	var text strings.Builder
	for _, step := range response.Steps {
		if step.Type != "model_output" {
			continue
		}
		for _, content := range step.Content {
			if content.Type != "text" {
				continue
			}
			text.WriteString(content.Text)
			for _, annotation := range content.Annotations {
				if annotation.Type != "word_info" {
					continue
				}
				start, err := parseOffset(annotation.StartOffset)
				if err != nil {
					return nil, err
				}
				end, err := parseOffset(annotation.EndOffset)
				if err != nil {
					return nil, err
				}
				if end < start {
					return nil, geminiapi.WrapError("reversed word offsets", llms.ErrIncompleteResponse)
				}
				out.Words = append(out.Words, llms.TranscriptWord{Word: annotation.Text, Speaker: annotation.Speaker, Start: start, End: end})
				out.DurationSeconds = math.Max(out.DurationSeconds, end)
			}
		}
	}
	out.Text = text.String()
	if usage := response.Usage; usage != nil {
		audioTokens := 0
		for _, modality := range usage.InputTokensByModality {
			if modality.Modality == "audio" {
				audioTokens += modality.Tokens
			}
		}
		out.Usage = llms.MediaUsage{Unit: llms.MediaUnitMinute, Quantity: float64(audioTokens) / 25 / 60}
		if rates, ok := transcriptionPrices[model]; ok {
			cost := (float64(audioTokens)*rates[0] + float64(transcriptionOutputTokens(usage))*rates[1]) / 1e6
			out.Usage.Cost = &cost
		}
	}
	return out, nil
}
func parseOffset(offset string) (float64, error) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(offset, "s"), 64)
	if err != nil {
		return 0, geminiapi.WrapError("word offset", err)
	}
	if n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, geminiapi.WrapError("invalid word offset", llms.ErrIncompleteResponse)
	}
	return n, nil
}

func interactionComplete(response *geminiapi.Interaction) (bool, error) {
	if response.Error != nil {
		return false, fmt.Errorf("gemini: interaction error: %s: %w", response.Error.Message, llms.ErrJobFailed)
	}
	switch response.Status {
	case "completed":
		return true, nil
	case "failed", "cancelled": //nolint:misspell // Gemini wire status.
		return false, fmt.Errorf("gemini: interaction status %q: %w", response.Status, llms.ErrJobFailed)
	default:
		return false, nil
	}
}
func transcriptionOutputTokens(usage *geminiapi.InteractionUsage) int {
	if usage.ModelInvocationTokenCounts == nil {
		return usage.TotalOutputTokens
	}
	tokens := 0
	for _, invocation := range usage.ModelInvocationTokenCounts {
		for _, detail := range invocation.CandidatesTokensDetails {
			if detail.Modality == "text" {
				tokens += detail.Tokens
			}
		}
	}
	return tokens
}
