package gemini

import (
	"errors"
	"os"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

const (
	testGemini15Pro = "gemini-1.5-pro"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "gemini-2.0-flash" {
		t.Errorf("expected default model to be gemini-2.0-flash, got %s", opts.Model)
	}
	if opts.APIKey != "" {
		t.Error("expected default API key to be empty")
	}
}

func TestApplyOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel(testGemini15Pro),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key to be test-key, got %s", opts.APIKey)
	}
	if opts.Model != testGemini15Pro {
		t.Errorf("expected model to be gemini-1.5-pro, got %s", opts.Model)
	}
}

func TestNewClientMissingAPIKey(t *testing.T) {
	// Ensure env vars are not set
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	_ = os.Unsetenv("GEMINI_API_KEY")
	_ = os.Unsetenv("GOOGLE_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalGemini != "" {
			_ = os.Setenv("GEMINI_API_KEY", originalGemini)
		}
		if originalGoogle != "" {
			_ = os.Setenv("GOOGLE_API_KEY", originalGoogle)
		}
	}()

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewClientWithGeminiEnvAPIKey(t *testing.T) {
	// Set GEMINI_API_KEY env var
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	_ = os.Setenv("GEMINI_API_KEY", "gemini-env-key")
	_ = os.Unsetenv("GOOGLE_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalGemini != "" {
			_ = os.Setenv("GEMINI_API_KEY", originalGemini)
		} else {
			_ = os.Unsetenv("GEMINI_API_KEY")
		}
		if originalGoogle != "" {
			_ = os.Setenv("GOOGLE_API_KEY", originalGoogle)
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderGemini {
		t.Errorf("expected provider to be gemini, got %s", client.Provider())
	}
	if client.Model() != "gemini-2.0-flash" {
		t.Errorf("expected default model, got %s", client.Model())
	}
}

func TestNewClientWithGoogleEnvAPIKey(t *testing.T) {
	// Set GOOGLE_API_KEY env var (fallback)
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	_ = os.Unsetenv("GEMINI_API_KEY")
	_ = os.Setenv("GOOGLE_API_KEY", "google-env-key")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalGemini != "" {
			_ = os.Setenv("GEMINI_API_KEY", originalGemini)
		}
		if originalGoogle != "" {
			_ = os.Setenv("GOOGLE_API_KEY", originalGoogle)
		} else {
			_ = os.Unsetenv("GOOGLE_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderGemini {
		t.Errorf("expected provider to be gemini, got %s", client.Provider())
	}
}

func TestNewClientWithExplicitAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("explicit-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderGemini {
		t.Errorf("expected provider to be gemini, got %s", client.Provider())
	}
}

func TestNewClientWithCustomModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel(testGemini15Pro),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != testGemini15Pro {
		t.Errorf("expected model to be gemini-1.5-pro, got %s", client.Model())
	}
}

func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	// Ensure provider-specific env vars are not set but LLM_API_KEY is
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	originalLLM := os.Getenv("LLM_API_KEY")
	_ = os.Unsetenv("GEMINI_API_KEY")
	_ = os.Unsetenv("GOOGLE_API_KEY")
	_ = os.Setenv("LLM_API_KEY", "llm-fallback-key")
	defer func() {
		if originalGemini != "" {
			_ = os.Setenv("GEMINI_API_KEY", originalGemini)
		}
		if originalGoogle != "" {
			_ = os.Setenv("GOOGLE_API_KEY", originalGoogle)
		}
		if originalLLM != "" {
			_ = os.Setenv("LLM_API_KEY", originalLLM)
		} else {
			_ = os.Unsetenv("LLM_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderGemini {
		t.Errorf("expected provider to be gemini, got %s", client.Provider())
	}
}

// TestClientImplementsInterface verifies that Client implements llms.LLM
func TestClientImplementsInterface(_ *testing.T) {
	var _ llms.LLM = (*Client)(nil)
}

// TestBuildRequest_SystemInstruction covers #12: a system message is promoted to
// the Gemini systemInstruction field and excluded from Contents.
func TestBuildRequest_SystemInstruction(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msgs := []llms.Message{
		{Role: llms.RoleSystem, Content: "you are helpful"},
		{Role: llms.RoleUser, Content: "hi"},
	}
	req := client.buildRequest(msgs, llms.ApplyOptions())
	if req.SystemInstruction == nil {
		t.Fatal("system message was not promoted to SystemInstruction")
	}
	if len(req.Contents) != 1 {
		t.Fatalf("expected system message excluded from Contents (1 user turn), got %d", len(req.Contents))
	}
	if req.Contents[0].Role != "user" {
		t.Errorf("expected remaining content role=user, got %q", req.Contents[0].Role)
	}
}

func TestBuildRequest_MultipleAndNonLeadingSystemMessages(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "hi"},
		{Role: llms.RoleSystem, Content: "be concise"},
		{Role: llms.RoleSystem, Content: "use JSON"},
	}
	opts := llms.ApplyOptions(llms.WithDisableMessageMerging())

	prepared, err := llms.PrepareMessages(msgs, opts)
	if err != nil {
		t.Fatalf("PrepareMessages: %v", err)
	}
	req := client.buildRequest(prepared, opts)

	if req.SystemInstruction == nil {
		t.Fatal("system messages were not promoted to SystemInstruction")
	}
	if got := req.SystemInstruction.Parts[0].Text; got != "be concise\n\nuse JSON" {
		t.Errorf("expected joined system instruction, got %q", got)
	}
	if len(req.Contents) != 1 || req.Contents[0].Role != "user" {
		t.Fatalf("expected one user content, got %+v", req.Contents)
	}
}

func TestBuildRequest_ToolMessageWithoutToolCallID(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "Get weather"},
		{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{{
			ID: "get_weather",
			Function: &llms.FunctionCall{
				Name:      "get_weather",
				Arguments: `{"city":"Portland"}`,
			},
		}}},
		{Role: llms.RoleTool, Name: "get_weather", Content: `{"temp":72}`},
	}
	opts := llms.ApplyOptions(llms.WithDisableMessageMerging())

	prepared, err := llms.PrepareMessages(msgs, opts)
	if err != nil {
		t.Fatalf("PrepareMessages: %v", err)
	}
	req := client.buildRequest(prepared, opts)

	if len(req.Contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(req.Contents))
	}
	if got := prepared[2].ToolCallID; got != "" {
		t.Errorf("expected missing ToolCallID to remain unchanged, got %q", got)
	}
}
