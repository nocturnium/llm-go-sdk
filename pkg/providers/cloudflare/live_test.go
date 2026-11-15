//go:build integration

package cloudflare

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
)

// Live Workers AI checks. They need CLOUDFLARE_API_TOKEN and
// CLOUDFLARE_ACCOUNT_ID and cost a few hundred neurons in total (inside the
// 10,000-neuron daily free allocation). Jev on Workers AI is not exercised: it
// bills through unified billing.

func liveClient(t *testing.T) *Client {
	t.Helper()
	testutil.RequireEnvAPIKey(t, llms.EnvCloudflareAPIToken)
	testutil.RequireEnvAPIKey(t, llms.EnvCloudflareAccountID)
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func liveCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func TestLiveCloudflare_Chat(t *testing.T) {
	c := liveClient(t)
	text, err := llms.Call(liveCtx(t), c, "Reply with the word hello.", llms.WithMaxTokens(256))
	if err != nil || text == "" {
		t.Fatalf("chat: %q %v", text, err)
	}
}

func TestLiveCloudflare_Stream(t *testing.T) {
	c := liveClient(t)
	stream, err := c.Stream(liveCtx(t), []llms.Message{{Role: llms.RoleUser, Content: "Say hi"}}, llms.WithModel("@cf/meta/llama-3.2-3b-instruct"), llms.WithMaxTokens(16))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := llms.CollectStream(stream)
	if err != nil {
		t.Fatal(err)
	}
	// Workers AI sends usage on every chunk; the final chunk carries the totals.
	if resp.Usage == nil || resp.Usage.PromptTokens == 0 || resp.Usage.CompletionTokens == 0 {
		t.Fatalf("stream usage %+v", resp.Usage)
	}
}

func TestLiveCloudflare_Embed(t *testing.T) {
	c := liveClient(t)
	resp, err := c.Embed(liveCtx(t), []string{"hello", "world"})
	if err != nil || len(resp.Embeddings) != 2 || len(resp.Embeddings[0].Vector) != 1024 {
		t.Fatalf("embed: %v %v", resp, err)
	}
}

func TestLiveCloudflare_DecideClef(t *testing.T) {
	c := liveClient(t)
	for _, model := range []string{"@cf/cloudflare/clef-flash", "@cf/cloudflare/clef", "@cf/cloudflare/clef-omni"} {
		resp, err := c.Decide(liveCtx(t), llms.DecisionRequest{
			State: "Checkout has been failing for every customer for an hour.",
			Questions: map[string]llms.DecisionQuestion{
				"urgent": {Kind: llms.DecisionKindNoul, Instructions: "Is this urgent?"},
				"team":   {Kind: llms.DecisionKindChoice, Instructions: "Which team?", Choices: []llms.DecisionChoice{{Label: "billing", Guidance: "payments"}, {Label: "tech"}}},
				"sev":    {Kind: llms.DecisionKindScore, Instructions: "Severity", Levels: []string{"low", "medium", "high"}},
			},
		}, llms.WithDecisionModel(model))
		if err != nil {
			t.Fatalf("%s: %v", model, err)
		}
		if p := resp.Answers["urgent"].Probability; p == nil || *p < 0 || *p > 1 {
			t.Errorf("%s: noul %v", model, p)
		}
		if resp.Answers["team"].Choice == "" || resp.Answers["sev"].Score == nil || resp.Usage.PromptTokens == 0 {
			t.Errorf("%s: %+v", model, resp)
		}
	}
}

func TestLiveCloudflare_DecideClairic(t *testing.T) {
	c := liveClient(t)
	resp, err := c.Decide(liveCtx(t), llms.DecisionRequest{
		State: "Invoice 4471 issued 2026-03-02, net 30, total $1,250.00.",
		Questions: map[string]llms.DecisionQuestion{
			"total": {Kind: llms.DecisionKindNumber, Instructions: "invoice total"},
			"due":   {Kind: llms.DecisionKindDate, Instructions: "payment due date"},
			"po":    {Kind: llms.DecisionKindText, Instructions: "purchase order number"},
		},
		ReferenceDate: "2026-03-02",
	}, llms.WithDecisionModel("@cf/cloudflare/clairic"))
	if err != nil {
		t.Fatal(err)
	}
	// Check the shapes the client maps, not the model's extraction quality.
	if _, ok := resp.Answers["total"].Value.(float64); !ok {
		t.Errorf("number answer not a number: %+v", resp.Answers["total"])
	}
	if _, ok := resp.Answers["due"].Value.(string); !ok {
		t.Errorf("date answer not a string: %+v", resp.Answers["due"])
	}
	if po := resp.Answers["po"]; po.Kind != llms.DecisionKindText || po.PNull == nil {
		t.Errorf("text answer: %+v", po)
	}
	if resp.Passes == 0 || resp.Breakdown["schema_tokens"] == 0 {
		t.Errorf("usage: passes=%d breakdown=%v", resp.Passes, resp.Breakdown)
	}
}

func TestLiveCloudflare_ListModels(t *testing.T) {
	c := liveClient(t)
	resp, err := c.ListModels(liveCtx(t), llms.WithModelTypes(llms.ModelTypeDecision))
	if err != nil {
		t.Fatal(err)
	}
	// Every model the client routes must be in the account catalog. The catalog
	// may list more decision models than the client supports.
	listed := map[string]llms.ModelInfo{}
	for _, m := range resp.Models {
		listed[m.ID] = m
	}
	for id := range decisionModels {
		m, ok := listed[id]
		if !ok {
			t.Errorf("%s is routed but not in the catalog", id)
			continue
		}
		if m.ContextLength == 0 {
			t.Errorf("%s: no context length from the catalog", id)
		}
	}
}

func TestLiveCloudflare_Errors(t *testing.T) {
	c := liveClient(t)
	_, err := llms.Call(liveCtx(t), c, "x", llms.WithModel("@cf/nope/nope"))
	if !errors.Is(err, llms.ErrModelNotFound) {
		t.Errorf("chat unknown model: %v", err)
	}
	bad, err := New(WithAPIKey("invalid-token"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := llms.Call(liveCtx(t), bad, "x"); !errors.Is(err, llms.ErrAuthenticationFailed) {
		t.Errorf("bad token: %v", err)
	}
}

func TestLiveCloudflare_Rerank(t *testing.T) {
	c := liveClient(t)
	resp, err := c.Rerank(liveCtx(t), "What is machine learning?", []string{"Paris is in France.", "Machine learning trains models from data."})
	if err != nil || len(resp.Results) != 2 || resp.Results[0].Index == resp.Results[1].Index {
		t.Fatalf("rerank: %+v %v", resp, err)
	}
}

func TestLiveCloudflare_MediaRoundTrip(t *testing.T) {
	c := liveClient(t)
	img, err := c.GenerateImage(liveCtx(t), "a red square", llms.WithImageExtra(map[string]any{"steps": 1}))
	if err != nil || len(img.Images) != 1 || img.Images[0].MIMEType != "image/jpeg" {
		t.Fatalf("image: %v", err)
	}
	speech, err := c.Synthesize(liveCtx(t), "Hello from Workers AI.")
	if errors.Is(err, llms.ErrServerError) {
		// Aura returned 500 (code 8008) intermittently on 2026-11-14 between passes.
		t.Skipf("provider-side error, not a client regression: %v", err)
	}
	if err != nil || len(speech.Audio.Data) == 0 || speech.Audio.MIMEType != "audio/mpeg" {
		t.Fatalf("speech: %v", err)
	}
	// Transcribe the synthesized audio back to text.
	tr, err := c.Transcribe(liveCtx(t), llms.MediaInput{Data: speech.Audio.Data, MIMEType: speech.Audio.MIMEType}, llms.WithTranscribeWordTimestamps(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(tr.Text), "hello") || len(tr.Words) == 0 || tr.DurationSeconds == 0 {
		t.Fatalf("transcript %+v", tr)
	}
}
