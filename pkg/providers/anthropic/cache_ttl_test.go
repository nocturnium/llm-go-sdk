package anthropic

import (
	"testing"
	"time"
)

// TestCacheControlTTL verifies the Anthropic cache_control "ttl" rendering: 1h or
// longer maps to "1h", anything shorter uses the default ephemeral cache ("").
// (Relocated from the root package's AnthropicTTL in v5.)
func TestCacheControlTTL(t *testing.T) {
	cases := map[time.Duration]string{
		0:                "",
		30 * time.Minute: "",
		time.Hour:        "1h",
		2 * time.Hour:    "1h",
	}
	for ttl, want := range cases {
		if got := cacheControlTTL(ttl); got != want {
			t.Errorf("cacheControlTTL(%v) = %q, want %q", ttl, got, want)
		}
	}
}
