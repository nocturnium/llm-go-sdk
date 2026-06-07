package llms

import (
	"encoding/base64"
	"testing"
)

const (
	contentTypeText   = "text"
	testImageSource   = "url"
	contentTypeBase64 = "base64"
	modelTypeImage    = "image"
)

func TestNewTextPart(t *testing.T) {
	part := NewTextPart(testHelloWorld)

	if part.Type != contentTypeText {
		t.Errorf("expected type=%s, got %s", contentTypeText, part.Type)
	}
	if part.Text != testHelloWorld {
		t.Errorf("expected text='Hello, world!', got %s", part.Text)
	}
	if part.Image != nil {
		t.Error("expected image to be nil")
	}
}

func TestNewImageURLPart(t *testing.T) {
	part := NewImageURLPart("https://example.com/image.jpg")

	if part.Type != modelTypeImage {
		t.Errorf("expected type=%s, got %s", modelTypeImage, part.Type)
	}
	if part.Image == nil {
		t.Fatal("expected image to be non-nil")
	}
	if part.Image.Source != testImageSource {
		t.Errorf("expected source=%s, got %s", testImageSource, part.Image.Source)
	}
	if part.Image.Data != "https://example.com/image.jpg" {
		t.Errorf("unexpected URL: %s", part.Image.Data)
	}
}

func TestNewImageBase64Part(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("fake-image-data"))
	part := NewImageBase64Part(data, MediaTypePNG)

	if part.Type != modelTypeImage {
		t.Errorf("expected type=%s, got %s", modelTypeImage, part.Type)
	}
	if part.Image == nil {
		t.Fatal("expected image to be non-nil")
	}
	if part.Image.Source != contentTypeBase64 {
		t.Errorf("expected source=%s, got %s", contentTypeBase64, part.Image.Source)
	}
	if part.Image.MediaType != MediaTypePNG {
		t.Errorf("expected media_type=%s, got %s", MediaTypePNG, part.Image.MediaType)
	}
	if part.Image.Data != data {
		t.Errorf("data mismatch")
	}
}

func TestNewImageFromBytes(t *testing.T) {
	data := []byte("fake-png-data")
	part := NewImageFromBytes(data, MediaTypePNG)

	if part.Type != modelTypeImage {
		t.Errorf("expected type=%s, got %s", modelTypeImage, part.Type)
	}
	if part.Image == nil {
		t.Fatal("expected image to be non-nil")
	}
	if part.Image.Source != contentTypeBase64 {
		t.Errorf("expected source=%s, got %s", contentTypeBase64, part.Image.Source)
	}

	// Verify the data can be decoded
	decoded, err := base64.StdEncoding.DecodeString(part.Image.Data)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}
	if string(decoded) != string(data) {
		t.Error("decoded data mismatch")
	}
}

func TestNewMultiPartMessage(t *testing.T) {
	msg := NewMultiPartMessage(RoleUser,
		NewTextPart("What's in this image?"),
		NewImageURLPart("https://example.com/cat.jpg"),
	)

	if msg.Role != RoleUser {
		t.Errorf("expected role=user, got %s", msg.Role)
	}
	if len(msg.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(msg.Parts))
	}
	if msg.Parts[0].Type != contentTypeText {
		t.Errorf("expected first part to be %s", contentTypeText)
	}
	if msg.Parts[1].Type != modelTypeImage {
		t.Errorf("expected second part to be %s", modelTypeImage)
	}
}

func TestNewImageMessage(t *testing.T) {
	msg := NewImageMessage("Describe this", "https://example.com/photo.png")

	if msg.Role != RoleUser {
		t.Errorf("expected role=user, got %s", msg.Role)
	}
	if len(msg.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(msg.Parts))
	}
	if msg.Parts[0].Text != "Describe this" {
		t.Errorf("unexpected text: %s", msg.Parts[0].Text)
	}
	if msg.Parts[1].Image == nil || msg.Parts[1].Image.Data != "https://example.com/photo.png" {
		t.Error("unexpected image content")
	}
}

func TestMessage_HasParts(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		expected bool
	}{
		{
			name:     "simple content",
			msg:      Message{Role: RoleUser, Content: "Hello"},
			expected: false,
		},
		{
			name:     "with parts",
			msg:      NewMultiPartMessage(RoleUser, NewTextPart("Hello")),
			expected: true,
		},
		{
			name:     "empty",
			msg:      Message{Role: RoleUser},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.HasParts(); got != tc.expected {
				t.Errorf("HasParts() = %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestMessage_Text(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		expected string
	}{
		{
			name:     "simple content",
			msg:      Message{Role: RoleUser, Content: "Hello"},
			expected: "Hello",
		},
		{
			name: "parts with text",
			msg: NewMultiPartMessage(RoleUser,
				NewTextPart("Line 1"),
				NewTextPart("Line 2"),
			),
			expected: "Line 1\nLine 2",
		},
		{
			name: "parts with mixed content",
			msg: NewMultiPartMessage(RoleUser,
				NewTextPart("Text"),
				NewImageURLPart("https://example.com/img.jpg"),
			),
			expected: "Text",
		},
		{
			name:     "empty",
			msg:      Message{Role: RoleUser},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.Text(); got != tc.expected {
				t.Errorf("Text() = %q, expected %q", got, tc.expected)
			}
		})
	}
}

func TestMessage_HasImages(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		expected bool
	}{
		{
			name:     "simple content",
			msg:      Message{Role: RoleUser, Content: "Hello"},
			expected: false,
		},
		{
			name:     "text parts only",
			msg:      NewMultiPartMessage(RoleUser, NewTextPart("Hello")),
			expected: false,
		},
		{
			name: "with image",
			msg: NewMultiPartMessage(RoleUser,
				NewTextPart("Check this"),
				NewImageURLPart("https://example.com/img.jpg"),
			),
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.HasImages(); got != tc.expected {
				t.Errorf("HasImages() = %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestMessage_Images(t *testing.T) {
	msg := NewMultiPartMessage(RoleUser,
		NewTextPart("Check these"),
		NewImageURLPart("https://example.com/img1.jpg"),
		NewImageURLPart("https://example.com/img2.jpg"),
	)

	images := msg.Images()
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if images[0].Data != "https://example.com/img1.jpg" {
		t.Errorf("unexpected first image URL")
	}
	if images[1].Data != "https://example.com/img2.jpg" {
		t.Errorf("unexpected second image URL")
	}
}

func TestValidateImageContent(t *testing.T) {
	tests := []struct {
		name    string
		img     *ImageContent
		wantErr bool
	}{
		{
			name:    "nil image",
			img:     nil,
			wantErr: true,
		},
		{
			name:    "invalid source",
			img:     &ImageContent{Source: "invalid", Data: "data"},
			wantErr: true,
		},
		{
			name:    "empty data",
			img:     &ImageContent{Source: testImageSource, Data: ""},
			wantErr: true,
		},
		{
			name:    "valid url",
			img:     &ImageContent{Source: testImageSource, Data: "https://example.com/img.jpg"},
			wantErr: false,
		},
		{
			name:    "invalid url",
			img:     &ImageContent{Source: testImageSource, Data: "not-a-url"},
			wantErr: true,
		},
		{
			name:    "base64 missing media type",
			img:     &ImageContent{Source: contentTypeBase64, Data: "SGVsbG8="},
			wantErr: true,
		},
		{
			name:    "base64 invalid media type",
			img:     &ImageContent{Source: contentTypeBase64, MediaType: "text/plain", Data: "SGVsbG8="},
			wantErr: true,
		},
		{
			name:    "base64 invalid encoding",
			img:     &ImageContent{Source: contentTypeBase64, MediaType: MediaTypePNG, Data: "not-base64!!!"},
			wantErr: true,
		},
		{
			name:    "valid base64",
			img:     &ImageContent{Source: contentTypeBase64, MediaType: MediaTypePNG, Data: "SGVsbG8="},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateImageContent(tc.img)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateImageContent() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestDetectMediaType(t *testing.T) {
	// PNG magic bytes
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

	// JPEG magic bytes
	jpegData := []byte{0xFF, 0xD8, 0xFF}

	tests := []struct {
		path     string
		data     []byte
		expected string
	}{
		{"image.png", nil, MediaTypePNG},
		{"image.jpg", nil, MediaTypeJPEG},
		{"image.jpeg", nil, MediaTypeJPEG},
		{"image.gif", nil, MediaTypeGIF},
		{"image.webp", nil, MediaTypeWebP},
		{"unknown.xyz", nil, ""},
		{"test.png", pngData, "image/png"},
		{"test.jpg", jpegData, "image/jpeg"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := detectMediaType(tc.path, tc.data)
			if result != tc.expected {
				t.Errorf("detectMediaType(%s) = %s, expected %s", tc.path, result, tc.expected)
			}
		})
	}
}
