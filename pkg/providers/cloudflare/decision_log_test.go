package cloudflare

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestDecideDoesNotLogState runs Decide with LLM_DEBUG_REQUESTS on, for both a
// successful and a failing call, and asserts the state never reaches stdout.
func TestDecideDoesNotLogState(t *testing.T) {
	t.Setenv("LLM_DEBUG_REQUESTS", "1")
	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			w.WriteHeader(status)
			if status == http.StatusOK {
				_, _ = io.WriteString(w, liveClefOmni)
				return
			}
			// A provider error that echoes the request.
			_, _ = io.WriteString(w, `{"error":{"message":"bad","echo":`+string(body)+`}}`)
		})
		out := captureStdout(t, func() { _, _ = c.Decide(context.Background(), clefRequest()) })
		if strings.Contains(out, secretState) || strings.Contains(out, "4471") {
			t.Fatalf("status %d: state reached the debug log:\n%s", status, out)
		}
		if !strings.Contains(out, "[DEBUG] LLM Request URL") {
			t.Fatalf("status %d: debug logging was not active, the test proves nothing:\n%s", status, out)
		}
		if !strings.Contains(out, "redacted: sensitive") {
			t.Fatalf("status %d: no redaction marker, was the request marked Sensitive?\n%s", status, out)
		}
	}
}

// captureStdout runs fn and returns what it printed. The debug log writes to
// stdout, so this test must not run in parallel.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	restored := false
	restore := func() {
		if !restored {
			restored = true
			os.Stdout = orig
			_ = w.Close()
		}
	}
	defer restore()
	t.Cleanup(restore)
	fn()
	restore()
	return <-done
}
