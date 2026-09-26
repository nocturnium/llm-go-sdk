package llmadk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openrouter"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/zai"
)

// Provider fixtures are recorded from live API calls (go test -tags=integration
// with LLMADK_RECORD=1) and replayed offline by TestFixtures, through the real
// provider clients, so the bridge is tested against what providers send rather
// than against what the tests believe they send. A fixture records only method,
// path, status, content type and response body; request headers, which carry
// the API key, are never written.

// fixture is one recorded scenario.
type fixture struct {
	Scenario   string     `json:"scenario"`
	Family     string     `json:"family"`
	Model      string     `json:"model"`
	RecordedAt string     `json:"recorded_at"`
	Exchanges  []exchange `json:"exchanges"`
}

type exchange struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

// family builds a provider client for live use (base == "") or for replay
// against a local server (base is that server's URL plus the provider's path
// prefix).
type family struct {
	name       string
	model      string
	pathPrefix string
	build      func(model, base string, hc *http.Client) (llms.LLM, error)
}

var families = map[string]family{
	"openai-chat": {name: "openai-chat", model: "gpt-5.4-mini", pathPrefix: "/v1",
		build: func(model, base string, hc *http.Client) (llms.LLM, error) {
			opts := []openai.Option{openai.WithAPIKey(key("OPENAI_API_KEY")), openai.WithModel(model), openai.WithHTTPClient(hc)}
			if base != "" {
				opts = append(opts, openai.WithBaseURL(base), openai.WithAllowPrivateIPs(), openai.WithAllowHTTP())
			}
			return openai.New(opts...)
		}},
	"openai-responses": {name: "openai-responses", model: "gpt-5.4-mini", pathPrefix: "/v1",
		build: func(model, base string, hc *http.Client) (llms.LLM, error) {
			opts := []openai.Option{openai.WithAPIKey(key("OPENAI_API_KEY")), openai.WithModel(model), openai.WithHTTPClient(hc), openai.WithResponsesAPI()}
			if base != "" {
				opts = append(opts, openai.WithBaseURL(base), openai.WithAllowPrivateIPs(), openai.WithAllowHTTP())
			}
			return openai.New(opts...)
		}},
	"anthropic": {name: "anthropic", model: "claude-haiku-4-5", pathPrefix: "/v1", build: buildAnthropic},
	"anthropic-sonnet-5": {name: "anthropic-sonnet-5", model: "claude-sonnet-5", pathPrefix: "/v1",
		build: buildAnthropic},
	"anthropic-adaptive": {name: "anthropic-adaptive", model: "claude-sonnet-4-6", pathPrefix: "/v1",
		build: buildAnthropic},
	"anthropic-always-on": {name: "anthropic-always-on", model: "claude-fable-5-1", pathPrefix: "/v1",
		build: buildAnthropic},
	"gemini": {name: "gemini", model: "gemini-3.5-flash-lite", pathPrefix: "/v1beta",
		build: func(model, base string, hc *http.Client) (llms.LLM, error) {
			opts := []gemini.Option{gemini.WithAPIKey(key("GEMINI_API_KEY")), gemini.WithModel(model), gemini.WithHTTPClient(hc)}
			if base != "" {
				opts = append(opts, gemini.WithBaseURL(base), gemini.WithAllowPrivateIPs(), gemini.WithAllowHTTP())
			}
			return gemini.New(opts...)
		}},
	"openrouter": {name: "openrouter", model: "openai/gpt-5.4-mini", pathPrefix: "/api/v1",
		build: func(model, base string, hc *http.Client) (llms.LLM, error) {
			opts := []openrouter.Option{openrouter.WithAPIKey(key("OPENROUTER_API_KEY")), openrouter.WithModel(model), openrouter.WithHTTPClient(hc)}
			if base != "" {
				opts = append(opts, openrouter.WithBaseURL(base), openrouter.WithAllowPrivateIPs(), openrouter.WithAllowHTTP())
			}
			return openrouter.New(opts...)
		}},
	// The GLM coding plan is served from the coding endpoint.
	"zai": {name: "zai", model: "glm-4.7", pathPrefix: "/api/coding/paas/v4",
		build: func(model, base string, hc *http.Client) (llms.LLM, error) {
			opts := []zai.Option{zai.WithAPIKey(key("ZAI_API_KEY", "ZAI_TOKEN")), zai.WithModel(model), zai.WithHTTPClient(hc), zai.WithUseCodingAPI()}
			if base != "" {
				opts = append(opts, zai.WithBaseURL(base), zai.WithAllowPrivateIPs(), zai.WithAllowHTTP())
			}
			return zai.New(opts...)
		}},
}

func buildAnthropic(model, base string, hc *http.Client) (llms.LLM, error) {
	opts := []anthropic.Option{anthropic.WithAPIKey(key("ANTHROPIC_API_KEY", "CLAUDE_API_KEY")), anthropic.WithModel(model), anthropic.WithHTTPClient(hc)}
	if base != "" {
		opts = append(opts, anthropic.WithBaseURL(base), anthropic.WithAllowPrivateIPs(), anthropic.WithAllowHTTP())
	}
	return anthropic.New(opts...)
}

// key returns the first set environment variable among names, or a
// placeholder so a client can be built for replay.
func key(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return "replay-key"
}

func fixturePath(scenario, fam string) string {
	return filepath.Join("testdata", "fixtures", fam, scenario+".json")
}

// replayServer serves a fixture's responses in order and checks each request
// the provider client sends against the wire rules of its family (see
// checkWireRequest). Request bodies are not compared with the recording: minted
// tool-call IDs differ on every run.
type replayServer struct {
	t      *testing.T
	fix    fixture
	mu     sync.Mutex
	next   int
	family string
}

func (s *replayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.next >= len(s.fix.Exchanges) {
		s.t.Errorf("replay: unexpected request %s %s after %d recorded exchanges", r.Method, r.URL.Path, len(s.fix.Exchanges))
		http.Error(w, "no more recorded exchanges", http.StatusGone)
		return
	}
	ex := s.fix.Exchanges[s.next]
	s.next++
	if r.Method != ex.Method || !strings.HasSuffix(r.URL.Path, ex.Path) {
		s.t.Errorf("replay: request %d is %s %s, recorded %s %s", s.next, r.Method, r.URL.Path, ex.Method, ex.Path)
	}
	checkWireRequest(s.t, s.family, body)
	w.Header().Set("Content-Type", ex.ContentType)
	w.WriteHeader(ex.Status)
	_, _ = io.WriteString(w, ex.Body)
}

// spy records the messages every call to the wrapped LLM was given, so a
// scenario can check the requests the bridge built, live and in replay alike.
type spy struct {
	llms.LLM
	mu    sync.Mutex
	calls [][]llms.Message
}

func (s *spy) record(messages []llms.Message) {
	s.mu.Lock()
	s.calls = append(s.calls, messages)
	s.mu.Unlock()
}

func (s *spy) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	s.record(messages)
	return s.LLM.GenerateContent(ctx, messages, options...)
}

func (s *spy) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	s.record(messages)
	return s.LLM.Stream(ctx, messages, options...)
}

func (s *spy) Unwrap() llms.LLM { return s.LLM }

func (s *spy) last() []llms.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return nil
	}
	return s.calls[len(s.calls)-1]
}

// scenario runs one conversation through ADK. clients returns the provider
// client for a family. A single-family scenario runs once per family it lists;
// a multi-family one (a provider switch) runs once with all of them.
type scenario struct {
	name     string
	families []string
	multi    bool
	run      func(t *testing.T, clients func(fam string) llms.LLM)
}

// runScenarios runs every scenario with clients from clientFor.
func runScenarios(t *testing.T, clientFor func(t *testing.T, sc scenario, fam string) llms.LLM) {
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			if sc.multi {
				t.Run(strings.Join(sc.families, "+"), func(t *testing.T) {
					sc.run(t, func(fam string) llms.LLM { return clientFor(t, sc, fam) })
				})
				return
			}
			for _, fam := range sc.families {
				t.Run(fam, func(t *testing.T) {
					sc.run(t, func(f string) llms.LLM { return clientFor(t, sc, f) })
				})
			}
		})
	}
}

// replayClient builds a family's client against a local server that replays
// the scenario's recorded fixture, skipping when none was recorded.
func replayClient(t *testing.T, sc scenario, fam string) llms.LLM {
	t.Helper()
	raw, err := os.ReadFile(fixturePath(sc.name, fam))
	if err != nil {
		t.Skipf("no fixture recorded for %s/%s", sc.name, fam)
	}
	var fix fixture
	if err := json.Unmarshal(raw, &fix); err != nil {
		t.Fatal(err)
	}
	rs := &replayServer{t: t, fix: fix, family: fam}
	server := httptest.NewServer(rs)
	t.Cleanup(func() {
		server.Close()
		rs.mu.Lock()
		defer rs.mu.Unlock()
		if !t.Failed() && rs.next != len(fix.Exchanges) {
			t.Errorf("replay %s/%s used %d of %d recorded exchanges", sc.name, fam, rs.next, len(fix.Exchanges))
		}
	})
	f := families[fam]
	llm, err := f.build(fix.Model, server.URL+f.pathPrefix, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return llm
}
