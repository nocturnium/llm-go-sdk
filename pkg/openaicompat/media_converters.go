package openaicompat

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

const (
	transcriptionFormatVerboseJSON  = "verbose_json"
	transcriptionFormatDiarizedJSON = "diarized_json"
	videoSize720pLandscape          = "1280x720"
	videoSize720pPortrait           = "720x1280"
	videoSize1024pPortrait          = "1024x1792"
	videoSize1024pLandscape         = "1792x1024"
)

// BuildImageRequest converts image options using model as the default model.
// The OpenAI wire ignores Seed, AspectRatio, NegativePrompt, and SafetyTolerance.
func BuildImageRequest(model, prompt string, opts *llms.ImageOptions) *ImageGenerationRequest {
	return &ImageGenerationRequest{Model: effectiveModel(model, opts.Model), Prompt: prompt, N: opts.N, Size: opts.Size, Quality: opts.Quality, OutputFormat: opts.OutputFormat, ExtraBody: opts.Extra}
}

// ConvertImageResponse decodes images, returning an error for malformed base64.
func ConvertImageResponse(resp *ImageResponse, model, format string) (*llms.ImageResponse, error) {
	if format == "" {
		format = "png"
	}
	out := &llms.ImageResponse{Model: model, Images: make([]llms.MediaAsset, 0, len(resp.Data))}
	for _, item := range resp.Data {
		asset := llms.MediaAsset{URL: item.URL, RevisedPrompt: item.RevisedPrompt, MIMEType: "image/" + format}
		if item.B64JSON != "" {
			data, err := base64.StdEncoding.DecodeString(item.B64JSON)
			if err != nil {
				return nil, fmt.Errorf("decode image: %w", err)
			}
			asset.Data = data
		}
		out.Images = append(out.Images, asset)
	}
	if resp.Usage != nil {
		out.Usage = tokenMediaUsage(resp.Usage)
	}
	return out, nil
}

func tokenMediaUsage(usage *ImageUsage) llms.MediaUsage {
	return llms.MediaUsage{Unit: llms.MediaUnitMTokenOut, Quantity: float64(usage.OutputTokens) / 1e6}
}

// BuildSpeechRequest converts options with an mp3 container and alloy voice by default.
func BuildSpeechRequest(model, input string, opts *llms.SpeechOptions) *SpeechRequest {
	format := opts.Format.Container
	if format == "" {
		format = "mp3"
	}
	voice := opts.Voice
	if voice == "" {
		voice = "alloy"
	}
	return &SpeechRequest{Model: effectiveModel(model, opts.Model), Input: input, Voice: voice, Instructions: opts.Instructions, Speed: opts.Speed, ResponseFormat: format}
}

// ConvertSpeechResponse attaches format and usage to binary audio. Token usage wins
// when available; binary responses without usage remain unpriced.
func ConvertSpeechResponse(data []byte, contentType string, req *SpeechRequest, usage *ImageUsage) *llms.SpeechResponse {
	out := &llms.SpeechResponse{Audio: llms.MediaAsset{Data: data, MIMEType: contentType}, Model: req.Model, Format: llms.AudioFormat{Container: req.ResponseFormat}}
	if usage != nil {
		out.Usage = tokenMediaUsage(usage)
	}
	return out
}

func convertSpeechEvent(event SpeechStreamEvent) llms.AudioChunk {
	if event.Err != nil {
		return llms.AudioChunk{Err: event.Err}
	}
	data, err := base64.StdEncoding.DecodeString(event.Audio)
	return llms.AudioChunk{Data: data, Err: err}
}

// BuildTranscriptionRequest converts transcription options; diarization takes
// precedence over word timestamps when both are requested.
func BuildTranscriptionRequest(model string, audio llms.MediaInput, opts *llms.TranscribeOptions) *TranscriptionRequest {
	req := &TranscriptionRequest{Model: effectiveModel(model, opts.Model), File: audio, Language: opts.Language, Prompt: opts.Prompt, ResponseFormat: "json", ExtraBody: opts.Extra}
	if opts.Diarize {
		req.ResponseFormat = transcriptionFormatDiarizedJSON
	} else if opts.WordTimestamps {
		req.ResponseFormat = transcriptionFormatVerboseJSON
		req.TimestampGranularities = []string{"word"}
	}
	return req
}

// ConvertTranscriptionResponse preserves timing and speaker data and accounts
// from explicit token or duration usage. Duration is a fallback only when usage
// is absent; unknown usage types and unknown model token prices remain unpriced.
func ConvertTranscriptionResponse(resp *TranscriptionResponse, model string) *llms.Transcription {
	out := &llms.Transcription{Text: resp.Text, Language: resp.Language, DurationSeconds: resp.Duration, Model: model, Segments: make([]llms.TranscriptSegment, 0, len(resp.Segments)), Words: make([]llms.TranscriptWord, 0, len(resp.Words))}
	for _, s := range resp.Segments {
		out.Segments = append(out.Segments, llms.TranscriptSegment{Start: s.Start, End: s.End, Text: s.Text, Speaker: s.Speaker})
	}
	for _, w := range resp.Words {
		out.Words = append(out.Words, llms.TranscriptWord{Start: w.Start, End: w.End, Word: w.Word})
	}
	if u := resp.Usage; u != nil {
		switch u.Type {
		case "duration":
			out.DurationSeconds = u.Seconds
			out.Usage = llms.MediaUsage{Unit: llms.MediaUnitMinute, Quantity: u.Seconds / 60}
		case "tokens":
			out.Usage = llms.MediaUsage{Unit: llms.MediaUnitMTokenOut, Quantity: float64(u.OutputTokens) / 1e6}
			if rates, ok := transcriptionTokenRates[model]; ok {
				cost := (float64(u.InputTokenDetails.AudioTokens)*rates.audioIn + float64(u.InputTokenDetails.TextTokens)*rates.textIn + float64(u.OutputTokens)*rates.textOut) / 1e6
				out.Usage.Cost = &cost
			}
		}
	} else if resp.Duration > 0 {
		out.Usage = llms.MediaUsage{Unit: llms.MediaUnitMinute, Quantity: resp.Duration / 60}
	}
	return out
}

// BuildVideoRequest converts video options using the landscape 720p size by default.
// FirstFrame is sent as a URL, data URL, or provider file identifier.
// The OpenAI wire ignores Audio, LastFrame, ReferenceImages, Seed, and NegativePrompt.
func BuildVideoRequest(model, prompt string, opts *llms.VideoOptions) *VideoCreateRequest {
	req := &VideoCreateRequest{Model: effectiveModel(model, opts.Model), Prompt: prompt, Size: videoSize(opts.Resolution, opts.AspectRatio)}
	if opts.DurationSeconds != 0 {
		req.Seconds = strconv.Itoa(opts.DurationSeconds)
	}
	if frame := opts.FirstFrame; frame != nil {
		switch {
		case frame.URL != "":
			req.InputReference = frame.URL
		case frame.FileID != "":
			req.InputReference = frame.FileID
		default:
			req.InputReference = "data:" + frame.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(frame.Data)
		}
	}
	return req
}

func videoSize(resolution, aspect string) string {
	switch resolution {
	case videoSize720pPortrait, videoSize720pLandscape, videoSize1024pPortrait, videoSize1024pLandscape:
		return resolution
	}
	sizes := map[string]string{
		"720p:16:9": videoSize720pLandscape, "720p:9:16": videoSize720pPortrait,
		"1024p:16:9": videoSize1024pLandscape, "1024p:9:16": videoSize1024pPortrait,
	}
	if resolution == "" {
		resolution = "720p"
	}
	if aspect == "" {
		aspect = "16:9"
	}
	if size, ok := sizes[resolution+":"+aspect]; ok {
		return size
	}
	return videoSize720pLandscape
}

// ConvertVideoStatus maps wire states and progress (percent) to SDK states and
// a completion fraction. Moderation failures carry a ModerationError.
func ConvertVideoStatus(obj *VideoObject) *llms.JobStatus {
	return convertVideoStatus(obj, llms.ProviderOpenAI)
}

func convertVideoStatus(obj *VideoObject, provider llms.Provider) *llms.JobStatus {
	status := &llms.JobStatus{}
	switch obj.Status {
	case "queued":
		status.State = llms.JobQueued
	case "in_progress":
		status.State = llms.JobRunning
	case "completed":
		status.State = llms.JobSucceeded
	case "failed":
		status.State = llms.JobFailed
	default:
		status.State = llms.JobFailed
		status.Err = fmt.Errorf("unknown video status %q: %w", obj.Status, llms.ErrJobFailed)
	}
	if obj.Progress != nil {
		fraction := *obj.Progress / 100
		status.Progress = &fraction
	}
	if obj.Error != nil {
		status.Err = errors.Join(status.Err, fmt.Errorf("%s: %s", obj.Error.Code, obj.Error.Message))
		code := strings.ToLower(obj.Error.Code)
		if obj.Status == "failed" && (strings.Contains(code, "moderation") || strings.Contains(code, "content_policy")) {
			status.State = llms.JobModerated
			status.Err = &llms.ModerationError{Provider: string(provider), Stage: llms.ModerationOutput, Reasons: []string{obj.Error.Code, obj.Error.Message}}
		}
	}
	if obj.Status == "completed" {
		usage := videoUsage(obj.Seconds, obj.Size)
		if usage.Unit != "" {
			if cost, ok := llms.MediaCost(string(provider), obj.Model, usage); ok {
				status.Cost = &cost
			}
		}
	}
	return status
}

// Rates are USD per million tokens, verified by the D1 live-probe feedback.
var transcriptionTokenRates = map[string]struct{ audioIn, textIn, textOut float64 }{
	"gpt-4o-transcribe":      {6, 2.50, 10},
	"gpt-4o-mini-transcribe": {3, 1.25, 5},
}

func validateTranscriptionFormat(model, format string) error {
	if (format == transcriptionFormatVerboseJSON && model != "whisper-1") || (format == transcriptionFormatDiarizedJSON && model != "gpt-4o-transcribe-diarize") {
		return fmt.Errorf("%w: model %q does not support %s", llms.ErrInvalidParameters, model, format)
	}
	return nil
}

func videoUsage(secondsText, size string) llms.MediaUsage {
	seconds, err := strconv.ParseFloat(secondsText, 64)
	if err != nil || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return llms.MediaUsage{}
	}
	usage := llms.MediaUsage{Quantity: seconds}
	if size == videoSize720pLandscape || size == videoSize720pPortrait {
		usage.Unit = llms.MediaUnitSecond
	}
	return usage
}
