package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestClient_DoBinary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/large" {
			w.Header().Set("Content-Length", strconv.Itoa(maxResponseSize+1))
			return
		}
		if r.Header.Get("X-Test") != "value" {
			t.Error("missing request header")
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Character-Cost", "5")
		w.Write([]byte{0, 1, 2})
	}))
	defer server.Close()
	client := NewClient(WithAllowPrivateIPs(true), WithAllowHTTP(true))
	response, err := client.DoBinary(context.Background(), http.MethodPost, server.URL, map[string]string{"text": "hello"}, map[string]string{"X-Test": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Data) != string([]byte{0, 1, 2}) || response.ContentType != "audio/wav" || response.Header.Get("Character-Cost") != "5" {
		t.Fatalf("bad response: %+v", response)
	}
	if _, err := client.DoBinary(context.Background(), http.MethodGet, server.URL+"/large", nil, nil); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("missing size cap: %v", err)
	}
}
