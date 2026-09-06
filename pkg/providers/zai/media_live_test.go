//go:build integration

package zai

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// TestLiveMedia_Cheapest is opt-in through the provider-specific API key.
// Documentation verification is not a claim that this route was live-tested.
// Workspace .env uses ZAI_TOKEN; export ZAI_API_KEY for this SDK/test gate.
func TestLiveMedia_Cheapest(t *testing.T) {
	key := os.Getenv(llms.EnvZAIAPIKey)
	if key == "" {
		t.Skip("provider-specific API key is not set")
	}
	c, err := New(WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := c.GenerateImage(ctx, "A small blue circle on a white background")
	if errors.Is(err, llms.ErrQuotaExceeded) || errors.Is(err, llms.ErrPlanRequired) || (err != nil && strings.Contains(err.Error(), "1113")) {
		t.Skipf("media quota/plan unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Images) != 1 || len(out.Images[0].Data) == 0 {
		t.Fatal("missing generated image")
	}
}
