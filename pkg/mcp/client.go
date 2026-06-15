// Package mcp is a minimal Model Context Protocol (MCP) client for the tools
// subset of the protocol: initialize, tools/list, and tools/call. It connects to
// an MCP server over stdio (a subprocess) or Streamable HTTP and can register the
// server's tools into an *llms.ToolRegistry so they drive llms.RunTools.
//
// Example (stdio):
//
//	mc, err := mcp.NewStdioClient(ctx, "npx",
//	    []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"})
//	if err != nil { return err }
//	defer mc.Close()
//
//	reg := llms.NewToolRegistry()
//	if err := mc.Register(reg); err != nil { return err }
//	resp, msgs, err := llms.RunTools(ctx, llm, messages, reg)
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/internal/httpclient"
)

const (
	defaultClientName = "llm-go-sdk"
)

// Client is a connected MCP client. It is bound to the context passed to its
// constructor: that context governs the connection's lifetime and is used for
// tool calls dispatched through Register. Methods are safe for concurrent use.
// Call Close to release resources.
type Client struct {
	transport  transport
	baseCtx    context.Context //nolint:containedctx // governs the client's connection lifetime
	nextID     atomic.Int64
	namePrefix string
	clientInfo Implementation
	serverInfo Implementation
}

type config struct {
	namePrefix      string
	clientInfo      Implementation
	env             []string
	dir             string
	httpClient      *http.Client
	headers         map[string]string
	timeout         time.Duration
	allowPrivateIPs bool
	allowHTTP       *bool // nil = follow allowPrivateIPs
}

// Option configures a Client.
type Option func(*config)

// WithNamePrefix prefixes every registered tool name. Use it to avoid collisions
// when tools from multiple servers share one registry (e.g. "fs_").
func WithNamePrefix(prefix string) Option {
	return func(c *config) { c.namePrefix = prefix }
}

// WithClientInfo sets the client name and version reported to the server.
func WithClientInfo(name, version string) Option {
	return func(c *config) { c.clientInfo = Implementation{Name: name, Version: version} }
}

// WithEnv adds or overrides subprocess environment entries for a stdio client.
// Stdio subprocesses do not inherit the full parent environment by default;
// they receive only a minimal safe environment plus these entries.
func WithEnv(env []string) Option {
	return func(c *config) {
		c.env = env
	}
}

// WithWorkDir sets the subprocess working directory for a stdio client.
func WithWorkDir(dir string) Option {
	return func(c *config) { c.dir = dir }
}

// WithHTTPHeaders sets additional headers (e.g. Authorization) sent on every
// request by an HTTP client.
func WithHTTPHeaders(headers map[string]string) Option {
	return func(c *config) { c.headers = headers }
}

// WithHTTPClient sets a custom *http.Client for an HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) { c.httpClient = hc }
}

// WithTimeout sets the per-request timeout for an HTTP client.
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// WithAllowPrivateIPs permits an HTTP client to reach private/loopback addresses.
// It is off by default (SSRF protection); enable it for local HTTP MCP servers
// such as http://localhost. Unless WithAllowHTTP is also set, enabling this also
// permits plain HTTP (local servers are typically not TLS).
func WithAllowPrivateIPs(allow bool) Option {
	return func(c *config) { c.allowPrivateIPs = allow }
}

// WithAllowHTTP independently controls whether plain (non-TLS) HTTP is permitted.
// When unset, it follows WithAllowPrivateIPs. Set WithAllowHTTP(false) to keep TLS
// enforced even while reaching a private HTTPS server, so credentials in
// WithHTTPHeaders are never sent in cleartext to a downgraded endpoint.
func WithAllowHTTP(allow bool) Option {
	return func(c *config) { c.allowHTTP = &allow }
}

func buildConfig(opts []Option) config {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.clientInfo.Name == "" {
		cfg.clientInfo.Name = defaultClientName
	}
	if cfg.clientInfo.Version == "" {
		cfg.clientInfo.Version = llms.Version
	}
	return cfg
}

// NewStdioClient launches an MCP server subprocess and completes the MCP
// handshake. The context governs the subprocess lifetime — pass a long-lived
// context (canceling it terminates the server). args is the server's argument
// list. Remember to Close the returned client.
func NewStdioClient(ctx context.Context, command string, args []string, opts ...Option) (*Client, error) {
	cfg := buildConfig(opts)
	t, err := newStdioTransport(ctx, command, args, cfg.env, cfg.dir)
	if err != nil {
		return nil, err
	}
	return newClient(ctx, t, cfg)
}

// NewHTTPClient connects to a Streamable HTTP MCP server at url and completes the
// MCP handshake. Requests are SSRF-protected by default; use WithAllowPrivateIPs
// for local servers. Remember to Close the returned client.
func NewHTTPClient(ctx context.Context, url string, opts ...Option) (*Client, error) {
	cfg := buildConfig(opts)
	allowHTTP := cfg.allowPrivateIPs
	if cfg.allowHTTP != nil {
		allowHTTP = *cfg.allowHTTP
	}
	hcOpts := []httpclient.ClientOption{
		httpclient.WithAllowPrivateIPs(cfg.allowPrivateIPs),
		httpclient.WithAllowHTTP(allowHTTP),
	}
	if cfg.httpClient != nil {
		hcOpts = append(hcOpts, httpclient.WithHTTPClient(cfg.httpClient))
	}
	if cfg.timeout > 0 {
		hcOpts = append(hcOpts, httpclient.WithTimeout(cfg.timeout))
	}
	t := newHTTPTransport(url, httpclient.NewClient(hcOpts...), cfg.headers)
	return newClient(ctx, t, cfg)
}

func newClient(ctx context.Context, t transport, cfg config) (*Client, error) {
	c := &Client{
		transport:  t,
		baseCtx:    ctx,
		namePrefix: cfg.namePrefix,
		clientInfo: cfg.clientInfo,
	}
	if err := c.initialize(ctx); err != nil {
		_ = t.close()
		return nil, err
	}
	return c, nil
}

func (c *Client) initialize(ctx context.Context) error {
	params := initializeParams{
		ProtocolVersion: protocolVersion,
		Capabilities:    map[string]any{}, // a tools-only client advertises no capabilities
		ClientInfo:      c.clientInfo,
	}
	var result InitializeResult
	if err := c.call(ctx, methodInitialize, params, &result); err != nil {
		return fmt.Errorf("mcp: initialize: %w", err)
	}
	if result.ProtocolVersion != protocolVersion {
		return fmt.Errorf("mcp: initialize: unsupported protocol version %q (supported %q)", result.ProtocolVersion, protocolVersion)
	}
	c.serverInfo = result.ServerInfo

	note, err := encodeNotification(methodInitialized, nil)
	if err != nil {
		return err
	}
	return c.transport.notify(ctx, note)
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	payload, err := encodeRequest(id, method, params)
	if err != nil {
		return fmt.Errorf("mcp: encode %s request: %w", method, err)
	}
	raw, err := c.transport.request(ctx, id, payload)
	if err != nil {
		return err
	}
	return decodeResult(raw, result)
}

// ListTools returns every tool the server advertises, following pagination.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var res listToolsResult
		if err := c.call(ctx, methodToolsList, listToolsParams{Cursor: cursor}, &res); err != nil {
			return nil, fmt.Errorf("mcp: list tools: %w", err)
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// CallTool invokes a tool by name with JSON-encoded arguments and returns the
// result. A result with IsError set is returned without error so callers can
// inspect the failure content.
func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*CallToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var res CallToolResult
	if err := c.call(ctx, methodToolsCall, callToolParams{Name: name, Arguments: arguments}, &res); err != nil {
		return nil, fmt.Errorf("mcp: call tool %q: %w", name, err)
	}
	return &res, nil
}

// Register discovers the server's tools and registers each into reg as an
// llms.Tool whose handler invokes the tool over MCP. Tool names are prefixed with
// the client's NamePrefix. The registered handlers use the client's connection
// context, so they remain callable for the client's lifetime (e.g. across an
// llms.RunTools agent loop).
func (c *Client) Register(reg *llms.ToolRegistry) error {
	tools, err := c.ListTools(c.baseCtx)
	if err != nil {
		return err
	}
	for _, mt := range tools {
		tool := llms.NewFunctionTool(c.namePrefix+mt.Name, mt.Description, mt.InputSchema)
		reg.Register(tool, c.toolHandler(mt.Name))
	}
	return nil
}

func (c *Client) toolHandler(remoteName string) llms.ToolHandler {
	return func(args json.RawMessage) (any, error) {
		res, err := c.CallTool(c.baseCtx, remoteName, args)
		if err != nil {
			return nil, err
		}
		if res.IsError {
			return nil, fmt.Errorf("mcp tool %q reported an error: %s", remoteName, res.Text())
		}
		return res.Text(), nil
	}
}

// ServerInfo returns the server's reported name and version (available after a
// successful connection).
func (c *Client) ServerInfo() Implementation { return c.serverInfo }

// Close releases the client's transport resources (terminating the subprocess
// for stdio clients).
func (c *Client) Close() error { return c.transport.close() }
