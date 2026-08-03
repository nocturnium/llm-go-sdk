package mcp

import "encoding/json"

// protocolVersion is the MCP protocol revision this client advertises.
const protocolVersion = "2025-06-18"

// Method names for the subset of MCP that this client implements.
const (
	methodInitialize    = "initialize"
	methodInitialized   = "notifications/initialized"
	methodToolsList     = "tools/list"
	methodToolsCall     = "tools/call"
	methodResourcesList = "resources/list"
	methodResourcesRead = "resources/read"
	methodPromptsList   = "prompts/list"
	methodPromptsGet    = "prompts/get"
)

// Server-initiated REQUEST method names: the server asks this client to do
// something and waits for a response carrying the request's id. Each is served
// only when a handler for it is registered; otherwise the client answers
// MethodNotFound.
const (
	methodSamplingCreateMessage = "sampling/createMessage"
	methodRootsList             = "roots/list"
	methodElicitationCreate     = "elicitation/create"
)

// Server-initiated notification method names this client recognizes. The server
// sends these without an id; the transports surface them to a registered handler.
const (
	methodNotificationsProgress         = "notifications/progress"
	methodNotificationsMessage          = "notifications/message"
	methodNotificationsToolsChanged     = "notifications/tools/list_changed"
	methodNotificationsResourcesChanged = "notifications/resources/list_changed"
	methodNotificationsResourceUpdated  = "notifications/resources/updated"
	methodNotificationsPromptsChanged   = "notifications/prompts/list_changed"
)

// Implementation identifies an MCP client or server.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeParams is the payload of the initialize request.
type initializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

// ClientCapabilities is what this client advertises during initialize.
//
// A nil sub-capability means the feature is NOT offered. Capabilities are
// derived from the request handlers registered at construction, so a client can
// never advertise a capability it would then refuse — a server that is told this
// client samples, and then gets MethodNotFound, has no way to recover.
//
// Fields mirror the MCP 2025-06-18 capability shape; see [ServerCapabilities]
// for the server-side counterpart.
type ClientCapabilities struct {
	// Sampling is non-nil when the client serves sampling/createMessage.
	Sampling *SamplingCapability `json:"sampling,omitempty"`
	// Roots is non-nil when the client serves roots/list.
	Roots *RootsCapability `json:"roots,omitempty"`
	// Elicitation is non-nil when the client serves elicitation/create.
	Elicitation *ElicitationCapability `json:"elicitation,omitempty"`
}

// SamplingCapability marks sampling support. It carries no fields, matching the
// spec's empty-object shape (compare [LoggingCapability]).
type SamplingCapability struct{}

// RootsCapability marks roots support.
type RootsCapability struct {
	// ListChanged reports whether the client emits notifications/roots/list_changed.
	ListChanged bool `json:"listChanged,omitempty"`
}

// ElicitationCapability marks elicitation support. It carries no fields.
type ElicitationCapability struct{}

// InitializeResult is the server's response to initialize.
type InitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities,omitempty"`
	ServerInfo      Implementation  `json:"serverInfo"`
	Instructions    string          `json:"instructions,omitempty"`
}

// ServerCapabilities is the typed view of the capabilities a server advertises in
// its initialize response. A nil sub-capability (e.g. Resources == nil) means the
// server did not advertise that feature; callers can gate calls on it (see
// [Client.ServerCapabilities]). Fields mirror the MCP 2025-06-18 capability shape.
type ServerCapabilities struct {
	// Tools is non-nil when the server exposes tools/list and tools/call.
	Tools *ToolsCapability `json:"tools,omitempty"`
	// Resources is non-nil when the server exposes resources/list and resources/read.
	Resources *ResourcesCapability `json:"resources,omitempty"`
	// Prompts is non-nil when the server exposes prompts/list and prompts/get.
	Prompts *PromptsCapability `json:"prompts,omitempty"`
	// Logging is non-nil when the server can emit notifications/message log records.
	Logging *LoggingCapability `json:"logging,omitempty"`
}

// ToolsCapability describes the server's tools support. ListChanged reports
// whether the server emits notifications/tools/list_changed.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability describes the server's resources support. Subscribe reports
// whether per-resource subscriptions are supported; ListChanged reports whether
// the server emits notifications/resources/list_changed.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability describes the server's prompts support. ListChanged reports
// whether the server emits notifications/prompts/list_changed.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// LoggingCapability marks that the server supports structured log notifications
// (notifications/message). The MCP logging capability carries no sub-fields.
type LoggingCapability struct{}

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

// callToolParams is the payload of a tools/call request. Meta carries optional
// request metadata such as a progressToken that opts the call into progress
// notifications.
type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Meta      *requestMeta    `json:"_meta,omitempty"`
}

// requestMeta is the MCP per-request _meta object. ProgressToken, when set, asks
// the server to emit notifications/progress correlated by that token.
type requestMeta struct {
	ProgressToken string `json:"progressToken,omitempty"`
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

// Resource is an MCP resource advertised by a server (resources/list).
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContents is one content item returned by resources/read. Text carries
// textual contents; Blob carries base64-encoded binary contents. Exactly one is
// set per item by spec-conforming servers.
type ResourceContents struct {
	URI      string `json:"uri,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // base64-encoded binary contents
}

// ReadResourceResult is the server's response to resources/read.
type ReadResourceResult struct {
	Contents []ResourceContents `json:"contents"`
}

// listResourcesParams carries the optional pagination cursor for resources/list.
type listResourcesParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// listResourcesResult is the server's response to resources/list.
type listResourcesResult struct {
	Resources  []Resource `json:"resources"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// readResourceParams is the payload of a resources/read request.
type readResourceParams struct {
	URI string `json:"uri"`
}

// Prompt is an MCP prompt template advertised by a server (prompts/list).
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes one argument a prompt template accepts.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage is one message of a rendered prompt (prompts/get). Content is a
// single content block; only text blocks carry readable content.
type PromptMessage struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
}

// GetPromptResult is the server's response to prompts/get.
type GetPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// listPromptsParams carries the optional pagination cursor for prompts/list.
type listPromptsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// listPromptsResult is the server's response to prompts/list.
type listPromptsResult struct {
	Prompts    []Prompt `json:"prompts"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

// getPromptParams is the payload of a prompts/get request.
type getPromptParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// ProgressNotification is a server-initiated notifications/progress message,
// reporting incremental progress for an in-flight request that carried a progress
// token. Total is zero when the server reports progress without a known bound.
type ProgressNotification struct {
	// ProgressToken correlates the notification to the request that opted in by
	// sending a _meta.progressToken; it is a string or number on the wire.
	ProgressToken json.RawMessage `json:"progressToken,omitempty"`
	Progress      float64         `json:"progress"`
	Total         float64         `json:"total,omitempty"`
	Message       string          `json:"message,omitempty"`
}

// LogMessage is a server-initiated notifications/message log record. Level is a
// syslog-style severity ("debug", "info", "warning", "error", ...); Logger names
// the emitting component; Data carries the structured payload.
type LogMessage struct {
	Level  string          `json:"level"`
	Logger string          `json:"logger,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}
