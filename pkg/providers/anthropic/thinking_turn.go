package anthropic

import (
	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// adjustmentThinkingSuspended names the request adjustment made when
// thinkingSuspended holds, reported on Response.Adjustments.
const adjustmentThinkingSuspended = "anthropic.thinking_suspended"

// thinkingSuspended reports whether a request must be sent without the manual
// extended thinking it asks for, because Anthropic would otherwise reject it.
// buildRequest consults it before applying thinking, so the request is built as
// a thinking-free one throughout: the forced tool choice and sampling settings
// that budget thinking would have overridden are kept.
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
// models cannot turn thinking off, so both are left alone, as is a request that
// would not have enabled budget thinking anyway (thinking off, or a structured
// output tool on a legacy model). messages must already have had foreign
// reasoning removed (see prepare).
func thinkingSuspended(model string, opts *llms.CallOptions, messages []llms.Message) bool {
	if classifyModel(model) != genLegacy || !opts.Reasoning.IsEnabled() || structuredOutputToolNameFor(opts) != "" {
		return false
	}
	usesTools := false
	for _, msg := range messages[llms.CurrentTurnStart(messages):] {
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
	return usesTools
}

// requestModel is the model a request with opts is sent to.
func (c *Client) requestModel(opts *llms.CallOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	return c.options.Model
}
