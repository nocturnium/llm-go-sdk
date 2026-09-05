package llms

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMediaInput_Validate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input MediaInput
		valid bool
	}{
		{"none", MediaInput{}, false}, {"mime only", MediaInput{MIMEType: "image/png"}, false},
		{"url", MediaInput{URL: "https://example.com/a"}, true}, {"data", MediaInput{Data: []byte{1}}, true}, {"file", MediaInput{FileID: "file"}, true},
		{"url and data", MediaInput{URL: "url", Data: []byte{1}}, false}, {"url and file", MediaInput{URL: "url", FileID: "file"}, false},
		{"data and file", MediaInput{Data: []byte{1}, FileID: "file"}, false}, {"all", MediaInput{URL: "url", Data: []byte{1}, FileID: "file"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if (err == nil) != tc.valid || (err != nil && !errors.Is(err, ErrInvalidParameters)) {
				t.Fatal(err)
			}
		})
	}
}

func TestMediaAsset_Fetch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write([]byte("asset")) }))
	defer server.Close()
	ctx := context.Background()
	asset := &MediaAsset{Data: []byte("cached"), URL: server.URL, ExpiresAt: time.Now().Add(-time.Hour)}
	if data, err := asset.Fetch(ctx, nil); err != nil || string(data) != "cached" || calls.Load() != 0 {
		t.Fatalf("cached: %q %v", data, err)
	}
	asset.Data = nil
	if _, err := asset.Fetch(ctx, nil); !errors.Is(err, ErrAssetExpired) {
		t.Fatal(err)
	}
	asset.ExpiresAt = time.Now().Add(time.Hour)
	if _, err := asset.Fetch(ctx, server.Client()); err == nil {
		t.Fatal("allowed insecure URL")
	}
	if _, err := asset.FetchWithOptions(ctx, server.Client(), MediaFetchOptions{AllowHTTP: true}); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected loopback refusal: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("network used before opt-out")
	}
	data, err := asset.FetchWithOptions(ctx, server.Client(), MediaFetchOptions{AllowPrivateIPs: true, AllowHTTP: true})
	if err != nil || string(data) != "asset" || string(asset.Data) != "asset" {
		t.Fatalf("download: %q %v", data, err)
	}
	if _, err := asset.Fetch(ctx, nil); err != nil || calls.Load() != 1 {
		t.Fatal("cache missed")
	}
	if _, err := (&MediaAsset{CloudURI: "gs://bucket/asset"}).Fetch(ctx, nil); !errors.Is(err, ErrInvalidParameters) {
		t.Fatal(err)
	}
	if _, err := (&MediaAsset{URL: server.URL}).Fetch(ctx, nil); err == nil {
		t.Fatal("default client allowed HTTP")
	}
}

func TestJobState_Terminal(t *testing.T) {
	for _, state := range []JobState{JobQueued, JobRunning, JobSucceeded, JobFailed, JobCancelled, JobModerated, "unknown"} {
		want := state == JobSucceeded || state == JobFailed || state == JobCancelled || state == JobModerated
		if state.Terminal() != want {
			t.Fatal(state)
		}
	}
}

func TestModerationError(t *testing.T) {
	err := &ModerationError{Stage: ModerationInput, Provider: "test", Reasons: []string{"reason"}, Charged: true}
	if !errors.Is(err, ErrContentFiltered) || !strings.Contains(err.Error(), "reason") {
		t.Fatal(err)
	}
	if !errors.Is(&APIError{StatusCode: 402}, ErrPlanRequired) {
		t.Fatal("402 classification missing")
	}
}

func TestMediaAsset_Fetch_Redirect(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
		if r.Header.Get("x-api-key") != "" {
			t.Error("cross-host redirect leaked credentials")
		}
		w.Write([]byte("redirected asset"))
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing initial API key header")
		}
		http.Redirect(w, r, strings.Replace(destination.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	}))
	defer origin.Close()
	asset := &MediaAsset{URL: origin.URL}
	data, err := asset.FetchWithOptions(context.Background(), origin.Client(), MediaFetchOptions{AllowHTTP: true, AllowPrivateIPs: true, Headers: map[string]string{"x-api-key": "test-key"}})
	if err != nil || string(data) != "redirected asset" || destinationCalls.Load() != 1 {
		t.Fatalf("redirect: %q %v", data, err)
	}
}
