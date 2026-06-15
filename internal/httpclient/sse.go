package httpclient

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
)

const maxSSEEventSize = 4 * 1024 * 1024 // 4MB

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	Event string
	Data  string
	ID    string
	Retry int
}

// SSEReader reads Server-Sent Events from a stream.
// SSEReader is safe for concurrent use; however, concurrent reads will
// be serialized and each event will only be returned to one caller.
//
// To interrupt a pending Read() operation when a context is canceled,
// call Close() on the reader. This will cause Read() to return with an error.
// Close() is safe to call multiple times and from any goroutine.
//
// Line length: SSEReader uses a bounded bufio.Reader loop rather than a
// bufio.Scanner, so individual SSE lines are not capped at the scanner's default
// 64KB token limit. Large single `data:` lines (common with base64-encoded
// images or large JSON payloads) are accepted up to maxSSEEventSize.
type SSEReader struct {
	mu     sync.Mutex
	br     *bufio.Reader
	reader io.ReadCloser
	closed atomic.Bool // Tracks if Close() has been called to prevent double-close
}

// NewSSEReader creates a new SSE reader from a response body
func NewSSEReader(r io.ReadCloser) *SSEReader {
	return &SSEReader{
		br:     bufio.NewReader(r),
		reader: r,
	}
}

// Read reads the next SSE event from the stream.
// Returns io.EOF when the stream is exhausted or when the reader has been closed.
//
// IMPORTANT: Read() blocks until data is available, an error occurs, or the reader
// is closed. There is no built-in timeout because:
//   - SSE connections are long-lived and heartbeat timing is server-defined
//   - Adding a timeout would require canceling legitimate waits for slow servers
//
// To implement timeouts:
//
//  1. Context-based: Launch Read() in a goroutine, call Close() when context expires.
//     Example:
//
//     go func() {
//     <-ctx.Done()
//     reader.Close()
//     }()
//     event, err := reader.Read() // Will return error when closed
//
//  2. Connection-level: Set read deadlines on the underlying net.Conn if accessible.
//     This is typically handled by the HTTP client timeout configuration.
//
//  3. Keep-alive detection: Many SSE servers send comment lines (":") as heartbeats.
//     If no events (including comments) arrive for a prolonged period, the connection
//     may be dead and should be restarted.
//
// For production use, callers should always implement one of these timeout strategies
// to handle network issues and server failures gracefully.
func (r *SSEReader) Read() (*SSEEvent, error) {
	// Check if already closed before acquiring lock
	if r.closed.Load() {
		return nil, io.EOF
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring lock
	if r.closed.Load() {
		return nil, io.EOF
	}

	var event SSEEvent
	var dataLines []string
	hasData := false
	eventSize := 0

	for {
		line, readErr := r.readLine()

		// readLine returns any data read before the error (e.g. a final line
		// without a trailing newline at EOF), so process the line first.
		if line != "" {
			eventSize += len(line)
			if eventSize > maxSSEEventSize {
				return nil, fmt.Errorf("SSE event exceeds maximum size of %d bytes", maxSSEEventSize)
			}

			// Strip the line terminator (\n and an optional preceding \r) to match
			// bufio.Scanner's ScanLines semantics.
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")

			switch {
			case line == "":
				// Empty line signals end of event.
				if hasData {
					event.Data = strings.Join(dataLines, "\n")
					return &event, nil
				}
				// Skip blank separators with no buffered data.
				if readErr == nil {
					eventSize = 0
					continue
				}
			case strings.HasPrefix(line, ":"):
				// Comment line: ignore.
			default:
				field, value, _ := strings.Cut(line, ":")
				// Remove leading space from value if present
				value = strings.TrimPrefix(value, " ")

				switch field {
				case "data":
					dataLines = append(dataLines, value)
					hasData = true
				case "event":
					event.Event = value
				case "id":
					event.ID = value
				case "retry":
					// We ignore retry for now
				}
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				// If we have buffered data at EOF, return it as a final event.
				if hasData {
					event.Data = strings.Join(dataLines, "\n")
					return &event, nil
				}
				return nil, io.EOF
			}
			return nil, readErr
		}
	}
}

func (r *SSEReader) readLine() (string, error) {
	var line []byte
	for {
		fragment, err := r.br.ReadSlice('\n')
		if len(line)+len(fragment) > maxSSEEventSize {
			return "", fmt.Errorf("SSE line exceeds maximum size of %d bytes", maxSSEEventSize)
		}
		line = append(line, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if len(line) > 0 {
				return string(line), err
			}
			return "", err
		}
		return string(line), nil
	}
}

// Close closes the underlying reader.
// Close is safe to call multiple times and from any goroutine.
// Calling Close will interrupt any pending Read() call.
func (r *SSEReader) Close() error {
	// Use CompareAndSwap to ensure we only close once
	if r.closed.CompareAndSwap(false, true) {
		return r.reader.Close()
	}
	// Already closed, return nil (idempotent)
	return nil
}

// IsClosed returns true if the reader has been closed.
func (r *SSEReader) IsClosed() bool {
	return r.closed.Load()
}
