package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// Live /ai/run bodies, 2026-11-14.
const (
	liveClefOmni    = `{"result":{"model":"clef-omni","answers":{"urgent":{"type":"noul","noul":0.9838},"dept":{"type":"choice","choice":"billing","probabilities":{"billing":0.8699,"tech":0.1301},"confidence":0.5472},"sev":{"type":"score","score":0.986,"legend":{"0":"low","1":"high"},"probabilities":{"0":0.014,"1":0.986},"confidence":0.9448}},"usage":{"input_tokens":261,"output_tokens":0}},"success":true,"errors":[],"messages":[]}`
	liveClairic     = `{"result":{"model":"clairic","answers":{"invoice":{"type":"text","p_null":0.026821065694093704,"span":[8,13],"guess":"#4471","confidence":0.9979984164237976,"value":"#4471"},"due":{"type":"date","p_null":0.0000064624268816260155,"guess":"2026-04-01","confidence":0.9989756692256798,"value":"2026-04-01"},"total":{"type":"number","p_null":0.000022426624127547257,"guess":1250,"confidence":0.9999128652167617,"value":1250},"units":{"type":"estimate","value":3,"mean":2.841173711894767,"median":3,"p10":3,"p90":3,"p_below":6.887772769914591e-7,"p_above":2.3889106159913354e-7}},"truncated":false,"usage":{"state_tokens":39,"image_tokens":0,"schema_tokens":334,"input_tokens":412,"output_tokens":0,"passes":2}},"success":true,"errors":[],"messages":[]}`
	liveClairicNull = `{"result":{"model":"clairic","answers":{"invoice":{"type":"text","p_null":0.9999992847442627,"span":[45,55],"guess":"next steps","confidence":0.40845373272895813,"value":null}},"truncated":false,"state_text":"The meeting went well","usage":{"state_tokens":11,"image_tokens":0,"schema_tokens":232,"input_tokens":254,"output_tokens":0,"passes":2}},"success":true,"errors":[],"messages":[]}`
	liveClairicList = `{"result":{"model":"clairic","answers":{"prices":{"type":"list","value":[4.5],"confidences":[0.9994454127657796],"confidence":0.9947146674565832}},"truncated":false,"usage":{"state_tokens":20,"image_tokens":0,"schema_tokens":100,"input_tokens":140,"output_tokens":0,"passes":1}},"success":true,"errors":[],"messages":[]}`
)

const secretState = "Checkout for account 4471 has failed for an hour."

func clefRequest() llms.DecisionRequest {
	return llms.DecisionRequest{
		State: secretState,
		Questions: map[string]llms.DecisionQuestion{
			"urgent": {Kind: llms.DecisionKindNoul, Instructions: "Urgent?"},
			"dept":   {Kind: llms.DecisionKindChoice, Instructions: "Team?", Choices: []llms.DecisionChoice{{Label: "tech", Guidance: "bugs"}, {Label: "billing"}}},
			"sev":    {Kind: llms.DecisionKindScore, Instructions: "Severity", Levels: []string{"low", "high"}},
		},
	}
}

func TestDecide_SystemOneWire(t *testing.T) {
	var path, raw string
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		data, _ := io.ReadAll(r.Body)
		raw = string(data)
		_, _ = io.WriteString(w, liveClefOmni)
	})
	resp, err := c.Decide(context.Background(), clefRequest(), llms.WithDecisionModel("@cf/cloudflare/clef-omni"))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/client/v4/accounts/"+testAccount+"/ai/run/@cf/cloudflare/clef-omni" {
		t.Errorf("path %s", path)
	}
	// The model rides in the path; a body "model" with the @cf ID fails Workers AI validation.
	if strings.Contains(raw, `"model"`) {
		t.Errorf("body carries model: %s", raw)
	}
	if !strings.Contains(raw, `"criteria":{"tech":"bugs","billing":null}`) {
		t.Errorf("choice order/guidance: %s", raw)
	}
	if a := resp.Answers["urgent"]; a.Kind != llms.DecisionKindNoul || *a.Probability != 0.9838 {
		t.Errorf("noul %+v", a)
	}
	if a := resp.Answers["dept"]; a.Choice != "billing" || a.Probabilities["tech"] != 0.1301 {
		t.Errorf("choice %+v", a)
	}
	if a := resp.Answers["sev"]; *a.Score != 0.986 || a.Legend["1"] != "high" {
		t.Errorf("score %+v", a)
	}
	if resp.Model != "@cf/cloudflare/clef-omni" || resp.Metadata["reported_model"] != "clef-omni" || resp.Usage.PromptTokens != 261 || resp.Usage.Cost != nil {
		t.Errorf("model/usage %s %+v", resp.Model, resp.Usage)
	}
}

func TestDecide_DefaultModelAndImages(t *testing.T) {
	var path string
	var body struct {
		State  any      `json:"state"`
		Images []string `json:"images"`
	}
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, liveClefOmni)
	})
	req := llms.DecisionRequest{
		Questions: map[string]llms.DecisionQuestion{"red": {Kind: llms.DecisionKindNoul, Instructions: "Mostly red?"}},
		Assets: map[string]llms.MediaInput{
			"b": {Data: []byte{2}, MIMEType: "image/jpeg"},
			"a": {Data: []byte{1}, MIMEType: "image/png"},
		},
	}
	if _, err := c.Decide(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "/run/@cf/cloudflare/clef-flash") {
		t.Errorf("default model path %s", path)
	}
	// Images go in name order as data URIs; an absent state is sent as "".
	if len(body.Images) != 2 || body.Images[0] != "data:image/png;base64,AQ==" || body.Images[1] != "data:image/jpeg;base64,Ag==" || body.State != "" {
		t.Errorf("body %+v", body)
	}
}

func TestDecide_Rejections(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent for an invalid decision") })
	q := clefRequest().Questions
	img := llms.MediaInput{Data: []byte{1}, MIMEType: "image/png"}
	cases := map[string]struct {
		req  llms.DecisionRequest
		opts []llms.DecisionOption
		want error
	}{
		"unsupported model":   {req: clefRequest(), opts: []llms.DecisionOption{llms.WithDecisionModel("@cf/meta/llama-3.2-3b-instruct")}, want: llms.ErrDecisionNotSupported},
		"jev not routed":      {req: clefRequest(), opts: []llms.DecisionOption{llms.WithDecisionModel("typesafe/jev")}, want: llms.ErrDecisionNotSupported},
		"missing instruction": {req: llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindNoul}}}, want: llms.ErrInvalidParameters},
		"bad question id":     {req: llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{"a b": {Kind: llms.DecisionKindNoul, Instructions: "x"}}}, want: llms.ErrInvalidParameters},
		"all-invalid id":      {req: llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{"###": {Kind: llms.DecisionKindNoul, Instructions: "x"}}}, want: llms.ErrInvalidParameters},
		"long question id":    {req: llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{strings.Repeat("a", 101): {Kind: llms.DecisionKindNoul, Instructions: "x"}}}, want: llms.ErrInvalidParameters},
		"clairic kind":        {req: llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{"t": {Kind: llms.DecisionKindText, Instructions: "x"}}}, want: llms.ErrInvalidParameters},
		"box":                 {req: llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{"b": {Kind: llms.DecisionKindBox, Instructions: "x"}}}, want: llms.ErrInvalidParameters},
		"reference date":      {req: llms.DecisionRequest{State: "x", Questions: q, ReferenceDate: "2026-01-01"}, want: llms.ErrInvalidParameters},
		"session id":          {req: clefRequest(), opts: []llms.DecisionOption{llms.WithDecisionSessionID("s")}, want: llms.ErrInvalidParameters},
		"url asset":           {req: llms.DecisionRequest{State: "x", Questions: q, Assets: map[string]llms.MediaInput{"a": {URL: "https://example.com/a.png"}}}, want: llms.ErrInvalidParameters},
		"audio asset":         {req: llms.DecisionRequest{State: "x", Questions: q, Assets: map[string]llms.MediaInput{"a": {Data: []byte{1}, MIMEType: "audio/wav"}}}, want: llms.ErrInvalidParameters},
		"five images":         {req: llms.DecisionRequest{State: "x", Questions: q, Assets: map[string]llms.MediaInput{"a": img, "b": img, "c": img, "d": img, "e": img}}, want: llms.ErrInvalidParameters},
		"reserved extra":      {req: clefRequest(), opts: []llms.DecisionOption{llms.WithDecisionExtra(map[string]any{"questions": 1})}, want: llms.ErrInvalidParameters},
		"model extra":         {req: clefRequest(), opts: []llms.DecisionOption{llms.WithDecisionExtra(map[string]any{"model": "x"})}, want: llms.ErrInvalidParameters},
		"other wire key":      {req: clefRequest(), opts: []llms.DecisionOption{llms.WithDecisionExtra(map[string]any{"fields": 1})}, want: llms.ErrInvalidParameters},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := c.Decide(context.Background(), tc.req, tc.opts...)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if !strings.HasPrefix(err.Error(), "cloudflare: ") {
				t.Fatalf("missing provider prefix: %v", err)
			}
		})
	}
	tooMany := map[string]llms.DecisionQuestion{}
	for i := range 65 {
		tooMany[strings.Repeat("q", i+1)] = llms.DecisionQuestion{Kind: llms.DecisionKindNoul, Instructions: "x"}
	}
	if _, err := c.Decide(context.Background(), llms.DecisionRequest{State: "x", Questions: tooMany}); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatalf("65 questions: %v", err)
	}
}

func TestDecide_ErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"no route", 400, `{"success":false,"errors":[{"code":7000,"message":"No route for that URI"}],"messages":[],"result":null}`, llms.ErrModelNotFound},
		{"bad input", 400, `{"errors":[{"message":"AiError: Bad input: Error: required properties","code":5006}],"success":false,"result":{},"messages":[]}`, llms.ErrInvalidParameters},
		{"validation", 422, `{"errors":[{"message":"images.0: image must be an embedded base64 data URI","code":3030}],"success":false,"result":{},"messages":[]}`, llms.ErrInvalidParameters},
		{"balance", 402, `{"errors":[{"message":"Insufficient balance; add money to your gateway or use BYOK","code":2021}],"success":false,"result":{},"messages":[]}`, llms.ErrQuotaExceeded},
		{"unsuccessful 200", 200, `{"success":false,"errors":[{"code":5007,"message":"No such model"}],"result":null}`, llms.ErrModelNotFound},
		{"unsuccessful 200 bad input", 200, `{"success":false,"errors":[{"code":5006,"message":"Bad input"}],"result":{}}`, llms.ErrInvalidParameters},
		{"unsuccessful 200 balance", 200, `{"success":false,"errors":[{"code":2021,"message":"Insufficient balance"}],"result":{}}`, llms.ErrQuotaExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := c.Decide(context.Background(), clefRequest())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestDecide_UnsuccessfulEnvelopeStatus pins the StatusCode an HTTP 200 with
// success:false reports, and that it is not retryable.
func TestDecide_UnsuccessfulEnvelopeStatus(t *testing.T) {
	for code, want := range map[string]int{"5006": 400, "7000": 400, "2021": 402} {
		c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":`+code+`,"message":"x"}],"result":{}}`)
		})
		_, err := c.Decide(context.Background(), clefRequest())
		var apiErr *llms.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != want || apiErr.IsRetryable() {
			t.Errorf("code %s: %+v (retryable %v)", code, apiErr, apiErr != nil && apiErr.IsRetryable())
		}
	}
}

func TestDecideErrorsRedactTokenAndAccountID(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"errors":[{"message":"Bad input","code":5006}],"success":false}`)
	})
	_, err := c.Decide(context.Background(), clefRequest())
	if err == nil {
		t.Fatal("expected error")
	}
	for _, leaked := range []string{"test-token", secretState, "4471"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("error leaks %q: %v", leaked, err)
		}
	}
	// The account ID is part of the request URL, which APIError reports for
	// debugging; it is an identifier, not a secret, but must not appear in the
	// message text itself.
	var apiErr *llms.APIError
	if errors.As(err, &apiErr) && strings.Contains(apiErr.Message, testAccount) {
		t.Fatalf("message leaks account ID: %q", apiErr.Message)
	}
}

func TestDecide_ContextCanceled(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent after cancel") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Decide(ctx, clefRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecide_MalformedResults(t *testing.T) {
	for name, body := range map[string]string{
		"no answers":   `{"result":{"model":"clef"},"success":true}`,
		"bad answer":   `{"result":{"answers":{"q":{"type":1}}},"success":true}`,
		"result array": `{"result":[1],"success":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			if _, err := c.Decide(context.Background(), clefRequest()); err == nil {
				t.Fatal("malformed result accepted")
			}
		})
	}
}

func TestClairicMapping(t *testing.T) {
	var raw string
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		raw = string(data)
		_, _ = io.WriteString(w, liveClairic)
	})
	two := 2
	lo, hi, step := 0.0, 10.0, 1.0
	resp, err := c.Decide(context.Background(), llms.DecisionRequest{
		State: "Invoice #4471 issued 2026-03-02, net 30, total $1,250.00 for 3 laptops.",
		Questions: map[string]llms.DecisionQuestion{
			"invoice": {Kind: llms.DecisionKindText, Instructions: "invoice number"},
			"due":     {Kind: llms.DecisionKindDate},
			"total":   {Kind: llms.DecisionKindNumber, Decimals: &two, Unit: "USD"},
			"units":   {Kind: llms.DecisionKindEstimate, Min: &lo, Max: &hi, Step: &step},
		},
		ReferenceDate: "2026-03-02",
	}, llms.WithDecisionModel("@cf/cloudflare/clairic"))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("FieldsKey", func(t *testing.T) {
		if !strings.Contains(raw, `"fields":{`) || strings.Contains(raw, `"questions"`) {
			t.Errorf("body %s", raw)
		}
	})
	t.Run("NumberSpec", func(t *testing.T) {
		if !strings.Contains(raw, `"total":{"decimals":2,"type":"number","unit":"USD"}`) {
			t.Errorf("body %s", raw)
		}
	})
	t.Run("EstimateSpec", func(t *testing.T) {
		if !strings.Contains(raw, `"units":{"max":10,"min":0,"step":1,"type":"estimate"}`) {
			t.Errorf("body %s", raw)
		}
	})
	t.Run("ReferenceDate", func(t *testing.T) {
		if !strings.Contains(raw, `"reference_date":"2026-03-02"`) {
			t.Errorf("body %s", raw)
		}
	})
	t.Run("Kind", func(t *testing.T) {
		if resp.Answers["due"].Kind != llms.DecisionKindDate || resp.Answers["units"].Kind != llms.DecisionKindEstimate {
			t.Errorf("%+v", resp.Answers)
		}
	})
	t.Run("Value", func(t *testing.T) {
		if resp.Answers["invoice"].Value != "#4471" || resp.Answers["due"].Value != "2026-04-01" || resp.Answers["total"].Value != 1250.0 {
			t.Errorf("%+v", resp.Answers)
		}
	})
	t.Run("Guess", func(t *testing.T) {
		if resp.Answers["total"].Guess != 1250.0 {
			t.Errorf("%+v", resp.Answers["total"])
		}
	})
	t.Run("PNull", func(t *testing.T) {
		if p := resp.Answers["invoice"].PNull; p == nil || *p != 0.026821065694093704 {
			t.Errorf("%v", p)
		}
	})
	t.Run("Span", func(t *testing.T) {
		if s := resp.Answers["invoice"].Span; s == nil || *s != (llms.DecisionSpan{Start: 8, End: 13}) {
			t.Errorf("%v", s)
		}
	})
	t.Run("Confidence", func(t *testing.T) {
		if c := resp.Answers["due"].Confidence; c == nil || *c != 0.9989756692256798 {
			t.Errorf("%v", c)
		}
	})
	t.Run("EstimateValue", func(t *testing.T) {
		if resp.Answers["units"].Value != 3.0 {
			t.Errorf("%v", resp.Answers["units"].Value)
		}
	})
	t.Run("Estimate", func(t *testing.T) {
		e := resp.Answers["units"].Estimate
		if e == nil || *e.Median != 3 || *e.Mean != 2.841173711894767 || *e.PBelow != 6.887772769914591e-7 || *e.PAbove != 2.3889106159913354e-7 || *e.P10 != 3 || *e.P90 != 3 {
			t.Errorf("%+v", e)
		}
	})
	t.Run("Truncated", func(t *testing.T) {
		if resp.Truncated {
			t.Error("truncated")
		}
	})
	t.Run("Breakdown", func(t *testing.T) {
		if resp.Breakdown["state_tokens"] != 39 || resp.Breakdown["schema_tokens"] != 334 || resp.Breakdown["image_tokens"] != 0 || len(resp.Breakdown) != 3 {
			t.Errorf("%v", resp.Breakdown)
		}
	})
	t.Run("Passes", func(t *testing.T) {
		if resp.Passes != 2 {
			t.Errorf("%d", resp.Passes)
		}
	})
	t.Run("Usage", func(t *testing.T) {
		// input_tokens already counts both passes: 2 × 39 + 334 = 412.
		if resp.Usage.PromptTokens != 412 || resp.Usage.CompletionTokens != 0 {
			t.Errorf("%+v", resp.Usage)
		}
	})
	for name, a := range resp.Answers {
		if a.Null || len(a.Raw) == 0 {
			t.Errorf("%s: %+v", name, a)
		}
	}
}

func TestClairicMapping_Null(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, liveClairicNull) })
	resp, err := c.Decide(context.Background(), llms.DecisionRequest{State: "The meeting went well.", Questions: map[string]llms.DecisionQuestion{"invoice": {Kind: llms.DecisionKindText}}}, llms.WithDecisionModel("@cf/cloudflare/clairic"))
	if err != nil {
		t.Fatal(err)
	}
	a := resp.Answers["invoice"]
	if !a.Null || a.Value != nil || a.Guess != "next steps" || a.Span == nil || a.PNull == nil {
		t.Fatalf("null answer %+v", a)
	}
	if resp.Metadata["state_text"] != "The meeting went well" {
		t.Fatalf("state_text %v", resp.Metadata)
	}
}

func TestClairicMapping_List(t *testing.T) {
	var raw string
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		raw = string(data)
		_, _ = io.WriteString(w, liveClairicList)
	})
	resp, err := c.Decide(context.Background(), llms.DecisionRequest{State: "2 lattes at €4.50", Questions: map[string]llms.DecisionQuestion{"prices": {Kind: llms.DecisionKindList, Items: llms.DecisionKindNumber}}}, llms.WithDecisionModel("@cf/cloudflare/clairic"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"items":"number"`) {
		t.Errorf("body %s", raw)
	}
	a := resp.Answers["prices"]
	if v, ok := a.Value.([]any); !ok || len(v) != 1 || v[0] != 4.5 || len(a.Confidences) != 1 {
		t.Fatalf("list %+v", a)
	}
}

func TestClairic_Rejections(t *testing.T) {
	c := mockClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent for an invalid Clairic field") })
	two, one := 2, 1.0
	for name, req := range map[string]llms.DecisionRequest{
		"noul kind":        {State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindNoul}}},
		"object state":     {State: map[string]any{"a": 1}, Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindText}}},
		"list bad items":   {State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindList, Items: llms.DecisionKindEstimate}}},
		"list decimals":    {State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindList, Items: llms.DecisionKindNumber, Decimals: &two}}},
		"items on text":    {State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindText, Items: llms.DecisionKindText}}},
		"unit on date":     {State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindDate, Unit: "d"}}},
		"min on number":    {State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindNumber, Min: &one}}},
		"choices on text":  {State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindText, Choices: []llms.DecisionChoice{{Label: "a"}}}}},
		"guidance on text": {State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindText, TrueGuidance: "a"}}},
		"reserved extra":   {State: "x", Questions: map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindText, Extra: map[string]any{"type": "number"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := c.Decide(context.Background(), req, llms.WithDecisionModel("@cf/cloudflare/clairic")); !errors.Is(err, llms.ErrInvalidParameters) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAdapterTableConcurrentRead(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "clairic") {
			_, _ = io.WriteString(w, liveClairic)
			return
		}
		_, _ = io.WriteString(w, liveClefOmni)
	})
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			model := "@cf/cloudflare/clef-omni"
			req := clefRequest()
			if i%2 == 0 {
				model = "@cf/cloudflare/clairic"
				req = llms.DecisionRequest{State: "x", Questions: map[string]llms.DecisionQuestion{"total": {Kind: llms.DecisionKindNumber}}}
			}
			if _, err := c.Decide(context.Background(), req, llms.WithDecisionModel(model)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// TestClairicMapping_SpanIsRunes pins the span unit against a live Clairic
// answer (2026-11-14): state "Café order: 2 lattes at €4.50 and 1 croissant."
// returned span [14,20] for "lattes". By runes that is "lattes"; by bytes it
// would be " latte", since "é" is two bytes.
func TestClairicMapping_SpanIsRunes(t *testing.T) {
	const state = "Café order: 2 lattes at €4.50 and 1 croissant."
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"result":{"model":"clairic","answers":{"first":{"type":"text","p_null":0.0031,"span":[14,20],"guess":"lattes","confidence":0.9977,"value":"lattes"}},"truncated":false,"usage":{"state_tokens":15,"image_tokens":0,"schema_tokens":100,"input_tokens":115,"output_tokens":0,"passes":1}},"success":true,"errors":[]}`)
	})
	resp, err := c.Decide(context.Background(), llms.DecisionRequest{State: state, Questions: map[string]llms.DecisionQuestion{"first": {Kind: llms.DecisionKindText, Instructions: "the first item name"}}}, llms.WithDecisionModel("@cf/cloudflare/clairic"))
	if err != nil {
		t.Fatal(err)
	}
	span := resp.Answers["first"].Span
	if span == nil || *span != (llms.DecisionSpan{Start: 14, End: 20}) {
		t.Fatalf("span %+v", span)
	}
	if got := string([]rune(state)[span.Start:span.End]); got != resp.Answers["first"].Value {
		t.Fatalf("rune slice %q, value %v", got, resp.Answers["first"].Value)
	}
	if byBytes := state[span.Start:span.End]; byBytes == "lattes" {
		t.Fatal("byte and rune offsets coincide; the fixture no longer distinguishes them")
	}
}
