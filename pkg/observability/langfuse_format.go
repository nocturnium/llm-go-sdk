package observability

import (
	"encoding/json"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

// InputFormat represents different ways to capture input for Langfuse
type InputFormat int

const (
	// InputFormatRaw captures just the last user message as a raw string
	InputFormatRaw InputFormat = iota

	// InputFormatMessages captures the full message array as JSON
	InputFormatMessages

	// InputFormatStructured captures messages in a Langfuse-style structured format
	InputFormatStructured
)

// OutputFormat represents different ways to capture output for Langfuse
type OutputFormat int

const (
	// OutputFormatRaw captures just the content string
	OutputFormatRaw OutputFormat = iota

	// OutputFormatResponse captures the full Response struct as JSON
	OutputFormatResponse

	// OutputFormatStructured captures response in a Langfuse-style structured format
	OutputFormatStructured
)

// messageForFormat is a simplified message structure for JSON formatting
type messageForFormat struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// structuredInput is the Langfuse-style structured input format
type structuredInput struct {
	Messages []messageForFormat `json:"messages"`
}

// structuredOutput is the Langfuse-style structured output format
type structuredOutput struct {
	Content      string          `json:"content"`
	FinishReason string          `json:"finish_reason,omitempty"`
	ToolCalls    []llms.ToolCall `json:"tool_calls,omitempty"`
	Usage        *llms.Usage     `json:"usage,omitempty"`
}

// FormatInput converts messages to a string format suitable for Langfuse
func FormatInput(messages []llms.Message, format InputFormat) string {
	if len(messages) == 0 {
		return ""
	}

	switch format {
	case InputFormatRaw:
		// Just return the last user message content
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == llms.RoleUser {
				return messages[i].Content
			}
		}
		// Fallback to last message if no user message found
		return messages[len(messages)-1].Content

	case InputFormatMessages:
		// Full message array as JSON
		data, err := json.Marshal(messages)
		if err != nil {
			return ""
		}
		return string(data)

	case InputFormatStructured:
		// Langfuse-style structured format
		structured := structuredInput{
			Messages: make([]messageForFormat, 0, len(messages)),
		}
		for _, m := range messages {
			structured.Messages = append(structured.Messages, messageForFormat{
				Role:    string(m.Role),
				Content: m.Content,
			})
		}
		data, err := json.Marshal(structured)
		if err != nil {
			return ""
		}
		return string(data)

	default:
		return ""
	}
}

// FormatOutput converts a response to a string format suitable for Langfuse
func FormatOutput(resp *llms.Response, format OutputFormat) string {
	if resp == nil {
		return ""
	}

	switch format {
	case OutputFormatRaw:
		return resp.Content

	case OutputFormatResponse:
		// Full response as JSON
		data, err := json.Marshal(resp)
		if err != nil {
			return resp.Content
		}
		return string(data)

	case OutputFormatStructured:
		// Langfuse-style structured format
		structured := structuredOutput{
			Content:      resp.Content,
			FinishReason: string(resp.FinishReason),
			ToolCalls:    resp.ToolCalls,
			Usage:        &resp.Usage,
		}
		data, err := json.Marshal(structured)
		if err != nil {
			return resp.Content
		}
		return string(data)

	default:
		return resp.Content
	}
}

// FormatMessagesCompact creates a compact string representation of messages
// useful for logging and debugging
func FormatMessagesCompact(messages []llms.Message) string {
	if len(messages) == 0 {
		return "[]"
	}

	result := make([]messageForFormat, 0, len(messages))
	for _, m := range messages {
		result = append(result, messageForFormat{
			Role:    string(m.Role),
			Content: truncateContent(m.Content, 100),
		})
	}

	data, err := json.Marshal(result)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// truncateContent truncates content to maxLen and adds ellipsis if truncated
func truncateContent(s string, maxLen int) string {
	return truncateUTF8(s, maxLen, "...")
}
