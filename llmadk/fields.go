package llmadk

// Field names a field of a genai request type, as "Type.Field" (for example
// "GenerateContentConfig.TopK"). Pass rejected fields to WithIgnore to drop them
// instead of failing the request.
type Field string

// Fields that are rejected by default and can be ignored with WithIgnore.
// FieldHTTPOptions covers the HTTP options other than Timeout and Headers, and
// FieldValidatedFunctionCalling the VALIDATED function-calling mode, which then
// behaves as AUTO.
const (
	FieldTopK                       Field = "GenerateContentConfig.TopK"
	FieldSeed                       Field = "GenerateContentConfig.Seed"
	FieldResponseLogprobs           Field = "GenerateContentConfig.ResponseLogprobs"
	FieldLogprobs                   Field = "GenerateContentConfig.Logprobs"
	FieldSafetySettings             Field = "GenerateContentConfig.SafetySettings"
	FieldCachedContent              Field = "GenerateContentConfig.CachedContent"
	FieldMediaResolution            Field = "GenerateContentConfig.MediaResolution"
	FieldSpeechConfig               Field = "GenerateContentConfig.SpeechConfig"
	FieldAudioTimestamp             Field = "GenerateContentConfig.AudioTimestamp"
	FieldImageConfig                Field = "GenerateContentConfig.ImageConfig"
	FieldRoutingConfig              Field = "GenerateContentConfig.RoutingConfig"
	FieldModelSelectionConfig       Field = "GenerateContentConfig.ModelSelectionConfig"
	FieldModelArmorConfig           Field = "GenerateContentConfig.ModelArmorConfig"
	FieldEnableEnhancedCivicAnswers Field = "GenerateContentConfig.EnableEnhancedCivicAnswers"
	FieldServiceTier                Field = "GenerateContentConfig.ServiceTier"
	FieldAudioTranscriptionConfig   Field = "GenerateContentConfig.AudioTranscriptionConfig"
	FieldHTTPOptions                Field = "GenerateContentConfig.HTTPOptions"
	FieldGoogleSearch               Field = "Tool.GoogleSearch"
	FieldValidatedFunctionCalling   Field = "FunctionCallingConfig.Mode"
	FieldRetrievalConfig            Field = "ToolConfig.RetrievalConfig"
	FieldServerSideToolInvocations  Field = "ToolConfig.IncludeServerSideToolInvocations"
)

// handling is how the bridge treats one genai field.
type handling int

const (
	// translated fields map to an llm-go-sdk option or message.
	translated handling = iota
	// ignored fields cannot change the result through llm-go-sdk (tool output
	// schemas, HTTP headers meant for another backend) and are dropped.
	ignored
	// rejected fields fail the request unless the caller ignores them.
	rejected
	// hard fields fail the request always: dropping them would change the
	// shape of the result (several candidates, audio output).
	hard
)

// fieldHandling classifies every exported field of the genai request types the
// bridge reads. A test enumerates those types by reflection and fails on any
// field missing here, so a genai release that adds a field cannot be silently
// ignored: someone has to decide what it means for the bridge.
var fieldHandling = map[string]handling{
	// GenerateContentConfig
	"GenerateContentConfig.HTTPOptions":                translated, // Timeout translated, Headers ignored, the rest rejected
	"GenerateContentConfig.SystemInstruction":          translated,
	"GenerateContentConfig.Temperature":                translated,
	"GenerateContentConfig.TopP":                       translated,
	"GenerateContentConfig.TopK":                       rejected,
	"GenerateContentConfig.CandidateCount":             hard, // above one
	"GenerateContentConfig.MaxOutputTokens":            translated,
	"GenerateContentConfig.StopSequences":              translated,
	"GenerateContentConfig.ResponseLogprobs":           rejected,
	"GenerateContentConfig.Logprobs":                   rejected,
	"GenerateContentConfig.PresencePenalty":            translated,
	"GenerateContentConfig.FrequencyPenalty":           translated,
	"GenerateContentConfig.Seed":                       rejected,
	"GenerateContentConfig.ResponseMIMEType":           translated,
	"GenerateContentConfig.ResponseSchema":             translated,
	"GenerateContentConfig.ResponseJsonSchema":         translated,
	"GenerateContentConfig.RoutingConfig":              rejected,
	"GenerateContentConfig.ModelSelectionConfig":       rejected,
	"GenerateContentConfig.SafetySettings":             rejected,
	"GenerateContentConfig.Tools":                      translated,
	"GenerateContentConfig.ToolConfig":                 translated,
	"GenerateContentConfig.Labels":                     ignored, // metadata; ADK may set an agent label
	"GenerateContentConfig.CachedContent":              rejected,
	"GenerateContentConfig.ResponseModalities":         hard, // anything but [TEXT]
	"GenerateContentConfig.MediaResolution":            rejected,
	"GenerateContentConfig.SpeechConfig":               rejected,
	"GenerateContentConfig.AudioTimestamp":             rejected,
	"GenerateContentConfig.ThinkingConfig":             translated,
	"GenerateContentConfig.ImageConfig":                rejected,
	"GenerateContentConfig.EnableEnhancedCivicAnswers": rejected,
	"GenerateContentConfig.ModelArmorConfig":           rejected,
	"GenerateContentConfig.ServiceTier":                rejected,
	"GenerateContentConfig.AudioTranscriptionConfig":   rejected,

	// HTTPOptions
	"HTTPOptions.BaseURL":               rejected,
	"HTTPOptions.BaseURLResourceScope":  rejected,
	"HTTPOptions.APIVersion":            rejected,
	"HTTPOptions.Headers":               ignored, // addressed to another backend
	"HTTPOptions.Timeout":               translated,
	"HTTPOptions.ExtraBody":             rejected,
	"HTTPOptions.ExtrasRequestProvider": rejected,
	"HTTPOptions.RetryOptions":          rejected,

	// Content and Part
	"Content.Parts":            translated,
	"Content.Role":             translated,
	"Part.MediaResolution":     ignored, // a rendering hint
	"Part.CodeExecutionResult": hard,
	"Part.ExecutableCode":      hard,
	"Part.FileData":            translated, // http(s) images; the rest rejected
	"Part.FunctionCall":        translated,
	"Part.FunctionResponse":    translated,
	"Part.InlineData":          translated, // images and text; the rest rejected
	"Part.Text":                translated,
	"Part.Thought":             translated,
	"Part.ThoughtSignature":    translated,
	"Part.VideoMetadata":       hard,
	"Part.ToolCall":            hard,
	"Part.ToolResponse":        hard,
	"Part.PartMetadata":        translated, // the bridge's reasoning envelope; other keys ignored
	"Part.AudioTranscription":  hard,
	"Part.MediaProcessing":     ignored, // a processing hint

	// Function calling
	"FunctionCall.ID":               translated,
	"FunctionCall.Args":             translated,
	"FunctionCall.Name":             translated,
	"FunctionCall.PartialArgs":      hard, // never emitted by the bridge
	"FunctionCall.WillContinue":     hard,
	"FunctionResponse.WillContinue": hard,
	"FunctionResponse.Scheduling":   ignored, // Live API scheduling hint
	"FunctionResponse.Parts":        hard,
	"FunctionResponse.ID":           translated,
	"FunctionResponse.Name":         translated,
	"FunctionResponse.Response":     translated,

	// Tools
	"Tool.Retrieval":             hard,
	"Tool.ComputerUse":           hard,
	"Tool.FileSearch":            hard,
	"Tool.GoogleSearch":          rejected, // translated with WithWebSearch
	"Tool.GoogleMaps":            hard,
	"Tool.CodeExecution":         hard,
	"Tool.EnterpriseWebSearch":   hard,
	"Tool.FunctionDeclarations":  translated,
	"Tool.GoogleSearchRetrieval": rejected, // translated with WithWebSearch
	"Tool.ParallelAISearch":      hard,
	"Tool.URLContext":            hard,
	"Tool.MCPServers":            hard,
	"Tool.ExaAISearch":           hard,

	"FunctionDeclaration.Description":          translated,
	"FunctionDeclaration.Name":                 translated,
	"FunctionDeclaration.Parameters":           translated,
	"FunctionDeclaration.ParametersJsonSchema": translated,
	"FunctionDeclaration.Response":             ignored, // describes the tool's output
	"FunctionDeclaration.ResponseJsonSchema":   ignored,
	"FunctionDeclaration.Behavior":             translated, // NON_BLOCKING rejected

	"ToolConfig.RetrievalConfig":                  rejected,
	"ToolConfig.FunctionCallingConfig":            translated,
	"ToolConfig.IncludeServerSideToolInvocations": rejected,

	"FunctionCallingConfig.AllowedFunctionNames":        translated,
	"FunctionCallingConfig.Mode":                        translated, // VALIDATED rejected
	"FunctionCallingConfig.StreamFunctionCallArguments": ignored,    // calls arrive whole, which is what it asks for

	"ThinkingConfig.IncludeThoughts": translated,
	"ThinkingConfig.ThinkingBudget":  translated,
	"ThinkingConfig.ThinkingLevel":   translated,

	// Schema, all converted to JSON Schema except the two Gemini-only hints.
	"Schema.AnyOf":            translated,
	"Schema.Default":          translated,
	"Schema.Description":      translated,
	"Schema.Enum":             translated,
	"Schema.Example":          ignored,
	"Schema.Format":           translated,
	"Schema.Items":            translated,
	"Schema.MaxItems":         translated,
	"Schema.MaxLength":        translated,
	"Schema.MaxProperties":    translated,
	"Schema.Maximum":          translated,
	"Schema.MinItems":         translated,
	"Schema.MinLength":        translated,
	"Schema.MinProperties":    translated,
	"Schema.Minimum":          translated,
	"Schema.Nullable":         translated,
	"Schema.Pattern":          translated,
	"Schema.Properties":       translated,
	"Schema.PropertyOrdering": ignored,
	"Schema.Required":         translated,
	"Schema.Title":            translated,
	"Schema.Type":             translated,
}
