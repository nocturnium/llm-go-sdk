package anthropic

import (
	"encoding/json"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/internal/anthropicapi"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

func TestConvertResponse_CacheUsage(t *testing.T) {
	var resp anthropicapi.MessagesResponse
	if err := json.Unmarshal([]byte(`{
		"content": [{"type": "text", "text": "Hello"}],
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 10,
			"output_tokens": 5,
			"cache_creation_input_tokens": 3,
			"cache_read_input_tokens": 7
		}
	}`), &resp); err != nil {
		t.Fatalf("unmarshal Anthropic response: %v", err)
	}

	result := convertResponse(&resp)

	assertUsage(t, result.Usage, llms.Usage{
		PromptTokens:        10,
		CompletionTokens:    5,
		TotalTokens:         15,
		CacheReadTokens:     7,
		CacheCreationTokens: 3,
	})
}

func TestOpenAICompatConvertResponse_CacheUsage(t *testing.T) {
	var resp openaicompat.ChatCompletionResponse
	if err := json.Unmarshal([]byte(`{
		"choices": [{"message": {"content": "Hello"}, "finish_reason": "stop"}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15,
			"prompt_tokens_details": {"cached_tokens": 7}
		}
	}`), &resp); err != nil {
		t.Fatalf("unmarshal OpenAI-compatible response: %v", err)
	}

	result := openaicompat.ConvertResponse(&resp)

	assertUsage(t, result.Usage, llms.Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		CacheReadTokens:  7,
	})
}

func assertUsage(t *testing.T, got, want llms.Usage) {
	t.Helper()

	if got.PromptTokens != want.PromptTokens {
		t.Errorf("PromptTokens = %d, want %d", got.PromptTokens, want.PromptTokens)
	}
	if got.CompletionTokens != want.CompletionTokens {
		t.Errorf("CompletionTokens = %d, want %d", got.CompletionTokens, want.CompletionTokens)
	}
	if got.TotalTokens != want.TotalTokens {
		t.Errorf("TotalTokens = %d, want %d", got.TotalTokens, want.TotalTokens)
	}
	if got.CacheReadTokens != want.CacheReadTokens {
		t.Errorf("CacheReadTokens = %d, want %d", got.CacheReadTokens, want.CacheReadTokens)
	}
	if got.CacheCreationTokens != want.CacheCreationTokens {
		t.Errorf("CacheCreationTokens = %d, want %d", got.CacheCreationTokens, want.CacheCreationTokens)
	}
}
