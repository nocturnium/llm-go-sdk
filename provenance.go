package llms

import (
	"crypto/rand"
)

// StampResponse records on resp which provider and model served it, and stamps
// the reasoning and tool-call signatures in it with that provider. It fills only
// fields that are empty, so a value a provider set more precisely, or a stamp
// that came from further down a middleware chain, is kept. model is the
// requested model ID (see [Response.Model]). A nil resp, or an empty p, is a
// no-op, matching [StreamSender.SetIdentity].
func StampResponse(resp *Response, p Provider, model string) {
	if resp == nil || p == "" {
		return
	}
	if resp.Provider == "" {
		resp.Provider = p
	}
	if resp.Model == "" {
		resp.Model = model
	}
	stampReasoning(resp.Reasoning, p, model)
	for i := range resp.ToolCalls {
		stampToolCall(&resp.ToolCalls[i], p)
	}
}

// stampReasoning stamps an unstamped r in place.
func stampReasoning(r *ReasoningContent, p Provider, model string) {
	if r == nil || r.Provider != "" {
		return
	}
	r.Provider = p
	if r.Model == "" {
		r.Model = model
	}
}

func stampToolCall(tc *ToolCall, p Provider) {
	if tc.Signature != "" && tc.SignatureProvider == "" {
		tc.SignatureProvider = p
	}
}

// stampChunk is StampResponse for one stream chunk. The chunk is a value whose
// Reasoning pointer and ToolCalls slice may be shared with the producer's own
// state, so both are copied before they are written. That costs one small
// allocation per reasoning delta, which is cheap next to the network read that
// produced it and keeps producers free to reuse what they send.
func stampChunk(chunk StreamChunk, p Provider, model string) StreamChunk {
	if chunk.Reasoning != nil && chunk.Reasoning.Provider == "" {
		chunk.Reasoning = chunk.Reasoning.Clone()
		stampReasoning(chunk.Reasoning, p, model)
	}
	for i := range chunk.ToolCalls {
		if chunk.ToolCalls[i].Signature != "" && chunk.ToolCalls[i].SignatureProvider == "" {
			calls := make([]ToolCall, len(chunk.ToolCalls))
			copy(calls, chunk.ToolCalls)
			for j := range calls {
				stampToolCall(&calls[j], p)
			}
			chunk.ToolCalls = calls
			break
		}
	}
	if chunk.Done && chunk.Error == nil {
		if chunk.Provider == "" {
			chunk.Provider = p
		}
		if chunk.Model == "" {
			chunk.Model = model
		}
	}
	return chunk
}

// toolCallIDAlphabet is the character set of minted tool-call IDs. Mistral
// accepts only IDs of exactly nine characters from [a-zA-Z0-9], and every other
// provider accepts those too, so minted IDs use that form.
const toolCallIDAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// toolCallIDLength is the length of a minted tool-call ID.
const toolCallIDLength = 9

// NewToolCallID returns a random tool-call ID of nine characters from
// [a-zA-Z0-9]. Its 53 bits of randomness make a collision within one
// conversation negligible, so agent frameworks that pair calls with results by
// ID across a whole session (Google ADK does) can rely on it.
func NewToolCallID() string {
	var buf [toolCallIDLength]byte
	// rand.Read never returns an error on supported platforms (it panics
	// instead), so there is nothing to handle.
	_, _ = rand.Read(buf[:])
	// 256 is not a multiple of 62, so the modulo slightly favors the first 8
	// characters; that bias costs a fraction of a bit and is irrelevant here.
	for i, b := range buf {
		buf[i] = toolCallIDAlphabet[int(b)%len(toolCallIDAlphabet)]
	}
	return string(buf[:])
}

// EnsureToolCallIDs gives every call in calls a non-empty ID that is unique
// within calls, minting one with [NewToolCallID] for a call whose ID is empty or
// repeats an earlier call's. Providers whose APIs return no IDs, or IDs that are
// not unique (Gemini historically used the function name), call it on the tool
// calls of each response. It modifies calls in place.
func EnsureToolCallIDs(calls []ToolCall) {
	if len(calls) == 0 {
		return
	}
	seen := make(map[string]bool, len(calls))
	for i := range calls {
		id := calls[i].ID
		for id == "" || seen[id] {
			id = NewToolCallID()
		}
		calls[i].ID = id
		seen[id] = true
	}
}

// DropForeignReplay returns messages with every replay payload that provider p
// did not issue removed: a [Message.Reasoning] stamped by another provider is
// dropped, and so is a [ToolCall.Signature] another provider issued. Those
// payloads authenticate the reasoning to the provider that produced them, and
// any other provider rejects them (Anthropic answers a foreign thinking
// signature with a 400), which is what happens when a fallback chain or an agent
// framework moves a conversation between providers. Unstamped payloads are kept.
//
// The input is not modified; messages that need a change are copied, and the
// original slice is returned when nothing is foreign. Providers call it on the
// prepared messages before building a request.
func DropForeignReplay(messages []Message, p Provider) []Message {
	var out []Message
	for i := range messages {
		msg := messages[i]
		changed := false
		if msg.Reasoning != nil && !msg.Reasoning.ReplayableBy(p) {
			msg.Reasoning = nil
			changed = true
		}
		copiedCalls := false
		for j := range msg.ToolCalls {
			if !msg.ToolCalls[j].SignatureReplayableBy(p) && msg.ToolCalls[j].Signature != "" {
				if !copiedCalls {
					msg.ToolCalls = append([]ToolCall(nil), msg.ToolCalls...)
					copiedCalls = true
				}
				msg.ToolCalls[j].Signature = ""
				msg.ToolCalls[j].SignatureProvider = ""
				changed = true
			}
		}
		if !changed {
			continue
		}
		if out == nil {
			out = append([]Message(nil), messages...)
		}
		out[i] = msg
	}
	if out == nil {
		return messages
	}
	return out
}
