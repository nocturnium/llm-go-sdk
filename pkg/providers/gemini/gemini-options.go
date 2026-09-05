package gemini

import (
	"net/http"
	"time"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// Option is a function that configures a Gemini client.
type Option func(*options)

// options contains configuration options for the Gemini client.
type options struct {
	ImageModel         string
	VideoModel         string
	SpeechModel        string
	SpeechVoice        string
	TranscriptionModel string
	PollPolicy         PollPolicy
	APIKey             string
	Model              string
	EmbeddingModel     string
	BaseURL            string
	HTTPClient         *http.Client
	Timeout            time.Duration
	// AllowPrivateIPs allows requests to private/loopback IPs.
	// Off by default for SSRF safety; see WithAllowPrivateIPs.
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// defaultOptions returns the default options for Gemini.
func defaultOptions() *options {
	return &options{
		Model:              "gemini-2.5-flash",
		ImageModel:         "gemini-3.1-flash-image",
		VideoModel:         "veo-3.1-lite-generate-preview",
		SpeechModel:        "gemini-3.1-flash-tts-preview",
		SpeechVoice:        "Kore",
		TranscriptionModel: "gemini-3.5-transcribe",
	}
}

// WithAPIKey sets the API key.
func WithAPIKey(key string) Option {
	return func(o *options) {
		o.APIKey = key
	}
}

// WithModel sets the model to use.
func WithModel(model string) Option {
	return func(o *options) {
		o.Model = model
	}
}

// WithEmbeddingModel sets the default embedding model to use
func WithEmbeddingModel(model string) Option {
	return func(o *options) {
		o.EmbeddingModel = model
	}
}

// WithBaseURL sets a custom base URL for the API
// This is primarily useful for testing with mock servers
func WithBaseURL(url string) Option {
	return func(o *options) {
		o.BaseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client for advanced configuration
// (timeouts, retries, custom transport, etc.)
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) {
		o.HTTPClient = client
	}
}

// WithTimeout sets the HTTP client timeout for requests.
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.Timeout = timeout
	}
}

// WithAllowPrivateIPs allows requests to private/loopback IPs.
func WithAllowPrivateIPs() Option {
	return func(o *options) {
		o.AllowPrivateIPs = true
	}
}

// WithAllowHTTP allows plain-HTTP (non-HTTPS) requests.
func WithAllowHTTP() Option {
	return func(o *options) {
		o.AllowHTTP = true
	}
}

// apply applies the options to the default options
func apply(opts ...Option) *options {
	options := defaultOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// PollPolicy controls video and transcription polling. Zero uses DefaultPollPolicy.
type PollPolicy struct {
	// Initial and Max bound successive delays.
	Initial, Max time.Duration
	// Multiplier grows delays; Jitter varies them by a fraction in [0,1].
	Multiplier, Jitter float64
	// Timeout bounds polling; zero relies on the caller's context.
	Timeout time.Duration
}

// DefaultPollPolicy returns 1s initial, 10s maximum, 1.5x growth and 20% jitter.
func DefaultPollPolicy() PollPolicy { return PollPolicy(httpclient.DefaultPollPolicy()) }

// WithPollPolicy sets video and transcription polling delays and deadline.
func WithPollPolicy(policy PollPolicy) Option { return func(o *options) { o.PollPolicy = policy } }

// WithImageModel sets the default native image generation and editing model.
func WithImageModel(model string) Option { return func(o *options) { o.ImageModel = model } }

// WithVideoModel sets the default Veo model.
func WithVideoModel(model string) Option { return func(o *options) { o.VideoModel = model } }

// WithSpeechModel sets the default PCM speech model.
func WithSpeechModel(model string) Option { return func(o *options) { o.SpeechModel = model } }

// WithSpeechVoice sets the default prebuilt voice name (otherwise Kore).
func WithSpeechVoice(voice string) Option { return func(o *options) { o.SpeechVoice = voice } }

// WithTranscriptionModel sets the default Interactions transcription model.
func WithTranscriptionModel(model string) Option {
	return func(o *options) { o.TranscriptionModel = model }
}
