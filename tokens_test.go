package llms

import (
	"testing"
)

func TestEstimateTokens_Empty(t *testing.T) {
	result := EstimateTokens("")
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestEstimateTokens_Simple(t *testing.T) {
	// "Hello world" is approximately 2-3 tokens
	result := EstimateTokens("Hello world")
	if result < 1 || result > 5 {
		t.Errorf("expected 1-5 tokens for 'Hello world', got %d", result)
	}
}

func TestEstimateTokens_LongText(t *testing.T) {
	// Longer text should have more tokens
	text := "The quick brown fox jumps over the lazy dog. This is a sample sentence for testing token estimation."
	result := EstimateTokens(text)

	// Expect roughly 20-30 tokens for this text
	if result < 15 || result > 40 {
		t.Errorf("expected 15-40 tokens, got %d", result)
	}
}

func TestEstimateTokens_Punctuation(t *testing.T) {
	// Punctuation should add tokens
	text := "Hello, world! How are you?"
	result := EstimateTokens(text)

	if result < 4 {
		t.Errorf("expected at least 4 tokens, got %d", result)
	}
}

func TestEstimateTokens_Code(t *testing.T) {
	// Code with symbols
	code := `func main() { fmt.Println("Hello") }`
	result := EstimateTokens(code)

	if result < 5 {
		t.Errorf("expected at least 5 tokens for code, got %d", result)
	}
}

func TestEstimateMessageTokens_Empty(t *testing.T) {
	result := EstimateMessageTokens(nil)
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}

	result = EstimateMessageTokens([]Message{})
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestEstimateMessageTokens_SingleMessage(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello"},
	}

	result := EstimateMessageTokens(messages)

	// Expect some tokens for content + message overhead
	if result < 5 {
		t.Errorf("expected at least 5 tokens, got %d", result)
	}
}

func TestEstimateMessageTokens_MultipleMessages(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: "You are a helpful assistant."},
		{Role: RoleUser, Content: "Hello!"},
		{Role: RoleAssistant, Content: "Hi there! How can I help you today?"},
	}

	result := EstimateMessageTokens(messages)

	// Expect more tokens for multiple messages
	if result < 20 {
		t.Errorf("expected at least 20 tokens, got %d", result)
	}
}

func TestEstimateMessageTokens_WithParts(t *testing.T) {
	messages := []Message{
		{
			Role: RoleUser,
			Parts: []ContentPart{
				{Type: "text", Text: "What is in this image?"},
			},
		},
	}

	result := EstimateMessageTokens(messages)

	if result < 5 {
		t.Errorf("expected at least 5 tokens, got %d", result)
	}
}

func TestEstimateMessageTokens_WithToolCalls(t *testing.T) {
	messages := []Message{
		{
			Role: RoleAssistant,
			ToolCalls: []ToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: &FunctionCall{
						Name:      "get_weather",
						Arguments: `{"location": "San Francisco"}`,
					},
				},
			},
		},
	}

	result := EstimateMessageTokens(messages)

	// Expect tokens for tool call structure + name + arguments
	if result < 10 {
		t.Errorf("expected at least 10 tokens for tool calls, got %d", result)
	}
}

func TestEstimateUsageFromMessages(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "Hello, how are you?"},
	}
	responseContent := "I'm doing well, thank you for asking!"

	usage := EstimateUsageFromMessages(messages, responseContent)

	if usage.PromptTokens <= 0 {
		t.Errorf("expected positive prompt tokens, got %d", usage.PromptTokens)
	}
	if usage.CompletionTokens <= 0 {
		t.Errorf("expected positive completion tokens, got %d", usage.CompletionTokens)
	}
	if usage.TotalTokens != usage.PromptTokens+usage.CompletionTokens {
		t.Errorf("expected total = prompt + completion, got %d != %d + %d",
			usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens)
	}
}

func TestUsageOrEstimate_WithUsage(t *testing.T) {
	// When usage is provided, it should be returned as-is
	usage := Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}
	messages := []Message{{Role: RoleUser, Content: "Hello"}}

	result := UsageOrEstimate(usage, messages, "Hi there!")

	if result.TotalTokens != 150 {
		t.Errorf("expected original usage, got %+v", result)
	}
}

func TestUsageOrEstimate_WithoutUsage(t *testing.T) {
	// When usage is empty, it should estimate
	usage := Usage{}
	messages := []Message{{Role: RoleUser, Content: "Hello"}}

	result := UsageOrEstimate(usage, messages, "Hi there!")

	if result.TotalTokens <= 0 {
		t.Errorf("expected estimated tokens, got %+v", result)
	}
}

func TestUsageOrEstimate_PartialUsage(t *testing.T) {
	// When only some fields are set, should calculate total
	usage := Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      0, // Missing total
	}
	messages := []Message{{Role: RoleUser, Content: "Hello"}}

	result := UsageOrEstimate(usage, messages, "Hi there!")

	if result.TotalTokens != 150 {
		t.Errorf("expected total to be calculated, got %d", result.TotalTokens)
	}
}

func TestTokenEstimator_CustomCharsPerToken(t *testing.T) {
	estimator := &TokenEstimator{CharsPerToken: 2.0}

	// With 2 chars per token, "Hello" (5 chars) should be ~2.5 tokens
	result := estimator.EstimateTokens("Hello")

	if result < 2 || result > 5 {
		t.Errorf("expected 2-5 tokens with custom chars per token, got %d", result)
	}
}

func TestTokenEstimator_ZeroCharsPerToken(t *testing.T) {
	// Should use default when zero
	estimator := &TokenEstimator{CharsPerToken: 0}
	result := estimator.EstimateTokens("Hello world")

	if result < 1 {
		t.Errorf("expected at least 1 token, got %d", result)
	}
}

func TestDefaultTokenEstimator(t *testing.T) {
	estimator := DefaultTokenEstimator()

	if estimator.CharsPerToken != 4.0 {
		t.Errorf("expected default chars per token to be 4.0, got %f", estimator.CharsPerToken)
	}
}
