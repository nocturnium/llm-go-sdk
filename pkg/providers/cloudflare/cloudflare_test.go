package cloudflare

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

const testAccount = "0123456789abcdef0123456789abcdef"

// mockClient serves handler at a local base standing in for
// https://api.cloudflare.com/client/v4/accounts/{id}/ai.
func mockClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	options := []Option{WithAPIKey("test-token"), WithBaseURL(server.URL + "/client/v4/accounts/" + testAccount + "/ai"), WithAllowHTTP(), WithAllowPrivateIPs()}
	c, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDefaultOptions(t *testing.T) {
	o := defaultOptions()
	if o.Model != DefaultModel || o.EmbeddingModel != DefaultEmbeddingModel || o.DecisionModel != DefaultDecisionModel || o.Timeout != 120*time.Second || o.AllowHTTP || o.AllowPrivateIPs || o.BaseURL != "" {
		t.Fatalf("defaults: %+v", o)
	}
}

func TestApplyOptions(t *testing.T) {
	hc := &http.Client{}
	o := apply(WithAPIKey("k"), WithAccountID("a"), WithModel("m"), WithEmbeddingModel("e"), WithDecisionModel("d"), WithBaseURL("https://x"), WithHTTPClient(hc), WithTimeout(time.Second), WithAllowPrivateIPs(), WithAllowHTTP(), nil)
	if o.APIKey != "k" || o.AccountID != "a" || o.Model != "m" || o.EmbeddingModel != "e" || o.DecisionModel != "d" || o.BaseURL != "https://x" || o.HTTPClient != hc || o.Timeout != time.Second || !o.AllowPrivateIPs || !o.AllowHTTP {
		t.Fatalf("options: %+v", o)
	}
}

func TestNewClientMissingAPIKey(t *testing.T) {
	t.Setenv(llms.EnvCloudflareAPIToken, "")
	t.Setenv(llms.EnvLLMAPIKey, "")
	if _, err := New(WithAccountID(testAccount)); !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewClientMissingAccountID(t *testing.T) {
	t.Setenv(llms.EnvCloudflareAccountID, "")
	_, err := New(WithAPIKey("k"))
	if !errors.Is(err, llms.ErrInvalidParameters) || !strings.Contains(err.Error(), llms.EnvCloudflareAccountID) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewClientRejectsUnsafeAccountID(t *testing.T) {
	for _, id := range []string{"../../zones", "abc/def", "a b", "abc?x=1"} {
		if _, err := New(WithAPIKey("k"), WithAccountID(id)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("%q: err = %v", id, err)
		}
	}
}

func TestNewClientWithEnvAPIKey(t *testing.T) {
	t.Setenv(llms.EnvCloudflareAPIToken, "env-token")
	t.Setenv(llms.EnvCloudflareAccountID, testAccount)
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if c.options.APIKey != "env-token" || c.aiBase.String() != "https://api.cloudflare.com/client/v4/accounts/"+testAccount+"/ai" {
		t.Fatalf("key/base: %q %s", c.options.APIKey, c.aiBase)
	}
}

func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	t.Setenv(llms.EnvCloudflareAPIToken, "")
	t.Setenv(llms.EnvLLMAPIKey, "generic")
	c, err := New(WithAccountID(testAccount))
	if err != nil || c.options.APIKey != "generic" {
		t.Fatalf("fallback: %v", err)
	}
}

func TestNewClientInvalidBaseURL(t *testing.T) {
	for _, base := range []string{"ftp://x", "https://user@x", "https://x?q=1", "://"} {
		if _, err := New(WithAPIKey("k"), WithBaseURL(base)); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("%q: err = %v", base, err)
		}
	}
	if _, err := New(WithAPIKey("k"), WithAccountID(testAccount), WithTimeout(-1)); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Errorf("negative timeout: %v", err)
	}
	if _, err := New(WithAPIKey("k"), WithBaseURL("http://proxy.internal/ai")); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Errorf("http without WithAllowHTTP: %v", err)
	}
	if _, err := New(WithAPIKey("k"), WithBaseURL("http://proxy.internal/ai"), WithAllowHTTP()); err != nil {
		t.Errorf("http with WithAllowHTTP: %v", err)
	}
}

func TestClientImplementsInterface(t *testing.T) {
	var c any = &Client{}
	if _, ok := c.(llms.LLM); !ok {
		t.Fatal("not llms.LLM")
	}
	if _, ok := c.(llms.CapableProvider); !ok {
		t.Fatal("not CapableProvider")
	}
}

func TestClientImplementsModelLister(t *testing.T) {
	var c any = &Client{}
	if _, ok := c.(llms.ModelLister); !ok {
		t.Fatal("not ModelLister")
	}
}

func TestClientImplementsEmbedder(t *testing.T) {
	var c any = &Client{}
	if _, ok := c.(llms.Embedder); !ok {
		t.Fatal("not Embedder")
	}
}

func TestCapabilities(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) {})
	caps := c.Capabilities()
	if !caps.Decision || !caps.Streaming || !caps.Embeddings || caps.MaxContextTokens != 0 {
		t.Fatalf("caps %+v", caps)
	}
	if c.Provider() != llms.ProviderCloudflare || c.Model() != DefaultModel {
		t.Fatalf("provider/model %s %s", c.Provider(), c.Model())
	}
}

// TestChat_Route checks chat goes to {base}/v1/chat/completions with the token.
// The body is the live gpt-oss-120b response, 2026-11-14.
func TestChat_Route(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/v4/accounts/"+testAccount+"/ai/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"id":"id-1","object":"chat.completion","created":1,"model":"@cf/openai/gpt-oss-120b","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":73,"completion_tokens":62,"total_tokens":135,"prompt_tokens_details":{"cached_tokens":0},"neurons":6.55}}`)
	})
	text, err := llms.Call(context.Background(), c, "Reply with hello.")
	if err != nil || text != "hello" {
		t.Fatalf("chat: %q %v", text, err)
	}
}

func TestErrorCodeMapping(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		// Live bodies, 2026-11-14.
		{400, `{"errors":[{"message":"AiError: No such model: No such model @cf/nope/nope or task","code":5007}],"success":false,"result":{},"messages":[]}`, llms.ErrModelNotFound},
		{401, `{"result":null,"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"messages":[]}`, llms.ErrAuthenticationFailed},
		{402, `{"errors":[{"message":"Insufficient balance; add money to your gateway or use BYOK","code":2021}],"success":false,"result":{},"messages":[]}`, llms.ErrQuotaExceeded},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := llms.Call(context.Background(), c, "x")
			if !errors.Is(err, tc.want) {
				t.Fatalf("chat err = %v, want %v", err, tc.want)
			}
			if errors.Is(err, llms.ErrPlanRequired) {
				t.Fatalf("402 must not read as ErrPlanRequired: %v", err)
			}
		})
	}
}

func TestMapErrorCode(t *testing.T) {
	for _, tc := range []struct {
		status        int
		code, message string
		want          string
	}{
		{402, "2021", "Insufficient balance", "insufficient_quota"},
		{400, "2021", "", "insufficient_quota"},
		{400, "5007", "No such model", "model_not_found"},
		{400, "7000", "No route for that URI", "model_not_found"},
		{400, "7000", "Invalid request body: input", "7000"},
		{402, "", "", "insufficient_quota"},
		{402, "plan_required", "", "plan_required"},
		{400, "5006", "Bad input", "5006"},
	} {
		if got := mapErrorCode(tc.status, tc.code, tc.message); got != tc.want {
			t.Errorf("mapErrorCode(%d, %q, %q) = %q, want %q", tc.status, tc.code, tc.message, got, tc.want)
		}
	}
}

func TestRegistry(t *testing.T) {
	t.Setenv(llms.EnvCloudflareAccountID, "")
	c, err := llms.New("cloudflare", llms.Config{APIKey: "k", Model: "@cf/meta/llama-3.2-3b-instruct", Extra: map[string]string{llms.ExtraCloudflareAccountID: testAccount}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Model() != "@cf/meta/llama-3.2-3b-instruct" || c.Provider() != llms.ProviderCloudflare {
		t.Fatalf("registry client %s %s", c.Provider(), c.Model())
	}
}

func TestPricingKeys(t *testing.T) {
	// A decision response's Model and Usage price directly: 254 input tokens on
	// Clairic at $0.10/M, the rate on developers.cloudflare.com/workers-ai/platform/pricing
	// (read 2026-11-14), which also matches the catalog's own price property.
	cost, ok := llms.EstimateCost(llms.ProviderCloudflare, "@cf/cloudflare/clairic", llms.Usage{PromptTokens: 254})
	if !ok || cost < 0.0000253 || cost > 0.0000255 {
		t.Fatalf("clairic cost %v %v", cost, ok)
	}
	for _, model := range []string{DefaultModel, DefaultEmbeddingModel, DefaultDecisionModel, "@cf/cloudflare/clef", "@cf/cloudflare/clef-omni"} {
		if _, ok := llms.EstimateCost(llms.ProviderCloudflare, model, llms.Usage{PromptTokens: 1}); !ok {
			t.Errorf("%s has no rate card", model)
		}
	}
	for _, model := range []string{DefaultSpeechModel, DefaultTranscriptionModel} {
		if _, ok := llms.GetMediaRate(string(llms.ProviderCloudflare), model); !ok {
			t.Errorf("%s has no media rate", model)
		}
	}
}
