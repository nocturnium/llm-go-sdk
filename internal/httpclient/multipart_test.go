package httpclient

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_DoMultipart_Retry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing Authorization header")
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
			t.Errorf("bad boundary: %v", err)
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("model") != "test-model" {
			t.Error("missing field on retry")
		}
		file, header, err := r.FormFile("audio")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil || string(data) != "complete audio" || header.Filename != "sample.wav" || header.Header.Get("Content-Type") != "audio/wav" {
			t.Errorf("bad file: %q %v", data, err)
		}
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	policy := DefaultRetryPolicy()
	policy.InitialDelay, policy.MaxDelay = time.Microsecond, time.Millisecond
	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true), WithRetryPolicy(policy))
	var out struct{ OK bool }
	err := client.DoMultipart(context.Background(), http.MethodPost, server.URL, map[string]string{"model": "test-model"}, []MultipartFile{{Field: "audio", Filename: "sample.wav", ContentType: "audio/wav", Data: []byte("complete audio")}}, map[string]string{"Authorization": "Bearer test-token"}, &out)
	if err != nil || !out.OK || calls != 2 {
		t.Fatalf("err=%v out=%+v calls=%d", err, out, calls)
	}
}

func TestClient_DoMultipart_Errors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(400)
		}
		w.Write([]byte("not JSON"))
	}))
	defer server.Close()
	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	ctx := context.Background()
	if err := client.DoMultipart(ctx, http.MethodPost, server.URL, nil, []MultipartFile{{Field: "file", Filename: "empty"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := client.DoMultipart(ctx, http.MethodPost, server.URL, nil, nil, nil, &out); err == nil {
		t.Fatal("accepted bad JSON")
	}
	if err := client.DoMultipart(ctx, http.MethodPost, server.URL+"/fail", nil, nil, nil, nil); err == nil {
		t.Fatal("accepted HTTP error")
	}
	if err := client.DoMultipart(ctx, "bad method", server.URL, nil, nil, nil, nil); err == nil {
		t.Fatal("accepted bad method")
	}
	if err := client.DoMultipart(ctx, http.MethodPost, server.URL, nil, []MultipartFile{{ContentType: "text/plain\r\nInjected: true"}}, nil, nil); err == nil {
		t.Fatal("accepted header injection")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := client.DoMultipart(canceled, http.MethodPost, server.URL, nil, nil, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := NewClient().DoMultipart(ctx, http.MethodPost, server.URL, nil, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatal(err)
	}
}

func TestClient_DoMultipart_SortedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		for _, name := range []string{"alpha", "middle", "zeta"} {
			part, err := reader.NextPart()
			if err != nil {
				t.Error(err)
				return
			}
			data, err := io.ReadAll(part)
			if err != nil || part.FormName() != name || string(data) != name {
				t.Errorf("part=%s data=%q err=%v", part.FormName(), data, err)
			}
			part.Close()
		}
		if _, err := reader.NextPart(); !errors.Is(err, io.EOF) {
			t.Errorf("expected end of form: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	if err := client.DoMultipart(context.Background(), http.MethodPost, server.URL, map[string]string{"zeta": "zeta", "alpha": "alpha", "middle": "middle"}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}
