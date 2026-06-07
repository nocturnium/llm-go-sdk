package openaicompat_test

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

// TestBaseProvider_Capabilities_StaticWinsOverRegistry verifies the capability
// MERGE: a provider that statically declares Vision:true must keep reporting
// Vision:true even when the registry has an entry that says Vision:false. The
// registry must only fill fields the static config leaves at their zero value;
// it must never downgrade an explicitly declared capability.
func TestBaseProvider_Capabilities_StaticWinsOverRegistry(t *testing.T) {
	const (
		provider = llms.Provider("capmerge-test-provider")
		model    = "capmerge-test-model"
	)

	// Registry entry intentionally contradicts the static config: Vision:false,
	// and Tools:true (which the static config does NOT set, so it should be
	// filled from the registry).
	llms.RegisterModelCapabilities(provider, model, llms.ModelCapabilities{
		MaxContextTokens:  64000,
		SupportsVision:    false, // contradicts static Vision:true
		SupportsTools:     true,  // static leaves Tools unset -> registry fills it
		SupportsStreaming: true,
	})

	cfg := openaicompat.ProviderConfig{
		Provider:     provider,
		ProviderName: "capmerge-test",
		DefaultModel: model,
		Capabilities: llms.Capabilities{
			Vision:          true, // explicit declaration must win
			MaxOutputTokens: 4096, // non-zero static int must win
		},
	}

	bp := openaicompat.NewBaseProvider(nil, cfg)
	caps := bp.Capabilities()

	if !caps.Vision {
		t.Error("Vision = false, want true (static Vision:true must win over registry Vision:false)")
	}
	if !caps.Tools {
		t.Error("Tools = false, want true (registry should fill the zero-valued static Tools)")
	}
	if !caps.Streaming {
		t.Error("Streaming = false, want true (from registry)")
	}
	if caps.MaxContextTokens != 64000 {
		t.Errorf("MaxContextTokens = %d, want 64000 (from registry, static left it zero)", caps.MaxContextTokens)
	}
	if caps.MaxOutputTokens != 4096 {
		t.Errorf("MaxOutputTokens = %d, want 4096 (non-zero static value must win)", caps.MaxOutputTokens)
	}
}

// TestBaseProvider_Capabilities_StaticOnly verifies that when the registry has
// no entry for the model, the static config is reported as-is.
func TestBaseProvider_Capabilities_StaticOnly(t *testing.T) {
	cfg := openaicompat.ProviderConfig{
		Provider:     llms.Provider("capmerge-unregistered-provider"),
		ProviderName: "capmerge-unregistered",
		DefaultModel: "totally-unknown-model",
		Capabilities: llms.Capabilities{
			Streaming:        true,
			Vision:           true,
			JSONMode:         true,
			Embeddings:       true,
			MaxContextTokens: 8192,
		},
	}

	bp := openaicompat.NewBaseProvider(nil, cfg)
	caps := bp.Capabilities()

	if !caps.Vision {
		t.Error("Vision = false, want true (static config)")
	}
	if !caps.Streaming {
		t.Error("Streaming = false, want true (static config)")
	}
	if !caps.Embeddings {
		t.Error("Embeddings = false, want true (explicit static declaration honored)")
	}
	if caps.MaxContextTokens != 8192 {
		t.Errorf("MaxContextTokens = %d, want 8192 (static config)", caps.MaxContextTokens)
	}
}
