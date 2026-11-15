# Decision models: API and providers (plan)

Status: approved and implemented 2026-11-14 (packets 1 to 11 done on branch feat/decision-core; packet 12, TypeSafe direct, deferred). Owner: Jordan Dziat. Target: next minor (v6.x), additive only. The implementation notes below record where live probes changed the plan.

## Implementation notes (2026-11-14)

Live probes during implementation changed these points. Where this section and
the rest of the plan disagree, this section wins.

- Kind constants are `DecisionKindNoul` ... `DecisionKindEstimate`. `DecisionChoice`,
  `DecisionBox` and `DecisionEstimate` are struct types, so the constants carry the
  `Kind` infix.
- Workers AI `/ai/run/{model}`: the model rides only in the path. A body `model`
  with the `@cf/...` ID fails validation (5006).
- Workers AI System One questions require `instructions` (5006 without them).
  Null or empty choice guidance is accepted.
- Images work on clef, clef-flash, clef-omni and Clairic as data-URI strings in an
  `images` array (object form is rejected with 3030; Clairic rejects raw base64).
  PNG, JPEG and WebP were each accepted live on clef-flash.
  Audio and video stay unsupported until probed.
- Code 7000 means both "No route for that URI" (unknown model) and "Invalid request
  body"; only the No-route message maps to `ErrModelNotFound`. 5007 is "No such
  model" on the chat route. 3030 arrives with HTTP 422.
- `typesafe/jev` is not in the Cloudflare adapter table: its `/ai/run` shape was
  never verified and the user skipped it there.
- Catalog paging works (`per_page`, `page`); `total_count` is wrong (334 reported,
  75 real). Paging stops on a page shorter than the server's `per_page`.
- Catalog context windows differ from the model pages: clef-flash is 24,576 in
  the catalog. `ListModels` reports the catalog value.
- Error codes are mapped in one place: `openaicompat.ClientConfig.ErrorCodeMapper`
  (backed by `httpclient.WithErrorCodeMapper`), used by chat, embeddings and
  `/ai/run`. `decisionwire.Classify(err)` then fills in what the status implies.
- OpenRouter: a live Jev call confirmed the System One shape and `usage.cost`
  (389 input tokens × $0.042/M = $0.0000163). An unknown model returns 400, not 404.
  No OpenRouter rate card was added: OpenRouter's reported cost outranks any card.
- OpenRouter `ListModels` queries `output_modalities=text,decisions`. Live, that
  returned 477 models: the same 436 text-output models the unfiltered call
  returned, plus 41 decision models.
- `LLM_DEBUG_REQUESTS` printed request bodies; `httpclient.Request.Sensitive` now
  redacts decision request and error bodies from that log.
- HTTP 524 is now a retryable `ErrTimeout` in both the error classifier and the
  transport retry list.
- Media (PR 4) is allowlisted to one live-verified model per interface:
  `bge-reranker-base`, `flux-1-schnell`, `aura-2-en`, `whisper-large-v3-turbo`.

## Goal

Add an optional `llms.Decider` capability for decision models (typed questions evaluated
against a state, returning typed, calibrated answers rather than text), following the
`SpeechSynthesizer`/`Transcriber` optional-interface pattern. Providers: OpenRouter
(TypeSafe Jev) and a new first-party Cloudflare Workers AI provider (chat, embeddings,
rerank, image, TTS, ASR, plus Clef, Clef-flash, Clef-omni, Clairic decision models).

## Verified facts (2026-11-14)

### OpenRouter (docs page; live-verified during implementation, see the notes above)

- `POST https://openrouter.ai/api/alpha/decisions`, bearer auth. Chat completions returns 400
  for these models, so `openaicompat` does not apply.
- Model IDs: `typesafe/jev-1.14` (pinned), `~typesafe/jev-latest` (alias), `typesafe/jev-1.13`.
- Request: `model`, `state` (string, object, or array), `questions` (map name to question),
  optional `assets`, `provider`, `session_id`, `trace`, `user`. Header `x-session-id` is
  overridden by body `session_id`.
- Question types: `noul` (optional `criteria` with `true`/`false` keys), `choice` (`criteria`
  map required), `score` (`criteria` array, >= 1), `box` (`instructions` required, Jev 1.14 only).
- Response: `id`, `model`, `provider`, `answers` (map), `annotations`, `usage` with
  `input_tokens`, `output_tokens`, `cost`, `cost_details`, `input_tokens_details.cached_tokens`,
  `is_byok`. An answer may be `type: refusal` with no value.
- Errors: 400, 401, 402, 403, 404, 413, 429, 500, 502, 503, 524, 529. Body `error.code`,
  `error.message`.
- No streaming, no async.

### Cloudflare Workers AI (live-called with the account token)

- REST: `POST https://api.cloudflare.com/client/v4/accounts/{account_id}/ai/run/{model}`
  with `Authorization: Bearer $CLOUDFLARE_API_TOKEN`. Every response is wrapped:
  `{"result": {...}, "success": bool, "errors": [{"code", "message"}], "messages": []}`.
- OpenAI-compatible chat and embeddings (docs): base URL
  `https://api.cloudflare.com/client/v4/accounts/{account_id}/ai/v1`, paths
  `/chat/completions` and `/embeddings`. `/responses` only for `@cf/openai/gpt-oss-120b` and
  `-20b`, non-streaming: out of scope.
- Catalog: `GET /ai/models/search` returned 75 models with `total_count` 334 and an empty
  page 2. Task counts: Text Generation 40, Text-to-Image 10, Text Embeddings 7, ASR 5, TTS 4,
  Text Classification 2 (one is `bge-reranker-base`), Image-to-Text 2, Translation 2,
  Multimodal Embeddings 1, Image Classification 1, Voice Activity Detection 1. Decision
  models are listed under Text Generation.
- Live probes, one call each (input shapes and outputs observed, not inferred):
  - `@cf/cloudflare/clef-flash`: 200. `{"model":"clef-flash","answers":{"is_urgent":{"type":"noul","noul":0.8483}},"usage":{"input_tokens":155,"output_tokens":0}}`.
  - `@cf/cloudflare/clef-omni` (noul, choice, score): 200. Answers:
    noul `{type, noul}`; choice `{type, choice, probabilities{label: p}, confidence}`;
    score `{type, score, legend{"0": label}, probabilities{"0": p}, confidence}`.
    `usage{input_tokens, output_tokens}`.
  - `@cf/cloudflare/clairic` (text, date, number, estimate, list, null): 200. Answers:
    text `{type, value, span[start,end], guess, p_null, confidence}`;
    date `{type, value, guess, p_null, confidence}`; number `{type, value, guess, p_null, confidence}`;
    estimate `{type, value, mean, median, p10, p90, p_below, p_above}`.
    Top level adds `truncated`. `usage{state_tokens, image_tokens, schema_tokens, input_tokens, output_tokens, passes}`;
    4 fields produced `passes: 2`.
  - `typesafe/jev` on Cloudflare: 402, code 2021 "Insufficient balance; add money to your
    gateway or use BYOK". Skipped by user decision; Jev is covered through OpenRouter.
  - `openai/decisions`: 400, code 7000 "No route for that URI", identical to a nonexistent
    model. Not routable on this account. Deferred, see Non-goals.
  - Bad question type on clef-flash: 400, code 5006 "AiError: Bad input: ...". Note the 400
    message quotes the request's schema path, not the state.
- Docs-only (no live call): Clef 27B and Clef-flash 9B, 64K context, 1 to 64 questions,
  question IDs up to 100 chars of `[A-Za-z0-9_.-]`. Clef-omni 30B MoE, text plus up to 4 images,
  4 audio clips (300 s), 2 videos (60 s), embedded only, $0.15/M input. Clairic (preview):
  up to 4 embedded images, 65,536 context, fields batched into passes of 8,192 tokens and
  each pass rebilled, $0.10/M input, `reference_date` for year-less dates, `list` field type
  with `items`. Clef is $0.24/M input on the Cloudflare pricing page (a third-party $0.09 figure was wrong).

## Unverified, each gated before the code that depends on it

| Item | Gate |
|---|---|
| TypeSafe direct API (`api.typesafe.ai/v1/systemone`, one secondary source) | Packet 12 does not start until a primary doc or live call confirms it |
| Jev per-question limits (255 options, 10 score levels, questions per request) | Validation in packet 2 enforces only limits the OpenRouter spec states; the rest are doc-comment notes |
| `jev-latest` target version | Tests pin `typesafe/jev-1.14`, never an alias |
| Clef-flash image support, Clef price | Resolved: PNG, JPEG and WebP accepted live on clef-flash; Clef $0.24/M from the Cloudflare pricing page |
| Clef-omni media item schema (`images`, `audio`, `videos`) | Images resolved: data-URI strings in `images`. Audio and video unprobed; non-image assets return `ErrInvalidParameters` |
| Cloudflare native `/ai/run` shapes for rerank, image, TTS, ASR | Resolved by schema reads and live probes for bge-reranker-base, flux-1-schnell, aura-2-en, whisper-large-v3-turbo; other models are not allowlisted |
| `/ai/models/search` pagination (`total_count` 334 vs 75 returned) | Resolved: `page` and `per_page` work; paging stops on a short page and ignores `total_count` |
| Cloudflare error code table beyond 2021, 5006, 7000 | Observed: 5007 no such model (chat), 3030 validation (422), 10000 auth (401), 8008 internal (500). Mapped in `mapErrorCode`; unknown codes fall back to HTTP status |
| Cloudflare per-model context length | Read from the search response `properties` when present, else leave `ContextLength` zero (AGENTS.md rule 2, no hardcoding) |

## Core design (`llms` root, PR 1)

Every shape below was seen on the wire (Clairic, clef-omni, clef-flash) or is quoted from the
OpenRouter spec (box, refusal, assets). Structs carry snake_case JSON tags for SDK serialisation only (matching `Usage`,
`response.go:99`). They are NOT the provider wire. Each adapter owns an explicit wire-to-core
mapping table (for example `type`→`Kind`, `noul`→`Probability`, `span:[a,b]`→`Span`), and
every row has a fixture test. Pointers mark fields where zero and absent must differ.

```go
// DecisionKind is the question or field type.
type DecisionKind string

const (
    DecisionKindNoul     DecisionKind = "noul"     // providers' "predicate" normalises here
    DecisionKindChoice   DecisionKind = "choice"
    DecisionKindScore    DecisionKind = "score"
    DecisionKindBox      DecisionKind = "box"      // image region, OpenRouter spec
    DecisionKindText     DecisionKind = "text"     // Clairic: exact span of State
    DecisionKindDate     DecisionKind = "date"
    DecisionKindNumber   DecisionKind = "number"
    DecisionKindList     DecisionKind = "list"     // Clairic only
    DecisionKindEstimate DecisionKind = "estimate"
)

// DecisionChoice is one option. Slice, not map: option order is visible to the
// model (position bias) and map iteration order is not controllable.
type DecisionChoice struct {
    Label    string `json:"label"`
    Guidance string `json:"guidance,omitempty"`
}

type DecisionQuestion struct {
    Kind         DecisionKind     `json:"kind"`
    Instructions string           `json:"instructions,omitempty"`
    Choices      []DecisionChoice `json:"choices,omitempty"`       // choice
    TrueGuidance  string          `json:"true_guidance,omitempty"`  // noul
    FalseGuidance string          `json:"false_guidance,omitempty"` // noul
    Levels       []string         `json:"levels,omitempty"`        // score, ordered low to high
    Items        DecisionKind     `json:"items,omitempty"`         // list: text, number or date only (live-probed 5006 otherwise). Decimals and Unit on a list question are rejected with ErrInvalidParameters until a number-list probe documents element config.
    Decimals     *int             `json:"decimals,omitempty"`      // number
    Unit         string           `json:"unit,omitempty"`          // number
    Min          *float64         `json:"min,omitempty"`           // estimate
    Max          *float64         `json:"max,omitempty"`           // estimate
    Step         *float64         `json:"step,omitempty"`          // estimate
    Extra        map[string]any   `json:"extra,omitempty"`         // provider passthrough, merged last
}

type DecisionRequest struct {
    State         any                          `json:"state"`          // string, object or array; sent verbatim, never logged
    Questions     map[string]DecisionQuestion  `json:"questions"`
    Assets        map[string]MediaInput        `json:"assets,omitempty"` // named; each must be referenced from State
    ReferenceDate string                       `json:"reference_date,omitempty"` // Clairic; other providers return ErrInvalidParameters if set
}

// DecisionSpan is a half-open range of RUNES (Unicode code points) into the State
// string. Verified: "Café" counts é as one position.
type DecisionSpan struct {
    Start int `json:"start"`
    End   int `json:"end"`
}

// DecisionBox is one bounding box, fractions of the image size. Field names are
// quoted from the OpenRouter spec DecisionsBoxAnswer; no live box answer observed yet.
type DecisionBox struct {
    Top        *float64 `json:"top"`
    Bottom     *float64 `json:"bottom"`
    Left       *float64 `json:"left"`
    Right      *float64 `json:"right"`
    Confidence *float64 `json:"confidence,omitempty"`
}

// All six are pointers: p_below and p_above can be legitimately 0. The estimate's
// top-level "value" lands in DecisionAnswer.Value, not here.
type DecisionEstimate struct {
    Mean   *float64 `json:"mean,omitempty"`
    Median *float64 `json:"median,omitempty"`
    P10    *float64 `json:"p10,omitempty"`
    P90    *float64 `json:"p90,omitempty"`
    PBelow *float64 `json:"p_below,omitempty"`
    PAbove *float64 `json:"p_above,omitempty"`
}

type DecisionAnswer struct {
    Kind          DecisionKind        `json:"kind"`
    Null          bool                `json:"null,omitempty"`          // Clairic null, or refusal. A null answer can still carry Span, Guess and PNull.
    Refused       bool                `json:"refused,omitempty"`       // OpenRouter type "refusal"; Null is also true, Value is nil
    Value         any                 `json:"value,omitempty"`         // text, date, number, list (value[])
    Guess         any                 `json:"guess,omitempty"`         // best-effort value when Null
    Choice        string              `json:"choice,omitempty"`
    Probability   *float64            `json:"probability,omitempty"`   // noul
    Score         *float64            `json:"score,omitempty"`         // fractional level index
    Legend        map[string]string   `json:"legend,omitempty"`        // score: level index to label
    Probabilities map[string]float64  `json:"probabilities,omitempty"`
    Confidence    *float64            `json:"confidence,omitempty"`
    Confidences   []float64           `json:"confidences,omitempty"`   // list: per element
    PNull         *float64            `json:"p_null,omitempty"`
    Span          *DecisionSpan       `json:"span,omitempty"`          // text
    Box           *DecisionBox        `json:"box,omitempty"`           // box
    Estimate      *DecisionEstimate   `json:"estimate,omitempty"`
    Raw           json.RawMessage     `json:"raw,omitempty"`           // provider answer object, always kept
}

type DecisionResponse struct {
    Model     string                    `json:"model"`
    Answers   map[string]DecisionAnswer `json:"answers"`
    Usage     Usage                     `json:"usage"`
    Truncated bool                      `json:"truncated,omitempty"`
    Passes    int                       `json:"passes,omitempty"`   // Clairic billing passes
    Breakdown map[string]int            `json:"breakdown,omitempty"` // Clairic state_tokens, image_tokens, schema_tokens (passes lives in Passes only)
    Metadata  map[string]any            `json:"metadata,omitempty"` // id, provider, annotations, is_byok; state_text is sensitive (see rules)
}

type Decider interface {
    Decide(ctx context.Context, req DecisionRequest, opts ...DecisionOption) (*DecisionResponse, error)
}
```

Rules attached to the types:

- `Assets` is the one media field. OpenRouter rejects any non-empty `Assets` with `ErrInvalidParameters`
  until packet 6 observes the in-text reference syntax (the spec documents none). Cloudflare
  takes `images` and `audio`/`videos` as embedded arrays built from `Assets`; `URL` and
  `FileID` sources return `ErrInvalidParameters` there.
- `ReferenceDate` and `Items` are validated per provider: unsupported values return
  `ErrInvalidParameters`, never silently dropped.
- `Box` answers are populated only when packet 6 observes one live; until then they arrive in `Raw`.
- `DecisionChoice` guidance for noul is `TrueGuidance`/`FalseGuidance`, not a choice list.
- `DecisionSpan` offsets are rune positions, so consumers slicing Go strings must convert.
- `Raw` is always populated, so fields the SDK does not model survive.
- Answer combinations: a populated answer never sets `Null`. Clairic `value: null` sets `Null` with
  `Guess`, `PNull` and `Span` still populated (live-observed 2026-11-14). OpenRouter refusal sets `Refused`; setting
  `Null` too is OUR mapping, not a wire fact (the spec shows `type` only), with `Value` and
  `Probability` nil.
  Each combination has a fixture in `TestDecisionAnswer_JSONRoundTrip`.
- Sensitive fields: `State`, `Metadata["state_text"]` and `Raw` are never logged, traced or
  placed in error strings. `TestDecideDoesNotLogState` runs the client with a capturing slog
  handler and asserts no state text appears.
- `Decide` is not added to `LLM`. Decision models are listed through `ModelLister` with
  `ModelTypeDecision`. No `ListDecisionModels`.
- `DecisionOptions`/`DecisionOption` with `WithDecisionModel`, `WithDecisionSessionID`,
  `WithDecisionUser`, `WithDecisionExtra`, `ApplyDecisionOptions`, following `speech.go`.
- `AsDecider`, `SupportsDecision`, `ErrDecisionNotSupported`, `HasDecision`.
- `Capabilities.Decision bool`, `ModelCapabilities.Decision bool`, `ModelTypeDecision` added
  in `ModelCapabilities.Types()` (`capabilities_registry.go:725` area) and copied in
  `ToCapabilities()`.
- `Usage.Cost` follows the existing convention: provider-reported USD when present
  (`pkg/openaicompat/media_converters.go:164`).
- SECURITY.md gains a line: decision `State`, `state_text` and `Raw` are sent or returned
  verbatim and never logged or placed in error strings.

Live fixtures that PR 1 round-trips (`TestDecisionAnswer_JSONRoundTrip`): clef-omni noul,
choice, score; clairic text (span), date, number, estimate, list; OpenRouter box and
refusal (spec examples; the box fixture's test comment says it is spec-sourced and that the
OpenRouter provider does not fill `Box` until packet 6).

## Provider error mapping

| Condition | Sentinel | Retryable |
|---|---|---|
| OpenRouter 402, Cloudflare 402 or code 2021 | `ErrQuotaExceeded` (overrides the default 402 to `ErrPlanRequired` in `errors.go:109`, with a comment saying why) | no |
| 413 | `ErrContextLengthExceeded` | no |
| 429 | `ErrRateLimited` | yes |
| 524 | `ErrTimeout` | yes |
| 529, 502, 503 | `ErrServiceUnavailable` | yes |
| 400 with unsupported question kind or media | `ErrInvalidParameters` | no |
| Cloudflare code 7000 | `ErrModelNotFound` (like 404: also fires for a mistyped base URL or account ID, so callers confirm with the message) | no |
| Cloudflare `success:false` with any other code | `APIError` by HTTP status | per status |

Scope of the 402 override: chat and embeddings go through `openaicompat`, so packet 9 adds
an optional `ErrorClassifier` hook to `openaicompat.ClientConfig` that the Cloudflare client
sets. Then the whole Cloudflare client returns `ErrQuotaExceeded` for 2021 and 402, with
no chat-only exception. The hook is additive and defaults to nil.

All errors wrapped `openrouter: ...` or `cloudflare: ...`. No token, account ID, or `State`
text in error strings, enforced by a test in packets 3 and 8.

## OpenRouter (`pkg/providers/openrouter/decision.go`, PR 2)

- Native HTTP through `internal/httpclient`, same as `video.go` and `transcription.go`.
  Doc comment tags the endpoint alpha with the spec date.
- Supported kinds in PR 2: noul, choice, score. `box` returns `ErrInvalidParameters` until
  packet 6 confirms the asset reference syntax (box needs an image, and `Assets` are rejected
  until then). Packet 2 tests both rejections. Other kinds return `ErrInvalidParameters`
  before any network call.
- `Assets` are validated (PNG/JPEG only, base64 without `data:` prefix, every asset named and
  referenced once). Any non-empty `Assets` returns `ErrInvalidParameters` until packet 6
  observes the in-text reference syntax, so packet 2 tests the rejection.
- `WithDecisionSessionID` sets body `session_id`. `Extra` keys are merged last.
- Wire-to-core mapping (fixture per row; rows marked spec are from the OpenRouter API
  reference, not a live call, and are confirmed or corrected in packet 6):

  | Wire | Core | Test | Source |
  |---|---|---|---|
  | request `questions{id}.type`, `instructions` | `Questions` key, `Kind`, `Instructions` | `TestOpenRouterMapping_Question` | spec |
  | request `criteria` (choice object, noul `true`/`false`, score array) | `Choices`, `TrueGuidance`/`FalseGuidance`, `Levels` | `TestOpenRouterMapping_Criteria` | spec |
  | request `session_id`, `user` | `WithDecisionSessionID`, `WithDecisionUser` | `TestOpenRouterMapping_Session` | spec |
  | request `assets` | `Assets` (rejected until packet 6) | `TestOpenRouterMapping_AssetsRejected` | spec |
  | answer `type` | `Kind` | `TestOpenRouterMapping_Kind` | spec |
  | answer `noul` | `Probability` | `TestOpenRouterMapping_Noul` | spec |
  | answer `choice`, `probabilities`, `confidence` | `Choice`, `Probabilities`, `Confidence` | `TestOpenRouterMapping_Choice` | spec |
  | answer `score`, `legend`, `probabilities`, `confidence` | `Score`, `Legend`, `Probabilities`, `Confidence` | `TestOpenRouterMapping_Score` | spec |
  | answer `box{top,bottom,left,right}`, `confidence` | kept in `Raw`; `Box` filled after packet 6 | `TestOpenRouterMapping_BoxInRaw` | spec |
  | answer `type:"refusal"` | `Refused`, `Null` (our mapping) | `TestOpenRouterMapping_Refusal` | spec |
  | `usage.input_tokens`, `output_tokens` | `Usage.PromptTokens`, `CompletionTokens` | `TestOpenRouterMapping_Usage` | spec |
  | `usage.input_tokens_details.cached_tokens` | `Usage.CacheReadTokens` | `TestOpenRouterMapping_CachedTokens` | spec |
  | `usage.cost` | `Usage.Cost` | `TestOpenRouterMapping_Cost` | spec |
  | `id`, `provider`, `annotations`, `usage.is_byok` | `Metadata` | `TestOpenRouterMapping_Metadata` | spec |
- Never sends `temperature` or `max_tokens`.
- Catalog: `ListModels` queries `GET /api/v1/models?output_modalities=text,decisions` and
  types decision models `ModelTypeDecision`; no cached fallback list.
- Pricing: no OpenRouter rate card is added. The live smoke confirmed `usage.cost`
  (389 tokens × $0.042/M), and OpenRouter's reported cost outranks any card.

## Cloudflare (`pkg/providers/cloudflare/`, PR 3 and PR 4)

- Options: `WithAPIKey`, `WithAccountID`, `WithModel`, `WithEmbeddingModel`, `WithBaseURL`
  (AI Gateway users), `WithTimeout`, `WithHTTPClient`, `WithAllowPrivateIPs`, `WithAllowHTTP`.
  Env: `EnvCloudflareAPIToken = "CLOUDFLARE_API_TOKEN"` and
  `EnvCloudflareAccountID = "CLOUDFLARE_ACCOUNT_ID"` in `apikey.go`; key resolution via
  `llms.RequireAPIKey("cloudflare", ..., EnvCloudflareAPIToken)` then `LLM_API_KEY`.
  Missing account ID wraps `ErrInvalidParameters`. Account ID is `url.PathEscape`d.
- Chat and embeddings: `openaicompat.BaseProvider` on the `/ai/v1` base URL. Default chat
  model `@cf/openai/gpt-oss-120b`, default embedding model `@cf/baai/bge-m3` (both present in
  the live catalog; named in the package doc).
- Decisions: one `Decide` with an explicit model-to-adapter table:
  `@cf/cloudflare/clef`, `clef-flash`, `clef-omni`, `typesafe/jev` to the System One adapter;
  `@cf/cloudflare/clairic` to the Clairic adapter. Unknown models return
  `ErrDecisionNotSupported`. Table is a package-level `map[string]adapter` read only after
  init (no writes), with a concurrent read test.
  - Wire-to-core mapping, System One (fixture per row in `TestSystemOneMapping`):

    | Wire | Core | Note |
    |---|---|---|
    | request `questions{id}.type` | `DecisionQuestion.Kind` | noul, choice, score |
    | request `criteria` (choice object) | `Choices []DecisionChoice` | order from the Go slice |
    | request `criteria` (noul `true`/`false`) | `TrueGuidance`, `FalseGuidance` | |
    | request `criteria` (score array) | `Levels` | |
    | answer `type` | `DecisionAnswer.Kind` | |
    | answer `noul` | `Probability` | |
    | answer `choice`, `probabilities`, `confidence` | `Choice`, `Probabilities`, `Confidence` | |
    | answer `score`, `legend`, `probabilities`, `confidence` | `Score`, `Legend`, `Probabilities`, `Confidence` | |
    | `usage.input_tokens`, `output_tokens` | `Usage` | |

    Media only for clef-omni after the item-schema probe.
  - Clairic adapter: `{model, state, fields{name:{type, instructions, decimals, unit, items, min, max, step}}, images, reference_date}`,
    kinds text, date, number, list, estimate. Wire-to-core mapping (fixture per row in `TestClairicMapping`):

    | Wire | Core | Test row |
    |---|---|---|
    | request `fields{name}` | `Questions` key | `TestClairicMapping_FieldsKey` |
    | request `fields{name}.type` | `DecisionQuestion.Kind` | `TestClairicMapping_FieldKind` |
    | request `instructions` | `Instructions` | `TestClairicMapping_Instructions` |
    | request `decimals`, `unit` | `Decimals`, `Unit` (rejected on list questions) | `TestClairicMapping_NumberSpec` |
    | request `items` | `Items` (text, number, date only) | `TestClairicMapping_Items` |
    | request `min`, `max`, `step` | `Min`, `Max`, `Step` | `TestClairicMapping_EstimateSpec` |
    | request `images` | `Assets` (embedded `Data` only) | `TestClairicMapping_Images` |
    | request `reference_date` | `ReferenceDate` | `TestClairicMapping_ReferenceDate` |
    | `type` | `Kind` | `TestClairicMapping_Kind` |
    | `value` (text, date, number) | `Value` | `TestClairicMapping_Value` |
    | `value` (list) and `confidences`, `confidence` | `Value`, `Confidences`, `Confidence` | `TestClairicMapping_List` |
    | `value` (estimate) | `Value` (top-level estimate value) | `TestClairicMapping_EstimateValue` |
    | `guess` | `Guess` | `TestClairicMapping_Guess` |
    | `value: null` | `Null=true`, `Value` nil; `Guess`, `PNull`, `Span` still set (live-observed) | `TestClairicMapping_Null` |
    | `p_null` | `PNull` | `TestClairicMapping_PNull` |
    | `span` `[start,end]` (runes) | `Span` as `DecisionSpan` | `TestClairicMapping_Span` |
    | `confidence` | `Confidence` | `TestClairicMapping_Confidence` |
    | estimate `mean`, `median`, `p10`, `p90`, `p_below`, `p_above` | `Estimate` | `TestClairicMapping_Estimate` |
    | `truncated` | `Truncated` | `TestClairicMapping_Truncated` |
    | `usage.state_tokens`, `image_tokens`, `schema_tokens` | `Breakdown` | `TestClairicMapping_Breakdown` |
    | `usage.passes` | `Passes` (the only home for the pass count) | `TestClairicMapping_Passes` |
    | `usage.input_tokens`, `output_tokens` | `Usage` | `TestClairicMapping_Usage` |
  - Envelope parser: reads `result`, treats `success:false` as an error with the first
    `errors[]` code and message.
- `ListModels`/`ModelInfo`: `/ai/models/search`, task name mapped to `ModelType`
  (decision models are tagged Text Generation by Cloudflare, so the adapter table overrides
  them to `ModelTypeDecision`). Copies returned; `TestConcurrentListModels`,
  `TestConcurrentModelInfo`.
- Media and rerank (PR 4): `Reranker` (`bge-reranker-base`), `ImageGenerator`
  (`flux-1-schnell`), `SpeechSynthesizer` (`aura-2-en`), `Transcriber`
  (`whisper-large-v3-turbo`; `whisper` and `nova-3` take other wire shapes) over `/ai/run`, each after its probe.
- Pricing: Clairic $0.10/M input and Clef-omni $0.15/M input rows in `cost.go` and the
  registry with a note that Clairic bills `passes` times the state tokens (image-token repetition
  unverified; one image call in packet 11 settles it); the
  reported `usage.input_tokens` already includes the repetition (verified on three live
  calls: 412 = 2×39 + 334, 254 = 2×11 + 232, 219 = 1×64 + 155, that is
  `passes × (state_tokens + image_tokens) + schema_tokens`), so the estimator uses it
  directly and does not multiply again.
- Deferred Cloudflare tasks with no SDK interface: Translation, Image-to-Text (unless usable
  as vision chat, checked per model), Image Classification, Voice Activity Detection,
  sentiment classification.

## Packets (all done 2026-11-14 except 12, deferred; the text below is the original plan, see the implementation notes for what changed)

Every packet: `go test -race ./...`, `go vet ./...`, `gofmt -s`, nitpick review, gate >= A-.

PR 1 (core, `feat:`)
1. `decision.go`, `Capabilities.Decision`, `ModelCapabilities.Decision`, `ModelTypeDecision`,
   `errors.go` sentinel notes. Tests: `TestApplyDecisionOptions`, `TestAsDecider`,
   `TestHasDecision`, `TestModelCapabilitiesTypes_Decision`, `TestDecisionAnswer_JSONRoundTrip`
   with fixtures copied from the three live Cloudflare responses and the OpenRouter spec
   example. CHANGELOG `[Unreleased]` entry, godoc on every exported symbol, `docs/providers.md`
   capability row, SECURITY.md line.

PR 2 (OpenRouter, `feat:`)
2. Request builder and validation, no network: `decision_validation_test.go` for every kind,
   asset rules, session ID, `Extra` merge.
3. HTTP and error mapping: `httptest` for 200, 400, 401, 402, 413, 429, 524, 529, context
   cancel; `TestDecideErrorsRedactKey`.
4. Usage: `cached_tokens`, `is_byok`, `cost` into `Usage.Cost`; `refusal` answers set `Refused`.
5. Models: `ListModels` filter (no cached fallback), `TestModelInfo` not-found,
   `TestConcurrentListModels`, `TestConcurrentModelInfo`.
6. Live smoke (`integration` tag): one `typesafe/jev-1.14` call, asserts `Usage.Cost` set.
   Needs `OPENROUTER_API_KEY` and user go-ahead for spend (about $0.00002).
7. Docs and pricing rows: `docs/providers.md`, `docs/guides/decisions.md`, CHANGELOG,
   `cost.go`, registry.

PR 3 (Cloudflare core, `feat:`)
8. Package, options, env constants, `/ai/run` client, envelope parser, System One and Clairic
   adapters, error table, `httptest` per shape from the live fixtures above,
   `TestDecideErrorsRedactTokenAndAccountID`, `TestAdapterTableConcurrentRead`. Starts with
   one live probe per clef-omni media kind.
9. Chat and embeddings via `openaicompat.BaseProvider`, `register.go`, `ProviderCloudflare`,
   `ListModels`/`ModelInfo` with the pagination probe, interface compliance checks, env table
   rows in AGENTS.md and README, package doc with default models, CHANGELOG. Tests from the
   AGENTS.md minimum list (`TestDefaultOptions`, `TestApplyOptions`,
   `TestNewClientMissingAPIKey`, `TestNewClientMissingAccountID`,
   `TestNewClientWithEnvAPIKey`, `TestNewClientWithLLMAPIKeyFallback`, interface tests,
   concurrent model tests).

PR 4 (Cloudflare media, `feat:`)
10. Rerank, image, TTS, ASR over `/ai/run`, each after one live probe; `httptest` per type.
11. Live smoke (`integration` tag): `clef-flash`, `clef-omni`, `clairic`, chat, embeddings,
    one media call per type. Jev on Cloudflare skipped.

Separate, gated
12. TypeSafe direct provider, after a primary doc or live call confirms the API.

## Non-goals

- No change to `LLM`, `GenerateContent`, or `Stream`.
- `openai/decisions` on Cloudflare: not routable on this account (code 7000). ASSUMED, not
  observed: the core can hold its array-of-answers shape without a core change. Verify when
  it routes. Not in PR 3.
- No Vercel AI Gateway or AIML API routes unless a consumer asks.
- No provider-agnostic `ask` helper. Callers use `AsDecider`.

## Risks

- OpenRouter endpoint is alpha. The parser keeps unknown fields in `Raw`; the integration
  smoke logs a warning (not a failure) when the response carries fields the SDK does not model.
- Core types are a one-way door once PR 1 is tagged (auto-release cuts a minor). PR 1 lands
  only after the fixtures from all live-probed shapes round-trip in `TestDecisionAnswer_JSONRoundTrip`.
- Pricing moves. Rate rows are added only with a source URL in the comment, and provider
  reported cost always wins.

## Decisions

- 2026-11-14: Plan A (OpenRouter first, TypeSafe direct deferred) plus Plan C (core
  interface as its own PR), Cloudflare as a first-party provider with chat, embeddings, and
  every other model type that has an existing SDK interface. Clairic (preview) ships in PR 3.
  Jev on Cloudflare is skipped. `openai/decisions` deferred (not routable).
- CTO round 1 (REVISE) items applied: live probes moved ahead of PR 1; `Decide` takes
  `DecisionRequest`; `DecisionKind` and answer struct defined; error sentinels named;
  cost convention cited; `ListDecisionModels` dropped; Cloudflare options completed;
  concurrency tests named per packet; pricing in `cost.go` and registry; "fail loudly"
  replaced by a warning; PR 1 carries its own docs and SECURITY note.
- CTO round 2 (REVISE): ordered `Choices` slice, noul guidance fields, rune `DecisionSpan`
  (live-verified), `Guess`, JSON tags, named `Assets`, `ErrorClassifier` hook, 7000 caveat,
  `ReferenceDate` rejection. Recursive list `Items` REJECTED on live evidence (5006).
- CTO round 3 (REVISE): tags scoped to SDK serialisation, pointer `Box` and `Estimate`,
  `Breakdown`, null/refusal combinations, sensitive-field rule, `openai/decisions` ASSUMED.
- CTO round 4 (REVISE): stale `Media` references removed, mapping tables, circular asset rule
  replaced by rejection, `Passes` single source.
- CTO round 5 (REVISE, core structs signed off): OpenRouter mapping table, `box` deferred to
  packet 6, Clairic request rows, Clairic billing verified on two live calls, Clairic null
  live-observed.
