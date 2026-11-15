package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn and returns what it printed. The debug log writes to
// stdout, so these tests must not run in parallel.
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
	// Restore even when fn panics or fails the test.
	defer restore()
	t.Cleanup(restore)
	fn()
	restore()
	return <-done
}

func TestDoJSON_SensitiveRedactsDebugLog(t *testing.T) {
	const secret = "payouts failing for account 4471"
	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			w.WriteHeader(status)
			// Echo the request, as a provider error can.
			_, _ = w.Write([]byte(`{"error":{"message":"bad input","echo":` + string(body) + `}}`))
		}))
		t.Setenv("LLM_DEBUG_REQUESTS", "1")
		c := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
		out := captureStdout(t, func() {
			var resp map[string]any
			_ = c.DoJSON(context.Background(), Request{Method: http.MethodPost, URL: srv.URL, Body: map[string]string{"state": secret}, Sensitive: true}, &resp)
		})
		srv.Close()
		if strings.Contains(out, secret) {
			t.Fatalf("status %d: sensitive body reached the debug log:\n%s", status, out)
		}
		if !strings.Contains(out, "redacted: sensitive") {
			t.Fatalf("status %d: expected a redaction marker in the debug log:\n%s", status, out)
		}
	}
}

func TestDoJSON_NonSensitiveStillLogsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("LLM_DEBUG_REQUESTS", "1")
	c := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	out := captureStdout(t, func() {
		var resp map[string]any
		_ = c.DoJSON(context.Background(), Request{Method: http.MethodPost, URL: srv.URL, Body: map[string]string{"q": "visible"}}, &resp)
	})
	if !strings.Contains(out, "visible") {
		t.Fatalf("debug body logging regressed for non-sensitive requests:\n%s", out)
	}
}

func TestDoRawAndDoStream_SensitiveRedactsErrorBody(t *testing.T) {
	const secret = "payouts failing for account 4471"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad","echo":` + string(body) + `}}`))
	}))
	defer srv.Close()
	t.Setenv("LLM_DEBUG_REQUESTS", "1")
	c := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	req := Request{Method: http.MethodPost, URL: srv.URL, Body: map[string]string{"state": secret}, Sensitive: true}
	out := captureStdout(t, func() {
		_, _, _ = c.DoRawWithHeaders(context.Background(), req)
		if body, err := c.DoStream(context.Background(), req); err == nil {
			_ = body.Close()
		}
	})
	if strings.Contains(out, secret) {
		t.Fatalf("sensitive error body reached the debug log:\n%s", out)
	}
	if strings.Count(out, "redacted: sensitive") < 2 {
		t.Fatalf("expected a redaction marker per call:\n%s", out)
	}
}
