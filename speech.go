package llms

import (
	"context"
	"errors"
)

// SpeechOptions contains options for speech requests. Zero values leave provider defaults in effect.
type SpeechOptions struct {
	// Model configures the request's Model value.
	Model string
	// Voice configures the request's Voice value.
	Voice string
	// Language configures the request's Language value.
	Language string
	// Instructions configures the request's Instructions value.
	Instructions string
	// Speed configures the request's Speed value.
	Speed *float64
	// Format configures the request's Format value.
	Format AudioFormat
	// Timestamps configures the request's Timestamps value.
	Timestamps bool
	// Extra configures the request's Extra value.
	Extra map[string]any
}

// SpeechOption modifies SpeechOptions.
type SpeechOption func(*SpeechOptions)

// ApplySpeechOptions applies options in order and returns the resulting options.
func ApplySpeechOptions(options ...SpeechOption) *SpeechOptions {
	opts := &SpeechOptions{}
	for _, opt := range options {
		opt(opts)
	}
	return opts
}

// WithSpeechModel sets Model for the request.
func WithSpeechModel(value string) SpeechOption { return func(o *SpeechOptions) { o.Model = value } }

// WithSpeechVoice sets Voice for the request.
func WithSpeechVoice(value string) SpeechOption { return func(o *SpeechOptions) { o.Voice = value } }

// WithSpeechLanguage sets Language for the request.
func WithSpeechLanguage(value string) SpeechOption {
	return func(o *SpeechOptions) { o.Language = value }
}

// WithSpeechInstructions sets Instructions for the request.
func WithSpeechInstructions(value string) SpeechOption {
	return func(o *SpeechOptions) { o.Instructions = value }
}

// WithSpeechSpeed sets Speed for the request.
func WithSpeechSpeed(value float64) SpeechOption { return func(o *SpeechOptions) { o.Speed = &value } }

// WithSpeechFormat sets Format for the request.
func WithSpeechFormat(value AudioFormat) SpeechOption {
	return func(o *SpeechOptions) { o.Format = value }
}

// WithSpeechTimestamps sets Timestamps for the request.
func WithSpeechTimestamps(value bool) SpeechOption {
	return func(o *SpeechOptions) { o.Timestamps = value }
}

// WithSpeechExtra sets Extra for the request.
func WithSpeechExtra(value map[string]any) SpeechOption {
	return func(o *SpeechOptions) { o.Extra = value }
}

// TranscribeOptions contains options for transcribe requests. Zero values leave provider defaults in effect.
type TranscribeOptions struct {
	// Model configures the request's Model value.
	Model string
	// Language configures the request's Language value.
	Language string
	// Prompt configures the request's Prompt value.
	Prompt string
	// Diarize configures the request's Diarize value.
	Diarize bool
	// WordTimestamps configures the request's WordTimestamps value.
	WordTimestamps bool
	// Keyterms configures the request's Keyterms value.
	Keyterms []string
	// Extra configures the request's Extra value.
	Extra map[string]any
}

// TranscribeOption modifies TranscribeOptions.
type TranscribeOption func(*TranscribeOptions)

// ApplyTranscribeOptions applies options in order and returns the resulting options.
func ApplyTranscribeOptions(options ...TranscribeOption) *TranscribeOptions {
	opts := &TranscribeOptions{}
	for _, opt := range options {
		opt(opts)
	}
	return opts
}

// WithTranscribeModel sets Model for the request.
func WithTranscribeModel(value string) TranscribeOption {
	return func(o *TranscribeOptions) { o.Model = value }
}

// WithTranscribeLanguage sets Language for the request.
func WithTranscribeLanguage(value string) TranscribeOption {
	return func(o *TranscribeOptions) { o.Language = value }
}

// WithTranscribePrompt sets Prompt for the request.
func WithTranscribePrompt(value string) TranscribeOption {
	return func(o *TranscribeOptions) { o.Prompt = value }
}

// WithTranscribeDiarization sets Diarize for the request.
func WithTranscribeDiarization(value bool) TranscribeOption {
	return func(o *TranscribeOptions) { o.Diarize = value }
}

// WithTranscribeWordTimestamps sets WordTimestamps for the request.
func WithTranscribeWordTimestamps(value bool) TranscribeOption {
	return func(o *TranscribeOptions) { o.WordTimestamps = value }
}

// WithTranscribeKeyterms sets Keyterms for the request.
func WithTranscribeKeyterms(value []string) TranscribeOption {
	return func(o *TranscribeOptions) { o.Keyterms = value }
}

// WithTranscribeExtra sets Extra for the request.
func WithTranscribeExtra(value map[string]any) TranscribeOption {
	return func(o *TranscribeOptions) { o.Extra = value }
}

// ErrSpeechNotSupported indicates an absent speech synthesis capability.
var ErrSpeechNotSupported = errors.New("speech synthesis not supported by this provider")

// ErrSpeechStreamNotSupported indicates an absent speech streaming capability.
var ErrSpeechStreamNotSupported = errors.New("speech streaming not supported by this provider")

// ErrTranscriptionNotSupported indicates an absent transcription capability.
var ErrTranscriptionNotSupported = errors.New("transcription not supported by this provider")

// ErrEmptyText indicates that synthesis text is empty.
var ErrEmptyText = errors.New("text is empty")

// SpeechSynthesizer generates speech independently of the LLM chat interface.
// Providers without streaming return ErrSpeechStreamNotSupported from StreamSpeech.
//
// Example:
//
//	if synthesizer, ok := llms.AsSpeechSynthesizer(client); ok {
//	    response, err := synthesizer.Synthesize(ctx, "Hello, world", llms.WithSpeechVoice("alloy"))
//	    // ...
//	}
type SpeechSynthesizer interface {
	// Synthesize returns generated audio or a validation/provider error.
	Synthesize(ctx context.Context, text string, opts ...SpeechOption) (*SpeechResponse, error)
	// StreamSpeech returns audio chunks; consumers must drain the channel or cancel ctx.
	// A successful stream ends with a Data-less chunk carrying Usage when the
	// provider reports or can compute it.
	StreamSpeech(ctx context.Context, text string, opts ...SpeechOption) (<-chan AudioChunk, error)
}

// SpeechResponse contains generated audio and its format and usage.
type SpeechResponse struct {
	// Audio is the generated asset.
	Audio MediaAsset
	// Format describes the audio encoding.
	Format AudioFormat
	// Alignment contains optional character timestamps.
	Alignment *Alignment
	// Model identifies the model used.
	Model string
	// Usage contains billable media usage.
	Usage MediaUsage
	// Metadata contains provider-specific response information.
	Metadata map[string]any
}

// Transcriber converts audio to text independently of the LLM chat interface.
//
// Example:
//
//	if transcriber, ok := llms.AsTranscriber(client); ok {
//	    transcript, err := transcriber.Transcribe(ctx, llms.MediaInput{Data: audio, MIMEType: "audio/wav"})
//	    // ...
//	}
type Transcriber interface {
	// Transcribe returns recognized text or a validation/provider error.
	Transcribe(ctx context.Context, audio MediaInput, opts ...TranscribeOption) (*Transcription, error)
}

// Transcription contains recognized text and optional timing and speaker information.
type Transcription struct {
	// Text is the complete recognized text.
	Text string
	// Language is the recognized language.
	Language string
	// DurationSeconds is the input audio duration.
	DurationSeconds float64
	// Segments contains timed portions of the transcript.
	Segments []TranscriptSegment
	// Words contains individual word timestamps when requested.
	Words []TranscriptWord
	// Model identifies the model used.
	Model string
	// Usage contains billable media usage.
	Usage MediaUsage
	// Metadata contains provider-specific response information.
	Metadata map[string]any
}

// TranscriptSegment is a timed portion of a transcript.
type TranscriptSegment struct {
	// Start and End are offsets in seconds.
	Start, End float64
	// Text is the recognized segment.
	Text string
	// Speaker is an optional speaker identifier.
	Speaker string
}

// TranscriptWord is a timed word in a transcript.
type TranscriptWord struct {
	// Start and End are offsets in seconds.
	Start, End float64
	// Word is the recognized word.
	Word string
	// Speaker is an optional speaker identifier.
	Speaker string
}

// SupportsSpeech reports whether v implements SpeechSynthesizer.
func SupportsSpeech(v any) bool { _, ok := v.(SpeechSynthesizer); return ok }

// AsSpeechSynthesizer returns the SpeechSynthesizer and true, or nil and false.
func AsSpeechSynthesizer(v any) (SpeechSynthesizer, bool) {
	s, ok := v.(SpeechSynthesizer)
	return s, ok
}

// SupportsTranscription reports whether v implements Transcriber.
func SupportsTranscription(v any) bool { _, ok := v.(Transcriber); return ok }

// AsTranscriber returns the Transcriber and true, or nil and false.
func AsTranscriber(v any) (Transcriber, bool) { t, ok := v.(Transcriber); return t, ok }
