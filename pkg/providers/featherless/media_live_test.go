//go:build integration

package featherless

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// TestLiveMedia_Cheapest is opt-in through the provider-specific API key.
// Documentation verification is not a claim that this route was live-tested.
func TestLiveMedia_Cheapest(t *testing.T) {
	key := os.Getenv(llms.EnvFeatherlessAPIKey)
	if key == "" {
		t.Skip("provider-specific API key is not set")
	}
	c, err := New(WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := c.Synthesize(ctx, "Hello.")
	if errors.Is(err, llms.ErrQuotaExceeded) || errors.Is(err, llms.ErrPlanRequired) {
		t.Skipf("media quota/plan unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Audio.Data) == 0 {
		t.Fatal("missing generated speech")
	}
}
