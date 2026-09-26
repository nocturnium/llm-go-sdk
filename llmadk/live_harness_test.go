//go:build integration

package llmadk

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func hasKey(names ...string) bool {
	return key(names...) != "replay-key"
}

// recorder is an http.RoundTripper that forwards to base and keeps every
// exchange.
type recorder struct {
	base      http.RoundTripper
	prefix    string
	mu        sync.Mutex
	exchanges []exchange
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	r.mu.Lock()
	r.exchanges = append(r.exchanges, exchange{
		Method:      req.Method,
		Path:        strings.TrimPrefix(req.URL.Path, r.prefix),
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        string(body),
	})
	r.mu.Unlock()
	return resp, nil
}

func (r *recorder) save(t *testing.T, scenario string, fam family, model string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	f := fixture{Scenario: scenario, Family: fam.name, Model: model, RecordedAt: time.Now().UTC().Format(time.RFC3339), Exchanges: r.exchanges}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := fixturePath(scenario, fam.name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("recorded %d exchanges to %s", len(f.Exchanges), path)
}

// familyKeys names the environment variables holding each family's API key.
var familyKeys = map[string][]string{
	"openai-chat":         {"OPENAI_API_KEY"},
	"openai-responses":    {"OPENAI_API_KEY"},
	"anthropic":           {"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
	"anthropic-adaptive":  {"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
	"anthropic-sonnet-5":  {"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
	"anthropic-always-on": {"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
	"gemini":              {"GEMINI_API_KEY"},
	"openrouter":          {"OPENROUTER_API_KEY"},
	"zai":                 {"ZAI_API_KEY", "ZAI_TOKEN"},
}

// liveClient builds a family's client against the real API, skipping without
// a key. With LLMADK_RECORD=1 a passing run is saved as the scenario's fixture.
func liveClient(t *testing.T, sc scenario, fam string) llms.LLM {
	t.Helper()
	if !hasKey(familyKeys[fam]...) {
		t.Skipf("%s: no API key in %v", fam, familyKeys[fam])
	}
	f := families[fam]
	rec := &recorder{base: http.DefaultTransport, prefix: f.pathPrefix}
	llm, err := f.build(f.model, "", &http.Client{Transport: rec, Timeout: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("LLMADK_RECORD") == "1" {
		t.Cleanup(func() {
			if !t.Failed() {
				rec.save(t, sc.name, f, f.model)
			}
		})
	}
	return llm
}
