package mcp

import (
	"context"
	"fmt"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

// ListPrompts returns every prompt template the server advertises, following
// pagination. Like [Client.ListTools] it pages with the prior cursor and checks
// ctx before each request.
//
// Prompts are an optional capability; check ServerCapabilities().Prompts for nil
// before calling if you want to avoid a CodeMethodNotFound RPCError from a server
// that does not implement them.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var all []Prompt
	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var res listPromptsResult
		if err := c.call(ctx, methodPromptsList, listPromptsParams{Cursor: cursor}, &res); err != nil {
			return nil, fmt.Errorf("mcp: list prompts: %w", err)
		}
		all = append(all, res.Prompts...)
		if res.NextCursor == "" || res.NextCursor == cursor {
			// A server that returns the same non-empty cursor forever would loop
			// until ctx cancellation; stop when the cursor fails to advance.
			break
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// GetPrompt renders a prompt template by name with the given string arguments and
// returns the resulting messages. Pass nil args for a prompt that takes none.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (*GetPromptResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var res GetPromptResult
	if err := c.call(ctx, methodPromptsGet, getPromptParams{Name: name, Arguments: args}, &res); err != nil {
		return nil, fmt.Errorf("mcp: get prompt %q: %w", name, err)
	}
	return &res, nil
}

// LLMMessages maps the rendered prompt's messages onto []llms.Message so a server
// prompt can seed a conversation passed to llms.RunTools or GenerateContent. It
// parallels how [Client.Register] bridges tools into the llms layer. Each prompt
// message becomes one llms.Message: the prompt role is mapped onto an llms.Role
// (any role other than "assistant" maps to RoleUser, since MCP prompt messages
// only carry "user" or "assistant"), and the text content block becomes Content.
// Non-text content blocks (image, embedded resource) contribute no text and yield
// an empty Content for that message.
func (r *GetPromptResult) LLMMessages() []llms.Message {
	if r == nil || len(r.Messages) == 0 {
		return nil
	}
	out := make([]llms.Message, 0, len(r.Messages))
	for _, pm := range r.Messages {
		msg := llms.Message{Role: promptRole(pm.Role)}
		if pm.Content.Type == "text" {
			msg.Content = pm.Content.Text
		}
		out = append(out, msg)
	}
	return out
}

// promptRole maps an MCP prompt-message role onto an llms.Role. MCP prompt
// messages use only "user" and "assistant"; anything else is treated as a user
// turn so the message remains valid input for llms.RunTools.
func promptRole(role string) llms.Role {
	if role == "assistant" {
		return llms.RoleAssistant
	}
	return llms.RoleUser
}
