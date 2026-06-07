package llms

import (
	"context"
	"testing"
)

func TestTraceContext_Creation(t *testing.T) {
	tc := NewTraceContext(
		WithUserID("user-123"),
		WithSessionID("session-456"),
		WithTags("production", "chatbot"),
		WithVersion("1.0.0"),
		WithEnvironment("production"),
		WithMetadata("feature", "chat"),
	)

	if tc.UserID != "user-123" {
		t.Errorf("expected UserID 'user-123', got '%s'", tc.UserID)
	}
	if tc.SessionID != "session-456" {
		t.Errorf("expected SessionID 'session-456', got '%s'", tc.SessionID)
	}
	if len(tc.Tags) != 2 || tc.Tags[0] != "production" || tc.Tags[1] != "chatbot" {
		t.Errorf("unexpected tags: %v", tc.Tags)
	}
	if tc.Version != "1.0.0" {
		t.Errorf("expected Version '1.0.0', got '%s'", tc.Version)
	}
	if tc.Environment != "production" {
		t.Errorf("expected Environment 'production', got '%s'", tc.Environment)
	}
	if tc.Metadata["feature"] != "chat" {
		t.Errorf("expected Metadata['feature'] = 'chat', got '%s'", tc.Metadata["feature"])
	}
}

func TestTraceContext_Clone(t *testing.T) {
	original := &TraceContext{
		TraceID:     "trace-1",
		SpanID:      "span-1",
		UserID:      "user-1",
		SessionID:   "session-1",
		Tags:        []string{"tag1", "tag2"},
		Metadata:    map[string]string{"key": "value"},
		Version:     "1.0",
		Environment: "test",
	}

	clone := original.Clone()

	// Verify values are copied
	if clone.TraceID != original.TraceID {
		t.Error("TraceID not cloned")
	}
	if clone.UserID != original.UserID {
		t.Error("UserID not cloned")
	}

	// Verify deep copy of slices
	clone.Tags[0] = "modified"
	if original.Tags[0] == "modified" {
		t.Error("Tags slice was not deep copied")
	}

	// Verify deep copy of maps
	clone.Metadata["key"] = "modified"
	if original.Metadata["key"] == "modified" {
		t.Error("Metadata map was not deep copied")
	}
}

func TestTraceContext_NilClone(t *testing.T) {
	var tc *TraceContext
	clone := tc.Clone()
	if clone != nil {
		t.Error("Clone of nil should return nil")
	}
}

func TestWithTraceContext(t *testing.T) {
	ctx := context.Background()
	tc := &TraceContext{
		UserID:    "user-123",
		SessionID: "session-456",
	}

	ctx = WithTraceContext(ctx, tc)

	retrieved := GetTraceContext(ctx)
	if retrieved == nil {
		t.Fatal("expected TraceContext, got nil")
	}
	if retrieved.UserID != "user-123" {
		t.Errorf("expected UserID 'user-123', got '%s'", retrieved.UserID)
	}
}

func TestGetTraceContext_NilContext(t *testing.T) {
	ctx := context.Background()
	tc := GetTraceContext(ctx)
	if tc != nil {
		t.Error("expected nil TraceContext from empty context")
	}
}

func TestPropagateAttributes(t *testing.T) {
	// Set up parent context (using alphanumeric keys for metadata per Langfuse constraints)
	parent := &TraceContext{
		UserID:      "user-parent",
		SessionID:   "session-parent",
		Tags:        []string{"parent-tag"},
		Version:     "1.0",
		Environment: "staging",
		Metadata:    map[string]string{"parentkey": "parent_value"},
	}
	ctx := WithTraceContext(context.Background(), parent)

	// Propagate with overrides (note: metadata keys must be alphanumeric)
	childCtx := PropagateAttributes(ctx,
		WithUserID("user-child"),
		WithTags("child-tag"),
		WithMetadata("childkey", "child_value"),
	)

	child := GetTraceContext(childCtx)
	if child == nil {
		t.Fatal("expected child TraceContext")
	}

	// Check overridden values
	if child.UserID != "user-child" {
		t.Errorf("expected UserID 'user-child', got '%s'", child.UserID)
	}

	// Check inherited values
	if child.SessionID != "session-parent" {
		t.Errorf("expected SessionID 'session-parent', got '%s'", child.SessionID)
	}
	if child.Version != "1.0" {
		t.Errorf("expected Version '1.0', got '%s'", child.Version)
	}
	if child.Environment != "staging" {
		t.Errorf("expected Environment 'staging', got '%s'", child.Environment)
	}

	// Check merged tags
	if len(child.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(child.Tags))
	}

	// Check merged metadata
	if child.Metadata["parentkey"] != "parent_value" {
		t.Error("parent metadata not inherited")
	}
	if child.Metadata["childkey"] != "child_value" {
		t.Error("child metadata not added")
	}

	// Verify parent is unchanged
	if parent.UserID != "user-parent" {
		t.Error("parent UserID was modified")
	}
}

func TestPropagateAttributes_NoParent(t *testing.T) {
	ctx := context.Background()

	childCtx := PropagateAttributes(ctx,
		WithUserID("user-new"),
		WithSessionID("session-new"),
	)

	child := GetTraceContext(childCtx)
	if child == nil {
		t.Fatal("expected child TraceContext")
	}
	if child.UserID != "user-new" {
		t.Errorf("expected UserID 'user-new', got '%s'", child.UserID)
	}
	if child.SessionID != "session-new" {
		t.Errorf("expected SessionID 'session-new', got '%s'", child.SessionID)
	}
}

func TestIsValidMetadataKey(t *testing.T) {
	tests := []struct {
		key   string
		valid bool
	}{
		{"validkey", true},
		{"ValidKey123", true},
		{"123numeric", true},
		{"", false},
		{"invalid-key", false},
		{"invalid_key", false},
		{"invalid.key", false},
		{"invalid key", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsValidMetadataKey(tt.key); got != tt.valid {
				t.Errorf("IsValidMetadataKey(%q) = %v, want %v", tt.key, got, tt.valid)
			}
		})
	}
}

func TestValidateMetadataValue(t *testing.T) {
	// Value at exactly 200 chars should be valid
	validValue := string(make([]byte, 200))
	if !ValidateMetadataValue(validValue) {
		t.Error("200 char value should be valid")
	}

	// Value over 200 chars should be invalid
	invalidValue := string(make([]byte, 201))
	if ValidateMetadataValue(invalidValue) {
		t.Error("201 char value should be invalid")
	}

	// Empty value should be valid
	if !ValidateMetadataValue("") {
		t.Error("empty value should be valid")
	}
}

func TestWithMetadata_InvalidKey(t *testing.T) {
	tc := NewTraceContext(
		WithMetadata("invalid-key", "value"),
	)

	if tc.Metadata != nil && tc.Metadata["invalid-key"] != "" {
		t.Error("invalid key should not be added to metadata")
	}
}

func TestWithMetadata_ValueTooLong(t *testing.T) {
	longValue := string(make([]byte, 201))
	tc := NewTraceContext(
		WithMetadata("validkey", longValue),
	)

	if tc.Metadata != nil && tc.Metadata["validkey"] != "" {
		t.Error("value over 200 chars should not be added")
	}
}

func TestWithMetadataMap(t *testing.T) {
	metadata := map[string]string{
		"valid1":      "value1",
		"valid2":      "value2",
		"invalid-key": "value3",
	}

	tc := NewTraceContext(
		WithMetadataMap(metadata),
	)

	if tc.Metadata["valid1"] != "value1" {
		t.Error("valid1 should be present")
	}
	if tc.Metadata["valid2"] != "value2" {
		t.Error("valid2 should be present")
	}
	if tc.Metadata["invalid-key"] != "" {
		t.Error("invalid-key should not be present")
	}
}
