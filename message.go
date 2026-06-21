package llms

import (
	"strconv"
)

// Message represents a chat message.
// Messages can contain either simple text (using Content) or multi-part content
// including images (using Parts). If Parts is non-empty, it takes precedence over Content.
// Message methods use value receivers because Message is a small value type
// passed by value in slices; Response and StreamChunk methods use pointer
// receivers for nil-safety.
type Message struct {
	Role       Role          `json:"role"`
	Content    string        `json:"content,omitempty"`      // Simple text content (for backward compatibility)
	Parts      []ContentPart `json:"parts,omitempty"`        // Multi-part content including text and images (for vision models)
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`   // Tool calls requested by the assistant
	ToolCallID string        `json:"tool_call_id,omitempty"` // ID of the tool call this message responds to (for tool role)
	Name       string        `json:"name,omitempty"`         // Name of the tool (for tool role)

	// CacheControl, when set, marks a prompt-caching breakpoint at this message.
	// Honored by providers with explicit cache breakpoints (Anthropic); ignored by
	// providers that cache automatically. See WithCache for call-level caching.
	CacheControl *CacheControl `json:"cache_control,omitempty"`

	// Reasoning carries an assistant turn's reasoning ("thinking") output back into
	// a follow-up request. Some providers require the prior reasoning to be echoed
	// to preserve the thinking context across turns without server-side state:
	// Anthropic extended-thinking blocks (via ReasoningContent.Signature) and the
	// OpenAI Responses API's encrypted reasoning items (via ReasoningContent.Metadata).
	// Copy a Response.Reasoning onto the assistant Message you append to the history
	// to round-trip it. Ignored on non-assistant messages and by providers that do
	// not need it.
	Reasoning *ReasoningContent `json:"reasoning,omitempty"`
}

// Role represents the role of a message sender
type Role string

// Role constants define the possible message sender roles.
const (
	// RoleSystem is the system role for system messages.
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ValidateMessages checks if the messages are valid for an LLM call.
// Returns nil if all messages are valid.
//
// Provider-neutral validations performed:
//   - Messages list is not empty
//   - Each message has a role
//   - Each role is known
//   - The conversation does not start with a tool message
func ValidateMessages(messages []Message) error {
	if len(messages) == 0 {
		return ErrEmptyMessages
	}

	var errs ValidationErrors

	for i, msg := range messages {
		// Check role is present
		if msg.Role == "" {
			errs = append(errs, ValidationError{
				Field:   "messages[" + strconv.Itoa(i) + "].role",
				Value:   msg.Role,
				Message: "role is required",
			})
			continue
		}

		// Validate role is a known value
		if !isValidRole(msg.Role) {
			errs = append(errs, ValidationError{
				Field:   "messages[" + strconv.Itoa(i) + "].role",
				Value:   msg.Role,
				Message: "invalid role: must be system, user, assistant, or tool",
			})
		}

		// A tool message is a result for a prior assistant tool call, so it
		// cannot be the first message in a conversation.
		if i == 0 && msg.Role == RoleTool {
			errs = append(errs, ValidationError{
				Field:   "messages[" + strconv.Itoa(i) + "].role",
				Value:   msg.Role,
				Message: "tool message cannot start a conversation",
			})
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// ValidateToolCallIDs enforces provider opt-in tool result linkage by requiring
// every tool message to include a non-empty ToolCallID. This is appropriate for
// providers such as OpenAI-compatible APIs and Anthropic that match tool results
// to tool calls by ID.
func ValidateToolCallIDs(messages []Message) error {
	var errs ValidationErrors
	for i, msg := range messages {
		if msg.Role == RoleTool && msg.ToolCallID == "" {
			errs = append(errs, ValidationError{
				Field:   "messages[" + strconv.Itoa(i) + "].tool_call_id",
				Value:   "",
				Message: "tool_call_id is required for tool messages",
			})
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// ValidateInlineSystem enforces provider opt-in inline system message rules:
// at most one system message is allowed, and it must be first if present. This
// is appropriate for providers that keep system messages inline with the chat
// history rather than hoisting or concatenating them into a dedicated API field.
func ValidateInlineSystem(messages []Message) error {
	var errs ValidationErrors
	systemMessageSeen := false
	for i, msg := range messages {
		if msg.Role != RoleSystem {
			continue
		}
		if systemMessageSeen {
			errs = append(errs, ValidationError{
				Field:   "messages[" + strconv.Itoa(i) + "].role",
				Value:   msg.Role,
				Message: "multiple system messages are not allowed",
			})
		} else if i > 0 {
			errs = append(errs, ValidationError{
				Field:   "messages[" + strconv.Itoa(i) + "].role",
				Value:   msg.Role,
				Message: "system message must be first in the conversation",
			})
		}
		systemMessageSeen = true
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// isValidRole checks if a role is one of the known valid roles.
func isValidRole(role Role) bool {
	switch role {
	case RoleSystem, RoleUser, RoleAssistant, RoleTool:
		return true
	default:
		return false
	}
}

// MergeConsecutiveMessages merges consecutive messages with the same role.
// This is useful because many LLM APIs reject consecutive messages from the same role.
// Tool messages are never merged as they represent distinct tool call responses.
//
// When merging:
//   - Content is concatenated with a newline separator
//   - Parts are combined into a single slice
//   - ToolCalls from all merged messages are combined
//   - The first message's other fields (ToolCallID, Name) are preserved
//
// A message carrying Reasoning is treated as a merge boundary so its per-turn
// reasoning round-trip data (encrypted/signed thinking) is never dropped.
func MergeConsecutiveMessages(messages []Message) []Message {
	if len(messages) <= 1 {
		return messages
	}

	// First, consolidate all system messages into one at the front
	messages = ConsolidateSystemMessages(messages)

	result := make([]Message, 0, len(messages))
	var current *Message

	for _, msg := range messages {
		// Never merge tool messages (they represent distinct tool call responses)
		// System messages are already consolidated, so treat them as boundaries
		if msg.Role == RoleTool || msg.Role == RoleSystem {
			if current != nil {
				result = append(result, *current)
				current = nil
			}
			result = append(result, msg)
			continue
		}

		// A reasoning-bearing turn is a merge boundary: its Reasoning (the
		// encrypted/signed thinking that round-trips across turns) is per-turn
		// data that merging cannot combine, so don't fold such a message into a
		// prior accumulator, and don't fold a following message into one. This
		// keeps each reasoning-bearing turn — and its Reasoning — intact.
		if msg.Reasoning != nil || (current != nil && current.Reasoning != nil) {
			if current != nil {
				result = append(result, *current)
			}
			msgCopy := msg
			current = &msgCopy
			continue
		}

		// Check if we can merge with current
		if current != nil && current.Role == msg.Role {
			// Merge content
			if msg.Content != "" {
				if current.Content != "" {
					current.Content += "\n" + msg.Content
				} else {
					current.Content = msg.Content
				}
			}

			// Merge parts
			if len(msg.Parts) > 0 {
				current.Parts = append(current.Parts, msg.Parts...)
			}

			// Merge tool calls
			if len(msg.ToolCalls) > 0 {
				current.ToolCalls = append(current.ToolCalls, msg.ToolCalls...)
			}

			// A cache breakpoint on any merged message marks the combined message
			// (the breakpoint belongs at the end of the cached prefix).
			if msg.CacheControl != nil {
				current.CacheControl = msg.CacheControl
			}
		} else {
			// Start new message group
			if current != nil {
				result = append(result, *current)
			}
			msgCopy := msg
			current = &msgCopy
		}
	}

	// Don't forget the last message
	if current != nil {
		result = append(result, *current)
	}

	return result
}

// ConsolidateSystemMessages merges all system messages into a single system message
// at the beginning of the conversation. This is required by many LLM APIs that only
// allow one system message. Multiple system messages are concatenated with newlines.
func ConsolidateSystemMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	var systemParts []string
	var nonSystemMessages []Message

	for _, msg := range messages {
		if msg.Role == RoleSystem {
			// Use Text so a system message built from Parts (rather than
			// the simple Content field) is not silently dropped.
			if text := msg.Text(); text != "" {
				systemParts = append(systemParts, text)
			}
		} else {
			nonSystemMessages = append(nonSystemMessages, msg)
		}
	}

	// If no system messages, return original
	if len(systemParts) == 0 {
		return messages
	}

	// Build result with consolidated system message at front
	result := make([]Message, 0, len(nonSystemMessages)+1)
	result = append(result, Message{
		Role:    RoleSystem,
		Content: joinStrings(systemParts, "\n\n"),
	})
	result = append(result, nonSystemMessages...)

	return result
}

// joinStrings joins strings with a separator (avoids importing strings package)
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	// Calculate total length
	n := len(sep) * (len(parts) - 1)
	for _, p := range parts {
		n += len(p)
	}

	// Build result
	b := make([]byte, 0, n)
	for i, p := range parts {
		if i > 0 {
			b = append(b, sep...)
		}
		b = append(b, p...)
	}
	return string(b)
}

// PrepareMessages preprocesses messages based on the given options.
// By default, it merges consecutive same-role messages unless disabled via options.
// After preprocessing, it validates the messages.
// Returns the processed messages and any validation error.
func PrepareMessages(messages []Message, opts *CallOptions) ([]Message, error) {
	// Apply message merging unless disabled
	processed := messages
	if opts == nil || !opts.DisableMessageMerging {
		processed = MergeConsecutiveMessages(messages)
	}

	// Validate the processed messages
	if err := ValidateMessages(processed); err != nil {
		return nil, err
	}

	return processed, nil
}
