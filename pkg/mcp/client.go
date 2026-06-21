// Package mcp is a Model Context Protocol (MCP) client (protocol revision
// 2025-06-18). It connects to an MCP server over stdio (a subprocess) or
// Streamable HTTP and covers tools (tools/list, tools/call — registerable into an
// *llms.ToolRegistry so they drive llms.RunTools), resources (resources/list,
// resources/read), prompts (prompts/list, prompts/get — bridgeable into
// []llms.Message), the server's advertised capabilities, and server-initiated
// progress and log notifications.
//
// One-call mount (the common case — connect and register tools):
//
//	reg := llms.NewToolRegistry()
//	closer, err := mcp.MountStdio(ctx, reg, "npx",
//	    []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"})
//	if err != nil { return err }
//	defer closer.Close()
//	resp, msgs, err := llms.RunTools(ctx, llm, messages, reg)
//
// Explicit client (when you also need resources, prompts, or notifications):
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
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/httpclient"
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
	serverCaps ServerCapabilities
	// notifier holds the server-notification handler state behind a pointer so the
	// Client struct itself stays comparable (a mutex by value would not be).
	notifier *notifier
}

// notifier carries the registered handlers for server-initiated notifications.
// It is pointer-held by Client and guards its fields with its own mutex.
//
// Handler invocation runs OFF the transport read path: dispatchNotification only
// parses the frame and hands the typed work to a single serial pump goroutine via
// queue. If that bounded hand-off queue fills, later notifications are dropped
// rather than invoked concurrently or allowed to stall response delivery. A slow
// or re-entrant handler (one that calls back into the Client) can therefore never
// wedge in-flight RPCs on the read goroutine (stdio readLoop / the HTTP request
// goroutine).
type notifier struct {
	mu           sync.RWMutex
	progressFn   func(ProgressNotification)
	logFn        func(LogMessage)
	callProgress map[string]func(ProgressNotification) // keyed by progress token

	queue     chan func() // buffered hand-off of handler invocations to the pump
	done      chan struct{}
	closeOnce sync.Once
	dropped   atomic.Uint64
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
		notifier:   newNotifier(),
	}
	t.onNotification(c.dispatchNotification)
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
	if len(result.Capabilities) > 0 {
		// Capabilities are advisory; a malformed map should not fail the handshake,
		// so an unmarshal error simply leaves the typed view zero-valued.
		_ = json.Unmarshal(result.Capabilities, &c.serverCaps)
	}

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
		if res.NextCursor == "" || res.NextCursor == cursor {
			// A server that returns the same non-empty cursor forever would loop
			// until ctx cancellation; stop when the cursor fails to advance.
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

// CallOption configures a single CallToolWithProgress invocation.
type CallOption func(*callConfig)

type callConfig struct {
	progress func(ProgressNotification)
}

// WithProgress asks the server to report incremental progress for this tool call
// and routes each notifications/progress message it sends to fn. The client
// generates a unique progress token, sends it in the request's _meta, and
// delivers only the progress notifications carrying that token to fn (other
// progress notifications still reach any client-level OnProgress handler). The
// handler runs on the client's serial notification pump off the transport read
// path (see [Client.OnProgress]); it stops receiving notifications once the call
// returns.
func WithProgress(fn func(ProgressNotification)) CallOption {
	return func(c *callConfig) { c.progress = fn }
}

// CallToolWithProgress is [Client.CallTool] with per-call options such as
// [WithProgress]. With no options it behaves exactly like CallTool.
func (c *Client) CallToolWithProgress(ctx context.Context, name string, arguments json.RawMessage, opts ...CallOption) (*CallToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var cc callConfig
	for _, o := range opts {
		o(&cc)
	}
	params := callToolParams{Name: name, Arguments: arguments}
	if cc.progress != nil {
		token := "progress-" + strconv.FormatInt(c.nextID.Add(1), 10)
		params.Meta = &requestMeta{ProgressToken: token}
		cleanup := c.registerCallProgress(token, cc.progress)
		defer cleanup()
	}
	var res CallToolResult
	if err := c.call(ctx, methodToolsCall, params, &res); err != nil {
		return nil, fmt.Errorf("mcp: call tool %q: %w", name, err)
	}
	return &res, nil
}

// Register discovers the server's tools and registers each into reg as an
// llms.Tool whose handler invokes the tool over MCP. Tool names are prefixed with
// the client's NamePrefix. Tool discovery uses the client's connection context;
// registered handlers use the context passed by llms.ToolRegistry.Handle so
// in-flight MCP calls can be canceled by callers such as llms.RunTools.
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
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		res, err := c.CallTool(ctx, remoteName, args)
		if err != nil {
			return nil, err
		}
		if res.IsError {
			return nil, &ToolError{Tool: remoteName, Content: res.Text()}
		}
		return res.Text(), nil
	}
}

// ToolError reports that an MCP tool ran but returned an error result (the
// tools/call response had isError set). It is distinct from an [RPCError], which
// signals a protocol-level failure. Callers (and llms.RunTools handlers) can
// detect it with errors.As:
//
//	var toolErr *mcp.ToolError
//	if errors.As(err, &toolErr) { /* the tool itself failed: toolErr.Content */ }
type ToolError struct {
	// Tool is the server-side tool name (without the client's NamePrefix).
	Tool string
	// Content is the concatenated text content of the error result.
	Content string
}

// Error reports the tool-execution failure message, naming the tool and its
// error content.
func (e *ToolError) Error() string {
	return fmt.Sprintf("mcp tool %q reported an error: %s", e.Tool, e.Content)
}

// ServerInfo returns the server's reported name and version (available after a
// successful connection).
func (c *Client) ServerInfo() Implementation { return c.serverInfo }

// ServerCapabilities returns the typed capabilities the server advertised during
// initialize. A nil sub-capability means the feature was not advertised; gate
// calls on it, e.g.:
//
//	if c.ServerCapabilities().Resources != nil { /* safe to ListResources */ }
func (c *Client) ServerCapabilities() ServerCapabilities { return c.serverCaps }

// Close releases the client's transport resources (terminating the subprocess
// for stdio clients) and stops the notification pump.
func (c *Client) Close() error {
	err := c.transport.close()
	c.notifier.stop()
	return err
}
