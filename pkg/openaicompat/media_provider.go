package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// GenerateImage generates images. Every BaseProvider implements ImageGenerator;
// a disabled Images flag returns ErrImageGenerationNotSupported.
func (p *BaseProvider) GenerateImage(ctx context.Context, prompt string, options ...llms.ImageOption) (*llms.ImageResponse, error) {
	if !p.config.Media.Images {
		return nil, WrapError(p.Provider(), "generate image", llms.ErrImageGenerationNotSupported)
	}
	req := BuildImageRequest(p.config.DefaultImageModel, prompt, llms.ApplyImageOptions(options...))
	// Resolve Extra last so validation and response metadata match the actual wire request.
	data, err := json.Marshal(req)
	if err == nil {
		err = json.Unmarshal(data, req)
	}
	if err == nil {
		err = validateImageRequest(req)
	}
	if err != nil {
		return nil, WrapError(p.Provider(), "generate image", err)
	}
	resp, err := p.client.CreateImage(ctx, req)
	if err != nil {
		return nil, WrapError(p.Provider(), "generate image", err)
	}
	out, err := ConvertImageResponse(resp, req.Model, req.OutputFormat)
	return out, WrapError(p.Provider(), "generate image", err)
}

func validateImageRequest(req *ImageGenerationRequest) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return llms.ErrEmptyPrompt
	}
	if req.N < 0 || req.N > 10 {
		return fmt.Errorf("image count must be 1-10 or unset: %w", llms.ErrInvalidParameters)
	}
	if req.Stream {
		return fmt.Errorf("use CreateImageStream for streaming images: %w", llms.ErrInvalidParameters)
	}
	return nil
}

// EditImage edits inline images. Every BaseProvider implements ImageEditor;
// a disabled ImageEdits flag returns ErrImageEditNotSupported. Supply mask and
// input_fidelity through WithImageExtra; mask must be a MediaInput.
func (p *BaseProvider) EditImage(ctx context.Context, prompt string, images []llms.MediaInput, options ...llms.ImageOption) (*llms.ImageResponse, error) {
	if !p.config.Media.ImageEdits {
		return nil, WrapError(p.Provider(), "edit image", llms.ErrImageEditNotSupported)
	}
	opts := llms.ApplyImageOptions(options...)
	generation := BuildImageRequest(p.config.DefaultImageModel, prompt, opts)
	if err := validateImageRequest(generation); err != nil {
		return nil, WrapError(p.Provider(), "edit image", err)
	}
	req := &ImageEditRequest{Model: generation.Model, Prompt: prompt, Images: images, N: opts.N, Size: opts.Size, Quality: opts.Quality, OutputFormat: opts.OutputFormat, ExtraBody: make(map[string]any, len(opts.Extra))}
	for k, v := range opts.Extra {
		if k == "mask" {
			mask, ok := v.(llms.MediaInput)
			if !ok {
				return nil, WrapError(p.Provider(), "edit image", fmt.Errorf("mask must be MediaInput: %w", llms.ErrInvalidParameters))
			}
			req.Mask = &mask
		} else {
			req.ExtraBody[k] = v
		}
	}
	fields, _, err := multipartMediaFields(req, req.ExtraBody)
	if err != nil {
		return nil, WrapError(p.Provider(), "edit image", err)
	}
	resp, err := p.client.EditImage(ctx, req)
	if err != nil {
		return nil, WrapError(p.Provider(), "edit image", err)
	}
	out, err := ConvertImageResponse(resp, fields["model"], fields["output_format"])
	return out, WrapError(p.Provider(), "edit image", err)
}

func validateSpeechRequest(req *SpeechRequest) error {
	if strings.TrimSpace(req.Input) == "" {
		return llms.ErrEmptyText
	}
	if utf8.RuneCountInString(req.Input) > 4096 {
		return fmt.Errorf("speech input exceeds 4096 characters: %w", llms.ErrInvalidParameters)
	}
	if req.Speed != nil && (math.IsNaN(*req.Speed) || *req.Speed < 0.25 || *req.Speed > 4) {
		return fmt.Errorf("speech speed must be 0.25-4: %w", llms.ErrInvalidParameters)
	}
	return nil
}

// Synthesize generates audio. Every BaseProvider implements SpeechSynthesizer;
// a disabled Speech flag returns ErrSpeechNotSupported. Binary responses report
// no billable usage because they do not include token accounting.
func (p *BaseProvider) Synthesize(ctx context.Context, text string, options ...llms.SpeechOption) (*llms.SpeechResponse, error) {
	if !p.config.Media.Speech {
		return nil, WrapError(p.Provider(), "synthesize", llms.ErrSpeechNotSupported)
	}
	req := BuildSpeechRequest(p.config.DefaultSpeechModel, text, llms.ApplySpeechOptions(options...))
	if err := validateSpeechRequest(req); err != nil {
		return nil, WrapError(p.Provider(), "synthesize", err)
	}
	data, contentType, err := p.client.CreateSpeech(ctx, req)
	if err != nil {
		return nil, WrapError(p.Provider(), "synthesize", err)
	}
	return ConvertSpeechResponse(data, contentType, req, nil), nil
}

// StreamSpeech returns audio chunks. A disabled SpeechStream flag or a legacy
// tts-1/tts-1-hd model returns ErrSpeechStreamNotSupported. Drain the channel or
// cancel ctx. D0 AudioChunk has no usage field; terminal usage is exposed by
// Client.CreateSpeechStream for callers needing streaming accounting.
func (p *BaseProvider) StreamSpeech(ctx context.Context, text string, options ...llms.SpeechOption) (<-chan llms.AudioChunk, error) {
	if !p.config.Media.SpeechStream {
		return nil, WrapError(p.Provider(), "stream speech", llms.ErrSpeechStreamNotSupported)
	}
	req := BuildSpeechRequest(p.config.DefaultSpeechModel, text, llms.ApplySpeechOptions(options...))
	if req.Model == "tts-1" || req.Model == "tts-1-hd" {
		return nil, WrapError(p.Provider(), "stream speech", llms.ErrSpeechStreamNotSupported)
	}
	if err := validateSpeechRequest(req); err != nil {
		return nil, WrapError(p.Provider(), "stream speech", err)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	events, err := p.client.CreateSpeechStream(streamCtx, req)
	if err != nil {
		cancel()
		return nil, WrapError(p.Provider(), "stream speech", err)
	}
	chunks := make(chan llms.AudioChunk, 8)
	go func() {
		defer close(chunks)
		defer cancel()
		for event := range events {
			if event.Type == "speech.audio.done" && event.Err == nil {
				return
			}
			chunk := convertSpeechEvent(event)
			chunk.Err = WrapError(p.Provider(), "stream speech", chunk.Err)
			select {
			case chunks <- chunk:
			case <-ctx.Done():
				return
			}
			if chunk.Err != nil {
				return
			}
		}
	}()
	return chunks, nil
}

// Transcribe recognizes inline audio. Every BaseProvider implements Transcriber;
// a disabled Transcription flag returns ErrTranscriptionNotSupported.
func (p *BaseProvider) Transcribe(ctx context.Context, audio llms.MediaInput, options ...llms.TranscribeOption) (*llms.Transcription, error) {
	if !p.config.Media.Transcription {
		return nil, WrapError(p.Provider(), "transcribe", llms.ErrTranscriptionNotSupported)
	}
	opts := llms.ApplyTranscribeOptions(options...)
	req := BuildTranscriptionRequest(p.config.DefaultTranscriptionModel, audio, opts)
	model := req.Model
	if override, ok := req.ExtraBody["model"].(string); ok {
		model = override
	}
	if opts.WordTimestamps {
		if err := validateTranscriptionFormat(model, transcriptionFormatVerboseJSON); err != nil {
			return nil, WrapError(p.Provider(), "transcribe", err)
		}
	}
	if opts.Diarize {
		if err := validateTranscriptionFormat(model, transcriptionFormatDiarizedJSON); err != nil {
			return nil, WrapError(p.Provider(), "transcribe", err)
		}
	}
	resp, err := p.client.CreateTranscription(ctx, req)
	if err != nil {
		return nil, WrapError(p.Provider(), "transcribe", err)
	}
	return ConvertTranscriptionResponse(resp, model), nil
}

// GenerateVideo submits a PollingVideoJob. Every BaseProvider implements
// VideoGenerator; a disabled Videos flag returns ErrVideoGenerationNotSupported.
// Poll and download errors are provider-wrapped. Cancellation is unsupported.
func (p *BaseProvider) GenerateVideo(ctx context.Context, prompt string, options ...llms.VideoOption) (llms.VideoJob, error) {
	if !p.config.Media.Videos {
		return nil, WrapError(p.Provider(), "generate video", llms.ErrVideoGenerationNotSupported)
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, WrapError(p.Provider(), "generate video", llms.ErrEmptyPrompt)
	}
	opts := llms.ApplyVideoOptions(options...)
	if opts.DurationSeconds != 0 && opts.DurationSeconds != 4 && opts.DurationSeconds != 8 && opts.DurationSeconds != 12 {
		return nil, WrapError(p.Provider(), "generate video", fmt.Errorf("video duration must be 4, 8, or 12: %w", llms.ErrInvalidParameters))
	}
	if opts.FirstFrame != nil {
		if err := opts.FirstFrame.Validate(); err != nil {
			return nil, WrapError(p.Provider(), "generate video", err)
		}
	}
	req := BuildVideoRequest(p.config.DefaultVideoModel, prompt, opts)
	obj, err := p.client.CreateVideo(ctx, req)
	if err != nil {
		return nil, WrapError(p.Provider(), "generate video", err)
	}
	if _, err := p.client.videoURL(obj.ID, false); err != nil {
		return nil, WrapError(p.Provider(), "generate video", err)
	}
	job := &llms.PollingVideoJob{JobID: obj.ID}
	job.PollFn = func(ctx context.Context) (*llms.JobStatus, error) {
		current, err := p.client.GetVideo(ctx, obj.ID)
		if err != nil {
			return nil, WrapError(p.Provider(), "poll video", err)
		}
		status := convertVideoStatus(current, p.Provider())
		status.Err = WrapError(p.Provider(), "video job", status.Err)
		return status, nil
	}
	job.ResultFn = func(ctx context.Context) (*llms.VideoResponse, error) {
		// Fetch final metadata rather than sharing mutable poll state across callers.
		current, err := p.client.GetVideo(ctx, obj.ID)
		if err != nil {
			return nil, WrapError(p.Provider(), "video result", err)
		}
		data, err := p.client.GetVideoContent(ctx, obj.ID)
		if err != nil {
			return nil, WrapError(p.Provider(), "video content", err)
		}
		usage := videoUsage(firstNonEmpty(current.Seconds, req.Seconds), firstNonEmpty(current.Size, req.Size))
		asset := llms.MediaAsset{Data: data, MIMEType: "video/mp4"}
		if current.ExpiresAt > 0 {
			asset.ExpiresAt = time.Unix(current.ExpiresAt, 0)
		}
		return &llms.VideoResponse{Model: effectiveModel(req.Model, current.Model), Videos: []llms.MediaAsset{asset}, Usage: usage}, nil
	}
	return job, nil
}

var (
	_ llms.ImageGenerator    = (*BaseProvider)(nil)
	_ llms.ImageEditor       = (*BaseProvider)(nil)
	_ llms.SpeechSynthesizer = (*BaseProvider)(nil)
	_ llms.Transcriber       = (*BaseProvider)(nil)
	_ llms.VideoGenerator    = (*BaseProvider)(nil)
)

// firstNonEmpty returns value when set, otherwise fallback.
func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
