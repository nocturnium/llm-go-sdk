package fal

import (
	"context"
	"errors"
	"reflect"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func imageFixture(f *fakeQueue, count int, nsfw []bool) {
	images := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		images = append(images, map[string]any{"url": f.assetURL("img.png"), "width": 1024, "height": 768, "content_type": "image/png"})
	}
	f.result = map[string]any{"images": images, "seed": 42, "has_nsfw_concepts": nsfw, "prompt": "p", "timings": map[string]any{"inference": 0.5}}
}
func TestImageBody(t *testing.T) {
	seed := int64(7)
	body, err := imageBody("cat", &llms.ImageOptions{Size: "512x256", AspectRatio: "16:9", N: 2, Seed: &seed, OutputFormat: "PNG", Extra: map[string]any{"guidance_scale": 3.5, "acceleration": "high"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"prompt": "cat", "image_size": map[string]any{"width": 512, "height": 256}, "num_images": 2, "seed": int64(7), "output_format": "png", "guidance_scale": 3.5, "acceleration": "high"}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body: %v", body)
	}
	for ratio, preset := range aspectRatios {
		body, err = imageBody("cat", &llms.ImageOptions{AspectRatio: ratio, OutputFormat: "jpg"})
		if err != nil || body["image_size"] != preset || body["output_format"] != "jpeg" {
			t.Fatalf("%s: %v %v", ratio, err, body)
		}
	}
	body, err = imageBody("cat", &llms.ImageOptions{})
	if err != nil || len(body) != 1 || body["prompt"] != "cat" {
		t.Fatalf("minimal: %v %v", err, body)
	}
	if _, err = imageBody(" ", &llms.ImageOptions{}); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	tolerance := 1
	invalidCases := map[string]*llms.ImageOptions{
		"size shape":       {Size: "512"},
		"size zero":        {Size: "0x10"},
		"size text":        {Size: "wxh"},
		"aspect":           {AspectRatio: "2:1"},
		"count":            {N: -1},
		"format":           {OutputFormat: "webp"},
		"negative":         {NegativePrompt: "x"},
		"quality":          {Quality: "hd"},
		"safety":           {SafetyTolerance: &tolerance},
		"reserved prompt":  {Extra: map[string]any{"prompt": "x"}},
		"reserved size":    {Extra: map[string]any{"image_size": "square"}},
		"reserved count":   {Extra: map[string]any{"num_images": 1}},
		"reserved seed":    {Extra: map[string]any{"seed": 1}},
		"reserved format":  {Extra: map[string]any{"output_format": "png"}},
		"reserved (mixed)": {Size: "1x1", Extra: map[string]any{"seed": 1}},
	}
	for name, o := range invalidCases {
		if _, err = imageBody("cat", o); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
func TestGenerateImage(t *testing.T) {
	f := newFakeQueue(t)
	f.statuses = []map[string]any{{"status": "IN_QUEUE", "queue_position": 1}, {"status": "COMPLETED", "metrics": map[string]any{"inference_time": 0.4}}}
	f.resultHeader = map[string]string{"X-Fal-Billable-Units": "2"}
	f.asset, f.assetType = []byte("png-bytes"), "image/png"
	imageFixture(f, 2, []bool{false, false})
	out, err := f.client().GenerateImage(context.Background(), "cat", llms.WithImageCount(2), llms.WithImageAspectRatio("1:1"))
	if err != nil {
		t.Fatal(err)
	}
	rec := f.lastSubmit()
	if rec.Path != "/fal-ai/flux/schnell" || rec.Body["num_images"] != 2.0 || rec.Body["image_size"] != "square_hd" {
		t.Fatalf("submit: %+v", rec)
	}
	if len(out.Images) != 2 || string(out.Images[0].Data) != "png-bytes" || out.Images[0].MIMEType != "image/png" || out.Images[0].URL != f.assetURL("img.png") || *out.Images[1].Seed != 42 || !out.Images[0].ExpiresAt.IsZero() {
		t.Fatalf("images: %+v", out.Images)
	}
	if out.Model != DefaultImageModel || out.Usage.Unit != llms.MediaUnitMegapixel || out.Usage.Quantity != 2*1024*768/1e6 || out.Usage.Cost != nil {
		t.Fatalf("usage: %+v %s", out.Usage, out.Model)
	}
	if out.Metadata["request_id"] != "req-1" || out.Metadata["billable_units"] != 2.0 {
		t.Fatalf("metadata: %v", out.Metadata)
	}
	if _, ok := out.Metadata["nsfw_indices"]; ok {
		t.Fatal("unexpected nsfw indices")
	}
}
func TestGenerateImageModelOverrideAndUnknownDimensions(t *testing.T) {
	f := newFakeQueue(t)
	f.result = map[string]any{"images": []map[string]any{{"url": f.assetURL("x"), "content_type": "image/jpeg"}}}
	out, err := f.client(WithImageModel("fal-ai/other")).GenerateImage(context.Background(), "cat", llms.WithImageModel("fal-ai/flux/dev"))
	if err != nil {
		t.Fatal(err)
	}
	if f.lastSubmit().Path != "/fal-ai/flux/dev" || out.Model != "fal-ai/flux/dev" {
		t.Fatalf("model: %+v", out)
	}
	if out.Usage.Unit != llms.MediaUnitMegapixel || out.Usage.Quantity != 0 || out.Images[0].Seed != nil {
		t.Fatalf("usage: %+v", out.Usage)
	}
}
func TestGenerateImageNSFW(t *testing.T) {
	f := newFakeQueue(t)
	imageFixture(f, 2, []bool{true, true})
	_, err := f.client().GenerateImage(context.Background(), "cat")
	var m *llms.ModerationError
	if !errors.As(err, &m) || m.Stage != llms.ModerationOutput || m.Reasons[0] != "has_nsfw_concepts" || m.Provider != "fal" || !errors.Is(err, llms.ErrContentFiltered) {
		t.Fatalf("all flagged: %v", err)
	}
	imageFixture(f, 3, []bool{true, false, true})
	out, err := f.client().GenerateImage(context.Background(), "cat")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Images) != 1 || !reflect.DeepEqual(out.Metadata["nsfw_indices"], []int{0, 2}) || out.Usage.Quantity != 3*1024*768/1e6 {
		t.Fatalf("partial: %+v %v %+v", out.Images, out.Metadata, out.Usage)
	}
}
func TestGenerateImageErrors(t *testing.T) {
	f := newFakeQueue(t)
	c := f.client()
	if _, err := c.GenerateImage(context.Background(), ""); !errors.Is(err, llms.ErrEmptyPrompt) {
		t.Fatal(err)
	}
	f.submitStatus, f.submitBody = 422, map[string]any{"detail": []map[string]any{{"msg": "bad prompt", "type": "content_policy_violation"}}}
	var m *llms.ModerationError
	if _, err := c.GenerateImage(context.Background(), "cat"); !errors.As(err, &m) || m.Stage != llms.ModerationInput {
		t.Fatal(err)
	}
	f.submitStatus = 0
	f.statuses = []map[string]any{{"status": "COMPLETED", "error": "blank", "error_type": "no_media_generated"}}
	if _, err := c.GenerateImage(context.Background(), "cat"); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatal(err)
	}
	f.statuses = nil
	f.resultStatus, f.result = 422, map[string]any{"detail": []map[string]any{{"msg": "blank", "type": "no_media_generated"}}}
	if _, err := c.GenerateImage(context.Background(), "cat"); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatal(err)
	}
	f.resultStatus, f.result = 0, map[string]any{"images": []any{}}
	if _, err := c.GenerateImage(context.Background(), "cat"); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatal(err)
	}
	imageFixture(f, 1, nil)
	f.assetStatus = 500
	if _, err := c.GenerateImage(context.Background(), "cat"); !errors.Is(err, llms.ErrServiceUnavailable) {
		t.Fatal(err)
	}
}
