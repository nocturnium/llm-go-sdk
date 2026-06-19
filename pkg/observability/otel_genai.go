package observability

import (
	"go.opentelemetry.io/otel/attribute"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

// GenAI Semantic Conventions for OpenTelemetry
// These follow the OpenTelemetry semantic conventions for GenAI/LLM instrumentation
// and are compatible with Langfuse's OpenTelemetry integration.
//
// Reference: https://opentelemetry.io/docs/specs/semconv/gen-ai/
// Reference: https://langfuse.com/integrations/native/opentelemetry

const (
	// GenAI System Attributes
	// These identify the AI system/provider being used

	// AttrGenAISystem is the name of the GenAI system (e.g., "openai", "anthropic")
	AttrGenAISystem = "gen_ai.system"

	// AttrGenAIOperationName is the name of the operation being performed
	AttrGenAIOperationName = "gen_ai.operation.name"

	// GenAI Request Attributes
	// These describe the request being made to the model

	// AttrGenAIRequestModel is the name of the model used for the request
	AttrGenAIRequestModel = "gen_ai.request.model"

	// AttrGenAIRequestTemperature is the temperature setting for the request
	AttrGenAIRequestTemperature = "gen_ai.request.temperature"

	// AttrGenAIRequestMaxTokens is the maximum tokens setting for the request
	AttrGenAIRequestMaxTokens = "gen_ai.request.max_tokens"

	// AttrGenAIRequestTopP is the top_p setting for the request
	AttrGenAIRequestTopP = "gen_ai.request.top_p"

	// AttrGenAIRequestFrequencyPenalty is the frequency penalty setting
	AttrGenAIRequestFrequencyPenalty = "gen_ai.request.frequency_penalty"

	// AttrGenAIRequestPresencePenalty is the presence penalty setting
	AttrGenAIRequestPresencePenalty = "gen_ai.request.presence_penalty"

	// AttrGenAIRequestStopSequences is the stop sequences for the request
	AttrGenAIRequestStopSequences = "gen_ai.request.stop_sequences"

	// GenAI Response Attributes
	// These describe the response from the model

	// AttrGenAIResponseModel is the model that generated the response
	AttrGenAIResponseModel = "gen_ai.response.model"

	// AttrGenAIResponseFinishReason is the reason the model stopped generating
	AttrGenAIResponseFinishReason = "gen_ai.response.finish_reason"

	// AttrGenAIResponseID is the unique ID of the response
	AttrGenAIResponseID = "gen_ai.response.id"

	// GenAI Content Attributes
	// These capture the actual prompt and completion content

	// AttrGenAIPrompt is the input prompt text (may be truncated)
	AttrGenAIPrompt = "gen_ai.prompt"

	// AttrGenAICompletion is the generated completion text (may be truncated)
	AttrGenAICompletion = "gen_ai.completion"

	// GenAI Usage Attributes
	// These track token usage and costs

	// AttrGenAIUsagePromptTokens is the number of tokens in the prompt
	AttrGenAIUsagePromptTokens = "gen_ai.usage.prompt_tokens"

	// AttrGenAIUsageCompletionTokens is the number of tokens in the completion
	AttrGenAIUsageCompletionTokens = "gen_ai.usage.completion_tokens"

	// AttrGenAIUsageTotalTokens is the total number of tokens used
	AttrGenAIUsageTotalTokens = "gen_ai.usage.total_tokens"

	// AttrGenAIUsageCost is the estimated cost in USD
	AttrGenAIUsageCost = "gen_ai.usage.cost"

	// Langfuse-Specific Attributes
	// These are specific to Langfuse's data model

	// AttrLangfuseUserID is the user ID for the trace
	AttrLangfuseUserID = "langfuse.user.id"

	// AttrLangfuseSessionID is the session ID for the trace
	AttrLangfuseSessionID = "langfuse.session.id"

	// AttrLangfuseTags are the tags for the trace
	AttrLangfuseTags = "langfuse.tags"

	// AttrLangfuseVersion is the version for the trace
	AttrLangfuseVersion = "langfuse.version"

	// AttrLangfuseRelease is the release for the trace
	AttrLangfuseRelease = "langfuse.release"

	// AttrLangfuseEnvironment is the environment for the trace
	AttrLangfuseEnvironment = "langfuse.environment"

	// AttrLangfusePromptName is the name of the prompt template used
	AttrLangfusePromptName = "langfuse.prompt.name"

	// AttrLangfuseTraceID is the Langfuse trace ID
	AttrLangfuseTraceID = "langfuse.trace.id"

	// AttrLangfuseObservationID is the Langfuse observation (span) ID
	AttrLangfuseObservationID = "langfuse.observation.id"

	// AttrLangfuseParentObservationID is the parent observation ID
	AttrLangfuseParentObservationID = "langfuse.parent_observation.id"

	// Alternative Conventions (OpenInference, MLflow)
	// These are recognized by Langfuse for compatibility with other frameworks

	// AttrInputValue is the OpenInference convention for input
	AttrInputValue = "input.value"

	// AttrOutputValue is the OpenInference convention for output
	AttrOutputValue = "output.value"

	// AttrMLflowSpanInputs is the MLflow convention for inputs
	AttrMLflowSpanInputs = "mlflow.spanInputs"

	// AttrMLflowSpanOutputs is the MLflow convention for outputs
	AttrMLflowSpanOutputs = "mlflow.spanOutputs"

	// Timing Attributes

	// AttrGenAITimeToFirstToken is the time to receive the first token (streaming)
	AttrGenAITimeToFirstToken = "gen_ai.time_to_first_token"

	// AttrGenAITokensPerSecond is the token generation rate
	AttrGenAITokensPerSecond = "gen_ai.tokens_per_second"
)

// GenAI Metric Names following OpenTelemetry semantic conventions
const (
	// MetricGenAIRequests counts the number of GenAI requests
	MetricGenAIRequests = "gen_ai.client.requests"

	// MetricGenAITokensPrompt counts prompt tokens used
	MetricGenAITokensPrompt = "gen_ai.client.tokens.prompt"

	// MetricGenAITokensCompletion counts completion tokens generated
	MetricGenAITokensCompletion = "gen_ai.client.tokens.completion"

	// MetricGenAIDuration measures request duration
	MetricGenAIDuration = "gen_ai.client.duration"

	// MetricGenAICost tracks estimated cost
	MetricGenAICost = "gen_ai.client.cost"

	// MetricGenAITimeToFirstToken measures time to first token for streaming
	MetricGenAITimeToFirstToken = "gen_ai.client.time_to_first_token"
)

// Attribute key helpers for OTel
var (
	// GenAI System
	keyGenAISystem        = attribute.Key(AttrGenAISystem)
	keyGenAIOperationName = attribute.Key(AttrGenAIOperationName)

	// GenAI Request
	keyGenAIRequestModel            = attribute.Key(AttrGenAIRequestModel)
	keyGenAIRequestTemperature      = attribute.Key(AttrGenAIRequestTemperature)
	keyGenAIRequestMaxTokens        = attribute.Key(AttrGenAIRequestMaxTokens)
	keyGenAIRequestTopP             = attribute.Key(AttrGenAIRequestTopP)
	keyGenAIRequestFrequencyPenalty = attribute.Key(AttrGenAIRequestFrequencyPenalty)
	keyGenAIRequestPresencePenalty  = attribute.Key(AttrGenAIRequestPresencePenalty)

	// GenAI Response
	keyGenAIResponseModel        = attribute.Key(AttrGenAIResponseModel)
	keyGenAIResponseFinishReason = attribute.Key(AttrGenAIResponseFinishReason)

	// GenAI Content
	keyGenAIPrompt     = attribute.Key(AttrGenAIPrompt)
	keyGenAICompletion = attribute.Key(AttrGenAICompletion)

	// GenAI Usage
	keyGenAIUsagePromptTokens     = attribute.Key(AttrGenAIUsagePromptTokens)
	keyGenAIUsageCompletionTokens = attribute.Key(AttrGenAIUsageCompletionTokens)
	keyGenAIUsageTotalTokens      = attribute.Key(AttrGenAIUsageTotalTokens)
	keyGenAIUsageCost             = attribute.Key(AttrGenAIUsageCost)

	// Langfuse-specific
	keyLangfuseUserID              = attribute.Key(AttrLangfuseUserID)
	keyLangfuseSessionID           = attribute.Key(AttrLangfuseSessionID)
	keyLangfuseTags                = attribute.Key(AttrLangfuseTags)
	keyLangfuseVersion             = attribute.Key(AttrLangfuseVersion)
	keyLangfuseRelease             = attribute.Key(AttrLangfuseRelease)
	keyLangfuseEnvironment         = attribute.Key(AttrLangfuseEnvironment)
	keyLangfuseTraceID             = attribute.Key(AttrLangfuseTraceID)
	keyLangfuseObservationID       = attribute.Key(AttrLangfuseObservationID)
	keyLangfuseParentObservationID = attribute.Key(AttrLangfuseParentObservationID)

	// Alternative conventions
	keyInputValue  = attribute.Key(AttrInputValue)
	keyOutputValue = attribute.Key(AttrOutputValue)

	// Timing
	keyGenAITimeToFirstToken = attribute.Key(AttrGenAITimeToFirstToken)
	keyGenAITokensPerSecond  = attribute.Key(AttrGenAITokensPerSecond)
)

// ProviderToGenAISystem maps Provider to GenAI system name
func ProviderToGenAISystem(p llms.Provider) string {
	switch p {
	case llms.ProviderOpenAI:
		return "openai"
	case llms.ProviderAnthropic:
		return "anthropic"
	case llms.ProviderGemini:
		return "google"
	case llms.ProviderGroq:
		return "groq"
	case llms.ProviderFireworks:
		return "fireworks"
	case llms.ProviderPerplexity:
		return "perplexity"
	case llms.ProviderMistral:
		return "mistral"
	case llms.ProviderDeepSeek:
		return "deepseek"
	case llms.ProviderCerebras:
		return "cerebras"
	case llms.ProviderOllama:
		return "ollama"
	case llms.ProviderAzure:
		return "azure"
	case llms.ProviderInfinity:
		return "infinity"
	case llms.ProviderTogetherAI:
		return "together"
	case llms.ProviderFeatherless:
		return "featherless"
	case llms.ProviderRunPod:
		return "runpod"
	default:
		return string(p)
	}
}
