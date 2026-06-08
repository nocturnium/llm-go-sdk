package mcp

import "encoding/json"

// protocolVersion is the MCP protocol revision this client advertises.
const protocolVersion = "2025-06-18"

// Method names for the tools subset of MCP that this client implements.
const (
	methodInitialize  = "initialize"
	methodInitialized = "notifications/initialized"
	methodToolsList   = "tools/list"
	methodToolsCall   = "tools/call"
)

// Implementation identifies an MCP client or server.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeParams is the payload of the initialize request.
type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      Implementation `json:"clientInfo"`
}

// InitializeResult is the server's response to initialize.
type InitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities,omitempty"`
	ServerInfo      Implementation  `json:"serverInfo"`
	Instructions    string          `json:"instructions,omitempty"`
}

// Tool is an MCP tool definition advertised by a server. InputSchema is a JSON
// Schema describing the tool's arguments.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// listToolsResult is the server's response to tools/list.
type listToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// listToolsParams carries the optional pagination cursor for tools/list.
type listToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// callToolParams is the payload of a tools/call request.
type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ContentBlock is a single block of a tool-call result. Only text blocks carry
// readable content; other types (image, resource) expose their Type.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// CallToolResult is the result of a tools/call request.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// Text concatenates the text of all text content blocks in the result.
func (r *CallToolResult) Text() string {
	var b []byte
	for _, c := range r.Content {
		if c.Type == "text" && c.Text != "" {
			if len(b) > 0 {
				b = append(b, '\n')
			}
			b = append(b, c.Text...)
		}
	}
	return string(b)
}
