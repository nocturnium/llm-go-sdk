package openaicompat

import (
	"crypto/sha256"
	"math/big"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

const nineCharAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// NineCharToolCallID maps a tool-call ID to one of exactly nine characters from
// [a-zA-Z0-9], the only form Mistral accepts. An ID already in that form is
// returned unchanged; any other ID (an OpenAI "call_..." or Anthropic
// "toolu_..." one left in history by a fallback or a model switch) maps to a
// deterministic hash of itself, so a call and its result, rewritten separately,
// still match.
func NineCharToolCallID(id string) string {
	if isNineCharID(id) {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	n := new(big.Int).SetBytes(sum[:])
	base := big.NewInt(int64(len(nineCharAlphabet)))
	var out [9]byte
	rem := new(big.Int)
	for i := range out {
		n.QuoRem(n, base, rem)
		out[i] = nineCharAlphabet[rem.Int64()]
	}
	return string(out[:])
}

func isNineCharID(id string) bool {
	if len(id) != 9 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		isAlnum := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
		if !isAlnum {
			return false
		}
	}
	return true
}

// normalizeToolCallIDs applies the provider's ToolCallIDFormat to every tool
// call and tool result in messages. It returns messages unchanged, or a copy,
// never modifying the input, and the adjustments to report.
func (p *BaseProvider) normalizeToolCallIDs(messages []llms.Message) ([]llms.Message, []string) {
	var normalize func(string) string
	switch p.config.ToolCallIDFormat {
	case ToolCallIDNineChar:
		normalize = NineCharToolCallID
	default:
		return messages, nil
	}
	var out []llms.Message
	set := func(i int, msg llms.Message) {
		if out == nil {
			out = append([]llms.Message(nil), messages...)
		}
		out[i] = msg
	}
	for i, msg := range messages {
		if msg.ToolCallID != "" {
			if id := normalize(msg.ToolCallID); id != msg.ToolCallID {
				msg.ToolCallID = id
				set(i, msg)
			}
		}
		copied := false
		for j, tc := range msg.ToolCalls {
			id := normalize(tc.ID)
			if id == tc.ID {
				continue
			}
			if !copied {
				msg.ToolCalls = append([]llms.ToolCall(nil), msg.ToolCalls...)
				copied = true
			}
			msg.ToolCalls[j].ID = id
			set(i, msg)
		}
	}
	if out == nil {
		return messages, nil
	}
	return out, []string{string(p.config.Provider) + ".tool_ids_rewritten"}
}
