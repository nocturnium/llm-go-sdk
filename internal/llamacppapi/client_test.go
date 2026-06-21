package llamacppapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/httpclient"
)

func testClient(t *testing.T, url string) *Client {
	t.Helper()
	return NewClient(ClientConfig{
		BaseURL:         url,
		AllowPrivateIPs: true, // httptest serves on 127.0.0.1
		AllowHTTP:       true,
	})
}

func TestNewClient_BaseURLNormalization(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", defaultBaseURL},
		{"http://host:8080", "http://host:8080"},
		{"http://host:8080/", "http://host:8080"},
		{"http://host:8080/v1", "http://host:8080"},
		{"http://host:8080/v1/", "http://host:8080"},
	}
	for _, tc := range cases {
		c := NewClient(ClientConfig{BaseURL: tc.in})
		if c.baseURL != tc.want {
			t.Errorf("NewClient(%q).baseURL = %q, want %q", tc.in, c.baseURL, tc.want)
		}
	}
}

func TestGetHeaders(t *testing.T) {
	if h := (&Client{}).getHeaders(); len(h) != 0 {
		t.Errorf("no api key should yield no headers, got %v", h)
	}
	h := (&Client{apiKey: "secret"}).getHeaders()
	if h["Authorization"] != "Bearer secret" {
		t.Errorf("Authorization = %q, want %q", h["Authorization"], "Bearer secret")
	}
}

func TestGetProps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			t.Errorf("path = %q, want /props", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"total_slots":4,"chat_template":"chatml"}`))
	}))
	defer srv.Close()

	props, err := testClient(t, srv.URL).GetProps(context.Background())
	if err != nil {
		t.Fatalf("GetProps: %v", err)
	}
	if props.TotalSlots != 4 || props.ChatTemplate != "chatml" {
		t.Errorf("got %+v", props)
	}
}

func TestGetHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %q, want /health", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok","slots_idle":3,"slots_processing":1}`))
	}))
	defer srv.Close()

	h, err := testClient(t, srv.URL).GetHealth(context.Background())
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if h.Status != "ok" || h.SlotsIdle != 3 || h.SlotsProcessing != 1 {
		t.Errorf("got %+v", h)
	}
}

func TestGetSlots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slots" {
			t.Errorf("path = %q, want /slots", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":0,"state":1,"n_ctx":4096},{"id":1,"state":0,"n_ctx":4096}]`))
	}))
	defer srv.Close()

	slots, err := testClient(t, srv.URL).GetSlots(context.Background())
	if err != nil {
		t.Fatalf("GetSlots: %v", err)
	}
	if len(slots) != 2 || slots[0].ID != 0 || slots[0].State != 1 || slots[1].NCtx != 4096 {
		t.Errorf("got %+v", slots)
	}
}

func TestGetProps_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer srv.Close()

	_, err := testClient(t, srv.URL).GetProps(context.Background())
	if err == nil {
		t.Fatal("expected an error on 500")
	}
	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode != http.StatusInternalServerError {
			t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
		}
		if apiErr.Provider != llms.ProviderLlamaCpp {
			t.Errorf("Provider = %q, want %q", apiErr.Provider, llms.ProviderLlamaCpp)
		}
	}
}

func TestWrapError(t *testing.T) {
	if WrapError("op", nil) != nil {
		t.Error("WrapError(nil) should be nil")
	}

	// httpclient.APIError is mapped to llms.APIError with the llamacpp provider.
	wrapped := WrapError("get props", &httpclient.APIError{
		StatusCode:    http.StatusNotFound,
		Message:       "not found",
		Type:          "invalid_request",
		Code:          "404",
		RequestURL:    "http://x/props",
		RequestMethod: http.MethodGet,
	})
	var apiErr *llms.APIError
	if !errors.As(wrapped, &apiErr) {
		t.Fatalf("expected *llms.APIError, got %T", wrapped)
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Provider != llms.ProviderLlamaCpp || apiErr.Message != "not found" {
		t.Errorf("mapped error = %+v", apiErr)
	}

	// An existing llms.APIError with no provider gets the llamacpp provider filled in.
	filled := WrapError("op", &llms.APIError{StatusCode: http.StatusBadRequest})
	if !errors.As(filled, &apiErr) || apiErr.Provider != llms.ProviderLlamaCpp {
		t.Errorf("expected provider filled, got %+v", apiErr)
	}

	// A generic error is wrapped as a provider error and preserves the cause.
	cause := errors.New("dial tcp: connection refused")
	generic := WrapError("get health", cause)
	if generic == nil || !errors.Is(generic, cause) {
		t.Errorf("expected wrapped provider error preserving cause, got %v", generic)
	}
}
