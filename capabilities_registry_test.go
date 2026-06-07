package llms

import (
	"sync"
	"testing"
)

func TestCapabilityRegistry_Get(t *testing.T) {
	r := NewCapabilityRegistry()

	t.Run("returns registered model capabilities", func(t *testing.T) {
		caps := r.Get(ProviderOpenAI, "gpt-4o")
		if caps.MaxContextTokens != 128000 {
			t.Errorf("got MaxContextTokens=%d, want 128000", caps.MaxContextTokens)
		}
		if !caps.SupportsVision {
			t.Error("expected SupportsVision=true")
		}
	})

	t.Run("returns provider defaults for unknown model", func(t *testing.T) {
		caps := r.Get(ProviderOpenAI, "unknown-model-xyz")
		if caps.MaxContextTokens != 128000 { // OpenAI default
			t.Errorf("got MaxContextTokens=%d, want 128000 (default)", caps.MaxContextTokens)
		}
	})

	t.Run("returns zero values for unknown provider", func(t *testing.T) {
		caps := r.Get("unknown-provider", "unknown-model")
		if caps.MaxContextTokens != 0 {
			t.Errorf("got MaxContextTokens=%d, want 0", caps.MaxContextTokens)
		}
	})

	t.Run("different models have different capabilities", func(t *testing.T) {
		gpt4o := r.Get(ProviderOpenAI, "gpt-4o")
		gpt4 := r.Get(ProviderOpenAI, "gpt-4")

		if gpt4o.MaxContextTokens == gpt4.MaxContextTokens {
			t.Error("gpt-4o and gpt-4 should have different context lengths")
		}
		if gpt4.SupportsVision {
			t.Error("gpt-4 should not support vision")
		}
		if !gpt4o.SupportsVision {
			t.Error("gpt-4o should support vision")
		}
	})
}

func TestCapabilityRegistry_Register(t *testing.T) {
	r := NewCapabilityRegistry()

	t.Run("registers custom capabilities", func(t *testing.T) {
		r.Register(ProviderOpenAI, "custom-model", ModelCapabilities{
			MaxContextTokens: 999999,
			SupportsVision:   true,
		})

		caps := r.Get(ProviderOpenAI, "custom-model")
		if caps.MaxContextTokens != 999999 {
			t.Errorf("got MaxContextTokens=%d, want 999999", caps.MaxContextTokens)
		}
	})

	t.Run("overwrites existing capabilities", func(t *testing.T) {
		r.Register(ProviderOpenAI, "gpt-4o", ModelCapabilities{
			MaxContextTokens: 1,
		})

		caps := r.Get(ProviderOpenAI, "gpt-4o")
		if caps.MaxContextTokens != 1 {
			t.Errorf("got MaxContextTokens=%d, want 1", caps.MaxContextTokens)
		}
	})
}

func TestCapabilityRegistry_RegisterDefault(t *testing.T) {
	r := NewCapabilityRegistry()

	t.Run("sets provider defaults", func(t *testing.T) {
		r.RegisterDefault("test-provider", ModelCapabilities{
			MaxContextTokens: 50000,
			SupportsTools:    true,
		})

		caps := r.Get("test-provider", "any-model")
		if caps.MaxContextTokens != 50000 {
			t.Errorf("got MaxContextTokens=%d, want 50000", caps.MaxContextTokens)
		}
	})
}

func TestCapabilityRegistry_GetWithDefaults(t *testing.T) {
	r := NewCapabilityRegistry()

	t.Run("applies defaults for zero values", func(t *testing.T) {
		r.Register("test", "partial", ModelCapabilities{
			MaxContextTokens: 100, // Set this
			// MaxOutputTokens is zero
		})

		defaults := ModelCapabilities{
			MaxContextTokens: 999,
			MaxOutputTokens:  888,
		}

		caps := r.GetWithDefaults("test", "partial", defaults)
		if caps.MaxContextTokens != 100 {
			t.Errorf("got MaxContextTokens=%d, want 100 (from registry)", caps.MaxContextTokens)
		}
		if caps.MaxOutputTokens != 888 {
			t.Errorf("got MaxOutputTokens=%d, want 888 (from defaults)", caps.MaxOutputTokens)
		}
	})
}

func TestCapabilityRegistry_Concurrent(_ *testing.T) {
	r := NewCapabilityRegistry()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Concurrent reads
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = r.Get(ProviderOpenAI, "gpt-4o")
			_ = r.Get(ProviderAnthropic, "claude-3-opus-20240229")
		}()
	}

	// Concurrent writes
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			r.Register(ProviderOpenAI, "concurrent-model", ModelCapabilities{
				MaxContextTokens: idx,
			})
		}(i)
	}

	wg.Wait()
}

func TestModelCapabilities_ToCapabilities(t *testing.T) {
	mc := ModelCapabilities{
		MaxContextTokens:  100000,
		MaxOutputTokens:   8000,
		SupportsVision:    true,
		SupportsTools:     true,
		SupportsStreaming: true,
		SupportsJSON:      true,
	}

	caps := mc.ToCapabilities()

	if caps.MaxContextTokens != 100000 {
		t.Errorf("got MaxContextTokens=%d, want 100000", caps.MaxContextTokens)
	}
	if caps.MaxOutputTokens != 8000 {
		t.Errorf("got MaxOutputTokens=%d, want 8000", caps.MaxOutputTokens)
	}
	if !caps.Vision {
		t.Error("expected Vision=true")
	}
	if !caps.Tools {
		t.Error("expected Tools=true")
	}
	if !caps.Streaming {
		t.Error("expected Streaming=true")
	}
	if !caps.JSONMode {
		t.Error("expected JSONMode=true")
	}
}

func TestGetModelCapabilities(t *testing.T) {
	// Test global registry access
	caps := GetModelCapabilities(ProviderOpenAI, "gpt-4o")
	if caps.MaxContextTokens == 0 {
		t.Error("expected non-zero MaxContextTokens from global registry")
	}
}

func TestDefaultCapabilityRegistry(t *testing.T) {
	// Verify global registry has defaults
	r := DefaultCapabilityRegistry()
	if r == nil {
		t.Fatal("DefaultCapabilityRegistry() returned nil")
	}

	// Check some known defaults exist
	providers := []Provider{ProviderOpenAI, ProviderAnthropic, ProviderGemini}
	for _, p := range providers {
		caps := r.Get(p, "any-model")
		if caps.MaxContextTokens == 0 {
			t.Errorf("provider %s should have default MaxContextTokens", p)
		}
	}
}

func TestCapabilityRegistry_AllProviderDefaults(t *testing.T) {
	r := NewCapabilityRegistry()

	providers := []Provider{
		ProviderOpenAI,
		ProviderTogetherAI,
		ProviderAnthropic,
		ProviderGemini,
		ProviderFeatherless,
		ProviderSynthetic,
		ProviderGroq,
		ProviderFireworks,
		ProviderPerplexity,
		ProviderMistral,
		ProviderDeepSeek,
		ProviderCerebras,
		ProviderOllama,
		ProviderAzure,
		ProviderInfinity,
		ProviderRunPod,
		ProviderZAI,
		ProviderLlamaCpp,
	}

	for _, provider := range providers {
		t.Run(string(provider), func(t *testing.T) {
			caps := r.Get(provider, "unknown-model")
			if caps.MaxContextTokens == 0 {
				t.Errorf("provider %s should have non-zero MaxContextTokens", provider)
			}
			// Infinity is an embeddings/reranking service, not a chat generator, so
			// MaxOutputTokens may remain zero while the input limit is still known.
			if provider != ProviderInfinity && caps.MaxOutputTokens == 0 {
				t.Errorf("provider %s should have non-zero MaxOutputTokens", provider)
			}
		})
	}
}

func TestCapabilityRegistry_CurrentGenerationModelDifferences(t *testing.T) {
	r := NewCapabilityRegistry()

	t.Run("openai reasoning model differs from legacy gpt-4", func(t *testing.T) {
		o3 := r.Get(ProviderOpenAI, "o3")
		gpt4 := r.Get(ProviderOpenAI, "gpt-4")

		if o3.MaxOutputTokens <= gpt4.MaxOutputTokens {
			t.Errorf("o3 MaxOutputTokens=%d, want greater than gpt-4 MaxOutputTokens=%d", o3.MaxOutputTokens, gpt4.MaxOutputTokens)
		}
		if !o3.SupportsVision {
			t.Error("expected o3 SupportsVision=true")
		}
		if gpt4.SupportsVision {
			t.Error("expected gpt-4 SupportsVision=false")
		}
	})

	t.Run("anthropic sonnet 4 differs from haiku", func(t *testing.T) {
		sonnet4 := r.Get(ProviderAnthropic, "claude-sonnet-4")
		haiku := r.Get(ProviderAnthropic, "claude-3-5-haiku-20241022")

		if sonnet4.MaxOutputTokens <= haiku.MaxOutputTokens {
			t.Errorf("claude-sonnet-4 MaxOutputTokens=%d, want greater than haiku MaxOutputTokens=%d", sonnet4.MaxOutputTokens, haiku.MaxOutputTokens)
		}
		if !sonnet4.SupportsVision || !sonnet4.SupportsTools {
			t.Error("expected claude-sonnet-4 to support vision and tools")
		}
	})

	t.Run("gemini 2.5 model has larger output window than 2.0 flash", func(t *testing.T) {
		flash25 := r.Get(ProviderGemini, "gemini-2.5-flash")
		flash20 := r.Get(ProviderGemini, "gemini-2.0-flash")

		if flash25.MaxOutputTokens <= flash20.MaxOutputTokens {
			t.Errorf("gemini-2.5-flash MaxOutputTokens=%d, want greater than gemini-2.0-flash MaxOutputTokens=%d", flash25.MaxOutputTokens, flash20.MaxOutputTokens)
		}
		if !flash25.SupportsVision || !flash25.SupportsTools || !flash25.SupportsJSON {
			t.Error("expected gemini-2.5-flash to support vision, tools, and JSON")
		}
	})
}

func TestCapabilityRegistry_InfinityDefaultIsNotChatLLM(t *testing.T) {
	r := NewCapabilityRegistry()

	caps := r.Get(ProviderInfinity, "any-embedding-model")
	if caps.MaxContextTokens == 0 {
		t.Fatal("expected Infinity to have non-zero MaxContextTokens")
	}
	if caps.SupportsTools {
		t.Error("expected Infinity SupportsTools=false")
	}
	if caps.SupportsStreaming {
		t.Error("expected Infinity SupportsStreaming=false")
	}
	if caps.SupportsJSON {
		t.Error("expected Infinity SupportsJSON=false")
	}
	if caps.SupportsVision {
		t.Error("expected Infinity SupportsVision=false")
	}
}

func TestCapabilityRegistry_CaseInsensitive(t *testing.T) {
	r := NewCapabilityRegistry()

	// Register with specific case
	r.Register(ProviderOpenAI, "My-Custom-Model", ModelCapabilities{
		MaxContextTokens: 12345,
	})

	t.Run("exact match works", func(t *testing.T) {
		caps := r.Get(ProviderOpenAI, "My-Custom-Model")
		if caps.MaxContextTokens != 12345 {
			t.Errorf("got %d, want 12345", caps.MaxContextTokens)
		}
	})

	t.Run("lowercase match works", func(t *testing.T) {
		// First register lowercase version
		r.Register(ProviderOpenAI, "my-custom-model", ModelCapabilities{
			MaxContextTokens: 12345,
		})
		caps := r.Get(ProviderOpenAI, "my-custom-model")
		if caps.MaxContextTokens != 12345 {
			t.Errorf("got %d, want 12345", caps.MaxContextTokens)
		}
	})
}

func TestAnthropicModels(t *testing.T) {
	r := DefaultCapabilityRegistry()

	models := []struct {
		id          string
		wantContext int
		wantOutput  int
		wantVision  bool
	}{
		{"claude-3-5-sonnet-20241022", 200000, 8192, true},
		{"claude-3-opus-20240229", 200000, 4096, true},
		{"claude-3-haiku-20240307", 200000, 4096, true},
	}

	for _, m := range models {
		t.Run(m.id, func(t *testing.T) {
			caps := r.Get(ProviderAnthropic, m.id)
			if caps.MaxContextTokens != m.wantContext {
				t.Errorf("MaxContextTokens=%d, want %d", caps.MaxContextTokens, m.wantContext)
			}
			if caps.MaxOutputTokens != m.wantOutput {
				t.Errorf("MaxOutputTokens=%d, want %d", caps.MaxOutputTokens, m.wantOutput)
			}
			if caps.SupportsVision != m.wantVision {
				t.Errorf("SupportsVision=%v, want %v", caps.SupportsVision, m.wantVision)
			}
		})
	}
}

func TestGeminiModels(t *testing.T) {
	r := DefaultCapabilityRegistry()

	t.Run("gemini-1.5-pro has 2M context", func(t *testing.T) {
		caps := r.Get(ProviderGemini, "gemini-1.5-pro")
		if caps.MaxContextTokens != 2097152 {
			t.Errorf("got %d, want 2097152", caps.MaxContextTokens)
		}
	})

	t.Run("gemini supports JSON mode", func(t *testing.T) {
		caps := r.Get(ProviderGemini, "gemini-2.0-flash")
		if !caps.SupportsJSON {
			t.Error("expected SupportsJSON=true")
		}
	})
}
