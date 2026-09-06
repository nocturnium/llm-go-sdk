package llms

import (
	"context"
	"errors"
	"fmt"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// VideoOptions contains options for video requests. Zero values leave provider defaults in effect.
type VideoOptions struct {
	// Model configures the request's Model value.
	Model string
	// DurationSeconds configures the request's DurationSeconds value.
	DurationSeconds int
	// Resolution configures the request's Resolution value.
	Resolution string
	// AspectRatio configures the request's AspectRatio value.
	AspectRatio string
	// Audio configures the request's Audio value.
	Audio *bool
	// Seed configures the request's Seed value.
	Seed *int64
	// NegativePrompt configures the request's NegativePrompt value.
	NegativePrompt string
	// FirstFrame configures the request's FirstFrame value.
	FirstFrame *MediaInput
	// LastFrame configures the request's LastFrame value.
	LastFrame *MediaInput
	// ReferenceImages configures the request's ReferenceImages value.
	ReferenceImages []MediaInput
	// OutputFormat configures the request's OutputFormat value.
	OutputFormat string
	// Extra configures the request's Extra value.
	Extra map[string]any
}

// VideoOption modifies VideoOptions.
type VideoOption func(*VideoOptions)

// ApplyVideoOptions applies options in order and returns the resulting options.
func ApplyVideoOptions(options ...VideoOption) *VideoOptions {
	opts := &VideoOptions{}
	for _, opt := range options {
		opt(opts)
	}
	return opts
}

// WithVideoModel sets Model for the request.
func WithVideoModel(value string) VideoOption { return func(o *VideoOptions) { o.Model = value } }

// WithVideoDuration sets DurationSeconds for the request.
func WithVideoDuration(value int) VideoOption {
	return func(o *VideoOptions) { o.DurationSeconds = value }
}

// WithVideoResolution sets Resolution for the request.
func WithVideoResolution(value string) VideoOption {
	return func(o *VideoOptions) { o.Resolution = value }
}

// WithVideoAspectRatio sets AspectRatio for the request.
func WithVideoAspectRatio(value string) VideoOption {
	return func(o *VideoOptions) { o.AspectRatio = value }
}

// WithVideoAudio sets Audio for the request.
func WithVideoAudio(value bool) VideoOption { return func(o *VideoOptions) { o.Audio = &value } }

// WithVideoSeed sets Seed for the request.
func WithVideoSeed(value int64) VideoOption { return func(o *VideoOptions) { o.Seed = &value } }

// WithVideoNegativePrompt sets NegativePrompt for the request.
func WithVideoNegativePrompt(value string) VideoOption {
	return func(o *VideoOptions) { o.NegativePrompt = value }
}

// WithVideoFirstFrame sets FirstFrame for the request.
func WithVideoFirstFrame(value MediaInput) VideoOption {
	return func(o *VideoOptions) { o.FirstFrame = &value }
}

// WithVideoLastFrame sets LastFrame for the request.
func WithVideoLastFrame(value MediaInput) VideoOption {
	return func(o *VideoOptions) { o.LastFrame = &value }
}

// WithVideoReferenceImages sets ReferenceImages for the request.
func WithVideoReferenceImages(value []MediaInput) VideoOption {
	return func(o *VideoOptions) { o.ReferenceImages = value }
}

// WithVideoFormat sets OutputFormat for the request.
func WithVideoFormat(value string) VideoOption {
	return func(o *VideoOptions) { o.OutputFormat = value }
}

// WithVideoExtra sets Extra for the request.
func WithVideoExtra(value map[string]any) VideoOption {
	return func(o *VideoOptions) { o.Extra = value }
}

// ErrVideoGenerationNotSupported indicates an absent video generation capability.
var ErrVideoGenerationNotSupported = errors.New("video generation not supported by this provider")

// ErrJobCancelNotSupported indicates a job cannot be canceled through this API.
var ErrJobCancelNotSupported = errors.New("job cancellation not supported")

// ErrJobFailed indicates asynchronous job execution failed.
var ErrJobFailed = errors.New("media job failed")

// VideoGenerator submits video generation jobs independently of the LLM interface.
//
// Example:
//
//	if generator, ok := llms.AsVideoGenerator(client); ok {
//	    job, err := generator.GenerateVideo(ctx, "Clouds over a mountain")
//	    // ...
//	}
type VideoGenerator interface {
	// GenerateVideo returns a job handle or a validation/provider error.
	GenerateVideo(ctx context.Context, prompt string, opts ...VideoOption) (VideoJob, error)
}

// VideoJob is a submitted video generation task.
//
// Example:
//
//	job, err := generator.GenerateVideo(ctx, "Clouds over a mountain")
//	if err != nil { return err }
//	response, err := job.Wait(ctx)
//	// ...
type VideoJob interface {
	// ID returns the provider's job identifier.
	ID() string
	// Poll returns current status or a provider/context error.
	Poll(ctx context.Context) (*JobStatus, error)
	// Wait polls until completion and returns a result or terminal error.
	Wait(ctx context.Context) (*VideoResponse, error)
	// Cancel requests cancellation, or returns ErrJobCancelNotSupported.
	Cancel(ctx context.Context) error
}

// VideoResponse contains generated videos and accounting metadata.
type VideoResponse struct {
	// Videos contains generated assets in response order.
	Videos []MediaAsset
	// Model identifies the model used.
	Model string
	// Usage contains billable media usage.
	Usage MediaUsage
	// Metadata contains provider-specific response information.
	Metadata map[string]any
}

// SupportsVideoGeneration reports whether v implements VideoGenerator.
func SupportsVideoGeneration(v any) bool { _, ok := v.(VideoGenerator); return ok }

// AsVideoGenerator returns the VideoGenerator and true, or nil and false.
func AsVideoGenerator(v any) (VideoGenerator, bool) { g, ok := v.(VideoGenerator); return g, ok }

// PollingVideoJob implements VideoJob using provider callbacks. Configure fields
// before use; callback implementations must honor ctx and synchronize shared state.
type PollingVideoJob struct {
	// JobID is the provider's identifier.
	JobID string
	// PollFn fetches the current job status.
	PollFn func(ctx context.Context) (*JobStatus, error)
	// ResultFn retrieves the successful result.
	ResultFn func(ctx context.Context) (*VideoResponse, error)
	// CancelFn requests cancellation; nil means unsupported.
	CancelFn func(ctx context.Context) error
	// Policy controls backoff. Zero uses httpclient.DefaultPollPolicy().
	Policy httpclient.PollPolicy
}

// ID returns the provider job identifier.
func (j *PollingVideoJob) ID() string { return j.JobID }

// Poll invokes PollFn, returning a context, configuration, or provider error.
func (j *PollingVideoJob) Poll(ctx context.Context) (*JobStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if j.PollFn == nil {
		return nil, fmt.Errorf("missing job poll callback: %w", ErrInvalidParameters)
	}
	status, err := j.PollFn(ctx)
	if err == nil && status == nil {
		return nil, fmt.Errorf("nil job status: %w", ErrInvalidParameters)
	}
	return status, err
}

// Cancel invokes CancelFn or returns ErrJobCancelNotSupported when it is nil.
func (j *PollingVideoJob) Cancel(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.CancelFn == nil {
		return ErrJobCancelNotSupported
	}
	return j.CancelFn(ctx)
}

// Wait polls until a terminal state, then retrieves the result on success.
// Failed jobs wrap ErrJobFailed and their cause; moderated jobs return a
// ModerationError; canceled jobs wrap context.Canceled. Polling timeout and
// caller cancellation propagate without invoking ResultFn.
func (j *PollingVideoJob) Wait(ctx context.Context) (*VideoResponse, error) {
	var status *JobStatus
	err := httpclient.Poll(ctx, j.Policy, func(ctx context.Context) (bool, error) {
		var err error
		status, err = j.Poll(ctx)
		if err != nil {
			return false, err
		}
		return status.State.Terminal(), nil
	})
	if err != nil {
		return nil, err
	}
	switch status.State {
	case JobSucceeded:
		if j.ResultFn == nil {
			return nil, fmt.Errorf("missing job result callback: %w", ErrInvalidParameters)
		}
		return j.ResultFn(ctx)
	case JobFailed:
		return nil, fmt.Errorf("job %s: %w", j.JobID, errors.Join(ErrJobFailed, status.Err))
	case JobModerated:
		var moderation *ModerationError
		if errors.As(status.Err, &moderation) {
			return nil, moderation
		}
		moderation = &ModerationError{Stage: ModerationOutput, Charged: status.Cost != nil && *status.Cost > 0}
		if status.Err != nil {
			moderation.Reasons = []string{status.Err.Error()}
		}
		return nil, moderation
	default:
		return nil, fmt.Errorf("job %s canceled: %w", j.JobID, context.Canceled)
	}
}

var _ VideoJob = (*PollingVideoJob)(nil)
