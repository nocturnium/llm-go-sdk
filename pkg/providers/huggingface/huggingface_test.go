package huggingface

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

func embeddingsServer(t *testing.T, gotPath, gotAuth *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.URL.Path
		*gotAuth = r.Header.Get("Authorization")
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		// Echo one embedding per input string.
		inputs, _ := req["input"].([]any)
		data := make([]map[string]any, len(inputs))
		for i := range inputs {
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": []float32{0.1, 0.2, 0.3}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  "bge",
			"data":   data,
			"usage":  map[string]any{"prompt_tokens": 4, "total_tokens": 4},
		})
	}))
}

func TestEmbed_OpenAICompatible(t *testing.T) {
	var gotPath, gotAuth string
	server := embeddingsServer(t, &gotPath, &gotAuth)
	defer server.Close()

	client, err := New(WithEndpoint(server.URL), WithAPIKey("hf_test"), WithAllowPrivateIPs(true), WithAllowHTTP(true))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotPath != "/v1/embeddings" {
		t.Errorf("path = %s, want /v1/embeddings", gotPath)
	}
	if gotAuth != "Bearer hf_test" {
		t.Errorf("auth = %q, want Bearer hf_test", gotAuth)
	}
	if len(resp.Embeddings) != 2 || len(resp.Embeddings[0].Vector) != 3 {
		t.Fatalf("embeddings = %+v", resp.Embeddings)
	}
	if resp.Usage.TotalTokens != 4 {
		t.Errorf("usage total = %d, want 4", resp.Usage.TotalTokens)
	}
}

func TestEmbedQueryAndDocuments(t *testing.T) {
	var p, a string
	server := embeddingsServer(t, &p, &a)
	defer server.Close()

	client, _ := New(WithEndpoint(server.URL), WithAPIKey("k"), WithAllowPrivateIPs(true), WithAllowHTTP(true))

	vec, err := client.EmbedQuery(context.Background(), "hi")
	if err != nil || len(vec) != 3 {
		t.Fatalf("EmbedQuery: vec=%v err=%v", vec, err)
	}
	vecs, err := client.EmbedDocuments(context.Background(), []string{"a", "b", "c"})
	if err != nil || len(vecs) != 3 {
		t.Fatalf("EmbedDocuments: n=%d err=%v", len(vecs), err)
	}
}

func TestNew_RequiresEndpoint(t *testing.T) {
	_, err := New(WithAPIKey("k"))
	if !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatalf("err = %v, want ErrInvalidParameters", err)
	}
}

func TestNew_RequiresToken(t *testing.T) {
	t.Setenv("HF_TOKEN", "")
	t.Setenv("HUGGINGFACE_API_KEY", "")
	if _, err := New(WithEndpoint("https://x.endpoints.huggingface.cloud")); err == nil {
		t.Fatal("expected error when no token is available")
	}
}

func TestNew_ResolvesTokenFromEnv(t *testing.T) {
	t.Setenv("HF_TOKEN", "hf_fromenv")
	if _, err := New(WithEndpoint("https://x.endpoints.huggingface.cloud")); err != nil {
		t.Fatalf("expected token resolved from HF_TOKEN: %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	c, _ := New(WithEndpoint("https://x"), WithAPIKey("k"))
	caps := c.Capabilities()
	// A HuggingFace Inference Endpoint serves either chat (TGI) or embeddings (TEI),
	// so the provider advertises both surfaces.
	if !caps.Embeddings || !caps.Streaming || !caps.Tools {
		t.Errorf("capabilities = %+v, want chat + embeddings", caps)
	}
	if c.Provider() != llms.ProviderHuggingFace {
		t.Errorf("provider = %q", c.Provider())
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://x.huggingface.cloud":     "https://x.huggingface.cloud/v1",
		"https://x.huggingface.cloud/":    "https://x.huggingface.cloud/v1",
		"https://x.huggingface.cloud/v1":  "https://x.huggingface.cloud/v1",
		"https://x.huggingface.cloud/v1/": "https://x.huggingface.cloud/v1",
	}
	for in, want := range cases {
		if got := normalizeEndpoint(in); got != want {
			t.Errorf("normalizeEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEmbed_ErrorWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	client, _ := New(WithEndpoint(server.URL), WithAPIKey("k"), WithAllowPrivateIPs(true), WithAllowHTTP(true))
	_, err := client.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "huggingface") {
		t.Fatalf("err = %v, want a huggingface-wrapped error", err)
	}
}

// TestGenerateContent_Chat verifies the TGI chat path: GenerateContent posts to
// the endpoint's OpenAI-compatible /v1/chat/completions route with the WithModel
// value and the bearer token, and parses the reply.
func TestGenerateContent_Chat(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel, _ = req["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-1",
			"object": "chat.completion",
			"model":  "tgi",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "hello from TGI"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 4, "total_tokens": 7},
		})
	}))
	defer server.Close()

	client, err := New(
		WithEndpoint(server.URL),
		WithAPIKey("hf_test"),
		WithModel("meta-llama/Llama-3.1-8B-Instruct"),
		WithAllowPrivateIPs(true), WithAllowHTTP(true),
	)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %s, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer hf_test" {
		t.Errorf("auth = %q, want Bearer hf_test", gotAuth)
	}
	if gotModel != "meta-llama/Llama-3.1-8B-Instruct" {
		t.Errorf("model = %q, want the WithModel value", gotModel)
	}
	if resp.Content != "hello from TGI" {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.Usage.TotalTokens != 7 {
		t.Errorf("usage total = %d, want 7", resp.Usage.TotalTokens)
	}
	if resp.FinishReason != llms.FinishReasonStop {
		t.Errorf("finishReason = %q, want stop", resp.FinishReason)
	}
}
