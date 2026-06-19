package mcp

import (
	"context"
	"io"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

// MountStdio launches a stdio MCP server, registers its tools into reg, and
// returns the connected client (an io.Closer) ready for llms.RunTools. It is a
// one-call convenience over [NewStdioClient] + [Client.Register]; for resources,
// prompts, or notification handlers, use NewStdioClient directly and keep the
// *Client. The caller owns the returned closer and must Close it to terminate the
// subprocess. On a registration failure the client is closed before returning.
//
//	reg := llms.NewToolRegistry()
//	closer, err := mcp.MountStdio(ctx, reg, "npx",
//	    []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"})
//	if err != nil { return err }
//	defer closer.Close()
//	resp, msgs, err := llms.RunTools(ctx, llm, messages, reg)
func MountStdio(ctx context.Context, reg *llms.ToolRegistry, command string, args []string, opts ...Option) (io.Closer, error) {
	c, err := NewStdioClient(ctx, command, args, opts...)
	if err != nil {
		return nil, err
	}
	if err := c.Register(reg); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// MountHTTP connects to a Streamable HTTP MCP server, registers its tools into
// reg, and returns the connected client (an io.Closer) ready for llms.RunTools.
// It is a one-call convenience over [NewHTTPClient] + [Client.Register]; for
// resources, prompts, or notification handlers, use NewHTTPClient directly and
// keep the *Client. On a registration failure the client is closed before
// returning.
func MountHTTP(ctx context.Context, reg *llms.ToolRegistry, url string, opts ...Option) (io.Closer, error) {
	c, err := NewHTTPClient(ctx, url, opts...)
	if err != nil {
		return nil, err
	}
	if err := c.Register(reg); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}
