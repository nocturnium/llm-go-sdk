package llms

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// PartType describes the kind of content in a multi-modal message part.
type PartType string

const (
	// PartTypeText identifies a text content part.
	PartTypeText PartType = "text"
	// PartTypeImage identifies an image content part.
	PartTypeImage PartType = "image"
)

// ImageSource describes how image data is provided.
type ImageSource string

const (
	// ImageSourceURL identifies image data by URL.
	ImageSourceURL ImageSource = "url"
	// ImageSourceBase64 identifies inline base64 image data.
	ImageSourceBase64 ImageSource = "base64"
)

// ContentPart represents a part of a message that can be text or image.
// Use this for multi-modal messages that include images.
type ContentPart struct {
	// Type is either "text" or "image"
	Type PartType `json:"type"`

	// Text content (when Type is "text")
	Text string `json:"text,omitempty"`

	// Image content (when Type is "image")
	Image *ImageContent `json:"image,omitempty"`
}

// ImageContent represents image data for vision models.
type ImageContent struct {
	// Source is either "base64" or "url"
	Source ImageSource `json:"source"`

	// MediaType is the MIME type (e.g., "image/png", "image/jpeg", "image/gif", "image/webp")
	MediaType string `json:"media_type"`

	// Data contains the base64-encoded image data (when Source is "base64")
	// or the URL (when Source is "url")
	Data string `json:"data"`
}

// Supported image media types
const (
	MediaTypePNG  = "image/png"
	MediaTypeJPEG = "image/jpeg"
	MediaTypeGIF  = "image/gif"
	MediaTypeWebP = "image/webp"
)

// NewTextPart creates a text content part.
func NewTextPart(text string) ContentPart {
	return ContentPart{
		Type: PartTypeText,
		Text: text,
	}
}

// NewImageURLPart creates an image content part from a URL.
func NewImageURLPart(url string) ContentPart {
	return ContentPart{
		Type: PartTypeImage,
		Image: &ImageContent{
			Source: ImageSourceURL,
			Data:   url,
		},
	}
}

// NewImageBase64Part creates an image content part from base64-encoded data.
func NewImageBase64Part(data, mediaType string) ContentPart {
	return ContentPart{
		Type: PartTypeImage,
		Image: &ImageContent{
			Source:    ImageSourceBase64,
			MediaType: mediaType,
			Data:      data,
		},
	}
}

// NewImageFromFile creates an image content part from a local file.
// It reads the file, encodes it as base64, and detects the media type from the extension.
// Returns an error if the file exceeds MaxImageSize (20MB).
func NewImageFromFile(path string) (ContentPart, error) {
	// Check file size before reading to prevent OOM on large files
	fileInfo, err := os.Stat(path)
	if err != nil {
		return ContentPart{}, fmt.Errorf("failed to stat image file: %w", err)
	}

	if fileInfo.Size() > MaxImageSize {
		return ContentPart{}, fmt.Errorf("image file size %d bytes exceeds maximum allowed size of %d bytes", fileInfo.Size(), MaxImageSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ContentPart{}, fmt.Errorf("failed to read image file: %w", err)
	}

	mediaType := detectMediaType(path, data)
	if mediaType == "" {
		return ContentPart{}, fmt.Errorf("unsupported image format for file: %s", path)
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	return NewImageBase64Part(encoded, mediaType), nil
}

// NewImageFromReader creates an image content part from an io.Reader.
// The mediaType must be specified (e.g., "image/png").
// Returns an error if the data exceeds MaxImageSize (20MB).
func NewImageFromReader(r io.Reader, mediaType string) (ContentPart, error) {
	// Use LimitReader to prevent reading more than MaxImageSize + 1 byte.
	// Reading exactly MaxImageSize+1 lets us detect oversized data.
	limitedReader := io.LimitReader(r, MaxImageSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return ContentPart{}, fmt.Errorf("failed to read image data: %w", err)
	}

	if len(data) > MaxImageSize {
		return ContentPart{}, fmt.Errorf("image data size exceeds maximum allowed size of %d bytes", MaxImageSize)
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	return NewImageBase64Part(encoded, mediaType), nil
}

// NewImageFromBytes creates an image content part from a byte slice.
// The mediaType must be specified (e.g., "image/png").
func NewImageFromBytes(data []byte, mediaType string) ContentPart {
	encoded := base64.StdEncoding.EncodeToString(data)
	return NewImageBase64Part(encoded, mediaType)
}

// NewMultiPartMessage creates a message with multiple content parts.
// This is useful for messages that include both text and images.
//
// Example:
//
//	msg := llms.NewMultiPartMessage(llms.RoleUser,
//	    llms.NewTextPart("What's in this image?"),
//	    llms.NewImageURLPart("https://example.com/image.jpg"),
//	)
func NewMultiPartMessage(role Role, parts ...ContentPart) Message {
	return Message{
		Role:  role,
		Parts: parts,
	}
}

// NewImageMessage creates a user message with a text prompt and an image URL.
// This is a convenience function for the common case of asking about an image.
func NewImageMessage(prompt, imageURL string) Message {
	return NewMultiPartMessage(RoleUser,
		NewTextPart(prompt),
		NewImageURLPart(imageURL),
	)
}

// NewImageFileMessage creates a user message with a text prompt and an image from a file.
func NewImageFileMessage(prompt, imagePath string) (Message, error) {
	imagePart, err := NewImageFromFile(imagePath)
	if err != nil {
		return Message{}, err
	}

	return NewMultiPartMessage(RoleUser,
		NewTextPart(prompt),
		imagePart,
	), nil
}

// HasParts returns true if the message has multi-part content.
func (m Message) HasParts() bool {
	return len(m.Parts) > 0
}

// Text returns all text content from the message.
// If the message uses the simple Content field, it returns that.
// If the message uses Parts, it concatenates all text parts.
func (m Message) Text() string {
	if m.Content != "" {
		return m.Content
	}

	var texts []string
	for _, part := range m.Parts {
		if part.Type == PartTypeText && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}

	return strings.Join(texts, "\n")
}

// HasImages returns true if the message contains any image parts.
func (m Message) HasImages() bool {
	for _, part := range m.Parts {
		if part.Type == PartTypeImage && part.Image != nil {
			return true
		}
	}
	return false
}

// Images returns all image content parts from the message.
func (m Message) Images() []*ImageContent {
	var images []*ImageContent
	for _, part := range m.Parts {
		if part.Type == PartTypeImage && part.Image != nil {
			images = append(images, part.Image)
		}
	}
	return images
}

// detectMediaType determines the media type from file extension and magic bytes.
func detectMediaType(path string, data []byte) string {
	// First, try to detect from magic bytes
	if len(data) > 0 {
		contentType := http.DetectContentType(data)
		if strings.HasPrefix(contentType, "image/") {
			return contentType
		}
	}

	// Fall back to extension
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return MediaTypePNG
	case ".jpg", ".jpeg":
		return MediaTypeJPEG
	case ".gif":
		return MediaTypeGIF
	case ".webp":
		return MediaTypeWebP
	default:
		return ""
	}
}

// MaxImageSize is the maximum allowed image size in bytes (20MB)
// Most LLM providers have limits around 20MB for base64 images
const MaxImageSize = 20 * 1024 * 1024

// ValidateImageContent validates that an image content part is well-formed.
func ValidateImageContent(img *ImageContent) error {
	if img == nil {
		return fmt.Errorf("image content is nil")
	}

	if img.Source != ImageSourceBase64 && img.Source != ImageSourceURL {
		return fmt.Errorf("invalid image source: %q (must be 'base64' or 'url')", img.Source)
	}

	if img.Data == "" {
		return fmt.Errorf("image data is empty")
	}

	if img.Source == ImageSourceBase64 {
		if img.MediaType == "" {
			return fmt.Errorf("media type is required for base64 images")
		}

		// Validate media type
		switch img.MediaType {
		case MediaTypePNG, MediaTypeJPEG, MediaTypeGIF, MediaTypeWebP:
			// Valid
		default:
			return fmt.Errorf("unsupported media type: %q", img.MediaType)
		}

		// Check approximate decoded size before actually decoding to prevent OOM.
		// Base64 encoding increases size by ~33%, so decoded size ≈ encoded size * 3/4.
		approxDecodedSize := len(img.Data) * 3 / 4
		if approxDecodedSize > MaxImageSize {
			return fmt.Errorf("image data appears to exceed maximum allowed size of %d bytes (estimated %d bytes)", MaxImageSize, approxDecodedSize)
		}

		// Validate base64 encoding without fully decoding to save memory.
		// For large images, we use a streaming decoder and only read a small sample
		// to verify the encoding is valid. This avoids allocating ~26MB for a 20MB image.
		decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(img.Data))

		// Read a small sample to validate the encoding is well-formed.
		// 1KB is enough to detect most encoding errors without large allocations.
		sampleBuf := make([]byte, 1024)
		_, err := io.ReadFull(decoder, sampleBuf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return fmt.Errorf("invalid base64 data: %w", err)
		}
	}

	if img.Source == ImageSourceURL {
		// Basic URL validation
		if !strings.HasPrefix(img.Data, "http://") && !strings.HasPrefix(img.Data, "https://") {
			return fmt.Errorf("invalid image URL: must start with http:// or https://")
		}

		// Validate URL length (browsers typically have ~2KB limit, but be generous)
		if len(img.Data) > 8192 {
			return fmt.Errorf("image URL exceeds maximum length of 8192 characters")
		}
	}

	return nil
}
