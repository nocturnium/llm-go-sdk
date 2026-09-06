package geminiapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestClient_MediaRoutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test" {
			t.Error("missing key")
		}
		switch r.URL.Path {
		case "/v1beta/models/veo-3.1-lite-generate-preview:predictLongRunning":
			if r.Method != http.MethodPost {
				t.Error("expected POST")
			}
			var req PredictLongRunningRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if len(req.Instances) != 1 || req.Instances[0].Prompt != "clouds" {
				t.Errorf("request = %+v", req)
			}
			_, _ = io.WriteString(w, `{"name":"models/veo/operations/job"}`)
		case "/v1beta/models/veo/operations/job":
			if r.Method != http.MethodGet {
				t.Error("expected GET")
			}
			_, _ = io.WriteString(w, `{"name":"models/veo/operations/job","done":true}`)
		case "/v1beta/interactions":
			if r.Method != http.MethodPost {
				t.Error("expected POST")
			}
			var req InteractionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Model != "gemini-3.5-transcribe" {
				t.Error("wrong model")
			}
			_, _ = io.WriteString(w, `{"id":"interaction","status":"in_progress"}`)
		case "/v1beta/interactions/interaction":
			if r.Method != http.MethodGet {
				t.Error("expected GET")
			}
			_, _ = io.WriteString(w, `{"id":"interaction","status":"completed"}`)
		case "/file":
			if r.URL.Query().Get("alt") != "media" {
				t.Error("missing query")
			}
			_, _ = io.WriteString(w, "mp4")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := NewClient(ClientConfig{BaseURL: server.URL + "/v1beta", APIKey: "test", AllowPrivateIPs: true, AllowHTTP: true})
	ctx := context.Background()
	op, err := c.PredictLongRunning(ctx, "models/veo-3.1-lite-generate-preview", &PredictLongRunningRequest{Instances: []VideoInstance{{Prompt: "clouds"}}})
	if err != nil || op.Name != "models/veo/operations/job" {
		t.Fatalf("operation = %+v, %v", op, err)
	}
	op, err = c.GetOperation(ctx, op.Name)
	if err != nil || !op.Done {
		t.Fatalf("operation = %+v, %v", op, err)
	}
	interaction, err := c.CreateInteraction(ctx, &InteractionRequest{Model: "gemini-3.5-transcribe"})
	if err != nil || interaction.Status != "in_progress" {
		t.Fatalf("interaction = %+v, %v", interaction, err)
	}
	interaction, err = c.GetInteraction(ctx, interaction.ID)
	if err != nil || interaction.Status != "completed" {
		t.Fatalf("interaction = %+v, %v", interaction, err)
	}
	data, err := c.DownloadFile(ctx, server.URL+"/file?alt=media")
	if err != nil || string(data) != "mp4" {
		t.Fatalf("download = %q, %v", data, err)
	}
}
func TestClient_MediaErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"message":"free_tier limit: 0","status":"RESOURCE_EXHAUSTED"}}`)
	}))
	defer server.Close()
	c := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "test", AllowPrivateIPs: true, AllowHTTP: true})
	ctx := context.Background()
	_, a := c.PredictLongRunning(ctx, "veo", &PredictLongRunningRequest{})
	_, b := c.GetOperation(ctx, "models/veo/operations/job")
	_, d := c.CreateInteraction(ctx, &InteractionRequest{})
	_, e := c.GetInteraction(ctx, "interaction")
	_, f := c.DownloadFile(ctx, server.URL+"/file")
	for _, err := range []error{a, b, d, e, f} {
		if !errors.Is(err, llms.ErrRateLimited) || llms.ProviderFromError(err) != llms.ProviderGemini || !strings.Contains(err.Error(), "free_tier") {
			t.Fatal(err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.CreateInteraction(canceled, &InteractionRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.DownloadFile(canceled, server.URL); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	invalid := NewClient(ClientConfig{BaseURL: "%"})
	if _, err := invalid.CreateInteraction(ctx, &InteractionRequest{}); err == nil {
		t.Fatal("accepted bad base URL")
	}
}
func TestClient_MediaResourceValidation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"name":"https://evil.example/operation"}`)
	}))
	defer server.Close()
	c := NewClient(ClientConfig{BaseURL: server.URL, AllowPrivateIPs: true, AllowHTTP: true})
	ctx := context.Background()
	for _, name := range []string{"", "https://evil.example/operation", "models/veo/operations/..", "models/../operations/job", "models/veo/operations/%2f", "models/veo/operations/job?key=secret", "models/veo/operations/job/extra", "models/veo/operations/"} {
		if _, err := c.GetOperation(ctx, name); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("accepted %q: %v", name, err)
		}
	}
	for _, id := range []string{"", ".", "..", "a/b", "%2e", "job?x=1", "job#fragment"} {
		if _, err := c.GetInteraction(ctx, id); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("accepted %q: %v", id, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid resources sent requests")
	}
	if _, err := c.PredictLongRunning(ctx, "veo", &PredictLongRunningRequest{}); !errors.Is(err, llms.ErrInvalidParameters) || !strings.Contains(err.Error(), "https://evil.example/operation") {
		t.Fatal("missing raw operation name or invalid-parameters cause")
	}
	if !validResourceID("ID_12-3.~") {
		t.Fatal("valid resource ID rejected")
	}
}

type mediaRoundTripper func(*http.Request) (*http.Response, error)

func (f mediaRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestClient_DownloadFile_Security(t *testing.T) {
	var calls atomic.Int32
	hc := &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("x-goog-api-key") != "test" {
			t.Error("missing key")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("file")), Request: r}, nil
	})}
	c := NewClient(ClientConfig{APIKey: "test", HTTPClient: hc})
	for _, uri := range []string{"", "%", "https://example.com/file", "https://generativelanguage.googleapis.com.evil.example/file", "https://evil.generativelanguage.googleapis.com/file", "https://user:pass@generativelanguage.googleapis.com/file", "https://generativelanguage.googleapis.com/file#fragment", "http://generativelanguage.googleapis.com/file", "https://127.0.0.1/file", "file:///tmp/file"} {
		if _, err := c.DownloadFile(context.Background(), uri); err == nil {
			t.Fatalf("accepted %q", uri)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("key sent before URL validation")
	}
	data, err := c.DownloadFile(context.Background(), "https://generativelanguage.googleapis.com/v1beta/files/a:download?alt=media")
	if err != nil || string(data) != "file" || calls.Load() != 1 {
		t.Fatalf("download = %q, %v", data, err)
	}
	for _, config := range []ClientConfig{{AllowHTTP: true}, {AllowPrivateIPs: true}} {
		config.APIKey = "test"
		config.HTTPClient = hc
		partial := NewClient(config)
		if _, err := partial.DownloadFile(context.Background(), "http://127.0.0.1/file"); err == nil {
			t.Fatal("HTTP and private-IP opt-outs must be independent")
		}
	}
}
func TestClient_DownloadFile_RedirectCredentials(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("x-goog-api-key") != "")
		_, _ = io.WriteString(w, "mp4")
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test" {
			t.Error("initial request missing key")
		}
		http.Redirect(w, r, strings.Replace(target.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	}))
	defer source.Close()
	c := NewClient(ClientConfig{BaseURL: source.URL, APIKey: "test", AllowPrivateIPs: true, AllowHTTP: true})
	data, err := c.DownloadFile(context.Background(), source.URL)
	if err != nil || string(data) != "mp4" || leaked.Load() {
		t.Fatalf("download = %q, %v; leaked = %t", data, err, leaked.Load())
	}
}

func TestClient_DownloadFile_HostPinWithOptOuts(t *testing.T) {
	for _, flags := range []ClientConfig{{AllowHTTP: true}, {AllowPrivateIPs: true}, {AllowHTTP: true, AllowPrivateIPs: true}} {
		var sent atomic.Int32
		flags.APIKey = "test"
		flags.HTTPClient = &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
			sent.Add(1)
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("file")), Request: r}, nil
		})}
		c := NewClient(flags)
		for _, uri := range []string{"http://evil.example/file", "https://evil.example/file"} {
			if _, err := c.DownloadFile(context.Background(), uri); !errors.Is(err, llms.ErrInvalidParameters) {
				t.Fatalf("host pin lost: %v", err)
			}
		}
		if sent.Load() != 0 {
			t.Fatal("sent key to foreign host")
		}
		flags.BaseURL = "https://custom.example/v1beta"
		c = NewClient(flags)
		if _, err := c.DownloadFile(context.Background(), "https://custom.example/file"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.DownloadFile(context.Background(), "https://custom.example:8443/file"); err == nil {
			t.Fatal("accepted foreign port")
		}
	}
}
func TestGetFinishReason_MediaModeration(t *testing.T) {
	for _, reason := range []string{"IMAGE_SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII", "image_safety"} {
		if got := GetFinishReason(reason); got != "content_filter" {
			t.Fatalf("%s = %s", reason, got)
		}
	}
}
