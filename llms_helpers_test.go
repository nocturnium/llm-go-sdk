package llms

import (
	"context"
	"errors"
	"testing"
)

// testWrapper (a minimal middleware wrapper) is declared in llms_test.go and
// reused here to exercise the unwrap / GetMiddleware / capability-passthrough paths.

func TestCallHelper(t *testing.T) {
	ctx := context.Background()

	mock := NewMockLLM(WithMockGenerateResponse(&Response{Content: "pong"}))
	got, err := Call(ctx, mock, "ping")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got != "pong" {
		t.Errorf("Call = %q, want pong", got)
	}

	boom := errors.New("boom")
	if _, err := Call(ctx, NewMockLLM(WithMockGenerateError(boom)), "x"); !errors.Is(err, boom) {
		t.Errorf("Call error = %v, want boom", err)
	}
}

func TestCapabilityHelpers(t *testing.T) {
	full := NewMockLLM(WithMockCapabilities(Capabilities{
		Streaming: true, Tools: true, Vision: true, Batch: true, JSONMode: true,
	}))
	none := NewMockLLM(WithMockCapabilities(Capabilities{}))

	checks := []struct {
		name string
		fn   func(LLM) bool
	}{
		{"Streaming", SupportsStreaming},
		{"Tools", SupportsTools},
		{"Vision", SupportsVision},
		{"Batch", SupportsBatch},
		{"JSONMode", SupportsJSONMode},
	}
	for _, c := range checks {
		if !c.fn(full) {
			t.Errorf("Supports%s(full) = false, want true", c.name)
		}
		if c.fn(none) {
			t.Errorf("Supports%s(none) = true, want false", c.name)
		}
		// Capability passthrough must survive a middleware wrapper.
		if !c.fn(testWrapper{LLM: full}) {
			t.Errorf("Supports%s(wrapped full) = false, want true", c.name)
		}
	}
}

func TestGetCapabilitiesHelper(t *testing.T) {
	caps := Capabilities{Vision: true, MaxContextTokens: 4096}
	mock := NewMockLLM(WithMockCapabilities(caps))
	if got := GetCapabilities(mock); !got.Vision || got.MaxContextTokens != 4096 {
		t.Errorf("GetCapabilities = %+v", got)
	}
	// A type that is not a CapableProvider yields the zero Capabilities.
	if got := GetCapabilities(testWrapper{LLM: mock}); !got.Vision {
		t.Errorf("GetCapabilities(wrapped) should pass through, got %+v", got)
	}
}

func TestGetMiddlewareChain(t *testing.T) {
	base := NewMockLLM()
	if mw := GetMiddleware(base); len(mw) != 0 {
		t.Errorf("GetMiddleware(bare) = %d wrappers, want 0", len(mw))
	}

	wrapped := testWrapper{LLM: testWrapper{LLM: base}}
	mw := GetMiddleware(wrapped)
	if len(mw) != 2 {
		t.Fatalf("GetMiddleware(double-wrapped) = %d, want 2", len(mw))
	}
	if UnwrapAll(wrapped) != LLM(base) {
		t.Error("UnwrapAll did not reach the base LLM")
	}
}
