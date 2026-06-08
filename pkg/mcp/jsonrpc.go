package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
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
		ID *json.Number `json:"id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.ID == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(probe.ID.String(), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// decodeResult unmarshals a JSON-RPC response's result into out, returning any
// transport-level RPC error carried by the response.
func decodeResult(raw []byte, out any) error {
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

// extractJSONMessage returns the JSON-RPC object from a transport response body
// that may be either a bare JSON object or a Server-Sent Events stream. An SSE
// response can carry several events (e.g. a notification before the response), so
// each event's "data:" lines are assembled independently and the last event whose
// payload is a JSON object — the JSON-RPC response — is returned.
func extractJSONMessage(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
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
		if payload := bytes.TrimSpace(data.Bytes()); len(payload) > 0 && payload[0] == '{' {
			last = payload
		}
	}
	return last
}
