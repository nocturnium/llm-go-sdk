package anthropic

import (
	"testing"
)

// TestKnownModels_CoversTheDefaultModel pins that the model a zero-config
// client uses has metadata here. It was absent, so every knownModels lookup
// for the default (display name, context length, pricing) missed.
func TestKnownModels_CoversTheDefaultModel(t *testing.T) {
	client, err := New(WithAPIKey("test"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	metadata, ok := knownModels[client.Model()]
	if !ok {
		t.Fatalf("default model %q is missing from knownModels", client.Model())
	}
	if metadata.contextLength == 0 || metadata.maxOutput == 0 {
		t.Errorf("default model %q has incomplete metadata: %+v", client.Model(), metadata)
	}
}
