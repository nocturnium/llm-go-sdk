package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxStdioLineBytes = 16 * 1024 * 1024

// errTransportClosed is returned when a request is made on a closed transport or
// the server's stdout closed while a request was in flight.
var errTransportClosed = errors.New("mcp: transport closed")

// stdioTransport speaks JSON-RPC over a subprocess's stdin/stdout using
// newline-delimited messages, the standard MCP stdio framing. A single
// background goroutine reads responses and dispatches them to waiting callers by
// id, so concurrent requests and context cancellation are handled safely.
type stdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex

	mu       sync.Mutex
	pending  map[int64]chan []byte
	closed   bool
	closeErr error

	notifyMu sync.RWMutex
	notifyFn func(raw []byte)

	requestMu sync.RWMutex
	requestFn func(raw []byte, id json.RawMessage)

	done      chan struct{}
	closeOnce sync.Once
}

func (t *stdioTransport) onNotification(fn func(raw []byte)) {
	t.notifyMu.Lock()
	t.notifyFn = fn
	t.notifyMu.Unlock()
}

func (t *stdioTransport) onRequest(fn func(raw []byte, id json.RawMessage)) {
	t.requestMu.Lock()
	t.requestFn = fn
	t.requestMu.Unlock()
}

// deliverRequest routes a server-initiated request to the registered sink. With
// no sink installed the client cannot serve the request, so it answers
// MethodNotFound rather than leaving the server waiting for a reply that will
// never come.
func (t *stdioTransport) deliverRequest(raw []byte, id json.RawMessage) {
	t.requestMu.RLock()
	fn := t.requestFn
	t.requestMu.RUnlock()
	if fn != nil {
		fn(raw, id)
		return
	}
	payload, err := encodeErrorResponse(id, CodeMethodNotFound, "mcp: client does not handle server-initiated requests")
	if err != nil {
		return
	}
	// Best effort: the read loop must keep serving responses regardless.
	_ = t.write(payload)
}

// respond writes a response frame for a server-initiated request. write is
// already serialized by writeMu, so this is safe concurrently with request and
// notify.
func (t *stdioTransport) respond(_ context.Context, payload []byte) error {
	return t.write(payload)
}

func (t *stdioTransport) deliverNotification(raw []byte) {
	t.notifyMu.RLock()
	fn := t.notifyFn
	t.notifyMu.RUnlock()
	if fn != nil {
		fn(raw)
	}
}

// newStdioTransport starts the server subprocess and begins reading its output.
// The provided context governs the subprocess lifetime: canceling it kills the
// process. The subprocess receives only a minimal safe environment by default
// (PATH, HOME, and common platform process keys when set); env entries add to or
// override that minimal set.
func newStdioTransport(ctx context.Context, command string, args, env []string, dir string) (*stdioTransport, error) {
	// The command is the MCP server the application developer chose to launch (the
	// whole point of a stdio MCP client), not untrusted input — analogous to a
	// configured binary path. Launching it from a variable is by design.
	// #nosec G204 -- caller-specified MCP server command, not untrusted input
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = mergeStdioEnv(minimalStdioEnv(), env)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: open stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %q: %w", command, err)
	}

	t := &stdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		pending: make(map[int64]chan []byte),
		done:    make(chan struct{}),
	}
	go t.readLoop(stdout)
	return t, nil
}

func (t *stdioTransport) readLoop(stdout io.Reader) {
	defer close(t.done)
	br := bufio.NewReader(stdout)
	for {
		line, err := readBoundedLine(br)
		if len(line) > 0 {
			t.dispatch(line)
		}
		if err != nil {
			t.fail(err)
			return
		}
	}
}

func readBoundedLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		fragment, err := br.ReadSlice('\n')
		if len(line)+len(fragment) > maxStdioLineBytes {
			return nil, fmt.Errorf("mcp: stdio line exceeds maximum size of %d bytes", maxStdioLineBytes)
		}
		line = append(line, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}

func minimalStdioEnv() []string {
	keys := []string{"PATH", "HOME", "SystemRoot", "ComSpec", "PATHEXT", "TEMP", "TMP", "USERPROFILE"}
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func mergeStdioEnv(base, overrides []string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	order := make([]string, 0, len(base)+len(overrides))
	for _, entry := range append(base, overrides...) {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		if _, found := values[key]; !found {
			order = append(order, key)
		}
		values[key] = entry
	}
	env := make([]string, 0, len(order))
	for _, key := range order {
		env = append(env, values[key])
	}
	return env
}

func (t *stdioTransport) dispatch(line []byte) {
	for _, msg := range splitJSONMessages(line) {
		t.dispatchOne(msg)
	}
}

func (t *stdioTransport) dispatchOne(line []byte) {
	// Classify before consulting pending. A server-initiated request carries an
	// id too, so routing on the id alone would either resolve an unrelated
	// in-flight call with an empty result or drop the request entirely.
	switch kind, rawID := classifyFrame(line); kind {
	case frameRequest:
		t.deliverRequest(line, rawID)
		return
	case frameNotification:
		t.deliverNotification(line)
		return
	case frameResponse:
		// Fall through to the id-correlated pending-response path below.
	}

	id, ok := messageID(line)
	if !ok {
		if isNullIDError(line) {
			t.dispatchNullIDError(line)
			return
		}
		// A response whose id could not be parsed: nothing can be correlated.
		return
	}
	t.mu.Lock()
	ch, found := t.pending[id]
	if found {
		delete(t.pending, id)
	}
	t.mu.Unlock()
	if found {
		ch <- line
	}
}

// dispatchNullIDError handles a JSON-RPC error response carrying a null/absent id.
// Per JSON-RPC such a frame is a parse/invalid-request error that cannot be
// correlated to any specific in-flight request, so it must not be broadcast to
// every pending channel (that would prematurely fail unrelated concurrent
// requests and then drop their real id-correlated responses).
//
//   - Exactly one request pending: the error is unambiguously the answer to it,
//     so deliver it there.
//   - Zero or more than one request pending: there is no safe correlation, so
//     treat it as a transport-level protocol failure (fail/teardown) — the
//     transport closes cleanly and every caller observes one consistent error
//     rather than a poisoned result.
func (t *stdioTransport) dispatchNullIDError(line []byte) {
	t.mu.Lock()
	if len(t.pending) == 1 {
		var ch chan []byte
		for id, c := range t.pending {
			ch = c
			delete(t.pending, id)
		}
		t.mu.Unlock()
		ch <- line
		return
	}
	t.mu.Unlock()
	t.fail(&RPCError{Code: CodeInvalidRequest, Message: "mcp: server returned an error with a null id; cannot correlate to a request"})
}

func isNullIDError(line []byte) bool {
	var resp rpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return false
	}
	return resp.Error != nil && string(resp.ID) == "null"
}

// fail marks the transport closed and unblocks every pending request.
func (t *stdioTransport) fail(err error) {
	t.mu.Lock()
	t.closed = true
	if t.closeErr == nil && err != nil && !errors.Is(err, io.EOF) {
		t.closeErr = err
	}
	pending := t.pending
	t.pending = make(map[int64]chan []byte)
	t.mu.Unlock()
	for _, ch := range pending {
		close(ch)
	}
}

func (t *stdioTransport) request(ctx context.Context, id int64, payload []byte) ([]byte, error) {
	ch := make(chan []byte, 1)

	t.mu.Lock()
	if t.closed {
		err := t.closeErr
		t.mu.Unlock()
		return nil, orClosed(err)
	}
	t.pending[id] = ch
	t.mu.Unlock()

	if err := t.write(payload); err != nil {
		t.removePending(id)
		return nil, err
	}

	select {
	case <-ctx.Done():
		t.removePending(id)
		return nil, ctx.Err()
	case <-t.done:
		// The transport closed, but a response may already have been delivered to
		// ch before EOF (a one-shot server answers, then closes stdout). Both cases
		// can be ready at once and select picks pseudo-randomly, so prefer a
		// delivered response: returning "transport closed" for a call the server
		// actually completed would make a non-idempotent tool (e.g. send email,
		// create ticket) look failed and be retried, duplicating the side effect.
		select {
		case line, ok := <-ch:
			if ok {
				return line, nil
			}
		default:
		}
		return nil, orClosed(t.closeErr)
	case line, ok := <-ch:
		if !ok {
			return nil, orClosed(t.closeErr)
		}
		return line, nil
	}
}

func (t *stdioTransport) notify(ctx context.Context, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return t.write(payload)
}

func (t *stdioTransport) write(payload []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.stdin.Write(payload); err != nil {
		return fmt.Errorf("mcp: write request: %w", err)
	}
	if _, err := t.stdin.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("mcp: write request: %w", err)
	}
	return nil
}

func (t *stdioTransport) removePending(id int64) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

func (t *stdioTransport) close() error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		t.mu.Unlock()

		// Closing stdin signals EOF so a well-behaved server exits on its own.
		_ = t.stdin.Close()

		// Wait for the reader to finish before reaping. cmd.Wait closes the stdout
		// pipe, so calling it while readLoop is still reading would race; the done
		// channel (closed by readLoop on exit) is the safe synchronization point. If
		// the server does not exit promptly, kill it to unblock the reader.
		select {
		case <-t.done:
		case <-time.After(2 * time.Second):
			if t.cmd.Process != nil {
				_ = t.cmd.Process.Kill()
			}
			<-t.done
		}
		_ = t.cmd.Wait()
	})
	return nil
}

func orClosed(err error) error {
	if err != nil {
		return err
	}
	return errTransportClosed
}
