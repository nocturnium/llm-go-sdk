package llms

import (
	"context"
	"errors"
)

// ImageOptions contains options for image requests. Zero values leave provider defaults in effect.
type ImageOptions struct {
	// Model configures the request's Model value.
	Model string
	// Size configures the request's Size value.
	Size string
	// AspectRatio configures the request's AspectRatio value.
	AspectRatio string
	// N configures the request's N value.
	N int
	// Seed configures the request's Seed value.
	Seed *int64
	// NegativePrompt configures the request's NegativePrompt value.
	NegativePrompt string
	// Quality configures the request's Quality value.
	Quality string
	// OutputFormat configures the request's OutputFormat value.
	OutputFormat string
	// SafetyTolerance configures the request's SafetyTolerance value.
	SafetyTolerance *int
	// Extra configures the request's Extra value.
	Extra map[string]any
}

// ImageOption modifies ImageOptions.
type ImageOption func(*ImageOptions)

// ApplyImageOptions applies options in order and returns the resulting options.
func ApplyImageOptions(options ...ImageOption) *ImageOptions {
	opts := &ImageOptions{}
	for _, opt := range options {
		opt(opts)
	}
	return opts
}

// WithImageModel sets Model for the request.
func WithImageModel(value string) ImageOption { return func(o *ImageOptions) { o.Model = value } }

// WithImageSize sets Size for the request.
func WithImageSize(value string) ImageOption { return func(o *ImageOptions) { o.Size = value } }

// WithImageAspectRatio sets AspectRatio for the request.
func WithImageAspectRatio(value string) ImageOption {
	return func(o *ImageOptions) { o.AspectRatio = value }
}

// WithImageCount sets N for the request.
func WithImageCount(value int) ImageOption { return func(o *ImageOptions) { o.N = value } }

// WithImageSeed sets Seed for the request.
func WithImageSeed(value int64) ImageOption { return func(o *ImageOptions) { o.Seed = &value } }

// WithImageNegativePrompt sets NegativePrompt for the request.
func WithImageNegativePrompt(value string) ImageOption {
	return func(o *ImageOptions) { o.NegativePrompt = value }
}

// WithImageQuality sets Quality for the request.
func WithImageQuality(value string) ImageOption { return func(o *ImageOptions) { o.Quality = value } }

// WithImageFormat sets OutputFormat for the request.
func WithImageFormat(value string) ImageOption {
	return func(o *ImageOptions) { o.OutputFormat = value }
}

// WithImageSafetyTolerance sets SafetyTolerance for the request.
func WithImageSafetyTolerance(value int) ImageOption {
	return func(o *ImageOptions) { o.SafetyTolerance = &value }
}

// WithImageExtra sets Extra for the request.
func WithImageExtra(value map[string]any) ImageOption {
	return func(o *ImageOptions) { o.Extra = value }
}

// ErrImageGenerationNotSupported indicates an absent image generation capability.
var ErrImageGenerationNotSupported = errors.New("image generation not supported by this provider")

// ErrImageEditNotSupported indicates an absent image editing capability.
var ErrImageEditNotSupported = errors.New("image editing not supported by this provider")

// ErrEmptyPrompt indicates that a generation prompt is empty.
var ErrEmptyPrompt = errors.New("prompt is empty")

// ImageGenerator generates images independently of the LLM chat interface.
// Use SupportsImageGeneration to check for this capability.
//
// Example:
//
//	if generator, ok := llms.AsImageGenerator(client); ok {
//	    response, err := generator.GenerateImage(ctx, "A moonlit forest")
//	    // ...
//	}
type ImageGenerator interface {
	// GenerateImage returns generated images or a validation/provider error.
	GenerateImage(ctx context.Context, prompt string, opts ...ImageOption) (*ImageResponse, error)
}

// ImageEditor edits existing images using a prompt.
//
// Example:
//
//	if editor, ok := llms.AsImageEditor(client); ok {
//	    response, err := editor.EditImage(ctx, "Add moonlight", images)
//	    // ...
//	}
type ImageEditor interface {
	// EditImage returns edited images or a validation/provider error.
	EditImage(ctx context.Context, prompt string, images []MediaInput, opts ...ImageOption) (*ImageResponse, error)
}

// ImageResponse contains generated images and their accounting metadata.
type ImageResponse struct {
	// Images contains the generated assets in response order.
	Images []MediaAsset
	// Model identifies the model used.
	Model string
	// Usage contains billable media usage.
	Usage MediaUsage
	// Metadata contains provider-specific response information.
	Metadata map[string]any
}

// SupportsImageGeneration reports whether v implements ImageGenerator.
func SupportsImageGeneration(v any) bool { _, ok := v.(ImageGenerator); return ok }

// AsImageGenerator returns the ImageGenerator and true, or nil and false.
func AsImageGenerator(v any) (ImageGenerator, bool) { g, ok := v.(ImageGenerator); return g, ok }

// SupportsImageEdit reports whether v implements ImageEditor.
func SupportsImageEdit(v any) bool { _, ok := v.(ImageEditor); return ok }

// AsImageEditor returns the ImageEditor and true, or nil and false.
func AsImageEditor(v any) (ImageEditor, bool) { e, ok := v.(ImageEditor); return e, ok }
