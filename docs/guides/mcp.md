# MCP (Model Context Protocol)

The [Model Context Protocol](https://modelcontextprotocol.io) is an open standard
for connecting LLMs to external tools and data. The `pkg/mcp` package is a
minimal MCP **client** for the tools subset of the protocol: it connects to an
MCP server, discovers its tools, and registers them into an
[`llms.ToolRegistry`](tools.md) so they drive `llms.RunTools` — exactly like
native Go tools.

It is a small, dependency-free implementation (stdio + Streamable HTTP
transports) scoped to `initialize`, `tools/list`, and `tools/call`.

## Connecting

### stdio (local servers)

Most MCP servers run as a subprocess and speak JSON-RPC over stdin/stdout:

```go
server, err := mcp.NewStdioClient(ctx, "npx",
    []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"},
    mcp.WithNamePrefix("fs_"),
)
if err != nil {
    return err
}
defer server.Close()
```

The context governs the subprocess lifetime — pass a long-lived context that
covers the whole session (cancelling it terminates the server).

### HTTP (remote servers)

```go
server, err := mcp.NewHTTPClient(ctx, "https://mcp.example.com/rpc",
    mcp.WithHTTPHeaders(map[string]string{"Authorization": "Bearer " + token}),
)
```

HTTP requests are SSRF-protected by default. For a local HTTP server
(`http://localhost:…`), pass `mcp.WithAllowPrivateIPs(true)`.

## Using the tools

Register the server's tools into a registry and run the agent loop:

```go
registry := llms.NewToolRegistry()
if err := server.Register(registry); err != nil {
    return err
}

resp, _, err := llms.RunTools(ctx, llm, messages, registry, llms.WithMaxIterations(8))
```

Each MCP tool becomes an `llms.Tool` whose handler invokes the tool over MCP and
returns its text content. A tool result flagged `isError` surfaces as a tool-error
message so the model can react rather than aborting the loop.

### Multiple servers

Give each server a distinct `WithNamePrefix` and register them all into the same
registry to avoid tool-name collisions:

```go
fsServer.Register(registry)   // mcp.WithNamePrefix("fs_")
gitServer.Register(registry)  // mcp.WithNamePrefix("git_")
```

## Calling tools directly

You can also discover and call tools without the agent loop:

```go
tools, err := server.ListTools(ctx)
result, err := server.CallTool(ctx, "read_file", json.RawMessage(`{"path":"/data/readme.md"}`))
fmt.Println(result.Text())
```

## Scope and limitations

- Implements the **tools** subset only (no resources, prompts, or sampling yet).
- The HTTP transport targets stateless endpoints; session resumption
  (`Mcp-Session-Id`) is not yet supported.

See [`examples/mcp`](https://github.com/nocturnium/llm-go-sdk/tree/main/examples/mcp)
for a complete program.
