package fal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

// fakeQueue simulates the fal queue: submit, status sequence, result, cancel
// and an asset host under /assets/. Zero-valued fields use success defaults.
type fakeQueue struct {
	t  *testing.T
	mu sync.Mutex

	submits       []submitRecord
	statusCalls   int
	assetRequests []http.Header

	submitStatus int
	submitBody   any
	// aliasURLs returns tracking URLs under /fal-ai/alias/ (fal's app-alias routing);
	// crossOrigin returns them on a foreign host and blankURLs omits them, so the
	// client must derive namespace routes.
	aliasURLs, crossOrigin, blankURLs bool

	statuses     []map[string]any
	statusCode   int
	statusBody   any
	result       any
	resultStatus int
	resultHeader map[string]string
	cancelStatus int
	cancelBody   any
	asset        []byte
	assetType    string
	assetStatus  int

	server *httptest.Server
}
type submitRecord struct {
	Path   string
	Body   map[string]any
	Header http.Header
}

func newFakeQueue(t *testing.T) *fakeQueue {
	t.Helper()
	f := &fakeQueue{t: t, asset: []byte("bytes"), assetType: "application/octet-stream"}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}
func (f *fakeQueue) writeJSON(w http.ResponseWriter, status int, v any) {
	// Compact bodies with no trailing newline, matching live fal responses.
	data, err := json.Marshal(v)
	if err != nil {
		f.t.Error(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func (f *fakeQueue) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		f.assetRequests = append(f.assetRequests, r.Header.Clone())
		if f.assetStatus != 0 {
			w.WriteHeader(f.assetStatus)
			return
		}
		w.Header().Set("Content-Type", f.assetType)
		_, _ = w.Write(f.asset)
		return
	}
	if r.Header.Get("Authorization") != "Key test-key" {
		f.t.Errorf("missing Authorization on %s %s", r.Method, r.URL.Path)
	}
	// Request routes live under the two-segment app namespace, like live fal;
	// anything deeper (the full model path) is a 405 there.
	if strings.Contains(r.URL.Path, "/requests/") {
		prefix := strings.SplitN(r.URL.Path, "/requests/", 2)[0]
		if strings.Count(strings.Trim(prefix, "/"), "/") != 1 {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}
	switch {
	case r.Method == http.MethodPost:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Error(err)
		}
		f.submits = append(f.submits, submitRecord{Path: r.URL.Path, Body: body, Header: r.Header.Clone()})
		if f.submitStatus != 0 {
			f.writeJSON(w, f.submitStatus, f.submitBody)
			return
		}
		prefix := f.server.URL + "/" + namespace(strings.Trim(r.URL.Path, "/"))
		if f.aliasURLs {
			prefix = f.server.URL + "/fal-ai/alias"
		}
		if f.crossOrigin {
			prefix = "https://evil.example.com/fal-ai/alias"
		}
		response := map[string]any{"request_id": "req-1", "status_url": prefix + "/requests/req-1/status", "response_url": prefix + "/requests/req-1", "cancel_url": prefix + "/requests/req-1/cancel", "queue_position": 3}
		if f.blankURLs {
			delete(response, "status_url")
			delete(response, "response_url")
			delete(response, "cancel_url")
		}
		f.writeJSON(w, http.StatusOK, response)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status"):
		if f.statusCode != 0 {
			f.writeJSON(w, f.statusCode, f.statusBody)
			return
		}
		i := f.statusCalls
		f.statusCalls++
		if i >= len(f.statuses) {
			i = len(f.statuses) - 1
		}
		if i < 0 {
			f.writeJSON(w, http.StatusOK, map[string]any{"status": "COMPLETED", "request_id": "req-1"})
			return
		}
		f.writeJSON(w, http.StatusOK, f.statuses[i])
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/cancel"):
		status, body := f.cancelStatus, f.cancelBody
		if status == 0 {
			status, body = http.StatusAccepted, map[string]any{"status": "CANCELLATION_REQUESTED"}
		}
		f.writeJSON(w, status, body)
	case r.Method == http.MethodGet:
		for k, v := range f.resultHeader {
			w.Header().Set(k, v)
		}
		if f.resultStatus != 0 {
			f.writeJSON(w, f.resultStatus, f.result)
			return
		}
		f.writeJSON(w, http.StatusOK, f.result)
	default:
		http.Error(w, "unexpected", http.StatusTeapot)
	}
}
func (f *fakeQueue) assetURL(name string) string { return f.server.URL + "/assets/" + name }
func (f *fakeQueue) client(opts ...Option) *Client {
	f.t.Helper()
	base := []Option{WithAPIKey("test-key"), WithBaseURL(f.server.URL), WithAllowHTTP(), WithAllowPrivateIPs(), WithPollPolicy(PollPolicy{Initial: time.Millisecond, Max: time.Millisecond})}
	c, err := New(append(base, opts...)...)
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}
func (f *fakeQueue) lastSubmit() submitRecord {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submits) == 0 {
		f.t.Fatal("no submit recorded")
	}
	return f.submits[len(f.submits)-1]
}

func TestDefaultOptions(t *testing.T) {
	o := defaultOptions()
	if o.BaseURL != "https://queue.fal.run" || o.ImageModel != "fal-ai/flux/schnell" || o.VideoModel != "fal-ai/minimax/hailuo-02/standard/text-to-video" || o.SpeechModel != "fal-ai/kokoro/american-english" || o.TranscriptionModel != "fal-ai/whisper" || o.Timeout != 120*time.Second || o.AllowHTTP || o.AllowPrivateIPs || o.QueuePriority != "" || o.PollPolicy != DefaultPollPolicy() {
		t.Fatalf("defaults: %+v", o)
	}
}
func TestApplyOptions(t *testing.T) {
	hc := &http.Client{}
	p := PollPolicy{Timeout: time.Minute}
	o := apply(WithAPIKey("key"), WithBaseURL("https://example.com"), WithHTTPClient(hc), WithTimeout(time.Second), WithImageModel("a/b"), WithVideoModel("c/d"), WithSpeechModel("e/f"), WithTranscriptionModel("g/h"), WithPollPolicy(p), WithQueuePriority("low"), WithAllowHTTP(), WithAllowPrivateIPs())
	if o.APIKey != "key" || o.BaseURL != "https://example.com" || o.HTTPClient != hc || o.Timeout != time.Second || o.ImageModel != "a/b" || o.VideoModel != "c/d" || o.SpeechModel != "e/f" || o.TranscriptionModel != "g/h" || o.PollPolicy != p || o.QueuePriority != "low" || !o.AllowHTTP || !o.AllowPrivateIPs {
		t.Fatalf("options: %+v", o)
	}
	c, err := New(WithAPIKey("key"), WithTimeout(0))
	if err != nil || c.options.Timeout != 120*time.Second {
		t.Fatalf("timeout fallback: %v", err)
	}
}
func TestNewClientMissingAPIKey(t *testing.T) {
	t.Setenv(llms.EnvFalAPIKey, "")
	t.Setenv(llms.EnvLLMAPIKey, "")
	if _, err := New(); !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Fatal(err)
	}
}
func TestNewClientWithEnvAPIKey(t *testing.T) {
	t.Setenv(llms.EnvFalAPIKey, "env-key")
	c, err := New()
	if err != nil || c.headers["Authorization"] != "Key env-key" {
		t.Fatalf("env key: %v %v", err, c)
	}
}
func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	t.Setenv(llms.EnvFalAPIKey, "")
	t.Setenv(llms.EnvLLMAPIKey, "generic")
	c, err := New()
	if err != nil || c.headers["Authorization"] != "Key generic" {
		t.Fatalf("fallback: %v", err)
	}
}
func TestNewClientValidation(t *testing.T) {
	cases := map[string][]Option{
		"bad url":        {WithBaseURL("://bad")},
		"no host":        {WithBaseURL("https://")},
		"userinfo":       {WithBaseURL("https://user:pw@queue.fal.run")},
		"query":          {WithBaseURL("https://queue.fal.run?x=1")},
		"scheme":         {WithBaseURL("ftp://queue.fal.run")},
		"priority":       {WithQueuePriority("urgent")},
		"empty model":    {WithImageModel("")},
		"slash model":    {WithVideoModel("/fal-ai/x")},
		"dot model":      {WithSpeechModel("fal-ai/../x")},
		"space model":    {WithTranscriptionModel("fal ai/x")},
		"double slash":   {WithImageModel("fal-ai//x")},
		"trailing slash": {WithImageModel("fal-ai/x/")},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(append([]Option{WithAPIKey("k")}, opts...)...); !errors.Is(err, llms.ErrInvalidParameters) {
				t.Fatal(err)
			}
		})
	}
	for _, priority := range []string{"", "normal", "low"} {
		if _, err := New(WithAPIKey("k"), WithQueuePriority(priority)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestClientImplementsInterfaces(t *testing.T) {
	c, err := New(WithAPIKey("k"))
	if err != nil {
		t.Fatal(err)
	}
	if !llms.SupportsImageGeneration(c) || !llms.SupportsVideoGeneration(c) || !llms.SupportsSpeech(c) || !llms.SupportsTranscription(c) || llms.SupportsImageEdit(c) {
		t.Fatal("interface support")
	}
	if c.Provider() != llms.ProviderFal || c.Provider() != "fal" || c.Model() != DefaultImageModel {
		t.Fatalf("identity: %s %s", c.Provider(), c.Model())
	}
	caps := c.Capabilities()
	if !caps.ImageGeneration || !caps.VideoGeneration || !caps.Speech || !caps.Transcription || caps.Vision {
		t.Fatalf("capabilities: %+v", caps)
	}
}
func TestValidateRequestID(t *testing.T) {
	for _, id := range []string{"", "a/b", "a b", strings.Repeat("a", 257), "a?b"} {
		if err := validateRequestID(id); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Fatalf("%q accepted", id)
		}
	}
	if err := validateRequestID("3f2a-1b_C"); err != nil {
		t.Fatal(err)
	}
}
func TestMergeExtraReserved(t *testing.T) {
	body := map[string]any{"prompt": "x"}
	if err := mergeExtra(body, map[string]any{"prompt": "y"}, "prompt"); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
	if err := mergeExtra(body, map[string]any{"extra": 1}, "prompt"); err != nil || body["extra"] != 1 || body["prompt"] != "x" {
		t.Fatalf("merge: %v %v", err, body)
	}
}
func TestNumber(t *testing.T) {
	for _, v := range []any{1, int64(2), 3.0, float32(4), json.Number("5")} {
		if _, ok := number(v); !ok {
			t.Fatalf("%v rejected", v)
		}
	}
	if _, ok := number("1"); ok {
		t.Fatal("string accepted")
	}
	if _, ok := number(json.Number("x")); ok {
		t.Fatal("bad json number accepted")
	}
}
func TestBillableUnits(t *testing.T) {
	metadata := map[string]any{}
	billableUnits(http.Header{}, metadata)
	billableUnits(http.Header{"X-Fal-Billable-Units": {"nope"}}, metadata)
	billableUnits(http.Header{"X-Fal-Billable-Units": {"-1"}}, metadata)
	if len(metadata) != 0 {
		t.Fatalf("unexpected: %v", metadata)
	}
	billableUnits(http.Header{"X-Fal-Billable-Units": {"2.5"}}, metadata)
	if metadata["billable_units"] != 2.5 {
		t.Fatalf("units: %v", metadata)
	}
}
func TestParseFalError(t *testing.T) {
	list := parseFalError(`{"detail":[{"loc":["body","prompt"],"msg":"first","type":"value_error"},{"msg":"second","type":"other"}]}`)
	if list.message != "first; second" || list.errorType != "value_error" {
		t.Fatalf("list: %+v", list)
	}
	flat := parseFalError(`{"detail":"Unauthorized","error_type":"unauthorized"}`)
	if flat.message != "Unauthorized" || flat.errorType != "unauthorized" {
		t.Fatalf("flat: %+v", flat)
	}
	raw := parseFalError("gateway down")
	if raw.message != "gateway down" || raw.errorType != "" {
		t.Fatalf("raw: %+v", raw)
	}
	// Pretty-printed bodies reach the client with sanitizer-escaped control characters.
	pretty := parseFalError(`{\n\t"detail": "Unauthorized",\n\t"error_type": "unauthorized"\n}\r\n`)
	if pretty.message != "Unauthorized" || pretty.errorType != "unauthorized" {
		t.Fatalf("pretty: %+v", pretty)
	}
	empty := parseFalError(`{"detail":""}`)
	if empty.message != `{"detail":""}` {
		t.Fatalf("empty detail: %+v", empty)
	}
}
func TestWrapError(t *testing.T) {
	if wrapError("x", nil) != nil {
		t.Fatal("nil")
	}
	plain := wrapError("op", errors.New("boom"))
	if plain == nil || !strings.HasPrefix(plain.Error(), "fal: op: boom") {
		t.Fatal(plain)
	}
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{401, `{"detail":"Unauthorized","error_type":"unauthorized"}`, llms.ErrAuthenticationFailed},
		{403, `{"detail":"Forbidden"}`, llms.ErrAuthenticationFailed},
		{402, `{"detail":"Exhausted balance","error_type":"exhausted_balance"}`, llms.ErrQuotaExceeded},
		{404, `{"detail":"Not found"}`, llms.ErrModelNotFound},
		{429, `{"detail":"Too many requests","error_type":"rate_limited"}`, llms.ErrRateLimited},
		{400, `{"detail":"Bad request","error_type":"bad_request"}`, llms.ErrInvalidParameters},
		{422, `{"detail":[{"msg":"prompt required","type":"missing"}]}`, llms.ErrInvalidParameters},
		{422, `{"detail":[{"msg":"blank output","type":"no_media_generated"}]}`, llms.ErrIncompleteResponse},
		{422, `{"detail":[{"msg":"nsfw","type":"content_policy_violation"}]}`, llms.ErrContentFiltered},
		{500, `{"detail":"boom","error_type":"internal_server_error"}`, llms.ErrServiceUnavailable},
		{500, `{"detail":"upstream","error_type":"downstream_service_error"}`, llms.ErrServiceUnavailable},
		{503, `{"detail":"upstream","error_type":"downstream_service_unavailable"}`, llms.ErrServiceUnavailable},
		{502, `bad gateway`, llms.ErrServiceUnavailable},
		{504, `{"detail":"slow","error_type":"generation_timeout"}`, llms.ErrTimeout},
		{504, `{"detail":"slow","error_type":"request_timeout"}`, llms.ErrTimeout},
		{504, `{"detail":"slow","error_type":"startup_timeout"}`, llms.ErrTimeout},
		{504, `{"detail":"slow"}`, llms.ErrTimeout},
		{405, ``, llms.ErrInvalidParameters},
		{409, `{"detail":"conflict"}`, llms.ErrInvalidParameters},
		{413, `payload too large`, llms.ErrInvalidParameters},
	}
	for _, tc := range cases {
		api := &httpclient.APIError{StatusCode: tc.status, Message: tc.body}
		err := wrapError("op", api)
		if !errors.Is(err, tc.want) {
			t.Fatalf("status %d %s: %v", tc.status, tc.body, err)
		}
		var got *httpclient.APIError
		if !errors.As(err, &got) || got != api {
			t.Fatalf("APIError lost: %v", err)
		}
		if !strings.HasPrefix(err.Error(), "fal: op: ") {
			t.Fatal(err)
		}
	}
	moderated := wrapError("op", &httpclient.APIError{StatusCode: 422, Message: `{"detail":[{"msg":"nsfw prompt","type":"content_policy_violation"}]}`})
	var m *llms.ModerationError
	if !errors.As(moderated, &m) || m.Provider != "fal" || m.Stage != llms.ModerationInput || m.Charged || m.Reasons[0] != "nsfw prompt" {
		t.Fatalf("moderation: %v", moderated)
	}
	typed := wrapError("op", &httpclient.APIError{StatusCode: 422, Message: `{"detail":[{"msg":"too big","type":"value_error"}]}`})
	if !strings.Contains(typed.Error(), "value_error: too big") {
		t.Fatal(typed)
	}
	unknown := wrapError("op", &httpclient.APIError{StatusCode: 306, Message: "odd"})
	if !strings.Contains(unknown.Error(), "odd") || errors.Is(unknown, llms.ErrInvalidParameters) {
		t.Fatal(unknown)
	}
	empty := wrapError("status", &httpclient.APIError{StatusCode: 405, Message: ""})
	if !strings.HasPrefix(empty.Error(), "fal: status: invalid parameters") || strings.Contains(empty.Error(), ": : ") {
		t.Fatal(empty)
	}
	typeOnly := wrapError("op", &httpclient.APIError{StatusCode: 400, Message: `{"detail":"","error_type":"bad_request"}`})
	if !strings.HasPrefix(typeOnly.Error(), "fal: op: bad_request: invalid parameters") {
		t.Fatal(typeOnly)
	}
}
func TestJobStatus(t *testing.T) {
	queued := jobStatus(&statusResponse{Status: "IN_QUEUE"})
	if queued.State != llms.JobQueued || queued.Progress == nil || *queued.Progress != 0 {
		t.Fatalf("queued: %+v", queued)
	}
	if jobStatus(&statusResponse{Status: "IN_PROGRESS"}).State != llms.JobRunning {
		t.Fatal("running")
	}
	if s := jobStatus(&statusResponse{Status: "COMPLETED"}); s.State != llms.JobSucceeded || s.Err != nil {
		t.Fatalf("succeeded: %+v", s)
	}
	failed := jobStatus(&statusResponse{Status: "COMPLETED", Error: "boom", ErrorType: "internal_server_error"})
	if failed.State != llms.JobFailed || !errors.Is(failed.Err, llms.ErrJobFailed) || !errors.Is(failed.Err, llms.ErrServiceUnavailable) {
		t.Fatalf("failed: %+v", failed)
	}
	generic := jobStatus(&statusResponse{Status: "COMPLETED", Error: "boom"})
	if generic.State != llms.JobFailed || !errors.Is(generic.Err, llms.ErrJobFailed) {
		t.Fatalf("generic: %+v", generic)
	}
	moderated := jobStatus(&statusResponse{Status: "COMPLETED", Error: "nsfw", ErrorType: "content_policy_violation"})
	var m *llms.ModerationError
	if moderated.State != llms.JobModerated || !errors.As(moderated.Err, &m) || m.Reasons[0] != "nsfw" {
		t.Fatalf("moderated: %+v", moderated)
	}
	unknown := jobStatus(&statusResponse{Status: "WEIRD"})
	if unknown.State != llms.JobFailed || !errors.Is(unknown.Err, llms.ErrJobFailed) {
		t.Fatalf("unknown: %+v", unknown)
	}
}
func TestSubmitRoutesAndPriority(t *testing.T) {
	f := newFakeQueue(t)
	c := f.client(WithQueuePriority("low"))
	q, err := c.submit(context.Background(), "fal-ai/flux/schnell", map[string]any{"prompt": "x"})
	if err != nil {
		t.Fatal(err)
	}
	rec := f.lastSubmit()
	if rec.Path != "/fal-ai/flux/schnell" || rec.Header.Get("X-Fal-Queue-Priority") != "low" || rec.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("submit: %+v", rec)
	}
	if q.RequestID != "req-1" || q.StatusURL != f.server.URL+"/fal-ai/flux/requests/req-1/status" || q.ResponseURL != f.server.URL+"/fal-ai/flux/requests/req-1" || q.CancelURL != f.server.URL+"/fal-ai/flux/requests/req-1/cancel" {
		t.Fatalf("urls: %+v", q)
	}
	c = f.client()
	if _, err = c.submit(context.Background(), "fal-ai/flux/schnell", nil); err != nil {
		t.Fatal(err)
	}
	if f.lastSubmit().Header.Get("X-Fal-Queue-Priority") != "" {
		t.Fatal("priority header sent by default")
	}
	if _, err = c.submit(context.Background(), "bad model", nil); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}
func TestSubmitAliasAndCrossOriginURLs(t *testing.T) {
	f := newFakeQueue(t)
	f.aliasURLs = true
	q, err := f.client().submit(context.Background(), "fal-ai/flux/schnell", nil)
	if err != nil || q.StatusURL != f.server.URL+"/fal-ai/alias/requests/req-1/status" || q.CancelURL != f.server.URL+"/fal-ai/alias/requests/req-1/cancel" {
		t.Fatalf("alias: %v %+v", err, q)
	}
	f.aliasURLs, f.crossOrigin = false, true
	q, err = f.client().submit(context.Background(), "fal-ai/flux/schnell", nil)
	if err != nil || q.StatusURL != f.server.URL+"/fal-ai/flux/requests/req-1/status" || q.ResponseURL != f.server.URL+"/fal-ai/flux/requests/req-1" {
		t.Fatalf("cross-origin fallback: %v %+v", err, q)
	}
}
func TestNamespace(t *testing.T) {
	for model, want := range map[string]string{"fal-ai/flux/schnell": "fal-ai/flux", "fal-ai/minimax/hailuo-02/standard/text-to-video": "fal-ai/minimax", "fal-ai/whisper": "fal-ai/whisper", "solo": "solo"} {
		if got := namespace(model); got != want {
			t.Fatalf("%s: %s", model, got)
		}
	}
}
func TestDerivedNamespaceRoutes(t *testing.T) {
	f := newFakeQueue(t)
	f.blankURLs = true
	f.statuses = []map[string]any{{"status": "IN_PROGRESS"}, {"status": "COMPLETED"}}
	videoFixture(f)
	c := f.client()
	job, err := c.GenerateVideo(context.Background(), "wave")
	if err != nil {
		t.Fatal(err)
	}
	q := &queueRequest{Model: DefaultVideoModel, RequestID: "req-1", StatusURL: f.server.URL + "/fal-ai/minimax/requests/req-1/status", ResponseURL: f.server.URL + "/fal-ai/minimax/requests/req-1", CancelURL: f.server.URL + "/fal-ai/minimax/requests/req-1/cancel"}
	if out, e := job.Wait(context.Background()); e != nil || len(out.Videos) != 1 {
		t.Fatalf("derived routes: %v", e)
	}
	if err = job.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The full model path must be rejected by the queue, proving derivation matters.
	full := &queueRequest{Model: q.Model, RequestID: q.RequestID, StatusURL: f.server.URL + "/" + DefaultVideoModel + "/requests/req-1/status"}
	if _, err = c.status(context.Background(), full); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatalf("full-path route should 405: %v", err)
	}
	if _, err = c.status(context.Background(), q); err != nil {
		t.Fatalf("namespace route: %v", err)
	}
}
func TestSubmitErrors(t *testing.T) {
	f := newFakeQueue(t)
	f.submitStatus, f.submitBody = 401, map[string]any{"detail": "Unauthorized", "error_type": "unauthorized"}
	if _, err := f.client().submit(context.Background(), "fal-ai/x", nil); !errors.Is(err, llms.ErrAuthenticationFailed) || !strings.HasPrefix(err.Error(), "fal: submit: ") {
		t.Fatal(err)
	}
	f.submitStatus, f.submitBody = 200, map[string]any{"request_id": "bad/id"}
	if _, err := f.client().submit(context.Background(), "fal-ai/x", nil); !errors.Is(err, llms.ErrInvalidParameters) {
		t.Fatal(err)
	}
}
func TestCancel(t *testing.T) {
	f := newFakeQueue(t)
	c := f.client()
	q, err := c.submit(context.Background(), "fal-ai/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.cancel(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	f.cancelStatus, f.cancelBody = 400, map[string]any{"status": "ALREADY_COMPLETED"}
	err = c.cancel(context.Background(), q)
	if !errors.Is(err, llms.ErrInvalidParameters) || !strings.Contains(err.Error(), "already completed") {
		t.Fatal(err)
	}
	f.cancelStatus, f.cancelBody = 404, map[string]any{"status": "NOT_FOUND"}
	if err = c.cancel(context.Background(), q); !errors.Is(err, llms.ErrModelNotFound) {
		t.Fatal(err)
	}
	f.cancelStatus, f.cancelBody = 500, map[string]any{"detail": "boom"}
	if err = c.cancel(context.Background(), q); !errors.Is(err, llms.ErrServiceUnavailable) {
		t.Fatal(err)
	}
}
func TestAwait(t *testing.T) {
	f := newFakeQueue(t)
	f.statuses = []map[string]any{{"status": "IN_QUEUE", "queue_position": 2}, {"status": "IN_PROGRESS"}, {"status": "COMPLETED"}}
	c := f.client()
	q, err := c.submit(context.Background(), "fal-ai/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.await(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if f.statusCalls != 3 {
		t.Fatalf("status calls: %d", f.statusCalls)
	}
	f.statuses = []map[string]any{{"status": "COMPLETED", "error": "nsfw", "error_type": "content_policy_violation"}}
	var m *llms.ModerationError
	if err = c.await(context.Background(), q); !errors.As(err, &m) {
		t.Fatal(err)
	}
	f.statuses = []map[string]any{{"status": "COMPLETED", "error": "boom", "error_type": "internal_server_error"}}
	if err = c.await(context.Background(), q); !errors.Is(err, llms.ErrJobFailed) || strings.Count(err.Error(), "fal:") != 1 {
		t.Fatal(err)
	}
	f.statusCode, f.statusBody = 429, map[string]any{"detail": "slow down"}
	if err = c.await(context.Background(), q); !errors.Is(err, llms.ErrRateLimited) || !strings.HasPrefix(err.Error(), "fal: status: ") {
		t.Fatal(err)
	}
	f.statusCode = 0
	f.statuses = []map[string]any{{"status": "IN_PROGRESS"}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err = c.await(ctx, q); !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), "fal: poll: ") {
		t.Fatal(err)
	}
	c = f.client(WithPollPolicy(PollPolicy{Initial: time.Millisecond, Max: time.Millisecond, Timeout: 20 * time.Millisecond}))
	if err = c.await(context.Background(), q); !errors.Is(err, httpclient.ErrPollTimeout) {
		t.Fatal(err)
	}
}
func TestFetchAsset(t *testing.T) {
	f := newFakeQueue(t)
	f.asset, f.assetType = []byte("pixels"), "image/png"
	c := f.client()
	asset, err := c.fetchAsset(context.Background(), f.assetURL("a.png"), "")
	if err != nil || string(asset.Data) != "pixels" || asset.MIMEType != "image/png" || asset.URL != f.assetURL("a.png") || !asset.ExpiresAt.IsZero() {
		t.Fatalf("asset: %v %+v", err, asset)
	}
	asset, err = c.fetchAsset(context.Background(), f.assetURL("a.png"), "image/jpeg")
	if err != nil || asset.MIMEType != "image/jpeg" {
		t.Fatalf("declared type: %v %+v", err, asset)
	}
	f.mu.Lock()
	header := f.assetRequests[0]
	f.mu.Unlock()
	if header.Get("Authorization") != "" || header.Get("User-Agent") != "llm-go-sdk/6" {
		t.Fatalf("asset headers leaked credentials: %v", header)
	}
	if _, err = c.fetchAsset(context.Background(), "", ""); !errors.Is(err, llms.ErrIncompleteResponse) {
		t.Fatal(err)
	}
	f.assetStatus = 404
	if _, err = c.fetchAsset(context.Background(), f.assetURL("missing"), ""); !errors.Is(err, llms.ErrModelNotFound) || !strings.HasPrefix(err.Error(), "fal: asset download: ") {
		t.Fatal(err)
	}
}
func TestFetchAssetSSRF(t *testing.T) {
	f := newFakeQueue(t)
	c, err := New(WithAPIKey("test-key"), WithBaseURL(f.server.URL), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.fetchAsset(context.Background(), f.assetURL("a"), ""); err == nil {
		t.Fatal("private asset host allowed without opt-in")
	}
	c, err = New(WithAPIKey("test-key"), WithBaseURL(f.server.URL), WithAllowPrivateIPs())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.fetchAsset(context.Background(), f.assetURL("a"), ""); err == nil {
		t.Fatal("plain HTTP asset allowed without opt-in")
	}
}
func TestRequestDecodeError(t *testing.T) {
	f := newFakeQueue(t)
	f.submitStatus, f.submitBody = 200, "not-an-object"
	if _, err := f.client().submit(context.Background(), "fal-ai/x", nil); err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatal(err)
	}
}
