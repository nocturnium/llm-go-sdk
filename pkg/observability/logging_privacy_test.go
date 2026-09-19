package observability

import (
	"context"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// capturingLogger keeps the last entry it was handed.
type capturingLogger struct{ last *LogEntry }

func (l *capturingLogger) LogRequest(_ context.Context, e *LogEntry)  { l.last = e }
func (l *capturingLogger) LogResponse(_ context.Context, e *LogEntry) { l.last = e }
func (l *capturingLogger) LogError(_ context.Context, e *LogEntry, _ error) {
	l.last = e
}

// TestLoggingMiddleware_WithLoggedContentFalse pins that a sink can be denied
// prompts and completions. The middleware handed raw content to every Logger
// with no opt-out, while the span middlewares gate it at the source.
func TestLoggingMiddleware_WithLoggedContentFalse(t *testing.T) {
	logger := &capturingLogger{}
	base := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genResp:  &llms.Response{Content: "the completion"},
	}
	mw := NewLoggingMiddleware(base, logger).WithLoggedContent(false)

	if _, err := mw.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "the prompt"}}); err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}

	if logger.last == nil {
		t.Fatal("logger saw no entry")
	}
	if logger.last.Content != "" {
		t.Errorf("entry carried the completion %q", logger.last.Content)
	}
	if len(logger.last.Messages) != 0 {
		t.Errorf("entry carried %d messages", len(logger.last.Messages))
	}
	if logger.last.Provider != llms.ProviderOpenAI {
		t.Error("metadata was scrubbed along with the content")
	}
}
