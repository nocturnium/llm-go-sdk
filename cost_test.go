package llms

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	testCostHelloResponse = "Hello!"
	testCostHelloWorld    = "Hello World"
	testCostModel         = "test-model"
)

func TestEstimateCost_DefaultModelsPriced(t *testing.T) {
	// The current default/flagship models must be priced so CostTracker does not
	// silently report $0 out of the box.
	cases := []struct {
		provider Provider
		model    string
	}{
		{ProviderAnthropic, "claude-sonnet-4-20250514"},
		{ProviderOpenAI, "gpt-4.1"},
		{ProviderOpenAI, "o3"},
		{ProviderOpenAI, "o4-mini"},
		{ProviderGemini, "gemini-2.5-pro"},
		{ProviderGemini, "gemini-2.5-flash"},
		{ProviderDeepSeek, "deepseek-chat"},
	}
	u := Usage{PromptTokens: 1000, CompletionTokens: 1000}
	for _, c := range cases {
		got, known := EstimateCostKnown(c.provider, c.model, u)
		if !known {
			t.Errorf("%s:%s pricing is unknown", c.provider, c.model)
		}
		if got <= 0 {
			t.Errorf("%s:%s estimated at $%v, expected > 0", c.provider, c.model, got)
		}
	}
}

func TestPricing_DefaultPricing(t *testing.T) {
	// Verify key models have pricing defined
	expectedModels := []string{
		"openai:gpt-4o",
		"openai:gpt-4o-mini",
		"anthropic:claude-3-5-sonnet-20241022",
		"anthropic:claude-3-opus-20240229",
		"gemini:gemini-1.5-pro",
		"gemini:gemini-1.5-flash",
		"togetherai:meta-llama/Llama-3.3-70B-Instruct-Turbo",
	}

	for _, model := range expectedModels {
		if _, ok := DefaultPricing[model]; !ok {
			t.Errorf("missing pricing for %s", model)
		}
	}
}

func TestNewCostTracker_Defaults(t *testing.T) {
	tracker := NewCostTracker()

	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}

	// Should have default pricing loaded
	pricing, ok := tracker.GetPricing(ProviderOpenAI, "gpt-4o")
	if !ok {
		t.Error("expected default pricing for openai:gpt-4o")
	}
	if pricing.PromptPerMillion != 2.50 {
		t.Errorf("PromptPerMillion = %f, want 2.50", pricing.PromptPerMillion)
	}
}

func TestNewCostTracker_CustomPricing(t *testing.T) {
	customPricing := map[string]Pricing{
		"openai:custom-model": {PromptPerMillion: 1.00, CompletionPerMillion: 2.00},
		"openai:gpt-4o":       {PromptPerMillion: 5.00, CompletionPerMillion: 20.00}, // Override default
	}

	tracker := NewCostTracker(customPricing)

	// Custom model should be available
	pricing, ok := tracker.GetPricing(ProviderOpenAI, "custom-model")
	if !ok {
		t.Error("expected custom pricing for openai:custom-model")
	}
	if pricing.PromptPerMillion != 1.00 {
		t.Errorf("PromptPerMillion = %f, want 1.00", pricing.PromptPerMillion)
	}

	// Override should work
	pricing, _ = tracker.GetPricing(ProviderOpenAI, "gpt-4o")
	if pricing.PromptPerMillion != 5.00 {
		t.Errorf("PromptPerMillion = %f, want 5.00 (overridden)", pricing.PromptPerMillion)
	}
}

func TestCostTracker_Record(t *testing.T) {
	tracker := NewCostTracker()

	usage := Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
	}

	cost, known := tracker.Record(ProviderOpenAI, "gpt-4o", usage)
	if !known {
		t.Fatal("expected pricing to be known")
	}

	result := tracker.GetUsage(ProviderOpenAI, "gpt-4o")
	if result == nil {
		t.Fatal("expected usage to be recorded")
	}

	if result.PromptTokens != 1000 {
		t.Errorf("PromptTokens = %d, want 1000", result.PromptTokens)
	}
	if result.CompletionTokens != 500 {
		t.Errorf("CompletionTokens = %d, want 500", result.CompletionTokens)
	}
	if result.Requests != 1 {
		t.Errorf("Requests = %d, want 1", result.Requests)
	}

	// Expected cost: (1000/1M * 2.50) + (500/1M * 10.00) = 0.0025 + 0.005 = 0.0075
	expectedCost := 0.0075
	if cost != expectedCost {
		t.Errorf("recorded cost = %f, want %f", cost, expectedCost)
	}
	if result.EstimatedCost != expectedCost {
		t.Errorf("EstimatedCost = %f, want %f", result.EstimatedCost, expectedCost)
	}
}

func TestCostTracker_RecordUnknownPricing(t *testing.T) {
	tracker := NewCostTracker()

	cost, known := tracker.Record(Provider("unpriced"), "missing-model", Usage{PromptTokens: 1000})
	if known {
		t.Fatal("expected pricing to be unknown")
	}
	if cost != 0 {
		t.Errorf("cost = %f, want 0", cost)
	}

	result := tracker.GetUsage(Provider("unpriced"), "missing-model")
	if result == nil {
		t.Fatal("expected usage to be recorded even with unknown pricing")
	}
	if result.EstimatedCost != 0 {
		t.Errorf("EstimatedCost = %f, want 0", result.EstimatedCost)
	}
}

func TestCostTracker_RecordMultiple(t *testing.T) {
	tracker := NewCostTracker()

	// Record multiple usages
	for i := 0; i < 3; i++ {
		tracker.Record(ProviderOpenAI, "gpt-4o", Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
		})
	}

	result := tracker.GetUsage(ProviderOpenAI, "gpt-4o")
	if result.PromptTokens != 300 {
		t.Errorf("PromptTokens = %d, want 300", result.PromptTokens)
	}
	if result.Requests != 3 {
		t.Errorf("Requests = %d, want 3", result.Requests)
	}
}

func TestCostTracker_RecordEmbedding(t *testing.T) {
	tracker := NewCostTracker()

	tracker.RecordEmbedding(ProviderOpenAI, "text-embedding-3-small", EmbeddingUsage{
		PromptTokens: 100,
		TotalTokens:  100,
	})

	result := tracker.GetUsage(ProviderOpenAI, "text-embedding-3-small")
	if result == nil {
		t.Fatal("expected usage to be recorded")
	}
	if result.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", result.PromptTokens)
	}
}

func TestCostTracker_GetUsage_NotFound(t *testing.T) {
	tracker := NewCostTracker()

	result := tracker.GetUsage(ProviderOpenAI, "nonexistent-model")
	if result != nil {
		t.Error("expected nil for nonexistent model")
	}
}

func TestCostTracker_GetTotalCost(t *testing.T) {
	tracker := NewCostTracker()

	// Record usage for multiple models
	tracker.Record(ProviderOpenAI, "gpt-4o", Usage{PromptTokens: 1000000})                    // $2.50
	tracker.Record(ProviderAnthropic, "claude-3-opus-20240229", Usage{PromptTokens: 1000000}) // $15.00

	total := tracker.GetTotalCost()
	expected := 17.50
	if total != expected {
		t.Errorf("TotalCost = %f, want %f", total, expected)
	}
}

func TestCostTracker_GetTotalTokens(t *testing.T) {
	tracker := NewCostTracker()

	tracker.Record(ProviderOpenAI, "gpt-4o", Usage{PromptTokens: 100, CompletionTokens: 50})
	tracker.Record(ProviderOpenAI, "gpt-4o-mini", Usage{PromptTokens: 200, CompletionTokens: 100})

	prompt, completion := tracker.GetTotalTokens()
	if prompt != 300 {
		t.Errorf("prompt tokens = %d, want 300", prompt)
	}
	if completion != 150 {
		t.Errorf("completion tokens = %d, want 150", completion)
	}
}

func TestCostTracker_GetTotalRequests(t *testing.T) {
	tracker := NewCostTracker()

	tracker.Record(ProviderOpenAI, "gpt-4o", Usage{})
	tracker.Record(ProviderOpenAI, "gpt-4o", Usage{})
	tracker.Record(ProviderAnthropic, "claude-3-opus-20240229", Usage{})

	total := tracker.GetTotalRequests()
	if total != 3 {
		t.Errorf("TotalRequests = %d, want 3", total)
	}
}

func TestCostTracker_Report(t *testing.T) {
	tracker := NewCostTracker()

	tracker.Record(ProviderOpenAI, "gpt-4o", Usage{PromptTokens: 100})
	tracker.Record(ProviderAnthropic, "claude-3-opus-20240229", Usage{PromptTokens: 200})

	report := tracker.Report()
	if len(report) != 2 {
		t.Errorf("report length = %d, want 2", len(report))
	}

	// Verify both models are in report
	models := make(map[string]bool)
	for _, u := range report {
		models[u.Model] = true
	}
	if !models["gpt-4o"] || !models["claude-3-opus-20240229"] {
		t.Error("expected both models in report")
	}
}

func TestCostTracker_Reset(t *testing.T) {
	tracker := NewCostTracker()

	tracker.Record(ProviderOpenAI, "gpt-4o", Usage{PromptTokens: 100})
	tracker.Reset()

	if tracker.GetTotalRequests() != 0 {
		t.Error("expected 0 requests after reset")
	}
	if len(tracker.Report()) != 0 {
		t.Error("expected empty report after reset")
	}
}

func TestCostTracker_SetPricing(t *testing.T) {
	tracker := NewCostTracker()

	tracker.SetPricing(ProviderOpenAI, "custom-model", Pricing{PromptPerMillion: 1.00, CompletionPerMillion: 2.00})

	pricing, ok := tracker.GetPricing(ProviderOpenAI, "custom-model")
	if !ok {
		t.Error("expected pricing to be set")
	}
	if pricing.PromptPerMillion != 1.00 {
		t.Errorf("PromptPerMillion = %f, want 1.00", pricing.PromptPerMillion)
	}
}

func TestCostTracker_GetPricing_NotFound(t *testing.T) {
	tracker := NewCostTracker()

	_, ok := tracker.GetPricing(ProviderOpenAI, "nonexistent-model")
	if ok {
		t.Error("expected ok to be false for nonexistent model")
	}
}

func TestCostTracker_Timestamps(t *testing.T) {
	tracker := NewCostTracker()

	before := time.Now()
	tracker.Record(ProviderOpenAI, "gpt-4o", Usage{})
	after := time.Now()

	result := tracker.GetUsage(ProviderOpenAI, "gpt-4o")
	if result.FirstUsed.Before(before) || result.FirstUsed.After(after) {
		t.Error("FirstUsed timestamp out of range")
	}
	if result.LastUsed.Before(before) || result.LastUsed.After(after) {
		t.Error("LastUsed timestamp out of range")
	}

	// Record again
	time.Sleep(10 * time.Millisecond)
	tracker.Record(ProviderOpenAI, "gpt-4o", Usage{})
	result = tracker.GetUsage(ProviderOpenAI, "gpt-4o")

	// FirstUsed should not change, LastUsed should
	if result.FirstUsed.After(after) {
		t.Error("FirstUsed should not change on subsequent records")
	}
	if !result.LastUsed.After(after) {
		t.Error("LastUsed should update on subsequent records")
	}
}

// mockCostLLM is a mock LLM for testing cost middleware
type mockCostLLM struct {
	provider Provider
	model    string
	callResp string
	callErr  error
	genResp  *Response
	genErr   error
	chunks   []StreamChunk
}

func (m *mockCostLLM) Call(_ context.Context, _ string, _ ...CallOption) (string, error) {
	if m.callErr != nil {
		return "", m.callErr
	}
	return m.callResp, nil
}

func (m *mockCostLLM) GenerateContent(_ context.Context, _ []Message, _ ...CallOption) (*Response, error) {
	if m.genErr != nil {
		return nil, m.genErr
	}
	return m.genResp, nil
}

func (m *mockCostLLM) Stream(_ context.Context, _ []Message, _ ...CallOption) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, len(m.chunks))
	for _, chunk := range m.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (m *mockCostLLM) Provider() Provider { return m.provider }
func (m *mockCostLLM) Model() string      { return m.model }

func TestNewCostMiddleware(t *testing.T) {
	llm := &mockCostLLM{provider: ProviderOpenAI, model: "gpt-4o"}

	middleware := NewCostMiddleware(llm, nil)

	if middleware.llm != llm {
		t.Error("llm not set correctly")
	}
	if middleware.tracker == nil {
		t.Error("tracker should be auto-created when nil")
	}
}

func TestNewCostMiddleware_WithTracker(t *testing.T) {
	llm := &mockCostLLM{provider: ProviderOpenAI, model: "gpt-4o"}
	tracker := NewCostTracker()

	middleware := NewCostMiddleware(llm, tracker)

	if middleware.Tracker() != tracker {
		t.Error("should use provided tracker")
	}
}

func TestCostMiddleware_Call(t *testing.T) {
	llm := &mockCostLLM{
		provider: ProviderOpenAI,
		model:    "gpt-4o",
		genResp: &Response{
			Content: testCostHelloResponse,
			Usage:   Usage{PromptTokens: 10, CompletionTokens: 5},
		},
	}
	tracker := NewCostTracker()
	middleware := NewCostMiddleware(llm, tracker)

	result, err := middleware.Call(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != testCostHelloResponse {
		t.Errorf("result = %s, want 'Hello!'", result)
	}

	// Verify tracking
	usage := tracker.GetUsage(ProviderOpenAI, "gpt-4o")
	if usage == nil {
		t.Fatal("expected usage to be tracked")
	}
	if usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", usage.PromptTokens)
	}
}

func TestCostMiddleware_GenerateContent(t *testing.T) {
	llm := &mockCostLLM{
		provider: ProviderAnthropic,
		model:    "claude-3-opus-20240229",
		genResp: &Response{
			Content: "Response",
			Usage:   Usage{PromptTokens: 100, CompletionTokens: 50},
		},
	}
	tracker := NewCostTracker()
	middleware := NewCostMiddleware(llm, tracker)

	resp, err := middleware.GenerateContent(context.Background(), []Message{
		{Role: RoleUser, Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Response" {
		t.Errorf("content = %s, want 'Response'", resp.Content)
	}

	// Verify tracking
	usage := tracker.GetUsage(ProviderAnthropic, "claude-3-opus-20240229")
	if usage.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d, want 50", usage.CompletionTokens)
	}
}

func TestCostMiddleware_GenerateContent_Error(t *testing.T) {
	expectedErr := errors.New("api error")
	llm := &mockCostLLM{
		provider: ProviderOpenAI,
		model:    "gpt-4o",
		genErr:   expectedErr,
	}
	tracker := NewCostTracker()
	middleware := NewCostMiddleware(llm, tracker)

	_, err := middleware.GenerateContent(context.Background(), []Message{
		{Role: RoleUser, Content: "Hello"},
	})
	if !errors.Is(err, expectedErr) {
		t.Errorf("err = %v, want %v", err, expectedErr)
	}

	// Should not track failed requests
	if tracker.GetTotalRequests() != 0 {
		t.Error("should not track failed requests")
	}
}

func TestCostMiddleware_Stream(t *testing.T) {
	llm := &mockCostLLM{
		provider: ProviderOpenAI,
		model:    "gpt-4o",
		chunks: []StreamChunk{
			{Content: "Hello"},
			{Content: " World"},
			{Usage: &Usage{PromptTokens: 20, CompletionTokens: 10}},
		},
	}
	tracker := NewCostTracker()
	middleware := NewCostMiddleware(llm, tracker)

	stream, err := middleware.Stream(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Consume stream
	var content string
	for chunk := range stream {
		content += chunk.Content
	}

	if content != testCostHelloWorld {
		t.Errorf("content = %s, want %q", content, testCostHelloWorld)
	}

	// Wait for goroutine to finish tracking
	time.Sleep(10 * time.Millisecond)

	usage := tracker.GetUsage(ProviderOpenAI, "gpt-4o")
	if usage == nil {
		t.Fatal("expected usage to be tracked")
	}
	if usage.PromptTokens != 20 {
		t.Errorf("PromptTokens = %d, want 20", usage.PromptTokens)
	}
}

func TestCostMiddleware_GetProvider(t *testing.T) {
	llm := &mockCostLLM{provider: ProviderGemini}
	middleware := NewCostMiddleware(llm, nil)

	if middleware.Provider() != ProviderGemini {
		t.Errorf("provider = %s, want gemini", middleware.Provider())
	}
}

func TestCostMiddleware_GetModel(t *testing.T) {
	llm := &mockCostLLM{model: testCostModel}
	middleware := NewCostMiddleware(llm, nil)

	if middleware.Model() != testCostModel {
		t.Errorf("model = %s, want test-model", middleware.Model())
	}
}

func TestCostMiddleware_Unwrap(t *testing.T) {
	llm := &mockCostLLM{}
	middleware := NewCostMiddleware(llm, nil)

	if middleware.Unwrap() != llm {
		t.Error("Unwrap should return underlying LLM")
	}
}

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		provider Provider
		model    string
		usage    Usage
		expected float64
	}{
		{
			provider: ProviderOpenAI,
			model:    "gpt-4o",
			usage:    Usage{PromptTokens: 1000000, CompletionTokens: 1000000},
			expected: 12.50, // 2.50 + 10.00
		},
		{
			provider: ProviderOpenAI,
			model:    "gpt-4o-mini",
			usage:    Usage{PromptTokens: 1000000, CompletionTokens: 1000000},
			expected: 0.75, // 0.15 + 0.60
		},
		{
			provider: ProviderAnthropic,
			model:    "claude-3-opus-20240229",
			usage:    Usage{PromptTokens: 1000000, CompletionTokens: 1000000},
			expected: 90.00, // 15.00 + 75.00
		},
		{
			provider: ProviderOpenAI,
			model:    "unknown-model",
			usage:    Usage{PromptTokens: 1000},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(string(tc.provider)+":"+tc.model, func(t *testing.T) {
			cost := EstimateCost(tc.provider, tc.model, tc.usage)
			if cost != tc.expected {
				t.Errorf("cost = %f, want %f", cost, tc.expected)
			}
		})
	}
}

func TestEstimateCost_UnknownPricing(t *testing.T) {
	cost, known := EstimateCostKnown(Provider("unpriced"), "missing-model", Usage{PromptTokens: 1000})
	if known {
		t.Fatal("expected pricing to be unknown")
	}
	if cost != 0 {
		t.Errorf("cost = %f, want 0", cost)
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		cost     float64
		expected string
	}{
		{0.001, "$0.0010"},
		{0.005, "$0.0050"},
		{0.01, "$0.01"},
		{0.50, "$0.50"},
		{1.00, "$1.00"},
		{10.50, "$10.50"},
		{100.00, "$100.00"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := FormatCost(tc.cost)
			if result != tc.expected {
				t.Errorf("FormatCost(%f) = %s, want %s", tc.cost, result, tc.expected)
			}
		})
	}
}
