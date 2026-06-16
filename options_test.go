package llms

import (
	"testing"
	"time"
)

// TestCallOptions_Defaults tests default call options values
func TestCallOptions_Defaults(t *testing.T) {
	opts := DefaultCallOptions()

	// Temperature and TopP default to nil (unset) so the provider/model applies
	// its own default rather than the SDK forcing one.
	if opts.Temperature != nil {
		t.Errorf("expected Temperature to be nil (unset), got %v", *opts.Temperature)
	}
	if opts.MaxTokens != nil {
		t.Errorf("expected MaxTokens to be nil (unset), got %d", *opts.MaxTokens)
	}
	if opts.TopP != nil {
		t.Errorf("expected TopP to be nil (unset), got %v", *opts.TopP)
	}
	if opts.FrequencyPenalty != nil {
		t.Errorf("expected FrequencyPenalty to be nil (unset), got %v", *opts.FrequencyPenalty)
	}
	if opts.PresencePenalty != nil {
		t.Errorf("expected PresencePenalty to be nil (unset), got %v", *opts.PresencePenalty)
	}
	if opts.StreamBufferSize != 100 {
		t.Errorf("expected StreamBufferSize 100, got %d", opts.StreamBufferSize)
	}
	if opts.StreamSendTimeout != 30*time.Second {
		t.Errorf("expected StreamSendTimeout 30s, got %v", opts.StreamSendTimeout)
	}
}

func TestWithStreamBufferSize_Valid(t *testing.T) {
	opts := ApplyOptions(WithStreamBufferSize(200))
	if opts.StreamBufferSize != 200 {
		t.Errorf("expected StreamBufferSize 200, got %d", opts.StreamBufferSize)
	}
}

func TestWithStreamBufferSize_Zero(t *testing.T) {
	// Test that 0 or negative doesn't change default
	opts := ApplyOptions(WithStreamBufferSize(0))
	if opts.StreamBufferSize != 100 {
		t.Errorf("expected StreamBufferSize to remain 100, got %d", opts.StreamBufferSize)
	}
}

func TestWithStreamSendTimeout_Override(t *testing.T) {
	opts := ApplyOptions(WithStreamSendTimeout(60 * time.Second))
	if opts.StreamSendTimeout != 60*time.Second {
		t.Errorf("expected StreamSendTimeout 60s, got %v", opts.StreamSendTimeout)
	}
}

func TestWithDisableMessageMerging_Enabled(t *testing.T) {
	opts := ApplyOptions(WithDisableMessageMerging())
	if !opts.DisableMessageMerging {
		t.Error("expected DisableMessageMerging to be true")
	}
}

func TestWithEstimateTokens_Enabled(t *testing.T) {
	opts := ApplyOptions(WithEstimateTokens())
	if !opts.EstimateTokens {
		t.Error("expected EstimateTokens to be true")
	}
}

func TestWithTrace_Sets(t *testing.T) {
	opts := ApplyOptions(WithTrace(TraceOptions{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		ParentID:  "parent-789",
		UserID:    "user-abc",
		SessionID: "session-def",
		Tags:      []string{"tag1", "tag2", "tag3"},
		Metadata:  map[string]any{"key1": "value1", "count": 2},
		Version:   "v1.2.3",
	}))

	if opts.Trace == nil {
		t.Fatal("expected Trace to be set")
	}
	if opts.Trace.TraceID != "trace-123" {
		t.Errorf("expected TraceID 'trace-123', got %s", opts.Trace.TraceID)
	}
	if opts.Trace.SpanID != "span-456" {
		t.Errorf("expected SpanID 'span-456', got %s", opts.Trace.SpanID)
	}
	if opts.Trace.ParentID != "parent-789" {
		t.Errorf("expected ParentID 'parent-789', got %s", opts.Trace.ParentID)
	}
	if opts.Trace.UserID != "user-abc" {
		t.Errorf("expected UserID 'user-abc', got %s", opts.Trace.UserID)
	}
	if opts.Trace.SessionID != "session-def" {
		t.Errorf("expected SessionID 'session-def', got %s", opts.Trace.SessionID)
	}
	if len(opts.Trace.Tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(opts.Trace.Tags))
	}
	if opts.Trace.Metadata["count"] != 2 {
		t.Errorf("expected metadata count=2, got %v", opts.Trace.Metadata["count"])
	}
	if opts.Trace.Version != "v1.2.3" {
		t.Errorf("expected Version 'v1.2.3', got %s", opts.Trace.Version)
	}
}

func TestApplyOptions_MultipleTraceOptions(t *testing.T) {
	opts := ApplyOptions(
		WithTemperature(0.5),
		WithMaxTokens(512),
		WithModel("claude-3"),
		WithTrace(TraceOptions{
			UserID: "user-123",
			Tags:   []string{"production", "test"},
		}),
	)

	if opts.Temperature == nil || *opts.Temperature != 0.5 {
		t.Errorf("expected Temperature 0.5, got %v", opts.Temperature)
	}
	if opts.MaxTokens == nil || *opts.MaxTokens != 512 {
		t.Errorf("expected MaxTokens 512, got %v", opts.MaxTokens)
	}
	if opts.Model != "claude-3" {
		t.Errorf("expected Model 'claude-3', got %s", opts.Model)
	}
	if opts.Trace == nil {
		t.Fatal("expected Trace to be set")
	}
	if opts.Trace.UserID != "user-123" {
		t.Errorf("expected Trace.UserID 'user-123', got %s", opts.Trace.UserID)
	}
	if len(opts.Trace.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(opts.Trace.Tags))
	}
}

func TestApplyOptions_PenaltyZeroIsExplicit(t *testing.T) {
	opts := ApplyOptions(
		WithFrequencyPenalty(0),
		WithPresencePenalty(0),
	)

	if opts.FrequencyPenalty == nil {
		t.Fatal("expected FrequencyPenalty to be set")
	}
	if *opts.FrequencyPenalty != 0 {
		t.Errorf("expected FrequencyPenalty 0, got %v", *opts.FrequencyPenalty)
	}
	if opts.PresencePenalty == nil {
		t.Fatal("expected PresencePenalty to be set")
	}
	if *opts.PresencePenalty != 0 {
		t.Errorf("expected PresencePenalty 0, got %v", *opts.PresencePenalty)
	}
}

func TestCallOptions_Validate_Valid(t *testing.T) {
	opts := DefaultCallOptions()
	err := opts.Validate()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCallOptions_Validate_InvalidTemperature(t *testing.T) {
	tests := []struct {
		name string
		temp float64
	}{
		{"too high", 2.5},
		{"negative", -0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &CallOptions{
				Temperature: float64Ptr(tt.temp),
				TopP:        float64Ptr(1.0),
			}
			if err := opts.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestCallOptions_Validate_InvalidMaxTokens(t *testing.T) {
	opts := &CallOptions{
		Temperature: float64Ptr(0.7),
		MaxTokens:   intPtr(-100),
		TopP:        float64Ptr(1.0),
	}
	if err := opts.Validate(); err == nil {
		t.Error("expected validation error for negative max tokens")
	}
}

func TestCallOptions_Validate_InvalidTopP(t *testing.T) {
	tests := []struct {
		name string
		topP float64
	}{
		{"too high", 1.5},
		{"negative", -0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &CallOptions{
				Temperature: float64Ptr(0.7),
				TopP:        float64Ptr(tt.topP),
			}
			if err := opts.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestCallOptions_Validate_InvalidFrequencyPenalty(t *testing.T) {
	tests := []struct {
		name    string
		penalty float64
	}{
		{"too high", 2.5},
		{"too low", -2.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &CallOptions{
				Temperature:      float64Ptr(0.7),
				TopP:             float64Ptr(1.0),
				FrequencyPenalty: float64Ptr(tt.penalty),
			}
			if err := opts.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestCallOptions_Validate_InvalidPresencePenalty(t *testing.T) {
	opts := &CallOptions{
		Temperature:     float64Ptr(0.7),
		TopP:            float64Ptr(1.0),
		PresencePenalty: float64Ptr(2.5),
	}
	if err := opts.Validate(); err == nil {
		t.Error("expected validation error")
	}
}

func TestCallOptions_Validate_ToolWithoutType(t *testing.T) {
	opts := &CallOptions{
		Temperature: float64Ptr(0.7),
		TopP:        float64Ptr(1.0),
		Tools: []Tool{
			{Type: "", Function: &FunctionDefinition{Name: "test"}},
		},
	}
	if err := opts.Validate(); err == nil {
		t.Error("expected validation error for tool without type")
	}
}

func TestCallOptions_Validate_FunctionToolWithoutDefinition(t *testing.T) {
	opts := &CallOptions{
		Temperature: float64Ptr(0.7),
		TopP:        float64Ptr(1.0),
		Tools: []Tool{
			{Type: "function", Function: nil},
		},
	}
	if err := opts.Validate(); err == nil {
		t.Error("expected validation error for function tool without definition")
	}
}

func TestCallOptions_Validate_FunctionToolWithoutName(t *testing.T) {
	opts := &CallOptions{
		Temperature: float64Ptr(0.7),
		TopP:        float64Ptr(1.0),
		Tools: []Tool{
			{Type: "function", Function: &FunctionDefinition{Name: ""}},
		},
	}
	if err := opts.Validate(); err == nil {
		t.Error("expected validation error for function tool without name")
	}
}

func TestCallOptions_Validate_ValidTool(t *testing.T) {
	opts := &CallOptions{
		Temperature: float64Ptr(0.7),
		TopP:        float64Ptr(1.0),
		Tools: []Tool{
			{Type: "function", Function: &FunctionDefinition{Name: "get_weather"}},
		},
	}
	if err := opts.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestWithThinkingMode_Enabled(t *testing.T) {
	// WithThinkingMode is deprecated; it now forwards to ReasoningConfig.Enabled.
	opts := ApplyOptions(WithThinkingMode(true))

	if opts.Reasoning == nil || opts.Reasoning.Enabled == nil {
		t.Fatal("expected Reasoning.Enabled to be set")
	}
	if !*opts.Reasoning.Enabled {
		t.Error("expected Reasoning.Enabled == true")
	}
	if !opts.Reasoning.IsEnabled() {
		t.Error("expected IsEnabled() == true")
	}
}

func TestWithThinkingMode_Disabled(t *testing.T) {
	opts := ApplyOptions(WithThinkingMode(false))

	if opts.Reasoning == nil || opts.Reasoning.Enabled == nil {
		t.Fatal("expected Reasoning.Enabled to be set")
	}
	if *opts.Reasoning.Enabled {
		t.Error("expected Reasoning.Enabled == false")
	}
	if opts.Reasoning.IsEnabled() {
		t.Error("expected IsEnabled() == false")
	}
}

func TestWithReasoningEffort(t *testing.T) {
	opts := ApplyOptions(WithReasoningEffort(ReasoningEffortHigh))
	if opts.Reasoning == nil {
		t.Fatal("expected Reasoning to be set")
	}
	if opts.Reasoning.Effort != ReasoningEffortHigh {
		t.Errorf("expected effort=high, got %v", opts.Reasoning.Effort)
	}
	if !opts.Reasoning.IsEnabled() {
		t.Error("expected IsEnabled() == true when an effort is set")
	}
}

func TestWithReasoningBudget(t *testing.T) {
	opts := ApplyOptions(WithReasoningBudget(8192))
	if opts.Reasoning == nil || opts.Reasoning.BudgetTokens != 8192 {
		t.Fatalf("expected budget=8192, got %+v", opts.Reasoning)
	}
	if !opts.Reasoning.IsEnabled() {
		t.Error("expected IsEnabled() == true when a budget is set")
	}
}

func TestWithWebSearch(t *testing.T) {
	opts := ApplyOptions(WithWebSearch(WebSearchConfig{
		Enabled:       true,
		Provider:      WebSearchBrave,
		ResultCount:   5,
		RecencyFilter: "week",
	}))

	if opts.WebSearch == nil {
		t.Fatal("expected WebSearch config")
	}
	if !opts.WebSearch.Enabled {
		t.Error("expected enabled")
	}
	if opts.WebSearch.Provider != WebSearchBrave {
		t.Errorf("expected brave, got %s", opts.WebSearch.Provider)
	}
	if opts.WebSearch.ResultCount != 5 {
		t.Errorf("expected 5, got %d", opts.WebSearch.ResultCount)
	}
}

func TestWithWebSearchEnabled(t *testing.T) {
	opts := ApplyOptions(WithWebSearchEnabled())

	if opts.WebSearch == nil {
		t.Fatal("expected WebSearch config")
	}
	if !opts.WebSearch.Enabled {
		t.Error("expected enabled")
	}
}
