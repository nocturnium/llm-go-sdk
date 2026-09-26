package anthropic

import (
	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/anthropicapi"
)

// adjustmentThinkingSuspended names the request adjustment made by
// suspendThinkingForUnsignedTurn, reported on Response.Adjustments.
const adjustmentThinkingSuspended = "anthropic.thinking_suspended"

// suspendThinkingForUnsignedTurn turns manual extended thinking off for one
// request that Anthropic would otherwise reject, and reports whether it did.
//
// With manual thinking (type "enabled", the only mode the pre-4.6 models have),
// the assistant side of an in-progress tool turn must open with a thinking
// block: Anthropic answers "a final assistant message must start with a thinking
// block" otherwise. Claude only thinks at the start of a turn, so the block sits
// on the turn's first assistant message and later steps legitimately carry none;
// the check is therefore turn-level. It fails only when no assistant message of
// the current turn carries a replayable block, which happens when the turn was
// produced by another provider (a fallback chain, or an agent framework that
// switched models) and the foreign reasoning was dropped. Sending the request
// without thinking is then the one form Anthropic accepts.
//
// Adaptive thinking (4.6 and later) is exempt from the rule, and always-on
// models cannot turn thinking off, so both are left alone. messages must already
// have had foreign reasoning removed (see prepare).
func suspendThinkingForUnsignedTurn(req *anthropicapi.MessagesRequest, messages []llms.Message) bool {
	if req.Thinking == nil || req.Thinking.Type != "enabled" {
		return false
	}
	turn := messages[llms.CurrentTurnStart(messages):]
	usesTools := false
	for _, msg := range turn {
		if msg.Role != llms.RoleAssistant {
			continue
		}
		if msg.Reasoning != nil && (msg.Reasoning.Signature != "" || len(redactedThinkingBlocks(msg.Reasoning)) > 0) {
			return false
		}
		if len(msg.ToolCalls) > 0 {
			usesTools = true
		}
	}
	if !usesTools {
		return false
	}
	req.Thinking = nil
	return true
}
