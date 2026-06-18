package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// jsonRPCVersion is the JSON-RPC protocol version every message carries.
const jsonRPCVersion = "2.0"

// rpcRequest is a JSON-RPC 2.0 request (a message expecting a response).
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcNotification is a JSON-RPC 2.0 notification (a message with no response).
type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Standard JSON-RPC 2.0 error codes (see the JSON-RPC and MCP specs). Servers may
// also return implementation-defined codes; inspect RPCError.Code directly for those.
const (
	// CodeParseError indicates invalid JSON was received by the server.
	CodeParseError = -32700
	// CodeInvalidRequest indicates the payload was not a valid Request object.
	CodeInvalidRequest = -32600
	// CodeMethodNotFound indicates the requested method does not exist.
	CodeMethodNotFound = -32601
	// CodeInvalidParams indicates invalid method parameters.
	CodeInvalidParams = -32602
	// CodeInternalError indicates an internal JSON-RPC error.
	CodeInternalError = -32603
)

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object returned by an MCP server. Calls such as
// [Client.CallTool] and [Client.ListTools] wrap it, so callers can inspect the
// protocol-level code with errors.As:
//
//	var rpcErr *mcp.RPCError
//	if errors.As(err, &rpcErr) && rpcErr.Code == mcp.CodeMethodNotFound {
//	    // the server does not implement the method
//	}
//
// A server-side tool that runs but reports failure is NOT an RPCError; see [ToolError].
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("mcp: rpc error %d: %s (%s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("mcp: rpc error %d: %s", e.Code, e.Message)
}

// encodeRequest marshals a JSON-RPC request with the given id, method, and params.
func encodeRequest(id int64, method string, params any) ([]byte, error) {
	return json.Marshal(rpcRequest{JSONRPC: jsonRPCVersion, ID: id, Method: method, Params: params})
}

// encodeNotification marshals a JSON-RPC notification.
func encodeNotification(method string, params any) ([]byte, error) {
	return json.Marshal(rpcNotification{JSONRPC: jsonRPCVersion, Method: method, Params: params})
}

// messageID returns the numeric id of a JSON-RPC message, or (0, false) if the
// message has no id (a notification or server-initiated request we do not handle).
func messageID(raw []byte) (int64, bool) {
	var probe struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || len(probe.ID) == 0 || bytes.Equal(probe.ID, []byte("null")) {
		return 0, false
	}

	var id int64
	var err error
	if len(probe.ID) > 0 && probe.ID[0] == '"' {
		var s string
		if err := json.Unmarshal(probe.ID, &s); err != nil {
			return 0, false
		}
		id, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, false
		}
		return id, true
	}

	var n json.Number
	if err := json.Unmarshal(probe.ID, &n); err != nil {
		return 0, false
	}
	id, err = strconv.ParseInt(n.String(), 10, 64)
	if err == nil {
		return id, true
	}
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil || math.Trunc(f) != f || f < math.MinInt64 || f > math.MaxInt64 {
		return 0, false
	}
	id = int64(f)
	return id, true
}

// decodeResult unmarshals a JSON-RPC response's result into out, returning any
// transport-level RPC error carried by the response.
func decodeResult(raw []byte, out any) error {
	if messages := splitJSONMessages(raw); len(messages) > 0 {
		raw = messages[0]
		for _, msg := range messages {
			var resp rpcResponse
			if err := json.Unmarshal(msg, &resp); err == nil && (len(resp.Result) > 0 || resp.Error != nil) {
				raw = msg
				break
			}
		}
	}
	var resp rpcResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("mcp: decode response: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("mcp: decode result: %w", err)
		}
	}
	return nil
}

// extractJSONMessage returns the JSON-RPC object or batch from a transport response body
// that may be either bare JSON or a Server-Sent Events stream. An SSE
// response can carry several events (e.g. a notification before the response), so
// each event's "data:" lines are assembled independently and the last event with
// a JSON-RPC payload is returned.
func extractJSONMessage(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return trimmed
	}
	var last []byte
	for _, event := range bytes.Split(body, []byte("\n\n")) {
		var data bytes.Buffer
		for _, line := range bytes.Split(event, []byte("\n")) {
			line = bytes.TrimRight(line, "\r")
			if rest, ok := bytes.CutPrefix(line, []byte("data:")); ok {
				data.Write(bytes.TrimSpace(rest))
				data.WriteByte('\n')
			}
		}
		if payload := bytes.TrimSpace(data.Bytes()); len(payload) > 0 && (payload[0] == '{' || payload[0] == '[') {
			last = payload
		}
	}
	return last
}

func splitJSONMessages(raw []byte) [][]byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] != '[' {
		return [][]byte{trimmed}
	}
	var batch []json.RawMessage
	if err := json.Unmarshal(trimmed, &batch); err != nil {
		return [][]byte{trimmed}
	}
	messages := make([][]byte, 0, len(batch))
	for _, msg := range batch {
		if trimmedMsg := bytes.TrimSpace(msg); len(trimmedMsg) > 0 {
			messages = append(messages, trimmedMsg)
		}
	}
	return messages
}
