package llms

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// MediaInput identifies exactly one media source.
type MediaInput struct {
	// URL identifies remotely hosted media.
	URL string
	// Data contains inline media bytes; an empty slice is not a source.
	Data []byte
	// MIMEType describes the media encoding.
	MIMEType string
	// FileID identifies a file already uploaded to the provider.
	FileID string
}

// Validate returns ErrInvalidParameters unless exactly one of URL, Data, or FileID is set.
func (m MediaInput) Validate() error {
	sources := 0
	if m.URL != "" {
		sources++
	}
	if len(m.Data) > 0 {
		sources++
	}
	if m.FileID != "" {
		sources++
	}
	if sources != 1 {
		return fmt.Errorf("media input requires exactly one source: %w", ErrInvalidParameters)
	}
	return nil
}

// MediaAsset describes generated media. Fetch mutates Data; callers must serialize
// access to the same asset and its returned byte slice.
type MediaAsset struct {
	// URL is an optionally expiring download URL.
	URL string
	// CloudURI identifies an asset requiring provider-specific cloud retrieval.
	CloudURI string
	// MIMEType describes the media encoding.
	MIMEType string
	// RevisedPrompt is the prompt used after provider revision.
	RevisedPrompt string
	// Data contains inline or previously fetched bytes.
	Data []byte
	// ExpiresAt is the URL expiry; zero means unknown.
	ExpiresAt time.Time
	// Seed records the generation seed when available.
	Seed *int64
}

// MediaFetchOptions permits explicit relaxation of asset download restrictions.
// Both flags default to false, independently enforcing HTTPS and public IPs.
type MediaFetchOptions struct {
	// Headers are sent with the download request; credential headers are stripped on cross-host redirects.
	Headers map[string]string
	// AllowPrivateIPs permits private, loopback, and link-local destinations.
	AllowPrivateIPs bool
	// AllowHTTP permits unencrypted HTTP downloads.
	AllowHTTP bool
}

// Fetch returns cached Data, even after URL expiry; otherwise it downloads URL,
// caches the bytes, and returns them. Expired URLs return ErrAssetExpired.
// A nil client uses SDK defaults. Downloads use SSRF validation and strip
// credentials on cross-host redirects. CloudURI requires provider-specific retrieval.
func (a *MediaAsset) Fetch(ctx context.Context, c *http.Client) ([]byte, error) {
	return a.FetchWithOptions(ctx, c, MediaFetchOptions{})
}

// FetchWithOptions is Fetch with explicit HTTP and private-IP opt-outs.
// The supplied client is copied before installing SDK security policies.
func (a *MediaAsset) FetchWithOptions(ctx context.Context, c *http.Client, opts MediaFetchOptions) ([]byte, error) {
	if len(a.Data) > 0 {
		return a.Data, nil
	}
	if !a.ExpiresAt.IsZero() && !a.ExpiresAt.After(time.Now()) {
		return nil, ErrAssetExpired
	}
	if a.URL == "" {
		return nil, fmt.Errorf("media asset has no download URL: %w", ErrInvalidParameters)
	}
	clientOpts := []httpclient.ClientOption{httpclient.WithAllowPrivateIPs(opts.AllowPrivateIPs), httpclient.WithAllowHTTP(opts.AllowHTTP)}
	if c != nil {
		clientOpts = append(clientOpts, httpclient.WithHTTPClient(c))
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	response, err := httpclient.NewClient(clientOpts...).DoBinary(ctx, http.MethodGet, a.URL, nil, opts.Headers)
	if err != nil {
		return nil, fmt.Errorf("fetch media asset: %w", err)
	}
	a.Data = response.Data
	return a.Data, nil
}

// JobState is a provider-independent asynchronous job state.
type JobState string

const (
	// JobQueued indicates a job awaiting execution.
	JobQueued JobState = "queued"
	// JobRunning indicates active execution.
	JobRunning JobState = "running"
	// JobSucceeded indicates a completed result is available.
	JobSucceeded JobState = "succeeded"
	// JobFailed indicates execution failed.
	JobFailed JobState = "failed"
	// JobCancelled indicates cancellation completed.
	JobCancelled JobState = "cancelled" //nolint:misspell // Canonical job state spelling.
	// JobModerated indicates content filtering stopped the job.
	JobModerated JobState = "moderated"
)

// Terminal reports whether the state ends job execution. Unknown states are nonterminal.
func (s JobState) Terminal() bool {
	switch s {
	case JobSucceeded, JobFailed, JobCancelled, JobModerated:
		return true
	default:
		return false
	}
}

// JobStatus describes an asynchronous job at one point in time.
type JobStatus struct {
	// State is the current job state.
	State JobState
	// Progress is an optional completion fraction between zero and one.
	Progress *float64
	// Err contains provider failure details.
	Err error
	// Cost is an optional provider-reported USD charge.
	Cost *float64
}

// MediaUnit identifies a media billing unit.
type MediaUnit string

const (
	// MediaUnitImage bills each generated image.
	MediaUnitImage MediaUnit = "image"
	// MediaUnitMegapixel bills each million pixels.
	MediaUnitMegapixel MediaUnit = "megapixel"
	// MediaUnitSecond bills each second of media.
	MediaUnitSecond MediaUnit = "second"
	// MediaUnitMinute bills each minute of media.
	MediaUnitMinute MediaUnit = "minute"
	// MediaUnitKChar bills each thousand input characters.
	MediaUnitKChar MediaUnit = "kchar"
	// MediaUnitMTokenOut bills each million output tokens.
	MediaUnitMTokenOut MediaUnit = "mtoken_out"
)

// MediaUsage describes billable quantity and optional actual cost.
type MediaUsage struct {
	// Unit is the billing unit.
	Unit MediaUnit
	// Quantity is the number of units consumed.
	Quantity float64
	// Cost is provider-reported USD spend and overrides estimated pricing.
	Cost *float64
}

// AudioFormat describes a generated audio stream or file.
type AudioFormat struct {
	// Container and Encoding identify the file format and codec.
	Container, Encoding string
	// SampleRate is samples per second; BitRate is bits per second.
	SampleRate, BitRate int
}

// AudioChunk is one streaming audio event.
type AudioChunk struct {
	// Data contains audio bytes.
	Data []byte
	// Alignment contains optional timestamps for this chunk.
	Alignment *Alignment
	// Err indicates a streaming failure.
	Err error
}

// Alignment associates characters with millisecond offsets in the audio.
// Chars, StartMS, and EndMS have matching lengths.
type Alignment struct {
	// Chars contains aligned characters.
	Chars []string
	// StartMS and EndMS contain each character's start and end offsets.
	StartMS, EndMS []int
}
