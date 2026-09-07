package fal

import (
	"net/http"
	"time"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// PollPolicy controls queue polling delays and deadlines for images, videos,
// speech and transcription. Zero uses DefaultPollPolicy. Timeout zero relies on
// the caller's context.
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

// Default model IDs. Each is a fal queue application path.
const (
	// DefaultImageModel is the default text-to-image application.
	DefaultImageModel = "fal-ai/flux/schnell"
	// DefaultVideoModel is the default text-to-video application.
	DefaultVideoModel = "fal-ai/minimax/hailuo-02/standard/text-to-video"
	// DefaultSpeechModel is the default text-to-speech application.
	DefaultSpeechModel = "fal-ai/kokoro/american-english"
	// DefaultTranscriptionModel is the default speech-to-text application.
	DefaultTranscriptionModel = "fal-ai/whisper"
)

// Option configures a client.
type Option func(*options)
type options struct {
	APIKey, BaseURL, ImageModel, VideoModel, SpeechModel, TranscriptionModel, QueuePriority string
	HTTPClient                                                                              *http.Client
	Timeout                                                                                 time.Duration
	AllowPrivateIPs, AllowHTTP                                                              bool
	PollPolicy                                                                              PollPolicy
}

func defaultOptions() *options {
	return &options{BaseURL: "https://queue.fal.run", ImageModel: DefaultImageModel, VideoModel: DefaultVideoModel, SpeechModel: DefaultSpeechModel, TranscriptionModel: DefaultTranscriptionModel, Timeout: 120 * time.Second, PollPolicy: DefaultPollPolicy()}
}
func apply(opts ...Option) *options {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithAPIKey sets the API key, overriding FAL_KEY and LLM_API_KEY.
func WithAPIKey(v string) Option { return func(o *options) { o.APIKey = v } }

// WithBaseURL sets the queue host (default https://queue.fal.run).
func WithBaseURL(v string) Option { return func(o *options) { o.BaseURL = v } }

// WithHTTPClient sets the HTTP client (copied before use).
func WithHTTPClient(v *http.Client) Option { return func(o *options) { o.HTTPClient = v } }

// WithTimeout sets the per-request timeout, including asset downloads.
// Nonpositive values use 120s. Queue polling is bounded by ctx and WithPollPolicy.
func WithTimeout(v time.Duration) Option { return func(o *options) { o.Timeout = v } }

// WithImageModel sets the default image application (default DefaultImageModel).
func WithImageModel(v string) Option { return func(o *options) { o.ImageModel = v } }

// WithVideoModel sets the default video application (default DefaultVideoModel).
func WithVideoModel(v string) Option { return func(o *options) { o.VideoModel = v } }

// WithSpeechModel sets the default speech application (default DefaultSpeechModel).
func WithSpeechModel(v string) Option { return func(o *options) { o.SpeechModel = v } }

// WithTranscriptionModel sets the default transcription application
// (default DefaultTranscriptionModel).
func WithTranscriptionModel(v string) Option { return func(o *options) { o.TranscriptionModel = v } }

// WithPollPolicy sets the queue polling policy for every operation.
func WithPollPolicy(v PollPolicy) Option { return func(o *options) { o.PollPolicy = v } }

// WithQueuePriority sets the X-Fal-Queue-Priority submit header: "normal" or "low".
// Empty (the default) omits the header.
func WithQueuePriority(v string) Option { return func(o *options) { o.QueuePriority = v } }

// WithAllowPrivateIPs permits private and loopback destinations (off by default).
func WithAllowPrivateIPs() Option { return func(o *options) { o.AllowPrivateIPs = true } }

// WithAllowHTTP permits unencrypted HTTP requests (off by default).
func WithAllowHTTP() Option { return func(o *options) { o.AllowHTTP = true } }
