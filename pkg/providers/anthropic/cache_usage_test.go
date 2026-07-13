package anthropic

import (
	"encoding/json"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/internal/anthropicapi"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/openaicompat"
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

	// OpenAI reports prompt_tokens inclusive of cached tokens; the SDK normalizes
	// PromptTokens to exclude them (10 - 7 = 3) so cost is computed uniformly.
	assertUsage(t, result.Usage, llms.Usage{
		PromptTokens:     3,
		CompletionTokens: 5,
		TotalTokens:      15,
		CacheReadTokens:  7,
	})
}

func newCacheTestClient(t *testing.T) *Client {
	t.Helper()
	client, err := New(WithAPIKey("test-key"), WithModel("claude-3-5-sonnet-20241022"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return client
}

func TestBuildRequest_SystemCacheDefaultOn(t *testing.T) {
	client := newCacheTestClient(t)
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "system prompt"},
		{Role: llms.RoleUser, Content: "hi"},
	}
	req, err := client.buildRequest(messages, llms.DefaultCallOptions(), false)
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}
	if len(req.System) != 1 || req.System[0].CacheControl == nil {
		t.Fatal("expected system block to be cached by default")
	}
	if req.System[0].CacheControl.Type != "ephemeral" || req.System[0].CacheControl.TTL != "" {
		t.Errorf("expected ephemeral/default TTL, got %+v", req.System[0].CacheControl)
	}
}

func TestBuildRequest_WithoutCacheDisablesSystem(t *testing.T) {
	client := newCacheTestClient(t)
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "system prompt"},
		{Role: llms.RoleUser, Content: "hi"},
	}
	opts := llms.ApplyOptions(llms.WithoutCache())
	req, err := client.buildRequest(messages, opts, false)
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}
	if len(req.System) != 1 || req.System[0].CacheControl != nil {
		t.Errorf("expected system caching disabled, got %+v", req.System[0].CacheControl)
	}
}

func TestBuildRequest_WithCacheCachesTools(t *testing.T) {
	client := newCacheTestClient(t)
	messages := []llms.Message{{Role: llms.RoleUser, Content: "hi"}}
	tool := llms.Tool{Type: llms.ToolTypeFunction, Function: &llms.FunctionDefinition{Name: "get_weather"}}
	opts := llms.ApplyOptions(llms.WithCacheTTL(time.Hour), llms.WithTools([]llms.Tool{tool}))
	req, err := client.buildRequest(messages, opts, false)
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}
	if len(req.Tools) != 1 || req.Tools[0].CacheControl == nil {
		t.Fatal("expected last tool to be cached when WithCache is set")
	}
	if req.Tools[0].CacheControl.TTL != "1h" {
		t.Errorf("expected 1h TTL, got %q", req.Tools[0].CacheControl.TTL)
	}
}

func TestConvertMessages_PerMessageCacheBreakpoint(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "cached context", CacheControl: &llms.CacheControl{TTL: time.Hour}},
		{Role: llms.RoleUser, Content: "question"},
	}
	result, err := convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	last := result[0].Content[len(result[0].Content)-1]
	if last.CacheControl == nil || last.CacheControl.TTL != "1h" {
		t.Errorf("expected cache breakpoint with 1h TTL on first message, got %+v", last.CacheControl)
	}
	// The unmarked message must not carry a breakpoint.
	if result[1].Content[0].CacheControl != nil {
		t.Error("expected no breakpoint on the unmarked message")
	}
}

func TestEnforceCacheLimit(t *testing.T) {
	cc := func() *anthropicapi.CacheControl { return &anthropicapi.CacheControl{Type: "ephemeral"} }
	req := &anthropicapi.MessagesRequest{
		System: []anthropicapi.SystemBlock{{CacheControl: cc()}},
		Tools:  []anthropicapi.Tool{{CacheControl: cc()}},
		Messages: []anthropicapi.Message{
			{Content: []anthropicapi.ContentPart{{CacheControl: cc()}, {CacheControl: cc()}}},
			{Content: []anthropicapi.ContentPart{{CacheControl: cc()}}}, // 5th — must be dropped
		},
	}
	enforceCacheLimit(req)

	count := 0
	if req.System[0].CacheControl != nil {
		count++
	}
	if req.Tools[0].CacheControl != nil {
		count++
	}
	for _, m := range req.Messages {
		for _, p := range m.Content {
			if p.CacheControl != nil {
				count++
			}
		}
	}
	if count != 4 {
		t.Errorf("expected breakpoints capped at 4, got %d", count)
	}
	// The 5th breakpoint (last message) must have been dropped.
	if req.Messages[1].Content[0].CacheControl != nil {
		t.Error("expected the 5th breakpoint to be dropped")
	}
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
