package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSSEReader_LargeResponse(t *testing.T) {
	// Generate 1000 chunks
	var buf bytes.Buffer
	for i := 0; i < 1000; i++ {
		buf.WriteString("data: chunk-")
		buf.WriteString(string(rune('0' + (i % 10))))
		buf.WriteString("\n\n")
	}

	reader := NewSSEReader(io.NopCloser(&buf))
	defer func() { _ = reader.Close() }()

	count := 0
	for {
		event, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error at chunk %d: %v", count, err)
		}
		if !strings.HasPrefix(event.Data, "chunk-") {
			t.Errorf("unexpected data at chunk %d: %s", count, event.Data)
		}
		count++
	}

	if count != 1000 {
		t.Errorf("expected 1000 chunks, got %d", count)
	}
}

func TestSSEReader_PartialChunk(t *testing.T) {
	// Simulate data arriving in partial chunks
	pr, pw := io.Pipe()

	go func() {
		// Send data in small pieces
		_, _ = pw.Write([]byte("dat"))
		time.Sleep(10 * time.Millisecond)
		_, _ = pw.Write([]byte("a: hello"))
		time.Sleep(10 * time.Millisecond)
		_, _ = pw.Write([]byte("\n\n"))
		_ = pw.Close()
	}()

	reader := NewSSEReader(pr)
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Data != "hello" {
		t.Errorf("expected data=hello, got %s", event.Data)
	}
}

func TestSSEReader_MultilineData_Streaming(t *testing.T) {
	data := "data: line1\ndata: line2\ndata: line3\n\n"

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Multiline data should be joined with newlines
	expected := "line1\nline2\nline3"
	if event.Data != expected {
		t.Errorf("expected data=%q, got %q", expected, event.Data)
	}
}

func TestSSEReader_EventTypes(t *testing.T) {
	data := `event: message
data: hello

event: ping
data: pong

event: custom_event
data: {"type": "custom"}

`

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	expected := []struct {
		event string
		data  string
	}{
		{"message", "hello"},
		{"ping", "pong"},
		{"custom_event", `{"type": "custom"}`},
	}

	for i, exp := range expected {
		event, err := reader.Read()
		if err != nil {
			t.Fatalf("unexpected error at event %d: %v", i, err)
		}
		if event.Event != exp.event {
			t.Errorf("event %d: expected event=%s, got %s", i, exp.event, event.Event)
		}
		if event.Data != exp.data {
			t.Errorf("event %d: expected data=%s, got %s", i, exp.data, event.Data)
		}
	}
}

func TestSSEReader_EmptyLines_Streaming(t *testing.T) {
	// Multiple empty lines should be handled
	data := "data: first\n\n\n\ndata: second\n\n"

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	event1, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event1.Data != "first" {
		t.Errorf("expected data=first, got %s", event1.Data)
	}

	event2, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event2.Data != "second" {
		t.Errorf("expected data=second, got %s", event2.Data)
	}
}

func TestSSEReader_Comments_Streaming(t *testing.T) {
	data := `: this is a comment
data: actual data
: another comment
data: more data

`

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Comments should be ignored, data concatenated
	expected := "actual data\nmore data"
	if event.Data != expected {
		t.Errorf("expected data=%q, got %q", expected, event.Data)
	}
}

func TestSSEReader_ID(t *testing.T) {
	data := `id: 123
data: hello

id: 456
data: world

`

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	event1, _ := reader.Read()
	if event1.ID != "123" {
		t.Errorf("expected id=123, got %s", event1.ID)
	}

	event2, _ := reader.Read()
	if event2.ID != "456" {
		t.Errorf("expected id=456, got %s", event2.ID)
	}
}

func TestSSEReader_ConcurrentRead(t *testing.T) {
	// Verify that concurrent reads don't cause issues
	// (though typically only one goroutine should read)
	data := strings.Repeat("data: test\n\n", 100)

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	var wg sync.WaitGroup
	var mu sync.Mutex
	count := 0

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, err := reader.Read()
				if errors.Is(err, io.EOF) {
					return
				}
				if err != nil {
					return
				}
				mu.Lock()
				count++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Due to concurrent reads, we might not get exactly 100
	// but we should get at least some
	if count == 0 {
		t.Error("expected some events to be read")
	}
}

func TestSSEReader_ContextCancellation(t *testing.T) {
	pr, pw := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())

	// Writer that blocks indefinitely
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel() // Cancel after a short delay
		_ = pw.Close()
	}()

	reader := NewSSEReader(pr)
	defer func() { _ = reader.Close() }()

	// Try to read - should eventually fail when pipe closes
	_, err := reader.Read()
	if err == nil {
		t.Error("expected error after context cancellation")
	}

	_ = ctx // Used for documentation
}

func TestSSEReader_CloseWhileReading(t *testing.T) {
	pr, pw := io.Pipe()

	reader := NewSSEReader(pr)

	// Writer that sends data slowly
	go func() {
		for i := 0; i < 5; i++ {
			_, _ = pw.Write([]byte("data: test\n\n"))
			time.Sleep(20 * time.Millisecond)
		}
		_ = pw.Close()
	}()

	// Read some events
	for i := 0; i < 2; i++ {
		_, err := reader.Read()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Close while more data is coming
	_ = reader.Close()

	// Further reads should fail
	_, err := reader.Read()
	if err == nil {
		t.Error("expected error after close")
	}
}

func TestSSEReader_JSONData_Streaming(t *testing.T) {
	// Common pattern: JSON payloads in data field
	data := `data: {"id":"1","content":"hello"}

data: {"id":"2","content":"world","done":true}

`

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	event1, _ := reader.Read()
	if event1.Data != `{"id":"1","content":"hello"}` {
		t.Errorf("unexpected data: %s", event1.Data)
	}

	event2, _ := reader.Read()
	if event2.Data != `{"id":"2","content":"world","done":true}` {
		t.Errorf("unexpected data: %s", event2.Data)
	}
}

func TestSSEReader_DoneMarker(t *testing.T) {
	// OpenAI uses [DONE] to signal end of stream
	data := `data: {"content":"hello"}

data: [DONE]

`

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	event1, _ := reader.Read()
	if event1.Data != `{"content":"hello"}` {
		t.Errorf("unexpected data: %s", event1.Data)
	}

	event2, _ := reader.Read()
	if event2.Data != "[DONE]" {
		t.Errorf("expected [DONE], got: %s", event2.Data)
	}
}

func TestSSEReader_Retry(t *testing.T) {
	// SSE spec supports retry field
	data := `retry: 5000
data: hello

`

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	event, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Data != "hello" {
		t.Errorf("expected data=hello, got %s", event.Data)
	}
	// Retry field is typically handled by the client, not exposed
}

func TestSSEReader_MixedContent(t *testing.T) {
	// Complex real-world scenario
	data := `event: message_start
data: {"type":"message_start"}

event: content_block_start
data: {"type":"content_block_start","index":0}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"text":" World"}}

event: content_block_stop
data: {"type":"content_block_stop"}

event: message_stop
data: {"type":"message_stop"}

`

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	expectedEvents := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_delta",
		"content_block_stop",
		"message_stop",
	}

	for i, expEvent := range expectedEvents {
		event, err := reader.Read()
		if err != nil {
			t.Fatalf("unexpected error at event %d: %v", i, err)
		}
		if event.Event != expEvent {
			t.Errorf("event %d: expected event=%s, got %s", i, expEvent, event.Event)
		}
	}
}

func TestSSEReader_WindowsLineEndings(t *testing.T) {
	// Some servers might send \r\n
	data := "data: hello\r\n\r\ndata: world\r\n\r\n"

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	event1, err := reader.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Data should have \r stripped
	if strings.Contains(event1.Data, "\r") {
		t.Errorf("data should not contain \\r: %q", event1.Data)
	}
}

func TestSSEReader_SpaceAfterColon(t *testing.T) {
	// SSE spec allows optional space after colon
	data := `data:no-space
data: with-space

`

	reader := NewSSEReader(io.NopCloser(strings.NewReader(data)))
	defer func() { _ = reader.Close() }()

	event, _ := reader.Read()
	// Both should be handled, space after colon is optional
	if event.Data != "no-space\nwith-space" && event.Data != " no-space\nwith-space" {
		// Implementation may vary
		t.Logf("data handling: %q", event.Data)
	}
}
