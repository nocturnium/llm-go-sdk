package gemini

import (
	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// skipSignatureValidator is the thought signature Google documents for function
// calls the model did not produce, such as history imported from another
// provider. It tells Gemini 3 to skip signature validation for that call.
const skipSignatureValidator = "skip_thought_signature_validator"

// adjustmentSignatureValidatorSkipped names the request adjustment made by
// skipValidationForUnsignedCalls, reported on Response.Adjustments.
const adjustmentSignatureValidatorSkipped = "gemini.signature_validator_skipped"

// skipValidationForUnsignedCalls marks function calls Gemini 3 would reject
// and reports whether it marked any. It returns messages unchanged, or a copy
// with the changes, never modifying the input.
//
// Gemini 3 answers "400 Function call is missing a thought_signature" for a
// function call in the current turn without the signature Gemini issued with
// it. Gemini signs only the first function call of each step, so only that call
// is checked. A call whose signature was dropped as foreign, or that never had
// one because another provider made it, gets Google's documented placeholder,
// which skips validation for it. Calls carrying Gemini's own signature are never
// touched: the placeholder costs the model some reasoning context, which is why
// Google reserves it for calls it did not produce.
func skipValidationForUnsignedCalls(messages []llms.Message, model string) ([]llms.Message, bool) {
	// Gemini 3 is the generation that validates call signatures.
	if !isGemini3OrLater(model) {
		return messages, false
	}
	var out []llms.Message
	for i := llms.CurrentTurnStart(messages); i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != llms.RoleAssistant {
			continue
		}
		first := -1
		for j, tc := range msg.ToolCalls {
			if tc.Function != nil {
				first = j
				break
			}
		}
		if first < 0 || msg.ToolCalls[first].Signature != "" {
			continue
		}
		if out == nil {
			out = append([]llms.Message(nil), messages...)
		}
		calls := append([]llms.ToolCall(nil), msg.ToolCalls...)
		calls[first].Signature = skipSignatureValidator
		calls[first].SignatureProvider = llms.ProviderGemini
		out[i].ToolCalls = calls
	}
	if out == nil {
		return messages, false
	}
	return out, true
}
