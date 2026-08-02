// Example: MCP (Model Context Protocol) tools
//
// Connects to an MCP server over stdio, registers its tools into a ToolRegistry,
// and lets the model use them through the RunTools agent loop. This example uses
// the reference filesystem server, but any stdio MCP server works.
//
// Prerequisites: Node.js (for npx) and an Anthropic API key.
//
// Run with:
//
//	ANTHROPIC_API_KEY=... go run ./examples/mcp /path/to/a/directory
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/mcp"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/anthropic"
)

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	// The connection context governs the MCP subprocess lifetime, so give it a
	// generous deadline that covers the whole agent loop.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Launch the reference filesystem MCP server, scoped to dir.
	server, err := mcp.NewStdioClient(ctx, "npx",
		[]string{"-y", "@modelcontextprotocol/server-filesystem", dir},
		mcp.WithNamePrefix("fs_"),
	)
	if err != nil {
		log.Fatalf("connect to MCP server: %v", err)
	}
	defer func() { _ = server.Close() }()

	fmt.Printf("connected to %s %s\n", server.ServerInfo().Name, server.ServerInfo().Version)

	// Expose the server's tools to the model.
	registry := llms.NewToolRegistry()
	if err := server.Register(registry); err != nil {
		log.Fatalf("register MCP tools: %v", err)
	}
	for _, tool := range registry.Tools() {
		fmt.Printf("  tool: %s\n", tool.Function.Name)
	}

	llm, err := anthropic.New(anthropic.WithModel("claude-3-5-sonnet-20241022"))
	if err != nil {
		log.Fatalf("anthropic: %v", err)
	}

	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "List the files in the directory and tell me what kind of project this looks like."},
	}

	resp, _, err := llms.RunTools(ctx, llm, messages, registry, llms.WithMaxIterations(8))
	if err != nil {
		log.Fatalf("run tools: %v", err)
	}

	fmt.Printf("\n%s\n", resp.Content)
}
