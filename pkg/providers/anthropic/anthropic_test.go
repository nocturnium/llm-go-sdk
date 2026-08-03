package anthropic

import (
	"context"
	"errors"
	"os"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/anthropicapi"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected default model to be claude-sonnet-4-20250514, got %s", opts.Model)
	}
	if opts.APIKey != "" {
		t.Error("expected default API key to be empty")
	}
	if opts.BaseURL != "" {
		t.Error("expected default BaseURL to be empty")
	}
}

func TestApplyOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("claude-opus-4-20250514"),
		WithBaseURL("https://custom.api.com"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key to be test-key, got %s", opts.APIKey)
	}
	if opts.Model != "claude-opus-4-20250514" {
		t.Errorf("expected model to be claude-opus-4-20250514, got %s", opts.Model)
	}
	if opts.BaseURL != "https://custom.api.com" {
		t.Errorf("expected BaseURL to be https://custom.api.com, got %s", opts.BaseURL)
	}
}

func TestNewClientMissingAPIKey(t *testing.T) {
	// Ensure env var is not set
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalKey != "" {
			t.Setenv("ANTHROPIC_API_KEY", originalKey)
		}
	}()

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewClientWithEnvAPIKey(t *testing.T) {
	// Set env var
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	t.Setenv("ANTHROPIC_API_KEY", "env-test-key")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalKey != "" {
			t.Setenv("ANTHROPIC_API_KEY", originalKey)
		} else {
			_ = os.Unsetenv("ANTHROPIC_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderAnthropic {
		t.Errorf("expected provider to be anthropic, got %s", client.Provider())
	}
	if client.Model() != "claude-sonnet-4-20250514" {
		t.Errorf("expected default model, got %s", client.Model())
	}
}

func TestNewClientWithExplicitAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("explicit-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderAnthropic {
		t.Errorf("expected provider to be anthropic, got %s", client.Provider())
	}
}

func TestNewClientWithCustomModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-opus-4-20250514"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != "claude-opus-4-20250514" {
		t.Errorf("expected model to be claude-opus-4-20250514, got %s", client.Model())
	}
}

func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	// Ensure provider-specific env var is not set but LLM_API_KEY is
	originalAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	originalLLM := os.Getenv("LLM_API_KEY")
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	t.Setenv("LLM_API_KEY", "llm-fallback-key")
	defer func() {
		if originalAnthropic != "" {
			t.Setenv("ANTHROPIC_API_KEY", originalAnthropic)
		}
		if originalLLM != "" {
			t.Setenv("LLM_API_KEY", originalLLM)
		} else {
			_ = os.Unsetenv("LLM_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderAnthropic {
		t.Errorf("expected provider to be anthropic, got %s", client.Provider())
	}
}

// TestClientImplementsInterface verifies that Client implements llms.LLM.
func TestClientImplementsInterface(_ *testing.T) {
	var _ llms.LLM = (*Client)(nil)
}

func TestClient_ValidatesToolCallIDs(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Get weather"},
		{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{{ID: "call-1"}}},
		{Role: llms.RoleTool, Name: "get_weather", Content: `{"temp":72}`},
	}

	_, err = client.GenerateContent(context.Background(), messages)
	assertValidationField(t, err, "messages[2].tool_call_id")

	_, err = client.Stream(context.Background(), messages)
	assertValidationField(t, err, "messages[2].tool_call_id")
}

func assertValidationField(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErrs llms.ValidationErrors
	if !errors.As(err, &validationErrs) {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	for _, validationErr := range validationErrs {
		if validationErr.Field == field {
			return
		}
	}
	t.Fatalf("expected validation field %q in %v", field, validationErrs)
}

// TestBuildRequest_PartsBasedSystemMessage verifies that a system message built
// from Parts (rather than the simple Content field) still reaches the request's
// System block. Previously buildRequest read msg.Content only, silently dropping
// a Parts-based system prompt. This is the FIX 5 acceptance test.
func TestBuildRequest_PartsBasedSystemMessage(t *testing.T) {
	client, err := New(WithAPIKey("test-key"), WithModel("claude-3-5-sonnet-20241022"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages := []llms.Message{
		{
			Role:  llms.RoleSystem,
			Parts: []llms.ContentPart{llms.NewTextPart("You are a pirate.")},
		},
		{Role: llms.RoleUser, Content: "Hello"},
	}

	req, err := client.buildRequest(messages, llms.DefaultCallOptions(), false)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}

	if len(req.System) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(req.System))
	}
	if req.System[0].Text != "You are a pirate." {
		t.Errorf("expected system text 'You are a pirate.', got %q", req.System[0].Text)
	}
}

// TestBuildRequest_ExtendedThinking verifies that a reasoning request maps to an
// Anthropic thinking budget, bumps max_tokens above the budget, and clears the
// sampling params (which Anthropic rejects alongside thinking).
func TestBuildRequest_ExtendedThinking(t *testing.T) {
	client, err := New(WithAPIKey("test-key"), WithModel("claude-sonnet-4-20250514"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages := []llms.Message{{Role: llms.RoleUser, Content: "Solve it."}}
	temp := 0.7
	opts := llms.ApplyOptions(
		llms.WithReasoningBudget(8192),
		llms.WithTemperature(temp),
		llms.WithMaxTokens(1000),
	)

	req, err := client.buildRequest(messages, opts, false)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}

	if req.Thinking == nil {
		t.Fatal("expected Thinking config to be set")
	}
	if req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != 8192 {
		t.Errorf("expected enabled/8192, got %+v", req.Thinking)
	}
	if req.MaxTokens <= req.Thinking.BudgetTokens {
		t.Errorf("expected max_tokens > budget, got max_tokens=%d budget=%d", req.MaxTokens, req.Thinking.BudgetTokens)
	}
	if req.Temperature != nil || req.TopP != nil {
		t.Error("expected temperature/top_p to be cleared when thinking is enabled")
	}
}

// TestBuildRequest_ThinkingSoftensForcingToolChoice verifies that a forcing
// tool_choice is softened to "auto" when thinking is enabled, since Anthropic
// rejects extended thinking combined with tool_choice "any"/"tool".
func TestBuildRequest_ThinkingSoftensForcingToolChoice(t *testing.T) {
	client, err := New(WithAPIKey("test-key"), WithModel("claude-sonnet-4-20250514"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tool := llms.Tool{Type: llms.ToolTypeFunction, Function: &llms.FunctionDefinition{Name: "f"}}

	for _, tc := range []struct {
		name   string
		choice llms.CallOption
	}{
		{"required", llms.WithToolChoiceRequired()},
		{"specific tool", llms.WithToolChoiceTool("f")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := llms.ApplyOptions(llms.WithReasoningBudget(2048), llms.WithTools([]llms.Tool{tool}), tc.choice)
			req, err := client.buildRequest([]llms.Message{{Role: llms.RoleUser, Content: "hi"}}, opts, false)
			if err != nil {
				t.Fatalf("buildRequest: %v", err)
			}
			if req.Thinking == nil {
				t.Fatal("expected thinking enabled")
			}
			if _, ok := req.ToolChoice.(anthropicapi.ToolChoiceAuto); !ok {
				t.Errorf("expected tool_choice softened to auto, got %#v", req.ToolChoice)
			}
		})
	}
}

// TestBuildRequest_EffortDerivesBudget verifies an effort level with no explicit
// budget derives one via ReasoningBudgetForEffort.
func TestBuildRequest_EffortDerivesBudget(t *testing.T) {
	client, err := New(WithAPIKey("test-key"), WithModel("claude-sonnet-4-20250514"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	opts := llms.ApplyOptions(llms.WithReasoningEffort(llms.ReasoningEffortLow))
	req, err := client.buildRequest([]llms.Message{{Role: llms.RoleUser, Content: "hi"}}, opts, false)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	if req.Thinking == nil || req.Thinking.BudgetTokens != llms.ReasoningBudgetForEffort(llms.ReasoningEffortLow) {
		t.Errorf("expected derived budget %d, got %+v", llms.ReasoningBudgetForEffort(llms.ReasoningEffortLow), req.Thinking)
	}
}

// TestConvertToolChoice_NonePreserved covers #16: tool_choice "none" must not be
// silently coerced to "auto".
func TestConvertToolChoice_NonePreserved(t *testing.T) {
	got := convertToolChoice(&llms.ToolChoice{Mode: llms.ToolChoiceNone})
	if got == nil {
		t.Fatal("tool_choice none produced nil")
	}
	if _, isAuto := got.(anthropicapi.ToolChoiceAuto); isAuto {
		t.Fatal("tool_choice none was coerced to auto (regression of #16)")
	}
}
