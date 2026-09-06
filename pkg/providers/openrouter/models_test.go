package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestListModels(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Error(r.URL)
		}
		fmt.Fprint(w, `{"data":[{"id":"google/chat","name":"Chat","context_length":100,"created":123,"architecture":{"input_modalities":["image"],"output_modalities":["text"]},"pricing":{"prompt":"0.1"}},{"id":"image","architecture":{"output_modalities":["image","audio","speech","video"]}}]}`)
	})
	ctx := context.Background()
	out, err := c.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Models) != 2 || out.Models[0].DisplayName != "Chat" || out.Models[0].ContextLength != 100 || out.Models[0].CreatedAt.Unix() != 123 || out.Models[0].FromCache || !out.Models[0].IsVision() || !out.Models[0].IsChat() || out.Models[1].DisplayName != "image" {
		t.Fatal(out)
	}
	for _, opts := range [][]llms.ListModelsOption{{llms.WithModelLimit(1)}, {llms.WithModelTypes(llms.ModelTypeImage)}} {
		out, err := c.ListModels(ctx, opts...)
		if err != nil || len(out.Models) != 1 {
			t.Fatal(out, err)
		}
	}
	for _, opts := range [][]llms.ListModelsOption{{llms.WithModelLimit(-1)}, {llms.WithModelCursor("cursor")}} {
		if _, err := c.ListModels(ctx, opts...); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatal(err)
		}
	}
	info, err := c.ModelInfo(ctx, "GOOGLE/CHAT")
	if err != nil || info.ID != "google/chat" {
		t.Fatal(info, err)
	}
	if _, err := c.ModelInfo(ctx, "missing"); !errors.Is(err, llms.ErrModelNotFound) {
		t.Fatal(err)
	}
}
func TestMediaDiscovery(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/images/models":
			fmt.Fprint(w, `{"data":[{"id":"image","name":"Image","architecture":{"input_modalities":["text"],"output_modalities":["image"]},"supported_parameters":{"resolution":{"values":["1K"]},"aspect_ratio":{"values":["1:1"]},"n":{"min":1,"max":10},"input_references":{"min":0,"max":2}},"supports_streaming":true,"endpoints":"/api/v1/images/models/image/endpoints"}]}`)
		case "/api/v1/videos/models":
			fmt.Fprint(w, `{"data":[{"id":"video","name":"Video","pricing_skus":{"duration_seconds":"0.05"},"supported_sizes":["1280x720"],"supported_resolutions":["480p"],"supported_aspect_ratios":["16:9"],"supported_durations":[1,5],"supported_frame_images":["first_frame"]}]}`)
		default:
			t.Error(r.URL)
		}
	})
	images, err := c.ListImageModels(context.Background())
	if err != nil || len(images) != 1 {
		t.Fatal(images, err)
	}
	if images[0].SupportedParameters.N.Max != 10 || images[0].SupportedParameters.Resolution.Values[0] != "1K" || !images[0].SupportsStreaming || images[0].Endpoints != "/api/v1/images/models/image/endpoints" {
		t.Fatal(images)
	}
	videos, err := c.ListVideoModels(context.Background())
	if err != nil || len(videos) != 1 || videos[0].SupportedDurations[0] != 1 || videos[0].SupportedFrameImages[0] != "first_frame" || videos[0].Name != "Video" || videos[0].PricingSKUs["duration_seconds"] != "0.05" || videos[0].SupportedSizes[0] != "1280x720" {
		t.Fatal(videos, err)
	}
}

func TestListSpeechModels(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" || r.URL.RawQuery != "output_modalities=speech" {
			t.Error(r.URL)
		}
		fmt.Fprint(w, `{"data":[{"id":"fish-audio/s2.1-pro","pricing":{"input_char":"0.000015"}},{"id":"hexgrad/kokoro-82m"}]}`)
	})
	models, err := c.ListSpeechModels(context.Background())
	if err != nil || len(models) != 2 {
		t.Fatal(models, err)
	}
	if models[0].ID != DefaultSpeechModel || models[0].Pricing == nil || models[0].Pricing.InputChar == nil || *models[0].Pricing.InputChar != "0.000015" || models[1].Pricing != nil {
		t.Fatal(models)
	}
}
