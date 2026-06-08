package mcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

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

	done      chan struct{}
	closeOnce sync.Once
}

// newStdioTransport starts the server subprocess and begins reading its output.
// The provided context governs the subprocess lifetime: canceling it kills the
// process. A nil env inherits the current environment.
func newStdioTransport(ctx context.Context, command string, args, env []string, dir string) (*stdioTransport, error) {
	// The command is the MCP server the application developer chose to launch (the
	// whole point of a stdio MCP client), not untrusted input — analogous to a
	// configured binary path. Launching it from a variable is by design.
	// #nosec G204 -- caller-specified MCP server command, not untrusted input
	cmd := exec.CommandContext(ctx, command, args...)
	if env != nil {
		cmd.Env = env
	}
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
		// ReadBytes accumulates an arbitrarily long line (no fixed-size cap), so
		// large tool results are not truncated.
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			t.dispatch(line)
		}
		if err != nil {
			t.fail(err)
			return
		}
	}
}

func (t *stdioTransport) dispatch(line []byte) {
	id, ok := messageID(line)
	if !ok {
		return // notification or server-initiated request: not handled by this client
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
