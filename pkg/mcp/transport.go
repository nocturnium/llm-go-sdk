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
	// close releases transport resources (terminating the subprocess for stdio).
	close() error
}
