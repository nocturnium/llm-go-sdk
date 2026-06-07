package llms

import "testing"

func TestProviderToGenAISystem(t *testing.T) {
	tests := []struct {
		provider Provider
		expected string
	}{
		{ProviderOpenAI, "openai"},
		{ProviderAnthropic, "anthropic"},
		{ProviderGemini, "google"},
		{ProviderGroq, "groq"},
		{ProviderFireworks, "fireworks"},
		{ProviderPerplexity, "perplexity"},
		{ProviderMistral, "mistral"},
		{ProviderDeepSeek, "deepseek"},
		{ProviderCerebras, "cerebras"},
		{ProviderOllama, "ollama"},
		{ProviderAzure, "azure"},
		{ProviderInfinity, "infinity"},
		{ProviderTogetherAI, "together"},
		{ProviderFeatherless, "featherless"},
		{ProviderRunPod, "runpod"},
		{Provider("unknown"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			result := ProviderToGenAISystem(tt.provider)
			if result != tt.expected {
				t.Errorf("ProviderToGenAISystem(%s) = %s, want %s", tt.provider, result, tt.expected)
			}
		})
	}
}

func TestGenAIAttributeConstants(t *testing.T) {
	// Verify key constants are defined correctly
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"AttrGenAISystem", AttrGenAISystem, "gen_ai.system"},
		{"AttrGenAIOperationName", AttrGenAIOperationName, "gen_ai.operation.name"},
		{"AttrGenAIRequestModel", AttrGenAIRequestModel, "gen_ai.request.model"},
		{"AttrGenAIRequestTemperature", AttrGenAIRequestTemperature, "gen_ai.request.temperature"},
		{"AttrGenAIRequestMaxTokens", AttrGenAIRequestMaxTokens, "gen_ai.request.max_tokens"},
		{"AttrGenAIRequestTopP", AttrGenAIRequestTopP, "gen_ai.request.top_p"},
		{"AttrGenAIResponseModel", AttrGenAIResponseModel, "gen_ai.response.model"},
		{"AttrGenAIResponseFinishReason", AttrGenAIResponseFinishReason, "gen_ai.response.finish_reason"},
		{"AttrGenAIUsagePromptTokens", AttrGenAIUsagePromptTokens, "gen_ai.usage.prompt_tokens"},
		{"AttrGenAIUsageCompletionTokens", AttrGenAIUsageCompletionTokens, "gen_ai.usage.completion_tokens"},
		{"AttrGenAIUsageTotalTokens", AttrGenAIUsageTotalTokens, "gen_ai.usage.total_tokens"},
		{"AttrLangfuseUserID", AttrLangfuseUserID, "langfuse.user.id"},
		{"AttrLangfuseSessionID", AttrLangfuseSessionID, "langfuse.session.id"},
		{"AttrLangfuseTraceID", AttrLangfuseTraceID, "langfuse.trace.id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestGenAIMetricConstants(t *testing.T) {
	// Verify metric constants are defined correctly
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"MetricGenAIRequests", MetricGenAIRequests, "gen_ai.client.requests"},
		{"MetricGenAITokensPrompt", MetricGenAITokensPrompt, "gen_ai.client.tokens.prompt"},
		{"MetricGenAITokensCompletion", MetricGenAITokensCompletion, "gen_ai.client.tokens.completion"},
		{"MetricGenAIDuration", MetricGenAIDuration, "gen_ai.client.duration"},
		{"MetricGenAICost", MetricGenAICost, "gen_ai.client.cost"},
		{"MetricGenAITimeToFirstToken", MetricGenAITimeToFirstToken, "gen_ai.client.time_to_first_token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}
