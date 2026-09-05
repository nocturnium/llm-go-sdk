package elevenlabs

import (
	"net/http"
	"time"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// PollPolicy controls asynchronous image and video polling delays and deadlines.
// Zero uses DefaultPollPolicy. Timeout zero relies on the caller's context.
type PollPolicy struct {
	// Initial and Max bound successive polling delays.
	Initial, Max time.Duration
	// Multiplier grows delays; Jitter varies them by a fraction in [0,1].
	Multiplier, Jitter float64
	// Timeout bounds the overall polling operation; zero uses only ctx.
	Timeout time.Duration
}

// DefaultPollPolicy returns 1s initial, 10s maximum, 1.5x growth and 20% jitter.
func DefaultPollPolicy() PollPolicy { return PollPolicy(httpclient.DefaultPollPolicy()) }

// VoiceSettings configures synthesis. Nil fields retain the provider defaults.
type VoiceSettings struct {
	// Stability controls voice stability in [0,1].
	Stability *float64 `json:"stability,omitempty"`
	// SimilarityBoost controls voice similarity in [0,1].
	SimilarityBoost *float64 `json:"similarity_boost,omitempty"`
	// Style controls style intensity in [0,1].
	Style *float64 `json:"style,omitempty"`
	// Speed sets a positive speech speed multiplier.
	Speed *float64 `json:"speed,omitempty"`
	// UseSpeakerBoost enables the native speaker boost setting.
	UseSpeakerBoost *bool `json:"use_speaker_boost,omitempty"`
}

// Option configures a client.
type Option func(*options)
type options struct {
	APIKey, Model, BaseURL, Voice, ImageModel, VideoModel, TranscriptionModel string
	HTTPClient                                                                *http.Client
	Timeout                                                                   time.Duration
	AllowPrivateIPs, AllowHTTP                                                bool
	VoiceSettings                                                             VoiceSettings
	PollPolicy                                                                PollPolicy
}

func defaultOptions() *options {
	return &options{BaseURL: "https://api.elevenlabs.io", Model: "eleven_flash_v2_5", Voice: "21m00Tcm4TlvDq8ikWAM", ImageModel: "gemini-3.1-flash-lite-image", VideoModel: "veo-3.1-fast-generate-001", TranscriptionModel: "scribe_v2", Timeout: 120 * time.Second, PollPolicy: DefaultPollPolicy()}
}
func apply(opts ...Option) *options {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithAPIKey sets the API key, overriding environment variables.
func WithAPIKey(v string) Option { return func(o *options) { o.APIKey = v } }

// WithModel sets the default speech model.
func WithModel(v string) Option { return func(o *options) { o.Model = v } }

// WithBaseURL sets the API host, including regional hosts.
func WithBaseURL(v string) Option { return func(o *options) { o.BaseURL = v } }

// WithHTTPClient sets the HTTP client (copied before use).
func WithHTTPClient(v *http.Client) Option { return func(o *options) { o.HTTPClient = v } }

// WithTimeout sets the per-request and stream timeout. Nonpositive values use 120s.
func WithTimeout(v time.Duration) Option { return func(o *options) { o.Timeout = v } }

// WithVoice sets the default voice ID.
func WithVoice(v string) Option { return func(o *options) { o.Voice = v } }

// WithImageModel sets the default image model.
func WithImageModel(v string) Option { return func(o *options) { o.ImageModel = v } }

// WithVideoModel sets the default video model.
func WithVideoModel(v string) Option { return func(o *options) { o.VideoModel = v } }

// WithTranscriptionModel sets the default transcription model.
func WithTranscriptionModel(v string) Option { return func(o *options) { o.TranscriptionModel = v } }

// WithPollPolicy sets the image and video polling policy.
func WithPollPolicy(v PollPolicy) Option { return func(o *options) { o.PollPolicy = v } }

// WithAllowPrivateIPs permits private and loopback destinations (off by default).
func WithAllowPrivateIPs() Option { return func(o *options) { o.AllowPrivateIPs = true } }

// WithAllowHTTP permits unencrypted HTTP requests (off by default).
func WithAllowHTTP() Option { return func(o *options) { o.AllowHTTP = true } }

// WithVoiceSettings sets voice settings, taking a copy of each supplied value.
func WithVoiceSettings(v VoiceSettings) Option {
	v = cloneSettings(v)
	return func(o *options) { o.VoiceSettings = cloneSettings(v) }
}
func cloneSettings(v VoiceSettings) VoiceSettings {
	v.Stability = copyValue(v.Stability)
	v.SimilarityBoost = copyValue(v.SimilarityBoost)
	v.Style = copyValue(v.Style)
	v.Speed = copyValue(v.Speed)
	v.UseSpeakerBoost = copyValue(v.UseSpeakerBoost)
	return v
}
func copyValue[T any](v *T) *T {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
