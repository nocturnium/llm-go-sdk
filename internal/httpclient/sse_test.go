package httpclient

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// mockReadCloser wraps a strings.Reader to implement io.ReadCloser
type mockReadCloser struct {
	*strings.Reader
}

func (m *mockReadCloser) Close() error {
	return nil
}

func newMockReadCloser(s string) io.ReadCloser {
	return &mockReadCloser{strings.NewReader(s)}
}

func TestSSEReader_SimpleEvent(t *testing.T) {
	data := "data: hello world\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "hello world" {
		t.Errorf("expected data='hello world', got '%s'", event.Data)
	}
}

func TestSSEReader_EventWithType(t *testing.T) {
	data := "event: message\ndata: test data\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Event != "message" {
		t.Errorf("expected event='message', got '%s'", event.Event)
	}
	if event.Data != "test data" {
		t.Errorf("expected data='test data', got '%s'", event.Data)
	}
}

func TestSSEReader_EventWithID(t *testing.T) {
	data := "id: 123\ndata: test\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.ID != "123" {
		t.Errorf("expected id='123', got '%s'", event.ID)
	}
}

func TestSSEReader_MultilineData(t *testing.T) {
	data := "data: line1\ndata: line2\ndata: line3\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "line1\nline2\nline3"
	if event.Data != expected {
		t.Errorf("expected data='%s', got '%s'", expected, event.Data)
	}
}

func TestSSEReader_MultipleEvents(t *testing.T) {
	data := "data: first\n\ndata: second\n\ndata: third\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	events := []string{"first", "second", "third"}
	for _, expected := range events {
		event, err := reader.Read()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if event.Data != expected {
			t.Errorf("expected data='%s', got '%s'", expected, event.Data)
		}
	}

	// Next read should return EOF
	_, err := reader.Read()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestSSEReader_Comments(t *testing.T) {
	data := ": this is a comment\ndata: actual data\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "actual data" {
		t.Errorf("expected data='actual data', got '%s'", event.Data)
	}
}

func TestSSEReader_EmptyLines(t *testing.T) {
	data := "\n\ndata: test\n\n\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "test" {
		t.Errorf("expected data='test', got '%s'", event.Data)
	}
}

func TestSSEReader_DataWithColon(t *testing.T) {
	data := "data: key: value\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "key: value" {
		t.Errorf("expected data='key: value', got '%s'", event.Data)
	}
}

func TestSSEReader_DataWithLeadingSpace(t *testing.T) {
	data := "data:  leading space\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only one leading space should be removed
	if event.Data != " leading space" {
		t.Errorf("expected data=' leading space', got '%s'", event.Data)
	}
}

func TestSSEReader_EmptyData(t *testing.T) {
	data := "data:\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "" {
		t.Errorf("expected empty data, got '%s'", event.Data)
	}
}

func TestSSEReader_JSONData(t *testing.T) {
	jsonData := `{"message": "hello", "count": 42}`
	data := "data: " + jsonData + "\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != jsonData {
		t.Errorf("expected data='%s', got '%s'", jsonData, event.Data)
	}
}

func TestSSEReader_OpenAIDoneMarker(t *testing.T) {
	data := "data: [DONE]\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "[DONE]" {
		t.Errorf("expected data='[DONE]', got '%s'", event.Data)
	}
}

func TestSSEReader_Close(t *testing.T) {
	reader := NewSSEReader(newMockReadCloser("data: test\n\n"))

	err := reader.Close()
	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}
}

func TestSSEReader_EOF(t *testing.T) {
	data := ""
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	_, err := reader.Read()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestSSEReader_DataAtEOF(t *testing.T) {
	// Data without trailing newlines
	data := "data: final"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "final" {
		t.Errorf("expected data='final', got '%s'", event.Data)
	}
}

// TestSSEReader_LargeDataLine verifies that a single `data:` line larger than
// bufio.Scanner's default 64KB token cap is parsed in full. SSE payloads such as
// base64-encoded images or large JSON tool arguments routinely exceed 64KB.
func TestSSEReader_LargeDataLine(t *testing.T) {
	// 256KB payload on a single line, well beyond the 64KB scanner cap.
	const payloadLen = 256 * 1024
	payload := strings.Repeat("A", payloadLen)
	data := "data: " + payload + "\n\n"

	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error reading large SSE line: %v", err)
	}

	if len(event.Data) != payloadLen {
		t.Fatalf("expected data length %d, got %d", payloadLen, len(event.Data))
	}
	if event.Data != payload {
		t.Error("large SSE data line was not parsed faithfully")
	}
}

// chunkReader hands out at most n bytes per Read so a CRLF pair can be forced
// to straddle a buffer boundary, the case where a naive CR lookahead sees
// nothing and later mistakes the paired LF for an event separator.
type chunkReader struct {
	data []byte
	n    int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := c.n
	if n > len(p) {
		n = len(p)
	}
	if n > len(c.data) {
		n = len(c.data)
	}
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

func TestSSEReader_SplitCRLF(t *testing.T) {
	raw := "data: first\r" + "\n\r\n" + "data: second\r\n\r\n"
	r := NewSSEReader(io.NopCloser(&chunkReader{data: []byte(raw), n: 11}))
	defer r.Close()

	first, err := r.Read()
	if err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if first.Data != "first" {
		t.Fatalf("first event data = %q, want %q", first.Data, "first")
	}
	second, err := r.Read()
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if second.Data != "second" {
		t.Fatalf("second event data = %q, want %q", second.Data, "second")
	}
}

func TestSSEReader_BareCRSeparator(t *testing.T) {
	r := NewSSEReader(io.NopCloser(strings.NewReader("data: one\r\rdata: two\r\r")))
	defer r.Close()

	got, err := r.Read()
	if err != nil || got.Data != "one" {
		t.Fatalf("first = %+v, %v; want data one", got, err)
	}
	got, err = r.Read()
	if err != nil || got.Data != "two" {
		t.Fatalf("second = %+v, %v; want data two", got, err)
	}
}

func TestSSEReader_LineTooLarge(t *testing.T) {
	data := "data: " + strings.Repeat("A", maxSSEEventSize+1) + "\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	_, err := reader.Read()
	if err == nil {
		t.Fatal("expected oversized SSE line to fail")
	}
	if !strings.Contains(err.Error(), "SSE line exceeds maximum size") {
		t.Fatalf("expected line-size error, got %v", err)
	}
}

// A line of exactly maxSSEEventSize bytes is within the documented bound and
// must be accepted. The old check ran after reading the terminating byte, so an
// exact-max line was wrongly rejected as oversized.
func TestSSEReader_ExactMaxLineAccepted(t *testing.T) {
	content := strings.Repeat("A", maxSSEEventSize-len("data: "))
	data := "data: " + content + "\n\n"
	reader := NewSSEReader(newMockReadCloser(data))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("exact-max line rejected: %v", err)
	}
	if len(event.Data) != len(content) {
		t.Fatalf("got %d data bytes, want %d", len(event.Data), len(content))
	}
}

func TestSSEReader_EventTooLarge(t *testing.T) {
	var data strings.Builder
	content := 0
	for content <= maxSSEEventSize {
		data.WriteString("data: ")
		data.WriteString(strings.Repeat("A", 1024))
		data.WriteString("\n")
		content += len("data: ") + 1024
	}

	reader := NewSSEReader(newMockReadCloser(data.String()))
	defer func() { _ = reader.Close() }()

	_, err := reader.Read()
	if err == nil {
		t.Fatal("expected oversized SSE event to fail")
	}
	if !strings.Contains(err.Error(), "SSE event exceeds maximum size") {
		t.Fatalf("expected event-size error, got %v", err)
	}
}
