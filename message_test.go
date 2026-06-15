package llms

import (
	"reflect"
	"testing"
)

func TestMergeConsecutiveMessages_Empty(t *testing.T) {
	result := MergeConsecutiveMessages(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	result = MergeConsecutiveMessages([]Message{})
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %v", result)
	}
}

func TestMergeConsecutiveMessages_Single(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Content != "Hello" {
		t.Errorf("expected 'Hello', got %s", result[0].Content)
	}
}

func TestMergeConsecutiveMessages_NoMergeNeeded(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi there"},
		{Role: RoleUser, Content: "How are you?"},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
}

func TestMergeConsecutiveMessages_MergeUserMessages(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleUser, Content: "How are you?"},
		{Role: RoleAssistant, Content: "I'm fine!"},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Content != "Hello\nHow are you?" {
		t.Errorf("expected merged content, got %s", result[0].Content)
	}
	if result[0].Role != RoleUser {
		t.Errorf("expected role User, got %s", result[0].Role)
	}
}

func TestMergeConsecutiveMessages_MergeAssistantMessages(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi there!"},
		{Role: RoleAssistant, Content: "How can I help?"},
		{Role: RoleUser, Content: "Tell me a joke"},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[1].Content != "Hi there!\nHow can I help?" {
		t.Errorf("expected merged assistant content, got %s", result[1].Content)
	}
}

func TestMergeConsecutiveMessages_ToolMessagesNotMerged(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{{ID: "1"}}},
		{Role: RoleTool, Content: "result1", ToolCallID: "1"},
		{Role: RoleTool, Content: "result2", ToolCallID: "2"},
		{Role: RoleAssistant, Content: "Here are the results"},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 4 {
		t.Fatalf("expected 4 messages (tool messages not merged), got %d", len(result))
	}
}

func TestMergeConsecutiveMessages_SystemMessagePreserved(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: "You are helpful"},
		{Role: RoleUser, Content: "Hello"},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != RoleSystem {
		t.Errorf("expected system role, got %s", result[0].Role)
	}
}

func TestConsolidateSystemMessages_MultipleSystemMessages(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: "You are helpful"},
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleSystem, Content: "Be concise"},
	}

	result := ConsolidateSystemMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != RoleSystem {
		t.Errorf("expected system role first, got %s", result[0].Role)
	}
	if result[0].Content != "You are helpful\n\nBe concise" {
		t.Errorf("expected consolidated system content, got %s", result[0].Content)
	}
	if result[1].Role != RoleUser {
		t.Errorf("expected user role second, got %s", result[1].Role)
	}
}

// TestConsolidateSystemMessages_PartsBased verifies that a system message built
// from Parts (rather than the simple Content field) is included in the
// consolidated system message rather than being silently dropped. This is part
// of the FIX 5 acceptance coverage.
func TestConsolidateSystemMessages_PartsBased(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Parts: []ContentPart{NewTextPart("You are helpful")}},
		{Role: RoleUser, Content: "Hello"},
	}

	result := ConsolidateSystemMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != RoleSystem {
		t.Errorf("expected system role first, got %s", result[0].Role)
	}
	if result[0].Content != "You are helpful" {
		t.Errorf("expected Parts-based system text to survive consolidation, got %q", result[0].Content)
	}
}

func TestConsolidateSystemMessages_NoSystemMessages(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi"},
	}

	result := ConsolidateSystemMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
}

func TestConsolidateSystemMessages_ConsecutiveSystemMessages(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: "First instruction"},
		{Role: RoleSystem, Content: "Second instruction"},
		{Role: RoleUser, Content: "Hello"},
	}

	result := ConsolidateSystemMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Content != "First instruction\n\nSecond instruction" {
		t.Errorf("expected consolidated content, got %s", result[0].Content)
	}
}

func TestConsolidateSystemMessages_SystemMessageInMiddle(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleSystem, Content: "New instruction"},
		{Role: RoleAssistant, Content: "Hi"},
	}

	result := ConsolidateSystemMessages(messages)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	// System message should be moved to front
	if result[0].Role != RoleSystem {
		t.Errorf("expected system role first, got %s", result[0].Role)
	}
	if result[1].Role != RoleUser {
		t.Errorf("expected user role second, got %s", result[1].Role)
	}
}

func TestMergeConsecutiveMessages_MultipleSystemMessagesConsolidated(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: "Instruction 1"},
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleSystem, Content: "Instruction 2"},
		{Role: RoleUser, Content: "World"},
	}

	result := MergeConsecutiveMessages(messages)

	// Should have: 1 consolidated system + 1 merged user
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != RoleSystem {
		t.Errorf("expected system role first, got %s", result[0].Role)
	}
	if result[0].Content != "Instruction 1\n\nInstruction 2" {
		t.Errorf("expected consolidated system content, got %s", result[0].Content)
	}
	if result[1].Role != RoleUser {
		t.Errorf("expected user role, got %s", result[1].Role)
	}
	if result[1].Content != "Hello\nWorld" {
		t.Errorf("expected merged user content, got %s", result[1].Content)
	}
}

func TestMergeConsecutiveMessages_MergeToolCalls(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{{ID: "1"}}},
		{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{{ID: "2"}}},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if len(result[0].ToolCalls) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(result[0].ToolCalls))
	}
}

func TestMergeConsecutiveMessages_MergeParts(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Parts: []ContentPart{{Type: "text", Text: "First"}}},
		{Role: RoleUser, Parts: []ContentPart{{Type: "text", Text: "Second"}}},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if len(result[0].Parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(result[0].Parts))
	}
}

func TestMergeConsecutiveMessages_MultipleConsecutive(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "1"},
		{Role: RoleUser, Content: "2"},
		{Role: RoleUser, Content: "3"},
		{Role: RoleAssistant, Content: "response"},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Content != "1\n2\n3" {
		t.Errorf("expected merged content '1\\n2\\n3', got %s", result[0].Content)
	}
}

func TestMergeConsecutiveMessages_MixedContentAndParts(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleUser, Parts: []ContentPart{{Type: "text", Text: "World"}}},
	}

	result := MergeConsecutiveMessages(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Content != "Hello" {
		t.Errorf("expected content 'Hello', got %s", result[0].Content)
	}
	if len(result[0].Parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(result[0].Parts))
	}
}

func TestPrepareMessages_MergesAndValidates(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleUser, Content: "How are you?"},
	}

	result, err := PrepareMessages(messages, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 merged message, got %d", len(result))
	}
	if result[0].Content != "Hello\nHow are you?" {
		t.Errorf("expected merged content, got %s", result[0].Content)
	}
}

func TestPrepareMessages_DisableMerging(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleUser, Content: "How are you?"},
	}

	opts := &CallOptions{DisableMessageMerging: true}
	result, err := PrepareMessages(messages, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 unmerged messages, got %d", len(result))
	}
}

func TestPrepareMessages_ValidationStillWorks(t *testing.T) {
	messages := []Message{
		{Role: "", Content: "Hello"}, // Invalid - no role
	}

	_, err := PrepareMessages(messages, nil)
	if err == nil {
		t.Error("expected validation error for missing role")
	}
}

func TestValidateMessages_ConsecutiveAfterMerging(t *testing.T) {
	// This tests that after merging, validation passes
	messages := []Message{
		{Role: RoleSystem, Content: "You are helpful"},
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleUser, Content: "World"},
	}

	result, err := PrepareMessages(messages, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 messages after merging, got %d", len(result))
	}
}

func TestValidateMessages_LeadingToolMessage(t *testing.T) {
	messages := []Message{
		{Role: RoleTool, Content: "result", ToolCallID: "call-1"},
	}

	err := ValidateMessages(messages)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestPrepareMessages_DisableMergingToolSequence(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: "You are helpful"},
		{Role: RoleUser, Content: "Get weather"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1"}}},
		{Role: RoleTool, Content: "sunny", ToolCallID: "call-1"},
	}

	result, err := PrepareMessages(messages, &CallOptions{DisableMessageMerging: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != len(messages) {
		t.Fatalf("expected %d messages, got %d", len(messages), len(result))
	}
}

func TestMergeConsecutiveMessages_PreservesOriginal(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleUser, Content: "World"},
	}

	// Make a copy of the original to verify it wasn't modified
	original := make([]Message, len(messages))
	copy(original, messages)

	result := MergeConsecutiveMessages(messages)

	// Verify original wasn't mutated
	if !reflect.DeepEqual(messages, original) {
		t.Error("original messages were mutated")
	}

	// Verify result is different
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
}
