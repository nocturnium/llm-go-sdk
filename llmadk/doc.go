// Package llmadk runs any llm-go-sdk model behind a Google ADK Go agent.
//
// [NewModel] adapts an [llms.LLM] (any of the SDK's providers, or a middleware
// chain around one: fallback, retries, caching, cost tracking, tracing) to ADK's
// [model.LLM], so an ADK agent can use Anthropic, OpenAI, Mistral, a local
// Ollama server or any other provider the SDK supports:
//
//	claude, err := anthropic.New()
//	if err != nil {
//		log.Fatal(err)
//	}
//	m, err := llmadk.NewModel(claude)
//	if err != nil {
//		log.Fatal(err)
//	}
//	agent, err := llmagent.New(llmagent.Config{
//		Name:        "assistant",
//		Model:       m,
//		Instruction: "You are a helpful assistant.",
//	})
//
// [Tools] turns an [llms.ToolRegistry] into ADK tools.
//
// # Compatibility
//
// Every field of ADK's request (the genai contents and generation config) is
// either translated, ignored because it cannot change the result, or rejected
// with an error that names it ([ErrUnsupportedConfigField], [ErrUnsupportedPart],
// [ErrUnsupportedTool]). Nothing is dropped silently. [WithIgnore] turns a
// rejected field into an ignored one, for configs written for Gemini, such as
// safety settings or top-k.
//
// Streaming follows ADK's contract: text and thought deltas arrive as partial
// responses, then one final response carries the whole turn, its tool calls,
// usage and finish reason.
//
// Tool-call IDs are unique across the session, because ADK pairs every call
// with its result by ID across the whole history. Reasoning a provider needs
// back on the next turn travels with the session and each provider replays only
// the reasoning it produced: Gemini's thought signatures sit in the part's
// ThoughtSignature as Gemini issued them, and every other provider's reasoning
// (Anthropic thinking signatures, OpenAI encrypted reasoning) sits in the part's
// PartMetadata under "llmgo.reasoning".
//
// With thoughts hidden (no ThinkingConfig, or IncludeThoughts false) another
// provider's thought text is still stored in that PartMetadata entry, because
// Anthropic needs the text back with its signature. It is not shown as a
// thought part, but it is present, base64-encoded, in session storage and in
// ADK's event stream.
//
// Structured output combined with tools is handled by the bridge: ADK sends both
// for non-Gemini models, which several providers cannot honor at once, so the
// bridge offers the schema as a set_model_response tool and turns the model's
// call to it into the final JSON answer.
package llmadk
