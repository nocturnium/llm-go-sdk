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

func TestClient_DoMultipart_RawBody(t *testing.T) {
	for _, body := range []string{"", "Hi.\n", "1\n00:00:00,000 --> 00:00:02,000\nHi.\n", "\x00\xffraw bytes"} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := r.ParseMultipartForm(1024); err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = r.MultipartForm.RemoveAll() }()
				if r.FormValue("model") != "transcribe" {
					t.Error("missing replayed form field")
				}
				if calls == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			policy := DefaultRetryPolicy()
			policy.InitialDelay, policy.MaxDelay = time.Microsecond, time.Millisecond
			client := NewClient(WithAllowHTTP(true), WithAllowPrivateIPs(true), WithRetryPolicy(policy))
			out := []byte("previous")
			err := client.DoMultipart(context.Background(), http.MethodPost, server.URL, map[string]string{"model": "transcribe"}, nil, nil, &out)
			if err != nil || string(out) != body || calls != 2 {
				t.Fatalf("out=%q calls=%d err=%v", out, calls, err)
			}
		})
	}
}

func TestClient_DoMultipart_BinaryResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("X-Generation-Id", "generation-1")
		w.Header().Add("X-Metadata", "one")
		w.Header().Add("X-Metadata", "two")
		_, _ = w.Write([]byte("RIFF\x00\xffWAVE"))
	}))
	defer server.Close()
	client := NewClient(WithAllowHTTP(true), WithAllowPrivateIPs(true))
	var out BinaryResponse
	err := client.DoMultipart(context.Background(), http.MethodPost, server.URL, nil, nil, nil, &out)
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Data) != "RIFF\x00\xffWAVE" || out.ContentType != "audio/wav" || out.Header.Get("X-Generation-Id") != "generation-1" || len(out.Header.Values("X-Metadata")) != 2 {
		t.Fatalf("response: %+v", out)
	}
}

func TestClient_DoMultipart_RawErrors(t *testing.T) {
	for _, binary := range []bool{false, true} {
		for _, status := range []int{http.StatusBadRequest, http.StatusOK} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if status == http.StatusOK {
					w.Header().Set("Content-Length", "104857601")
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "failure")
			}))
			client := NewClient(WithAllowHTTP(true), WithAllowPrivateIPs(true))
			raw := []byte("previous")
			response := BinaryResponse{Data: []byte("previous"), ContentType: "original"}
			var out any = &raw
			if binary {
				out = &response
			}
			err := client.DoMultipart(context.Background(), http.MethodPost, server.URL, nil, nil, nil, out)
			server.Close()
			if err == nil {
				t.Fatal("accepted HTTP error or oversized response")
			}
			if status == http.StatusOK && !strings.Contains(err.Error(), "maximum size") {
				t.Fatal(err)
			}
			if string(raw) != "previous" || string(response.Data) != "previous" || response.ContentType != "original" {
				t.Fatal("modified output on failure")
			}
		}
	}
}

func TestMultipartRawBody_UnknownLengthAndReadError(t *testing.T) {
	// Supply chunks without allocating a second full-size fixture.
	response := &http.Response{ContentLength: -1, Body: io.NopCloser(io.MultiReader(
		io.LimitReader(multipartZeroReader{}, maxResponseSize), strings.NewReader("x"),
	))}
	if _, err := readMultipartRawBody(response); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("oversized chunked body: %v", err)
	}
	response.Body = io.NopCloser(multipartFailedReader{})
	if _, err := readMultipartRawBody(response); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read error: %v", err)
	}
}

type multipartZeroReader struct{}

func (multipartZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type multipartFailedReader struct{}

func (multipartFailedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestClient_DoMultipart_PlainRepeatedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		for _, want := range []string{"word", "segment"} {
			part, err := reader.NextPart()
			if err != nil {
				t.Error(err)
				return
			}
			disposition := part.Header.Get("Content-Disposition")
			if strings.Contains(disposition, "filename") || part.Header.Get("Content-Type") != "" {
				t.Errorf("file headers on plain field: %v", part.Header)
			}
			if disposition != `form-data; name="timestamp_granularities[]"` {
				t.Errorf("unexpected disposition %q", disposition)
			}
			data, err := io.ReadAll(part)
			if err != nil || string(data) != want {
				t.Errorf("got %q %v", data, err)
			}
			_ = part.Close()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewClient(WithAllowHTTP(true), WithAllowPrivateIPs(true))
	err := client.DoMultipart(context.Background(), http.MethodPost, server.URL, nil, []MultipartFile{
		{Field: "timestamp_granularities[]", Data: []byte("word")},
		{Field: "timestamp_granularities[]", Data: []byte("segment")},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}
