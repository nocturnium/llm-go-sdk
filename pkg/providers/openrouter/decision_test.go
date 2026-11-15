package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// liveDecisionResponse is a live typesafe/jev-1.14 response, 2026-11-14 (id shortened).
const liveDecisionResponse = `{"model":"typesafe/jev-1.14-20261014","answers":{"urgent":{"type":"noul","noul":0.96},"dept":{"type":"choice","choice":"billing","probabilities":{"billing":0.97,"tech":0.03},"confidence":0.94},"anger":{"type":"score","score":2,"legend":{"0":"calm","1":"annoyed","2":"furious"},"probabilities":{"0":0,"1":0,"2":1},"confidence":1}},"usage":{"input_tokens":389,"output_tokens":61,"cost":1.6338e-05,"is_byok":false,"cost_details":{"upstream_inference_cost":1.6338e-05}},"id":"gen-dec-1","provider":"TypeSafe"}`

// specAnnotationsResponse adds the annotations array from the OpenRouter API
// reference (url_citation items). It was not observed live: Jev returned none.
const specAnnotationsResponse = `{"model":"m","answers":{"q":{"type":"noul","noul":0.5}},"usage":{"input_tokens":1,"output_tokens":0},"annotations":[{"type":"url_citation","url_citation":{"url":"https://example.com"}}]}`

const secretState = "Help! payouts for account 4471 have failed for 3 days."

func decisionRequest() llms.DecisionRequest {
	return llms.DecisionRequest{
		State: secretState,
		Questions: map[string]llms.DecisionQuestion{
			"urgent": {Kind: llms.DecisionKindNoul, Instructions: "Urgent?"},
			"dept":   {Kind: llms.DecisionKindChoice, Choices: []llms.DecisionChoice{{Label: "tech", Guidance: "bugs"}, {Label: "billing", Guidance: "payments"}}},
			"anger":  {Kind: llms.DecisionKindScore, Levels: []string{"calm", "annoyed", "furious"}},
		},
	}
}

func TestDecide_LiveShape(t *testing.T) {
	var body map[string]json.RawMessage
	var raw string
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/alpha/decisions" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth header missing")
		}
		data, _ := io.ReadAll(r.Body)
		raw = string(data)
		_ = json.Unmarshal(data, &body)
		_, _ = io.WriteString(w, liveDecisionResponse)
	})
	resp, err := c.Decide(context.Background(), decisionRequest(), llms.WithDecisionSessionID("s1"), llms.WithDecisionUser("u1"), llms.WithDecisionExtra(map[string]any{"trace": map[string]any{"name": "t"}}))
	if err != nil {
		t.Fatal(err)
	}
	if string(body["model"]) != `"typesafe/jev-1.14"` || string(body["session_id"]) != `"s1"` || string(body["user"]) != `"u1"` || string(body["trace"]) != `{"name":"t"}` {
		t.Errorf("body: %s", raw)
	}
	if !strings.Contains(raw, `"criteria":{"tech":"bugs","billing":"payments"}`) {
		t.Errorf("choice order not kept: %s", raw)
	}
	if strings.Contains(raw, "temperature") || strings.Contains(raw, "max_tokens") {
		t.Errorf("sampling parameters sent: %s", raw)
	}
	if resp.Model != "typesafe/jev-1.14-20261014" {
		t.Errorf("model %q", resp.Model)
	}
	if a := resp.Answers["urgent"]; a.Kind != llms.DecisionKindNoul || *a.Probability != 0.96 {
		t.Errorf("noul %+v", a)
	}
	if a := resp.Answers["dept"]; a.Choice != "billing" || *a.Confidence != 0.94 {
		t.Errorf("choice %+v", a)
	}
	if a := resp.Answers["anger"]; *a.Score != 2 || a.Legend["2"] != "furious" {
		t.Errorf("score %+v", a)
	}
	u := resp.Usage
	if u.PromptTokens != 389 || u.CompletionTokens != 61 || u.Cost == nil || *u.Cost != 1.6338e-05 {
		t.Errorf("usage %+v", u)
	}
	if resp.Metadata["id"] != "gen-dec-1" || resp.Metadata["provider"] != "TypeSafe" || resp.Metadata["is_byok"] != false {
		t.Errorf("metadata %+v", resp.Metadata)
	}
	if _, ok := resp.Metadata["annotations"]; ok {
		t.Errorf("annotations reported though the response had none: %+v", resp.Metadata)
	}
}

func TestDecide_SpecAnnotations(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, specAnnotationsResponse) })
	resp, err := c.Decide(context.Background(), llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindNoul}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := resp.Metadata["annotations"].(json.RawMessage)
	if !ok || !strings.Contains(string(raw), "url_citation") {
		t.Fatalf("annotations %v", resp.Metadata["annotations"])
	}
}

func TestDecide_ModelPrecedence(t *testing.T) {
	var model string
	handler := func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		model = body.Model
		_, _ = io.WriteString(w, liveDecisionResponse)
	}
	c := mockClient(t, handler, WithDecisionModel("typesafe/jev-1.13"))
	if _, err := c.Decide(context.Background(), decisionRequest()); err != nil || model != "typesafe/jev-1.13" {
		t.Fatalf("client default: %q %v", model, err)
	}
	if _, err := c.Decide(context.Background(), decisionRequest(), llms.WithDecisionModel("cloudflare/clef")); err != nil || model != "cloudflare/clef" {
		t.Fatalf("per-call: %q %v", model, err)
	}
}

func TestDecide_RejectsBeforeNetwork(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent for an invalid decision") })
	base := decisionRequest()
	cases := map[string]struct {
		req  llms.DecisionRequest
		opts []llms.DecisionOption
	}{
		"assets":         {req: llms.DecisionRequest{State: "x", Questions: base.Questions, Assets: map[string]llms.MediaInput{"img": {Data: []byte{1}, MIMEType: "image/png"}}}},
		"reference date": {req: llms.DecisionRequest{State: "x", Questions: base.Questions, ReferenceDate: "2026-01-01"}},
		"box":            {req: llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{"b": {Kind: llms.DecisionKindBox, Instructions: "the cat"}}}},
		"clairic kind":   {req: llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{"t": {Kind: llms.DecisionKindText}}}},
		"no state":       {req: llms.DecisionRequest{Questions: base.Questions}},
		"reserved extra": {req: base, opts: []llms.DecisionOption{llms.WithDecisionExtra(map[string]any{"state": "y"})}},
		"long session":   {req: base, opts: []llms.DecisionOption{llms.WithDecisionSessionID(strings.Repeat("s", 257))}},
		"long user":      {req: base, opts: []llms.DecisionOption{llms.WithDecisionUser(strings.Repeat("u", 257))}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := c.Decide(context.Background(), tc.req, tc.opts...)
			if !errors.Is(err, llms.ErrInvalidParameters) {
				t.Fatalf("err = %v", err)
			}
			if !strings.HasPrefix(err.Error(), "openrouter: ") {
				t.Fatalf("missing provider prefix: %v", err)
			}
		})
	}
}

func TestDecide_ErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{400, `{"error":{"message":"Model typesafe/nope does not exist","code":400}}`, llms.ErrModelNotFound},
		{400, `{"error":{"message":"Invalid discriminator value","code":400}}`, llms.ErrInvalidParameters},
		{401, `{"error":{"message":"No auth credentials found","code":401}}`, llms.ErrAuthenticationFailed},
		{402, `{"error":{"message":"Insufficient credits","code":402}}`, llms.ErrQuotaExceeded},
		{413, `{"error":{"message":"Payload too large","code":413}}`, llms.ErrContextLengthExceeded},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := c.Decide(context.Background(), decisionRequest())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if errors.Is(err, llms.ErrPlanRequired) {
				t.Fatalf("402 on decisions must not read as ErrPlanRequired: %v", err)
			}
		})
	}
}

func TestDecideErrorsRedactKey(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad","code":400}}`)
	})
	_, err := c.Decide(context.Background(), decisionRequest())
	if err == nil {
		t.Fatal("expected error")
	}
	for _, leaked := range []string{"test-key", secretState, "4471"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("error leaks %q: %v", leaked, err)
		}
	}
}

func TestDecide_ContextCanceled(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent after cancel") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Decide(ctx, decisionRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecide_MalformedResponses(t *testing.T) {
	for name, body := range map[string]string{
		"no answers":   `{"model":"m","usage":{}}`,
		"bad answer":   `{"model":"m","answers":{"q":{"type":1}},"usage":{}}`,
		"not json":     `<html>`,
		"answers list": `{"model":"m","answers":[1],"usage":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			if _, err := c.Decide(context.Background(), decisionRequest()); err == nil {
				t.Fatal("malformed response accepted")
			}
		})
	}
}

func TestDecide_Capability(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) {})
	if !llms.HasDecision(c) || !llms.SupportsDecision(c) {
		t.Fatal("OpenRouter client does not report decision support")
	}
}

func TestSiblingBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://openrouter.ai/api/v1":  "https://openrouter.ai/api/alpha",
		"https://openrouter.ai/api/v1/": "https://openrouter.ai/api/alpha",
		"https://proxy.example/or":      "https://proxy.example/or/alpha",
	} {
		u, _ := url.Parse(in)
		if got := siblingBaseURL(u, "alpha").String(); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestConcurrentDecide(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, liveDecisionResponse) })
	req := decisionRequest()
	done := make(chan error, 8)
	for range 8 {
		go func() {
			resp, err := c.Decide(context.Background(), req)
			if err == nil && resp.Answers["dept"].Choice != "billing" {
				err = errors.New("wrong answer")
			}
			done <- err
		}()
	}
	for range 8 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}
