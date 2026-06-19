package mcp

import "context"

// transport carries JSON-RPC messages to an MCP server. Implementations
// correlate responses to requests by id where the wire protocol requires it
// (stdio); HTTP relies on the request/response pairing.
type transport interface {
	// request sends an already-encoded JSON-RPC request carrying the given id and
	// returns the raw response message bytes.
	request(ctx context.Context, id int64, payload []byte) ([]byte, error)
	// notify sends an already-encoded JSON-RPC notification; no response is awaited.
	notify(ctx context.Context, payload []byte) error
	// onNotification registers a sink for server-initiated frames (notifications:
	// messages with no id, such as notifications/progress). The transport delivers
	// each such raw JSON-RPC frame to fn. fn may be called concurrently with
	// requests and must not block; passing nil clears the sink. Transports that
	// cannot surface server-initiated frames leave fn unused.
	onNotification(fn func(raw []byte))
	// close releases transport resources (terminating the subprocess for stdio).
	close() error
}
