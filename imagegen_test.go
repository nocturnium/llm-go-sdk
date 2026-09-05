package llms

import (
	"reflect"
	"testing"
)

func testImageSeedPointer() *int64 { value := int64(7); return &value }

func testImageSafetyTolerancePointer() *int { value := 3; return &value }

func TestApplyImageOptions(t *testing.T) {
	if got := ApplyImageOptions(); !reflect.DeepEqual(got, &ImageOptions{}) {
		t.Fatalf("defaults: %+v", got)
	}
	got := ApplyImageOptions(WithImageModel("Model"), WithImageSize("Size"), WithImageAspectRatio("AspectRatio"), WithImageCount(3), WithImageSeed(int64(7)), WithImageNegativePrompt("NegativePrompt"), WithImageQuality("Quality"), WithImageFormat("OutputFormat"), WithImageSafetyTolerance(3), WithImageExtra(map[string]any{"custom": true}))
	want := &ImageOptions{Model: "Model", Size: "Size", AspectRatio: "AspectRatio", N: 3, Seed: testImageSeedPointer(), NegativePrompt: "NegativePrompt", Quality: "Quality", OutputFormat: "OutputFormat", SafetyTolerance: testImageSafetyTolerancePointer(), Extra: map[string]any{"custom": true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Embedding an interface provides a capability-only test double; no call is made.
type imageCapabilityStub struct {
	ImageGenerator
	ImageEditor
}

func TestImageCapabilities(t *testing.T) {
	for _, value := range []any{nil, 42, imageCapabilityStub{}} {
		_, want := value.(imageCapabilityStub)
		if SupportsImageGeneration(value) != want || SupportsImageEdit(value) != want {
			t.Fatal("incorrect capability assertion")
		}
		g, ok := AsImageGenerator(value)
		if ok != want || (g == nil) == want {
			t.Fatal("incorrect image generator cast")
		}
		e, ok := AsImageEditor(value)
		if ok != want || (e == nil) == want {
			t.Fatal("incorrect image editor cast")
		}
	}
}
