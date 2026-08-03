package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

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
	// onRequest registers a sink for server-initiated requests (frames carrying
	// both a method and an id, such as sampling/createMessage). Unlike a
	// notification, the server is waiting for a response carrying that id, so fn
	// is responsible for eventually calling respond. fn is called on the
	// transport's read path and must not block; passing nil clears the sink.
	// Transports that cannot deliver server-initiated requests leave fn unused.
	onRequest(fn func(raw []byte, id json.RawMessage))
	// respond writes an already-encoded JSON-RPC response frame. It differs from
	// request in awaiting nothing, and from notify in that the frame carries an
	// id. Transports that cannot send unsolicited frames return
	// errRespondUnsupported.
	respond(ctx context.Context, payload []byte) error
	// supportsInbound reports whether this transport can actually deliver
	// server-initiated requests and send responses back. A transport that cannot
	// must not have capabilities advertised on its behalf: telling a server this
	// client samples, when the request can never arrive, leaves the server waiting
	// on a promise the transport cannot keep.
	supportsInbound() bool
	// close releases transport resources (terminating the subprocess for stdio).
	close() error
}

// errRespondUnsupported is returned by respond on transports that cannot send a
// frame outside a request/response exchange.
var errRespondUnsupported = errors.New("mcp: transport cannot send unsolicited responses")
