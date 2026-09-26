package llms

// CurrentTurnStart returns the index of the first message of the conversation's
// current turn: the model's work on the latest user message, meaning the
// assistant messages and tool results after it. It is the index just past the
// last user or system message, and len(messages) when the conversation ends on
// one, since no turn is in progress then.
//
// Providers that validate reasoning per turn use it to find the messages the
// rule applies to: Anthropic requires the assistant side of an in-progress tool
// turn to open with a thinking block, and Gemini 3 requires the function calls
// of the current turn to carry thought signatures.
func CurrentTurnStart(messages []Message) int {
	i := len(messages)
	for i > 0 {
		switch messages[i-1].Role {
		case RoleAssistant, RoleTool:
			i--
		default:
			return i
		}
	}
	return i
}
