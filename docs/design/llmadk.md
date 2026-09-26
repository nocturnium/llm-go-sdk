# Design: `llmadk`, the multi-provider backend for Google ADK Go

Status: r4.1 FINAL (2026-09-26); design approved: CTO A (GO), 10x A-. r4.1 folds in their
last minor notes. History: r1: CTO B+, 10x C+. r2: CTO B+, 10x B+. r3: CTO A- (A with
R7), 10x B+ (bridge A-, two root specs). r4 answers every finding; ids cite where each is
resolved (CTO-n / 10x-n for r1, CTO rN-n / 10x rN-n for later rounds).
Targets: ADK `google.golang.org/adk/v2` v2.4.0, genai v1.70.0, root `llm-go-sdk/v6`.

## 1. Product

**What:** any of our 21 providers, and any `llms.LLM` middleware chain (resilience,
fallback, cache, cost, observability), running as the `model.LLM` behind an ADK agent.

**Who:** Go teams on ADK who need a non-Gemini model or several. ADK Go ships only Gemini,
Apigee, and a Responses-API-only OpenAI adapter that flattens content to text. Existing
third-party adapters (adk-go-anyllm, adk-anthropic-go, adk-openai, kagent models) are
single-provider or unreleased.

**"Full compatibility" is defined as** a published provider x feature matrix
(docs/guides/adk.md) where every cell is one of: supported and live-verified, supported
and fixture-verified, or rejected with a named typed error. No cell may be "silently
lossy". The matrix is the contract; tests (section 9) are what fill it in.

**Success metric:** v0.1.0 ships with a matrix whose live-verified cells cover OpenAI
(chat and Responses), Anthropic, Gemini; importers on pkg.go.dev and an adk.dev models-page
listing are tracked after release.

**Non-goals:** ADK Live/bidi audio; Gemini-only server tools (code execution, URL context,
retrieval, maps, file search, computer use); ADK session/memory/artifact services;
name-based registry glue (ADK never calls `model.NewLLM`, YAML agents are Gemini-only:
`internal/configurable/configurable.go:117`) (CTO-9, 10x-15); the reverse adapter
`FromADK` is deferred to v0.2 (CTO Q3, 10x-16).

## 2. Verified facts this design rests on

- `model.LLM` / `LLMRequest` / `LLMResponse`: `model/llm.go:26-68`; unchanged v2.0.0 to v2.4.0
  except JSON tags.
- Flow executes function calls only from non-partial responses: `base_flow.go:678`
  (`if resp.Partial { continue }`). An event with empty finish and empty content is dropped
  (`base_flow.go:655`).
- Aggregator (internal, not importable): every chunk yielded `Partial=true` (function-call
  parts included), `TurnComplete = finish != ""` on partials only (:64), then one final
  from `Close()` (:306-329).
- ADK strips every `adk-` function-call ID before the model sees history
  (`contents_processor.go:246`, `utils.go:55-66`), so ID-less history is the normal path
  for providers that return no IDs.
- ADK sets only `SystemInstruction`, `Tools`, and (from `OutputSchema`)
  `ResponseSchema`+`ResponseMIMEType` (`basic_processor.go:53-56`). For names not starting
  `gemini-`, schema and tools arrive together (`googlellm/variant.go:95-105`). For
  `gemini-*` names ADK instead injects `set_model_response` whose
  `ParametersJsonSchema` is a `*genai.Schema` (`outputschema_processor.go:133`).
- The compaction summarizer copies `SafetySettings`, `TopK`, `Seed`, `Labels`,
  `HTTPOptions` from the root agent config (`llm_summarizer.go:489-501`); compaction
  triggers on `UsageMetadata.PromptTokenCount` (`compactioninternal/tail_retention.go:265-276`).
- Long-running and deferring tools emit no FunctionResponse (`base_flow.go:1281-1285`),
  leaving a dangling call in history (`contents_processor.go:537-540`).
- `load_artifacts` appends arbitrary-MIME inline parts (`load_artifacts_tool.go:235`).
- Every functiontool sets `FunctionDeclaration.ResponseJsonSchema` (`function.go` `Declaration()`).
- Root facts: Anthropic JSON mode replaces all tools with its forced tool
  (`pkg/providers/anthropic/anthropic.go:575-601`); Gemini provider uses the function name
  as the tool-call ID (`pkg/providers/gemini/converters.go:54`); `FallbackChain.Provider()`
  returns entry 0 (`fallback.go:400-409`); `ToolRegistry.Handle` turns handler errors into a
  successful `"Error: ..."` message (`tools.go:340-343`); only `zai` reads `WebSearch`;
  reasoning replay payloads live in `ReasoningContent.Metadata`
  (`openaicompat/responses.go:226`, `anthropic/converters.go:101-108`).

## 3. Packaging and release (CTO-7, 10x-10, CTO-11)

Nested module in directory `llmadk/`, module path `github.com/nocturnium/llm-go-sdk/llmadk`,
package `llmadk`, tags `llmadk/vX.Y.Z`, starting `llmadk/v0.1.0`. Stays v0 (ADK and genai
are pre-stable; genai ships ~weekly minors); pins ADK to a minor. Reason for nesting:
ADK's `go 1.26.6` and 32 direct requires would otherwise raise the root Go floor and enter
every v6 consumer's version resolution.

Packet **P0 (merged before any `llmadk/` commit)**, release-lane hardening:
- `auto-release.yml`: `git describe --tags --abbrev=0 --match 'v[0-9]*'`; Makefile `VERSION` same.
- `cliff.toml`: `tag_pattern = "^v[0-9]+\\."`; root bump computed with
  `--exclude-path 'llmadk/**'` so `feat(llmadk):` commits cannot cut a root release.
- `GORELEASER_CURRENT_TAG` and `GORELEASER_PREVIOUS_TAG` both pinned to matched root tags
  (a root tag and an `llmadk` tag can share a commit).
- `--exclude-path 'llmadk/**'` applied to the RELEASABLE commit count
  (`auto-release.yml:73`) as well as to `--bumped-version`.
- New `llmadk-release.yml`: git-cliff with `--include-path 'llmadk/**'` and
  `tag_pattern = "^llmadk/v"`, tags `llmadk/vX.Y.Z`; refuses to tag if `llmadk/go.mod` has a
  `replace` or requires an unreleased root version.
- CI job `llmadk` (`working-directory: llmadk`, `GOWORK=off`): build, vet, `test -race`,
  golangci-lint, govulncheck, apidiff (`llmadk/api.txt`), codecov flag.
- Dependabot `gomod` entry for `/llmadk`.
- Evidence gate (CTO r2-7, 10x r2-5): dry runs of both decide steps on a scratch branch
  with `llmadk/v0.0.1-test` tags (deleted after), covering: (a) `feat(llmadk):` touching
  only `llmadk/` cuts no root release; (b) a commit touching both trees cuts a root release
  and an llmadk release; (c) first llmadk release with no prior `llmadk/v*` tag yields
  `llmadk/v0.1.0`, prefix kept; (d) root and llmadk tags on the same commit resolve to the
  right previous/current tags in each lane. git-cliff's mixed-commit semantics are
  unconfirmed until (b) runs; if it does not behave, the lane computes path membership
  itself with `git diff --name-only`.

## 4. Root prerequisites: root minors v6.10.0 (R-a) and v6.11.0 (R-b), R1-R10

Each is a root bug or gap the bridge exposed; each ships with its own tests and a
CHANGELOG entry.
- R1 **Served-by identity** (CTO-4, CTO r2-2): `Response.Provider` and `Response.Model`,
  and the same on the Done `StreamChunk`. `Model` is defined as the **effective requested
  model ID** (the `WithModel` override, else the client default), never the version string
  the API reports; a reported snapshot, when a provider returns one, goes in
  `Response.ModelVersion`. `FallbackChain`, resilience, cache, and observability
  middleware preserve the serving entry's values.
- R2 **Session-unique tool-call IDs** (CTO r2-1, 10x r2-1): every provider that does not
  receive a globally unique ID from its API mints one with crypto/rand, 9 chars
  `[a-zA-Z0-9]` (valid for Mistral's rule and all others). Gemini: add `id` to
  `internal/geminiapi.FunctionCall`/`FunctionResponse` and prefer the native ID when the API
  returns one; otherwise mint. Gemini's FunctionResponse name resolution looks the
  `ToolCallID` up in the preceding assistant `ToolCalls` instead of using the ID as the name
  (`gemini/converters.go:160-166`) (CTO r2-4). Providers whose APIs return unique IDs
  (OpenAI `call_`, Anthropic `toolu_`, Responses `call_`/`fc_`) keep them.
- R3 **`ToolRegistry.Handler(name) (ToolHandler, bool)`** accessor (10x-11).
- R4 **Stream contract** (CTO-10): `StreamChunk.ToolCalls` godoc states tool calls are
  complete and delivered on the Done chunk; contract test across openaicompat chat,
  Responses, Anthropic, Gemini, and every middleware that re-emits streams.
- R5 **Reasoning provenance in the root** (10x r2-3; replaces r2's FallbackChain strip):
  `ReasoningContent` gains `Provider` and `Model`; `ToolCall` gains `SignatureProvider`.
  Each provider stamps what it mints. Each provider's request converter replays only
  reasoning and tool-call signatures stamped with its own provider (and, for providers
  whose signatures are model-bound, its model) and drops the rest. Unstamped reasoning
  (from pre-v6.10 callers) keeps today's behavior. This makes fallback correct per entry
  in both directions without the bridge or `FallbackChain` guessing who will serve.
- R6 **Anthropic thinking-block rule** (CTO r2-3, 10x r3-1, CTO r3-4): applies only when
  the request uses manual thinking (`Thinking.Type == "enabled"`, the `genLegacy` path,
  `anthropic.go:680-696`); a no-op for adaptive thinking (gen46/gen47, which Anthropic
  exempts from the rule) and for always-on models (`genAlwaysOn`, which cannot disable
  thinking, `anthropic.go:657-661`). The check is turn-level, matching Anthropic's rule:
  walk back from the last message across tool_result-only user messages to the first
  assistant message of the current turn; suspend thinking for this one request only if
  no assistant message in that turn carries a replayable (own-provider, R5) thinking or
  redacted_thinking block. Ordinary Claude loops (block on step 1, none on steps 2..n)
  therefore keep thinking on. Recorded in `Response.Adjustments` (R8).
- R7 **Gemini 3 unsigned function calls** (CTO r3-1, 10x r3-2): Gemini 3 rejects a
  current-turn function call without a thought signature (400 "Function call is missing a
  thought_signature"). When the Gemini converter, targeting a Gemini 3+ model, finds a
  function call in the current turn with no own-provider signature (none, or dropped as
  foreign by R5), it inspects only the **first** functionCall part of each step of the
  current turn (Gemini signs only the first of parallel calls, so calls 2..n are unsigned
  by design) and sets Google's documented placeholder `skip_thought_signature_validator`
  there only if that part lacks a Gemini-own signature; other parts are never touched.
  Never applied to calls carrying Gemini's own signature (Google warns of quality loss).
  Recorded in `Response.Adjustments`. R6 and R7 share one root helper that computes the
  current turn (walk back across tool_result-only user messages), so the two rules cannot
  drift (10x r4-1/2).
- R8 **`Response.Adjustments []string`** (CTO r3-3, 10x r3-5), also on the Done
  `StreamChunk`: named, response-level notices of request adjustments a provider made
  (`anthropic.thinking_suspended`, `gemini.signature_validator_skipped`,
  `mistral.tool_ids_rewritten`). Keeps `Reasoning` nil on responses that did no reasoning.
- R9 **Stamp preservation** (10x r3-3): a reflection test asserts every exported field of
  `ReasoningContent` and `ToolCall` (including R5 stamps) survives `CollectStream`,
  `cloneResponse`, the Responses stream final (`responses_stream.go:214-218`),
  FallbackChain stream re-emit, cache replay, and the bridge's stream aggregation. All 14
  explicit `ReasoningContent` constructions are audited and routed through one copy helper.
- R10 **Mistral IDs** (10x r3-4, 10x r4-5, CTO r4-2): a tool-call-ID rewriter hook on
  `openaicompat.ProviderConfig` (Mistral is a BaseProvider, `mistral.go:37,72`; no one-off
  converter fork), set by the Mistral provider. IDs already matching `^[a-zA-Z0-9]{9}$`
  pass through; others (OpenAI `call_`, Anthropic `toolu_` from fallback or a model switch)
  map to a deterministic 9-char alphanumeric hash, applied identically to the call and its
  result. `Adjustments` records only when a rewrite happened.
- R2 addendum (10x r3-6): the Gemini FunctionResponse name lookup falls back to today's
  behavior (ID as name) when the `ToolCallID` matches no preceding call, so manual callers
  keep working.
- Routers (CTO r4-1, 10x r4-4): OpenRouter reasoning carries no replayable payload today
  (openaicompat chat surfaces reasoning text only; no `reasoning_details` passback in
  `pkg/`). It is always stamped `openrouter`; the reported upstream, when present, goes in
  an informational field only. Revisit when `reasoning_details` passback lands.

v0.1.0 requires root v6.11.0 (enforced by `llmadk/go.mod` and the release workflow).

## 5. Public surface

```go
func NewModel(llm llms.LLM, opts ...Option) (model.LLM, error)

func WithName(name string) Option                 // default llm.Model(); a "gemini-" prefix changes ADK behavior (documented)
func WithCallOptions(o ...llms.CallOption) Option // applied before request-derived options
func WithIgnore(fields ...Field) Option           // per-field ignore instead of reject
func WithWebSearch() Option                       // caller asserts provider web search; enables GoogleSearch mapping
func WithCapabilityChecks() Option                // opt-in 6.6 preflight
func WithTimeout(d time.Duration) Option          // HTTPOptions.Timeout wins when set

func Tools(reg *llms.ToolRegistry) ([]tool.Tool, error)

// Field names a genai field for WithIgnore; constants cover every rejectable field.
type Field string

var ErrUnsupportedConfigField, ErrUnsupportedPart, ErrUnsupportedTool,
    ErrCapability, ErrRequestNil, ErrNoContents, ErrFunctionCallArgs,
    ErrIncompleteStream error
```

## 6. Request translation (genai -> llms)

### 6.1 Contents

| genai | llms / action |
|---|---|
| `Config.SystemInstruction` text parts | one leading `RoleSystem` message, parts joined `\n\n` |
| role `user` or empty role, Text | `RoleUser` (Parts when mixed with images) |
| role `model` Text | `RoleAssistant` content |
| role `model` Thought part | ignored as text; its envelope (6.4) restores `Message.Reasoning` |
| role `user` Thought part (reachable via `ConvertForeignEvent`, `contents_processor.go:736`) | dropped |
| `FunctionCall{ID,Name,Args}` (model) | `ToolCalls[]`, Arguments = json.Marshal(Args); per-call envelope restores `ToolCall.Signature` |
| `FunctionCall.PartialArgs` / `WillContinue` in history | `ErrUnsupportedPart` (we never emit them) |
| `FunctionResponse{ID,Name,Response}` | one `RoleTool` message, ToolCallID, Name, Content = json.Marshal(Response) |
| `FunctionResponse.Parts` non-empty, `WillContinue=true` | `ErrUnsupportedPart`; `Scheduling` ignored |
| `InlineData` image/* (user) | base64 image part |
| `InlineData` text/*, application/json (user, e.g. load_artifacts) | decoded to a text part, prefixed with display name when present |
| `InlineData` other MIME, any `InlineData` on role model | `ErrUnsupportedPart` naming the MIME |
| `FileData` image/* with http(s) URI | image URL part |
| `FileData` `gs://` or Gemini Files API URI, or non-image | `ErrUnsupportedPart` |
| `ExecutableCode`, `CodeExecutionResult`, `ToolCall`, `ToolResponse`, `VideoMetadata`, `AudioTranscription` | `ErrUnsupportedPart` |
| `PartMetadata` (other than our key), `MediaResolution`, `MediaProcessing` | ignored (hints) |
| all-zero part | skipped |

Ordering: within one `user` Content, FunctionResponse parts become tool messages before
the Content's other parts.

**IDs (CTO-3, 10x-9).** Output side (7.1) guarantees every call we emit has a unique,
non-`adk-` ID, so ADK keeps it and history pairs by ID. Input side is the fallback for
foreign history: an empty `FunctionCall.ID` gets a synthesized ID, 9 chars `[a-zA-Z0-9]`
(valid for Mistral's rule and every other provider), derived deterministically from a hash
of (turn index, part index, name) and checked against every other ID in the request, so
re-sent history is byte-identical and prompt caches stay warm; the matching empty-ID
FunctionResponse is paired by name in order. Compaction shifting turn indices changes these
IDs, which costs cache hits only.

**Dangling calls (10x-8).** An assistant tool call with no FunctionResponse before the next
non-tool message (or end of history), which ADK produces for long-running and deferred
tools, gets a synthesized tool message `{"status":"pending","detail":"no result yet; do
not call again unless asked"}`. Chosen over dropping the call because the model then knows
the operation is outstanding, matching ADK's long-running tool note. A later real response
for the same ID (long-running completion, HITL confirmation re-send) supersedes the
placeholder; duplicate responses for one ID keep the last.

### 6.2 Config: translate, ignore, or reject

Default reject for anything untranslatable; `WithIgnore(Field...)` turns specific fields
into silent drops; **HARD** rows cannot be ignored because dropping them changes result
shape. Default-ignored set: `Labels` (adk-python sets an agent label; a future ADK Go that
does must not break every agent), `HTTPOptions.Headers`, `FunctionDeclaration.Response`,
`FunctionDeclaration.ResponseJsonSchema` (describe tool output, not input), and
`ToolConfig.FunctionCallingConfig.StreamFunctionCallArguments` (we deliver complete calls,
which is the behavior it asks for).

| Field | Handling |
|---|---|
| Temperature, TopP, MaxOutputTokens, StopSequences, PresencePenalty, FrequencyPenalty | direct |
| Tools[].FunctionDeclarations | `llms.Tool`, schema per 6.3; `Behavior=NON_BLOCKING` reject |
| Tools[].GoogleSearch / GoogleSearchRetrieval | reject unless `WithWebSearch()` (only zai honors web search today) (10x-7) |
| other Tools[] built-ins | `ErrUnsupportedTool`, HARD |
| ToolConfig mode AUTO | auto; with AllowedFunctionNames the tool list is filtered to them (10x-12) |
| NONE | none |
| ANY | 0 names: required; 1: tool(name); N: required + tool list filtered |
| VALIDATED | reject (no provider-neutral schema-validated calling) |
| ToolConfig.RetrievalConfig, IncludeServerSideToolInvocations=true | reject |
| ResponseMIMEType `application/json` + schema, no function tools | `WithJSONSchema("response", schema, false)` |
| ResponseMIMEType `application/json` + schema + function tools | bridge-owned structured output, 6.5 (CTO-1, 10x-1) |
| ResponseMIMEType `application/json`, no schema | `WithJSONMode()` |
| ResponseMIMEType "" / text/plain | nothing; other MIME reject |
| ThinkingConfig | mirrors openaimodel precedence (`request.go:485-540`): Budget < -1 reject; a non-UNSPECIFIED Level wins over Budget; Level -> Effort (unknown level reject); Budget 0 -> Enabled=false; -1 -> Enabled=true, no budget; >0 -> BudgetTokens; explicit UNSPECIFIED level with no budget -> Effort medium (as openaimodel) |
| ThinkingConfig nil or IncludeThoughts=false | request unchanged; visible thought text omitted from output, round-trip envelope kept (6.4) |
| HTTPOptions.Timeout | per-call context timeout; other HTTPOptions (besides Headers) reject |
| CandidateCount > 1, ResponseModalities other than [TEXT] | reject, HARD |
| TopK, Seed, Logprobs/ResponseLogprobs, SafetySettings, CachedContent, MediaResolution, SpeechConfig, AudioTimestamp, ImageConfig, RoutingConfig, ModelSelectionConfig, ModelArmorConfig, EnableEnhancedCivicAnswers, ServiceTier, AudioTranscriptionConfig | reject (ignorable; doc shows the `WithIgnore` line for Gemini sample configs and the compaction summarizer copy) |
| `LLMRequest.Model` non-empty and != Name() | `llms.WithModel(req.Model)` |
| `LLMRequest.Tools` | not read (ADK bookkeeping) |

### 6.3 Schemas (10x-3, CTO-2)

Type switch on `ParametersJsonSchema` / `ResponseJsonSchema`:
`*genai.Schema`/`genai.Schema` -> converter; `*jsonschema.Schema`, `map[string]any`,
`json.RawMessage`, `[]byte` -> marshal; anything else reject. Precedence matches
openaimodel: `Parameters` over `ParametersJsonSchema` when both set.
Converter: Type upper->lower; Nullable -> `type:[t,"null"]`; Enum, Format, Items,
Properties, Required, AnyOf, Min/MaxItems, Min/MaxLength, Min/MaxProperties,
Minimum/Maximum, Pattern, Default, Title, Description; PropertyOrdering and Example
dropped. Neither set -> `{"type":"object","properties":{}}`.

### 6.4 Reasoning and signature envelope (CTO-4/5, 10x-4/5/6, 10x r2-2/3/7, CTO r2-2/5)

genai carries `ThoughtSignature []byte` on parts and ADK persists it in sessions
(including database sessions) and ships it to clients in its REST/SSE event stream. We
store a versioned envelope there, not a bare signature:

```
"llmgo" 0x01 <JSON {"p":provider,"m":model,"c":content?,"s":signature,"md":metadata,"k":"thought"|"call"}>
```

- **Output.** A Thought part carries the full `ReasoningContent` (Signature, Metadata, and
  its R5 Provider/Model stamp), so the Anthropic signature+text pair, Anthropic
  `redacted_thinking`, and OpenAI Responses encrypted reasoning items all round-trip. A
  `FunctionCall` part carries a `"call"` envelope for `ToolCall.Signature` and its
  `SignatureProvider`.
- **Visible text vs `c`.** With `IncludeThoughts=true` the thought text is the part's
  `Text` and `c` is omitted (no duplicate storage). With `IncludeThoughts=false` or nil
  `ThinkingConfig`, `Text` is empty and `c` holds the text, because Anthropic requires the
  signed text on replay. Consequence, documented in godoc and the matrix: hidden thought
  text is present, base64-encoded, inside `ThoughtSignature` in session storage and in
  ADK's event stream. Sealing the envelope (`WithEnvelopeKey`, AEAD) is a v0.2 follow-up.
- **Thought-only turns (10x r2-2).** If the response has no text and no tool calls and
  `IncludeThoughts=false`, no Thought part is emitted at all: `Content` is empty and
  `ErrorCode` carries the finish when not STOP, matching native Gemini. This prevents
  ADK's thought-only re-call loop (`base_flow.go:120,176-190`). A turn with no answer and
  no call has nothing that needs replay.
- **Input.** Every envelope is restored onto `Message.Reasoning` / `ToolCall.Signature`
  with its stamp intact; the bridge does not decide replay. The root provider converter
  that ends up serving the request applies R5, so a FallbackChain entry replays exactly
  its own reasoning, whichever entry served earlier turns.
- **Stamp fallback.** If a response carries no R5 stamp (third-party `llms.LLM` or
  middleware not yet stamping), the bridge stamps with `Response.Provider`/`Model` (R1),
  else with the llm's `Provider()` and the effective requested model (`req.Model`, then a
  `WithModel` in `WithCallOptions`, then `Model()`), unless the unwrapped llm is a known
  router (FallbackChain), in which case it leaves the stamp empty and R5 treats it as
  unstamped.
- **Foreign bytes (CTO r3-2, 10x r4-3).** Non-envelope `ThoughtSignature` bytes come from
  ADK's native Gemini model; they are restored as a Gemini-stamped signature with unknown
  model, encoded with `base64.StdEncoding` (our `geminiapi.Part.ThoughtSignature` is the
  wire base64 string, `internal/geminiapi/types.go:33`; genai holds raw bytes; pinned by a
  bytes-in, same-wire-string-out test),
  and R5 decides (our Gemini provider replays them, same API; every other provider drops
  them). Our envelope bytes would reach ADK's native Gemini if an agent switches back; the
  live gate checks Gemini's reaction.
- **Callback edits.** With `IncludeThoughts=true`, `c` is omitted and replay uses the
  part's visible text; an after-model callback that edits thought text breaks the
  signature match (Anthropic 400). Documented.
- **Hardening (CTO r3-5).** Envelopes over 8 MiB are rejected before decoding; malformed
  or oversize envelopes are dropped, never errors (a corrupted session must not brick the
  agent), and each drop is counted in the next response's `CustomMetadata`
  (`llmgo.envelopes_dropped`) and logged via slog at warn. Version byte
  `0x01` is a persisted format: changes bump it and keep the old decoder.

### 6.5 Structured output with tools (CTO-1, 10x-1, CTO r2-6, 10x r2-6)

Triggered only for non-`gemini-*` names (ADK sends no `ResponseSchema` for `gemini-*`
names, `basic_processor.go:53`, and does its own `set_model_response` there). When the
request has a response schema **and** function declarations, the bridge never passes
both down:
- Adds a synthetic function `set_model_response` whose parameters are the response schema,
  and appends ADK's instruction text for it, copied verbatim from
  `outputschema_processor.go:32-37` (internal package) with a test pinning it, to the
  system message.
- A user function already named `set_model_response` is a collision: `ErrUnsupportedTool`.
- AllowedFunctionNames filtering (AUTO, ANY) always keeps the synthetic tool.
- Output: a response whose only call is `set_model_response` becomes the final Text part
  (JSON of its args, `FinishReason STOP`, no FunctionCall). A response with
  `set_model_response` **and** other calls emits only the other calls (the loop continues;
  the model answers again later). Plain text answers pass through.
- Streaming: prose streamed before a `set_model_response` call has already been shown as
  partials; documented.
- Tested on the real Anthropic client (fixture and live), not a fake.

### 6.6 Capability preflight, opt-in (CTO-6, 10x r2-4)

`Capabilities` cannot express "model-dependent", which several providers declare as
`false` (Ollama and RunPod `Tools:false`, `Vision:false`, `ollama.go:20-21`,
`runpod.go:22-23`), so a default-on gate would reject working local tool agents.
`WithCapabilityChecks()` opts in: tools with `!Tools`, image parts with `!Vision`, JSON
schema with `!JSONMode` -> `ErrCapability`. Default off; provider errors pass through
wrapped. Fixture row: Ollama tool loop with the default (no preflight) succeeds.

## 7. Response translation (llms -> genai)

### 7.1 Parts
- Order: Thought part (per 6.4), Text part, FunctionCall parts.
- FunctionCall: Args = json.Unmarshal(Arguments) (`{}` for empty/null; invalid ->
  `ErrFunctionCallArgs`).
- **IDs (CTO r2-1, 10x r2-1).** ADK pairs calls and responses across the whole session
  (`contents_processor.go:268-283,478-545`), so IDs must be session-unique. After R2 every
  root provider returns session-unique IDs. The bridge still enforces it for third-party
  `llms.LLM`s: an ID is kept only if it is non-empty, not `adk-`-prefixed, has at least 9
  characters, and is not already present in the request's history or earlier in this
  response; otherwise it is replaced with a crypto/rand 9-char alphanumeric ID. The
  replacement is what ADK persists, so pairing stays consistent.
- `set_model_response` calls handled per 6.5.

### 7.2 Finish, usage, metadata
- stop->STOP; length->MAX_TOKENS; tool_calls->STOP; content_filter->SAFETY; empty finish
  with content -> STOP (10x-14); anything else -> OTHER.
- Empty content and finish != STOP -> `ErrorCode = string(FinishReason)` (matches ADK
  converter). STOP always has non-nil `Content` (role model).
- Usage (10x-2): `PromptTokenCount = PromptTokens + CacheReadTokens + CacheCreationTokens`;
  `CandidatesTokenCount = CompletionTokens - ReasoningTokens` (floored at 0);
  `ThoughtsTokenCount = ReasoningTokens`; `CachedContentTokenCount = CacheReadTokens`;
  `TotalTokenCount` = prompt + candidates + thoughts. Verified per family by fixture: which
  providers include cache tokens in PromptTokens is checked in P2, not assumed.
- `ModelVersion` = the API-reported `Response.ModelVersion` when present, else the
  served-by `Response.Model` (R1), else the effective requested model.
- Anthropic never reports `ReasoningTokens`, so its `ThoughtsTokenCount` is 0 and thinking
  counts in candidates; documented as a matrix cell note.
- `CustomMetadata`: `llmgo.provider`, `llmgo.model` (served-by), `llmgo.adjustments` (R8),
  `llmgo.response_id`,
  `llmgo.service_tier`, `llmgo.cost_usd`, `llmgo.cache_creation_tokens` (numbers documented
  as float64 after a session round-trip).
- Errors: `fmt.Errorf("llmadk: %w", err)`; `errors.Is` against llms sentinels holds.
- GoogleSearch path (only with `WithWebSearch`): `SearchResults` -> `GroundingMetadata`
  chunks (title, URI).

## 8. Streaming (10x-14)

- `stream=true`: `llm.Stream` on a context derived from the caller's (shadowed, never
  reassigned). Text chunk -> `Partial:true` Text part; reasoning chunk -> `Partial:true`
  Thought part (visible text per IncludeThoughts; no envelope on partials). Non-Done
  `ToolCalls` ignored (as `CollectStream`). Done chunk -> one final (`Partial:false`,
  `TurnComplete:true`) built by the same 7.x code as the non-streaming path, with merged
  text, merged reasoning in the envelope, tool calls, usage, finish.
- Error chunk -> `yield(nil, err)`, return. Channel closed without Done or error ->
  `yield(nil, ErrIncompleteStream)` (wraps `llms.ErrStreamInterrupted`).
- `yield` returning false: cancel the derived context, spawn a goroutine that drains the
  channel to close, return immediately. `yield` is never called after it returns false.
- Stamp source (10x r3-3): the final's reasoning stamp is taken from the last stamped
  reasoning chunk, else the Done chunk's R1 identity, else the 6.4 stamp fallback.
- `Response.Adjustments` (R8) on the Done chunk -> `CustomMetadata["llmgo.adjustments"]`.
- Re-rangeable: each range issues a fresh call.
- `stream=false`: `GenerateContent`, exactly one non-partial response.

## 9. Tool bridge (10x-11)

`Tools(reg)` wraps each registered tool with `functiontool.New[map[string]any,
map[string]any]`, `InputSchema` resolved from our raw JSON via jsonschema-go at `Tools()`
time (error names the tool). The handler comes from `reg.Handler(name)` (R3); handler
errors are returned as errors (ADK OnToolError fires); a map result passes through, any
other result becomes `{"result": v}` with strings kept as strings.

## 10. Testing

1. **Field classification (CTO-8):** a reflection test enumerates every exported field of
   genai `GenerateContentConfig`, `Part`, `Content`, `FunctionDeclaration`, `FunctionCall`,
   `FunctionResponse`, `Schema`, `ToolConfig`, `FunctionCallingConfig`, `ThinkingConfig`,
   `Tool`, `HTTPOptions` and fails on any field missing from the bridge's classification
   table (translated / ignored / rejected / HARD). This is the genai-churn tripwire.
2. **Unit, table-driven:** every row of 6.1, 6.2, 6.3, 7.x; envelope encode/decode,
   stamp fallback rules, 8 MiB cap, malformed-envelope drop, version byte; ID synthesis
   determinism, 9-char form, session-level dedupe against history; dangling-call
   placeholder and supersede; usage arithmetic; thought-only turn emits no Thought part;
   6.5 collision, mixed calls, AllowedFunctionNames keeps the synthetic tool; ADK
   instruction text pinned. >=95% statements.
3. **Streaming contract:** N partials then one final; yield-after-false never happens
   (instrumented yield panics if called again); early break returns immediately and the
   provider goroutine exits (goleak); close without Done -> ErrIncompleteStream; ctx
   cancel mid-stream; re-range issues a second call; final equals non-streaming result for
   the same scripted provider output.
4. **Real ADK end-to-end** (llmagent + runner + in-memory session, and sqlite database
   session for persistence): tool loop; parallel calls to the same tool; the same tool
   called in 3 consecutive turns (on the Gemini provider fixture) with sqlite history
   reloading intact and each response paired to its own call; `adk-` ID stripping;
   thought-only MAX_TOKENS turn with IncludeThoughts=false makes exactly one model call; long-running tool with dangling call then completion; HITL confirmation;
   agent transfer carrying Thought parts; output schema with tools; `load_artifacts` text;
   compaction threshold reached from cached-usage accounting; streaming SSE mode;
   ThoughtSignature and CustomMetadata surviving the sqlite round-trip.
5. **Provider families on real provider clients** against `httptest` servers replaying
   fixtures **recorded from live calls** with committed provenance (date, model, request
   hash), never hand-written: openaicompat chat, OpenAI Responses (stream and not),
   Anthropic (thinking, redacted_thinking, cache hit), Gemini (parallel same-name calls).
   Quirk rows: Groq/Cerebras (response_format with tools), Mistral (ID rule), DeepSeek
   (reasoning replay), Perplexity (no tools; ErrCapability with `WithCapabilityChecks`),
   Ollama tool loop with default options succeeds. Ollama and llama.cpp run as openaicompat,
   not separate families. Root R-packet fixtures: FallbackChain serving from entry 1 with
   thinking+tools across turns in both directions (Anthropic as fallback, Gemini as
   fallback); R6: manual-thinking loop at step 3 keeps thinking on (no suspension),
   foreign-only turn suspends, adaptive and always-on models are no-ops; R7: OpenAI to
   Gemini 3 fallback mid-loop gets the placeholder on the first unsigned call only, Gemini's
   own signed calls untouched, and an own-signed parallel FC1+FC2 replay gets no
   placeholder and no adjustment; R10 own valid Mistral IDs produce no adjustment; R9 stamp-survival reflection test; R10 Mistral receiving
   `toolu_`/`call_` history.
6. **Live gate, required for `llmadk/v0.1.0` (CTO-8):** tag `integration`; OpenAI chat and
   Responses (encrypted reasoning replay), Anthropic thinking+tools replay and
   structured-plus-tools, Gemini provider (session-unique IDs, function-call signatures),
   FallbackChain in both directions (Anthropic->OpenAI, OpenAI->Anthropic with thinking on),
   an ADK model switch into Claude mid-tool-loop (R6), an adaptive-thinking Claude turn
   with no thinking block (confirms Anthropic's exemption; if Anthropic does reject it, R6
   widens to adaptive), the same row on an always-on model (if rejected, a typed error is
   documented since thinking cannot be disabled there) (CTO r4-3), native Gemini
   signatures from a Vertex-backed ADK model replayed on the Gemini API, OpenAI to Gemini 3 mid-loop and an ADK switch into our Gemini (R7),
   a configured model alias
   (provenance match), envelope bytes sent to ADK native Gemini. Keys present locally
   (names checked, not values): `OPENAI_API_KEY`, `CLAUDE_API_KEY` (harness maps it to
   `ANTHROPIC_API_KEY`), `GEMINI_API_KEY`, `OPENROUTER_API_KEY`. No Z.AI key, so the
   GoogleSearch cell is fixture-verified only. Results recorded in
   `llmadk/testdata/live/<date>.md` and linked from the matrix.
7. **Fuzz:** `FuzzContentsToMessages`, `FuzzSchemaToJSON`, `FuzzEnvelopeDecode`.
8. **Canary:** weekly CI job builds and tests `llmadk` against `adk@latest` and
   `genai@latest`; failure opens an issue.
9. **Gates:** `-race`, vet, golangci-lint, gofmt, govulncheck, apidiff, nitpick review,
   10x + CTO re-review of the implementation at >= A-.

## 11. Delivery

| Packet | Scope | Exit |
|---|---|---|
| P0 | release-lane hardening (section 3) | dry-run evidence; merged to main first |
| R-a | root v6.10.0: R1, R2, R3, R4, R5, R8, R9 (identity, IDs, provenance, adjustments field, stamp preservation) | R fixtures; released; unblocks P2 |
| R-b | root v6.11.0: R6, R7, R10 (per-provider request adjustments) | R fixtures + live FallbackChain rows; required before P7 |
| P1 | `llmadk` scaffold, CI job, NewModel non-streaming text, errors, real-ADK e2e harness, field-classification test skeleton | e2e "hello" through llmagent green |
| P2 | contents, IDs, dangling calls, config table, schemas, capability preflight, usage | unit rows + e2e tool loop + parallel same-name + 3-turn same-tool |
| P3 | streaming | section 10.3 suite |
| P4 | envelope/provenance, thinking, structured-output-with-tools | 10.4 transfer/sqlite/schema+tools rows |
| P5 | Tools bridge | handler error and schema resolution tests |
| P6 | recorded fixtures for families + quirks, fuzz, canary | 10.5, 10.7, 10.8 |
| P7 | live gate, compatibility matrix doc, example `examples/adk`, godoc, `llmadk/v0.1.0` | 10.6 evidence, reviewers >= A- |

## 12. Follow-ups (not v0.1)

- Root generic file/document part (PDF, audio) so `ErrUnsupportedPart` rows can become
  translations; first in line because `load_artifacts` surfaces PDFs.
- `FromADK` reverse adapter (v0.2, designed around partial-then-repeating-final Gemini
  streams).
- Document duplicate spans when ADK telemetry and our OTel middleware are both on.
- `WithEnvelopeKey`: AEAD-sealed envelope so hidden thought text is not readable in
  session storage or ADK's event stream (CTO r2-5).
- Graduation to v1: two ADK minors without model-facing flow changes and a green canary
  for a quarter.

## 13. Risks

- Persisted formats (envelope, synthesized IDs) live in users' session DBs: versioned
  from day one.
- ADK types in our API tie us to ADK majors; contained by v0 and the nested module.
- From provider documentation, not yet live-probed: Anthropic's turn-level thinking rule
  and its adaptive exemption, Gemini 3's unsigned-call 400 and the placeholder value, and
  whether signatures are model-bound. The live gate verifies each before v0.1.0.
- R7's placeholder costs some Gemini quality on the affected step (Google's warning); it
  fires only for foreign or unsigned calls.
- R2, R6, R7, and R10 change root behavior in a minor release (Gemini tool-call IDs,
  Anthropic thinking suspension, Gemini placeholder, Mistral ID rewriting); each gets a
  CHANGELOG entry and an `Adjustments` notice, and the Gemini name lookup keeps manual
  callers that send only `ToolCallID` working.
- tool-arg validation through jsonschema-go (Tools bridge) is stricter than `RunTools`;
  documented as a behavior difference.
- OpenRouter credits were exhausted on 2026-09-25 (separate project); live rows via
  OpenRouter may wait on a top-up.
