# Full-review adjudication

A pedantic `nitpick full-review` of the whole tree (2026-09-15, reviewer
`z-ai/glm-5.3-flash`, `min_severity: nit`, `persona.nitpick: pedantic`) produced
557 findings: 10 error, 93 warning, 116 info, 338 nit. Its triage stage failed,
so the set is neither deduplicated nor ranked.

This file records what was done with them. Every error and warning is either
fixed on the branch or listed below with the reason it was rejected. The
remaining nit-level classes are a standing floor, described at the end.

## Rejected findings, with the check that rejected them

| Finding | Why it does not stand |
| --- | --- |
| Model tables and mode pricing are "invented" (`modes.go`, `openai/models.go`, `anthropic/models.go`, `capabilities_registry.go`) | The reviewer's model predates the 2026 flagships. These tables were transcribed from first-party pricing pages in PR #46 and are re-verified on each refresh. |
| `DefaultFallbackSelector` ignores HTTP 429 | It calls `isProviderUnhealthy`, which resolves to `APIError.IsRetryable`, which covers 429. |
| `normalizeErrorType` lets `DeadlineExceeded` shadow `ErrStreamTimeout` | `ErrStreamTimeout` is `errors.New`, wrapping nothing, so the two never collide. A table test now pins every branch. |
| Featherless `WithSpeechUsageHandler` is never wired | `speech.go` reads it from the options on each terminal event. |
| Transcribe's `ExtraBody["model"]` override never reaches the wire | `marshalMediaExtra` merges extras over the typed request, so the override lands in `fields["model"]`, which is also what the format validation reads. |
| Anthropic `tool_choice: {"type":"none"}` is undocumented | Anthropic's tool-use docs list `none` alongside `auto`, `any` and `tool`. |
| Resilience `Stream` leaks the breaker permit | `WrapStreamWithFinalizer` drains the source on its own goroutine and runs the finalizer on early exit, bounded by the stream send timeout. |
| MCP elicitation test passes on the mock's fallback | `newClient` installs `dispatchRequest` as the transport's request sink before the handshake, so the client answers. |
| Mistral sends granularities and keyterms as file parts | A `MultipartFile` with an empty `Filename` is written as a plain form field. |
| `include_encrypted_reasoning` is not a Responses parameter | It is consumed into the `include` list and never forwarded. |
| `LoggingMiddleware.Stream` drops error chunks | The chunk is forwarded before the error branch returns; a test now pins it. |

## Round-by-round

| Pass | Errors | Warnings | Info | Nit |
| --- | --- | --- | --- | --- |
| Pedantic review, first run | 10 | 93 | 116 | 338 |
| After the error and warning batches | 1 | 30 | 44 | 222 |
| After the verification batch | 0 | 33 | 41 | 210 |

The warning count stops falling because what remains is the adjudicated set
above (model tables the reviewer's cutoff predates, ExtraBody's documented
escape hatch, the Responses reasoning parameter this SDK consumes itself) plus
the nit-level floor below.

The model slop pass ran to zero: 12 findings, then 4, 2, 2, 2, 1, 2, 1, 5, 1, 3,
4, 1 and finally **0 on b7a28f4**, with every round's findings fixed before the
next pass. What that pass still reports is the deterministic tells floor below.

## Standing floor

`nitpick slop` reports these on a clean tree, deliberately:

- **restating-comment (~205), oversized-doc-comment (44).** Go doc comments open
  with the symbol name, so an exported declaration's godoc reads as restating by
  construction, and `.golangci.yml` records that exported-symbol docs are
  enforced by review here rather than by revive. The oversized ones carry
  verified pricing, wire-format and protocol rationale that trimming would
  delete. See the memory note `nitpick-slop-policy`.
- **changelog-comment (12) in `CHANGELOG.md`.** Describing what changed is that
  file's purpose.

Everything else the tells flag (em dashes, arrows, shouted words, filler) is
cleared across the Go tree, the docs, the workflows and the contributor guides.

## Second full review (2026-09-17, reviewer `z-ai/glm-5.3-flash`)

A rerun on `fix/ollama-native-auth` produced 155 findings (8 error, 59 warning,
88 info) with triage by `openai/gpt-5.6-luna`. 12 files (openrouter, perplexity)
went unreviewed after 2 failed batches, so their absence of findings means
nothing. An earlier attempt with `poolside/laguna-s-2.1` was abandoned at 33 of
88 batches: the same config that ran 1m14s per batch on 2026-09-15 was taking
24m per batch.

Fixed: `message.go` Content/Parts merge loss, zone-scoped IPv6 literals past
`ValidateURL` and `ssrfDialControl`, Ollama `truncate,omitempty`,
`CircuitBreaker.Reset` on a closed breaker, the Z.AI thinking toggle leaking
into OpenAI bodies, a numeric error `code` failing envelope parsing, base64
validated only for its first kilobyte, magic-byte sniffing outside the
supported types, ElevenLabs extras replacing validated media refs, an
unrecognized Z.AI coding extra silently disabling the endpoint, Langfuse's
response-model attribute ignoring a per-call override, `return_documents=false`
dropped by omitempty, and a batch cancelled before its requests started
returning a nil error.

### Rejected, with the check that rejected them

| Finding | Why it does not stand |
| --- | --- |
| togetherai `Extra{"Width": 2000}` bypasses the reserved-key guard and bills 2 megapixels | Map keys marshal sorted, so `"width"` decodes after `"Width"` and the typed value wins. The case-variant key reaches the wire but changes neither validation nor pricing. |
| codeql.yml's schedule trigger bypasses the private-repo guard | The `if: github.event.repository.private == false` is on the job, so it gates every trigger including schedule. |
| featherless and openrouter merge Extra without a reserved-field guard | `SpeechRequest.MarshalJSON` drops every reserved key from ExtraBody before the request is marshaled. |
| llamacpp's zero-value client cannot reach its own default base URL | `llamacpp.go:68` passes AllowPrivateIPs and AllowHTTP from the options, and the local providers default both to true. |
| openai `models.go` invents pricing; mistral, groq, fireworks, azure invent model metadata | The reviewer's cutoff predates the 2026 flagships; these tables were transcribed from first-party pages. |
| `include_encrypted_reasoning` is not a Responses parameter | Unchanged from the first review: it is consumed into the `include` list and never forwarded. |
| zai uploads keyterms as file parts | Unchanged from the first review: a `MultipartFile` with an empty `Filename` is written as a plain form field. |
| togetherai sends `response_format: "base64"`, not the OpenAI enum | Together documents `base64` and `url` for the request; `b64_json` is the response field this code reads back. |
| `extractJSON` returns the first balanced object rather than the payload | The proposed remedy (try each candidate) does not address the described failure, where the first candidate unmarshals cleanly, and preferring a later object would discard legitimate output. |
| A Responses stream ending without `response.completed` is reported as a clean stop | Both stream paths treat EOF as a clean finish; changing one would split the behavior across compat providers. |
| Azure labels every discovered model as chat; anthropic coerces an unknown tool-choice mode to auto | Fixing either means inventing a classification or an error path in a function that returns no error. |
| `ToLangfuseGeneration` keys do not match Langfuse's ingestion API | The finding states its key names cannot be verified from the diff, and the repository rule forbids acting on unverified external schemas. |
| mcp `stdio` write is not cancellable | Accepted as real and deferred: the fix needs an owner goroutine serializing writes, since an abandoned writer would hold writeMu and wedge every later request. |

### The 12 files the run could not reach

A follow-up review over `pkg/providers/openrouter` and `pkg/providers/perplexity`
(26 files) returned 11 findings: 1 error, 3 warning, 7 info. Fixed: a numeric
`code` in an OpenRouter batch error payload, which failed the decode of the
whole batch, and the Perplexity package doc, which advertised citations and
search_domain_filter that the package exposes no way to reach.

| Finding | Why it does not stand |
| --- | --- |
| Perplexity registers no provider factory | `pkg/providers/perplexity/register.go` calls `llms.RegisterProvider("perplexity", ...)`, and `pkg/providers/all/all_test.go` asserts the name resolves. The reviewer saw a subset of the package. |
| OpenRouter speech and video should reject reserved Extra keys rather than drop them | The marshalers drop reserved keys by design, matching `SpeechRequest.MarshalJSON` in openaicompat; only the togetherai image path errors, and that difference is deliberate. |
| OpenRouter documents typed media options as silently dropped | Same policy question as the main run, unchanged: rejecting them is a behavior change across every media provider, not a fix to this package. |

## Adversarial Go review (2026-09-19, branch fix/adversarial-review)

A hostile-reviewer pass over the tree at v6.9.3 returned 29 findings: 5
ship-blockers, 13 high, 9 medium and 2 low. All 29 were triaged against this
file; 25 were fixed, 2 were documented rather than changed (404 mapping to
ErrModelNotFound, the 10000-entry success-rate cap), and the otel panic policy
was hardened without a test, which cannot be driven from outside the package.

The five blockers were a circuit breaker that could not close below three
requests per half-open window, an SSRF dialer that missed `DialTLSContext`, a
redirect check that compared hostnames without scheme or port, an EOF that
could not be told from `[DONE]`, and a nil response that deadlocked
`ProcessBatch` with the results mutex held.

Two adjudications from earlier rounds were overturned by this review and the
fixes now stand: the truncated-stream EOF (anthropic already distinguished it,
so the "changing one would split the behavior" reason was wrong) and the
breaker-permit leak (neither stream wrapper drained its source, contrary to the
reason recorded for rejecting it).

Validation reran both `full-review` (177 findings, 5 error) and `repo-score`
(171 findings, 3 error). Four of those errors were real and fixed: a
write-only marshaler described as a string, the anthropic default model missing
from `knownModels`, a model-name sanitizer that could rebuild `..`, and a
failed Responses body returned as a success. The rest were the classes already
rejected above. repo-score put the tree at 0.10 slop, 1.91 bugs and 0.07
security weighted findings per thousand lines, over the files a model answered
for: two batches failed provider-side, leaving 12 files outside the
denominator.

## llmadk and the provenance packets (2026-09-26)

`nitpick review` of the whole stack (base 704041b, reviewer
`poolside/laguna-s-2.1`) read 116 of 118 changed files and reported 2 errors;
24 files failed provider-side with stream errors. A `nitpick full-review` of
the 11 core files among them (llmadk request, response, model, options, fields,
errors and tools, the fixture and live harnesses, the wire checks, and
`openaicompat/converters.go`) reported nothing.

| Finding | Why it does not stand |
| --- | --- |
| Gemini streamed tool calls get empty IDs on models before Gemini 3 (`gemini.go`) | The stream path calls `llms.EnsureToolCallIDs` on the accumulated calls before the final chunk, so an absent native ID is minted; `TestStream_IdentityAndToolCallContract` asserts two distinct minted IDs. |
| The new Gemini default `gemini-3.5-flash` does not exist | It is in the live `models.list` response for the account and answered a live call on 2026-09-26; the llmadk live gate ran the 3.5 family. |

## Security and accounting remediation (2026-10-09, branch feat/security-accounting-pricing-remediation)

Two `nitpick full-review` runs (`gpt-5.6-luna` review and triage, `min_severity:
warning`) over the whole tree. The reviewer's error set is largely nondeterministic
between runs: run 18 reported 7 errors, run 19 reported 16, run 20 reported 9, and
the sets only partially overlap. Each reported error was reproduced or refuted
against the code before acting.

Fixed: nil-options panic in `WrapStream`/`WrapStreamWithFinalizer`; a duration
that rounded to 2^63 and converted to a negative retry delay; the rate limiter
spending request capacity before the token wait could be met; `Cache` missing
from the response cache key (a `WithoutCache` call could read another call's
usage and cost); conflicting embedded JSON names emitted as a schema field
`encoding/json` then drops; `ExtraBody` keys the standard Anthropic fields own;
assistant history serialized with `output_text` instead of `input_text` in the
Responses input; CRLF-separated SSE events parsed as one; `ResultFn` invoked
after cancellation in `PollingVideoJob.Wait`; `ErrEmptyInput` returned for an
empty provider response to a nonempty query; `ValidationErrors{}` matching
`ErrInvalidParameters`; a nil channel closed in `ProcessStream`; nil responses
dereferenced in the OTel and Langfuse middlewares; a prerelease tag with a hyphen
rejected by the release workflow; and a single-function `ANY` allow list not
filtering the tool list in the ADK bridge.

| Finding | Why it does not stand |
| --- | --- |
| `actions/checkout@v7` and `setup-go@v7` are not released refs, so CI fails at checkout (`ci.yml`) | Both are published major tags: the GitHub tags API returns `v7.0.1`/`v7` for checkout and `v7.0.0`/`v7` for setup-go. The claim is stale knowledge, not a broken workflow. |
| `pkg/providers/synthetic` references an undefined `Client` type and cannot compile | `type Client struct` is defined in `synthetic.go`; `go build ./pkg/providers/synthetic/` exits 0. The reviewer saw `models.go` alone. |
| `WithSpeechUsageHandler` is never wired into the client | `speech.go` reads `c.options.SpeechUsageHandler` on the terminal `speech.audio.done` event. The reviewer saw the options file alone. |
| `resp.Usage` is a nil dereference in the structured-outputs guide | `Response.Usage` is a value field (`Usage`, not `*Usage`), so it cannot be nil; the example compiles and runs. |
| `Config.EmbeddingModel` is dropped by the Mistral factory | `llms.Config` has no `EmbeddingModel` field, so no caller-supplied value is dropped. Every provider factory forwards `Model`, not an embedding model. |
| `llmadk/config.go:182` treats the zero thinking level as unspecified | The empty `ThinkingLevel` being meaningful is the intended contract; see the note above. |
| `pkg/observability/otel_langfuse_test.go:299` global tracer provider blocks later tests | The suite passes reliably with `-shuffle` seeds 1-5; latent only. |

Follow-up rounds (21 and 22) on the same branch reported a still mostly different
error set each run. Fixed from those rounds: a comparable-interface LLM key that
panicked map insertion during cycle detection; a nil stream returned without an
error in the OTel middleware; stale closed-state completions left outstanding
across state transitions in the public circuit-breaker API; request parameters
overwriting caller metadata in the log entry; data races on a counting test
logger; a merge writing merged parts into a caller-owned slice; model filters and
sorters returning shallow copies that aliased the caller's `Pricing` pointer and
`Types` array; CR-only SSE line endings merged into one event in both SSE
readers; a reused Anthropic `StreamEvent` keeping a stale `MessageDelta`; an
oversized polling interval overflowing to a negative duration in the OpenRouter
batch waiter; and the auto-release tag staying behind after a failed GoReleaser
run, which would have skipped that commit's release forever.

| Finding | Why it does not stand |
| --- | --- |
| `GetWithDefaults` should apply boolean capability defaults | The method documents defaults for the two numeric fields and is only called to fill `MaxContextTokens`/`MaxOutputTokens`; booleans have no comparable "unset" state, so a false cannot be distinguished from a deliberate false. |
| `MetricsMiddleware.Call` should invoke the underlying `Call` | `LLM` has no `Call` method; the one-shot helper is the package function `llms.Call`, and routing through `GenerateContent` is the only way to obtain usage for cost and token metrics. The comment documents the choice. |
| A custom `json.Marshaler`/`Unmarshaler` should not be forced to a string schema | The SDK defines the round-trip contract for these types as a JSON string; a type that marshals to another shape is not supported by the typed-decode path, and the existing test pins the string schema. |
| `GenerateContentConfig.Labels` is silently dropped | `fields.go` marks it `ignored` deliberately: it is Google billing metadata ADK sets per agent, and rejecting it would break every ADK Go caller. The comment records the decision. |

Round 23 on the same branch reported eight errors, again with almost no overlap
with rounds 21 and 22. The real ones were already closed by the commits above:
nil options in `ApplyOptions` and its siblings, the CR-terminator lookahead in
the SSE reader, panic recovery in the MCP notification pump, and the missing
success callback on an empty fallback stream. Added the rate-card refresh that
the 2026-10-09 review called for: the current `gpt-5.6-sol` standard, Batch,
Flex, and Fast cards; Batch/Flex/Fast cards for the `gpt-6` families; the exact
`gpt-5.2-pro`, `gpt-5-pro`, `o1-pro`, `o3-pro`, `chat-latest`, and older OpenAI
snapshot IDs; and the OpenAI image-token and duration cards. The remaining
run-23 errors are the stale CI action-version and `.goreleaser.yml` regex claims
below.

| Finding | Why it does not stand |
| --- | --- |
| `.goreleaser.yml` escaped exclamation marks break changelog parsing | `goreleaser check` validates the file with v2.13.0, and the escaped `\!` regexes parse as intended. |
| `mockserver.go` concurrently closes shared notification channels | Every close goes through `closeOnce`, which recovers the already-closed panic; the race detector passes on the suite. |
| `llms.go:92` function pointers do not uniquely identify wrapper values | Function pointers can collide for closures from one code object, which is why `seenKey` only uses the pointer as a fallback key; cycle detection still terminates and the reviewer supplied no concrete collision. |
| Capping a token charge at the bucket burst silently under-enforces the limit | The cap only applies to a single request larger than the bucket, which would otherwise fail permanently; the case is documented in the code and covered by the rate-limit tests. |
| Mode pricing ignores long-context tiers | Known and documented in `resolveModePricing` and pinned by `TestBuiltInModeCardsCarryNoLongContextTier`; the published long-context Batch/Flex columns are not transcribed rather than guessed. |

Rounds 24 through 26 kept rotating the error set (19, 10, and 18 errors) while
the branch tests stayed green. Each round's real findings were reproduced before
fixing. Fixed across those rounds: a split CRLF pair, delivered across a buffer
boundary, that the old lookahead treated as an event separator; a provider
lookup that returned the first empty `ProviderError` instead of the inner one
that named the provider; unexported anonymous structs whose exported fields
`encoding/json` promotes but the schema generator dropped; Z.AI speaker IDs
returned still quoted when the API sends a JSON string; decoded-image size not
enforced after the padded `DecodedLen` estimate; MCP tool and prompt pagination
that only caught an immediately repeated cursor, not a cycling one; a public
circuit-breaker result that classified its probe flag outside the state lock and
leaked a closed-state in-flight mark when a retry released its permit; JSON
extraction that stopped at the first balanced-but-invalid bracket pair in prose;
two direct fields sharing a JSON name producing a schema `encoding/json` then
ignores; self-referential `genai.Schema` values recursing to a stack overflow; a
mixed text/image system message whose parts were reordered during consolidation;
and a llamacpp model listing that ignored the properties call's cancellation.
Also refreshed the verified rate cards for the same providers and extended
`ModelInfo` copy isolation to Anthropic and Synthetic.

| Finding | Why it does not stand |
| --- | --- |
| `Example_messages` and `Example_streaming` fail example validation | `go vet` and `go test` both accept them; a suffix after `Example_` that matches no exported identifier is allowed and only skips the output check. |
| `OpenRouter` drops `Extra["input_references"]` | `videoFor` reads that key into the typed `InputReferences` field, which is what marshals; the merge switch then correctly refuses to overwrite it. |
| `ProcessStream` never writes to its `chunks` parameter | `sender` is constructed over that same channel (`NewStreamSender(ctx, chunks, ...)`), so closing it here and sending through `sender` are the two halves of one channel. |
| `GetWithDefaults` should apply boolean capability defaults | Documented and pinned as numeric-only; a false cannot be distinguished from a deliberate false, so booleans have no default to apply. |
| `llmadk` omits a pending result when a tool call is not immediately followed by a tool message | `repairToolResults` walks the following run of tool messages, and a regression test confirms the placeholder is inserted when a user message intervenes. |
| `.github/workflows/*` reference unreleased `actions/checkout@v7` | The tags exist: the GitHub tags API returns `v7.0.1`/`v7` for checkout and `v7.0.0`/`v7` for setup-go. |
| `scripts/release-decide.sh` uses an undefined `$1` in `count()` | The filter is passed as `count`'s first positional argument, which `jq` receives through shell interpolation of `$1`; a direct invocation of the same function returns the expected count. |
| `pkg/observability/otel_langfuse_test.go` leaves later tests on the wrong recorder | The suite passes with `-shuffle` seeds 1-8 after the global-provider cleanup the file already performs. |

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/release.yml` uses unpublished action major versions | The GitHub tags API returns `v7` for Goreleaser, `v7` for upload-artifact, and `v8` for download-artifact; the workflow refs resolve. |
| `internal/httpclient/security.go` accepts hostnames that resolve to private addresses | Literal-host checks are only the first layer; the shared transport's dial `Control` hook rejects private, loopback, link-local, and metadata addresses after DNS resolution. |
| `llmadk` drops `GenerateContentConfig.Labels` | `Labels` is deliberately classified as ignored request metadata in `llmadk/fields.go`, and `TestUnsupportedFieldsCovered` pins that table. |

| Finding | Why it does not stand |
| --- | --- |
| `errors.go` leaks credentials containing `?` or `#` before `@` | `url.Parse` rejects those forms with `invalid port`, and `errors_url_edge_test.go` already pins that the error string contains no credential prefix. |
| `llmadk` classifies `FunctionCallingConfig.Mode` as translated, so `WithIgnore` cannot suppress `VALIDATED` | The table is a coverage map, not the enforcement point; `config.go` calls `reject(FieldValidatedFunctionCalling, false, "VALIDATED")`, and `TestTranslateFunctionCalling` covers both the rejection and the ignore override. |
| `MetricsMiddleware.Call` returns the mock's `callResp` | `llms.LLM` has no `Call` method, so `MetricsMiddleware.Call` delegates to `GenerateContent` to obtain usage; the mock's `genResp` field is therefore correct. |
| ElevenLabs duration fallback should ignore spacing and audio-event entries | The package doc and `TestTranscribe` deliberately define the fallback as `max(word.End)` over the provider words array, including spacing and audio events. |

| Finding | Why it does not stand |
| --- | --- |
| `.github/ISSUE_TEMPLATE/feature_request.md` starts with indented YAML front matter | The first line is `---` at column zero; the reviewer misread the leading blank line. |
| `capabilities_registry.go` discards boolean defaults in `GetWithDefaults` | The method is documented and tested as numeric-only: an `int` zero is distinguishable from an unset value, while a `bool` false is not. |
| `internal/httpclient/poll.go` reports a late successful callback as a timeout | `TestPoll_DoneOnDeadlinePoll` passes; caller cancellation wins over a callback that ignores its context. |
| `llmadk/fake_test.go` closes a shared `streamDone` channel twice | The fake now guards the close with a `sync.Once`, and the llmadk race suite passes. |
| `llmadk/request_test.go` indexes an empty second tool-call slice | `TestTranslate_IDlessResponsesPairWithinTheirTurn` runs and passes. |
| `pkg/providers/togetherai/image.go` omits the `output_format` JSON tag | The decode struct already carries `json:"output_format"`; the reviewer saw a different field. |

| Finding | Why it does not stand |
| --- | --- |
| `structured_schema_test.go` declares an invalid recursive slice type | `GOWORK=off go test -run TestSchemaFrom_RecursiveSliceDoesNotOverflow` passes; the flagged `recSlice` definition compiles. |
| `llmadk` does not add `set_model_response` to `AllowedFunctionNames` | `toolConfig` filters only the caller's tools and always keeps the synthetic tool; the inline comment and translation tests pin that behavior. |
| Featherless `WithSpeechUsageHandler` is never invoked | `speech.go` reads `c.options.SpeechUsageHandler` on the terminal `speech.audio.done` event, and `TestStreamSpeech_ReportedCharacters` covers it. |

| Finding | Why it does not stand |
| --- | --- |
| `errors.go` leaks credentials for `https://user:se?cret@host/v1` | `url.Parse` returns `invalid port` for that form; the sanitizer only sees the error path, which strips through the last `@`. |
| `llmadk` drops `GenerateContentConfig.Labels` | `Labels` is explicitly ignored metadata in the field-handling table, not a translated request field. |
| `ratelimit_burst_test.go` expects an oversized non-blocking estimate to succeed | The non-blocking path rejects an effective charge above the burst by design; the table case asserts only that `Wait` returns without wedging, and the preceding blocking cases cover debt financing. |

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/release.yml` references unavailable checkout/setup/artifact majors | The GitHub tags API returns `v7` for checkout, `v7` for setup-go, `v7` for Goreleaser and upload-artifact, and `v8` for download-artifact. |
| `errors.go` leaks `user:se` for `https://user:se#cret@host.example.com/v1` | `url.Parse` returns `invalid port ":se"` for that form, so `sanitizeErrorURL` uses the error path, which cuts through the last `@`; the hash/question cases pass in `errors_url_edge_test.go`. |
| `internal/geminiapi/types_test.go` references undefined `testGetWeather` | The constant is declared in `client_test.go`; `GOWORK=off go test ./internal/geminiapi` compiles and passes. |
| `internal/httpclient/client.go` custom dialers can return private remote addresses | A caller-supplied dialer is trusted extension code, not attacker-controlled input; the default transport's dial-time control hook remains the SSRF enforcement point. |
| `llmadk` drops `GenerateContentConfig.Labels` | `Labels` is deliberately classified as ignored metadata in `llmadk/fields.go`. |
| Featherless `WithSpeechUsageHandler` is never wired | `speech.go` invokes `c.options.SpeechUsageHandler` on the terminal `speech.audio.done` event; `TestStreamSpeech_ReportedCharacters` covers it. |
| `scripts/release-decide.sh` reads `.commits` one level too deep | The git-cliff `--context` output is grouped by tag and the script's `count()` probes each group; a direct invocation returns the expected counts. |

Round 44 (2026-10-10) reported 14 errors. The real ones were fixed:
`extractJSONMessage` now normalizes CRLF before splitting events (it reuses
`collectJSONPayloads`); `dispatchOne`/`dispatchNullIDError` send under `t.mu`
and `fail` closes pending channels under the same lock, removing a
send-on-closed-channel race; `DeliverTerminal` re-arms `terminalSent` when the
bounded wait abandons delivery so a later `EnsureTerminal` can retry; Groq
decodes `speaker_id` as JSON instead of trimming quotes; OpenRouter no longer
lists `response_format` as a reserved transcription key while still reading it
from `Extra`; `structured.go` guards a nil `reflect.Type` for interface targets
and matches struct fields case-insensitively the way `json.Unmarshal` does; and
the AGENTS.md provider example now declares `EmbeddingModel` and
`ProviderConfig`.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/ci.yml` references unavailable action majors | `actions/checkout@v7` and `actions/setup-go@v7` are published major tags; the refs resolve. |
| `Makefile` API baseline is missing | `api/v6.txt` is tracked (`git ls-files api/v6.txt` lists it); `apidiff` has its input. |
| `llmadk/config.go` drops `Labels` | `Labels` is deliberately classified as ignored request metadata in `llmadk/fields.go`. |
| `pkg/providers/azure/models.go` references an undefined `Client` | `type Client struct` is declared in `azure.go`; the package builds. |
| Featherless `WithSpeechUsageHandler` is not wired | `speech.go` invokes it on the terminal `speech.audio.done` event; `TestStreamSpeech_ReportedCharacters` covers it. |

Round 46 (2026-10-10) reported 8 errors. Fixed: `CircuitBreaker` now buckets
`inflightClosed` by generation so a pre-`Reset` completion cannot retire a
post-`Reset` request's mark (a plain counter allowed a stale completion to let
the breaker enter half-open while a new request was outstanding);
`BaseProvider.Capabilities` treats the `Media` block as additive, so an explicit
static `ImageGeneration`/`Speech`/... declaration is no longer overwritten by a
false `Media` flag; `EmbeddingResponse` now decodes both the standard `data`
array and the legacy `embeddings` key; `TokenEstimator` clamps a denormal-small
`CharsPerToken` so the estimate cannot overflow to `+Inf` and clamp to 1;
`parseTyped` tries every embedded scalar candidate, not just the first, so a
bool before the intended string does not reject a recoverable response; and the
Mistral transcription test records a handler error for the test goroutine
instead of calling `t.Fatal` inside the HTTP handler.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/ci.yml` references an unpublished `actions/checkout@v7` | `v7` is a published checkout tag and `v7` is published for setup-go; the refs resolve. |
| `capabilities_registry.go` should apply boolean defaults | Documented and pinned as numeric-only: a bool false cannot be distinguished from a deliberate false, so there is no zero value to substitute. |

Round 47 (2026-10-10) reported 10 errors. Fixed: a bare `CircuitBreaker{}` now
initializes its in-flight bucket map lazily instead of panicking on the first
admission; `internal/httpclient` drops every non-safe request header on a
cross-origin redirect, not just the fixed provider credential list, so a secret
in a caller-supplied custom header cannot leak; `validateStructCandidate`
accepts `{}` for a struct with no JSON-visible fields; `assertSchema` compares
with `reflect.DeepEqual` so a nested schema map cannot panic the comparison; the
v3-to-v4 migration guide now describes the v4 field removal accurately instead
of attributing it to v5; and the changelog's two Gemini-default entries are
reconciled.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/release.yml` references unavailable action versions | `v7` is published for checkout, setup-go, goreleaser-action and upload-artifact, and `v8` for download-artifact; the refs resolve. |
| `errors.go` leaks credentials for a URL with `?`/`#` in userinfo | `url.Parse` rejects those forms with `invalid port`, so `sanitizeErrorURL` takes the hand-cut path; the probe shows `user:se` stripped from every form. |
| `pkg/observability/metrics_test.go` populates the wrong mock field | `MetricsMiddleware.Call` delegates to `GenerateContent`, so the fixture's `genResp` is the response the middleware reads; `TestMetricsMiddleware_Call_Success` passes. |
| `ratelimit_bounded_test.go` measures current tokens, not reserved capacity | `TestRecordTokens_HugeCountIsBounded` passes: with a slow refill the reservations are still outstanding when `Tokens()` is read, so the drained delta is visible. |
| `pkg/openaicompat/toolcall_ids.go` loses earlier tool-call rewrites | The inner loop mutates the shared `msg.ToolCalls` copy and writes the same message back, so every rewrite accumulates; a three-call regression test confirms all IDs are distinct and nine-character. |

Round 45 (2026-10-10) reported 7 errors. Fixed: `CircuitBreaker.Reset` now
clears `inflightClosed`, so an outstanding pre-reset request cannot pin the
breaker away from half-open forever; `extractJSONCandidates` no longer descends
into a nested object inside a valid enclosing value (a wrapper object can no
longer satisfy the schema with its nested payload); and the schema generator no
longer flattens an embedded type that implements `UnmarshalJSON`, which would
advertise promoted fields the promoted decoder never populates. Three
provenance tests that read a handler-captured request body now guard it with a
mutex, removing the latent handler/test-goroutine race.

| Finding | Why it does not stand |
| --- | --- |
| `llmadk/model.go` blocks on cancellation because `DrainStream` is synchronous | `DrainStream` starts its own goroutine, so `stopped` returns immediately; the adapter does not wait on the provider's channel. |
| `scripts/release-decide.sh` passes a revision range as the `--context` output path | `--context` is a boolean flag; the range is a positional argument. `scripts/release-decide_test.sh` passes against git-cliff 2.13.1. |
| Featherless `WithSpeechUsageHandler` is not wired | `speech.go` invokes it on the terminal `speech.audio.done` event; `TestStreamSpeech_ReportedCharacters` covers it. |

Round 48 (2026-10-10) reported 12 errors. Fixed: the streaming guide no longer
discards the `Stream` setup error before ranging over the channel; the
embeddings example bounds the printed vector and guards an empty response;
`pkg/providers/anthropic` model listing wraps API errors with
`anthropicapi.WrapError` for consistent provider mapping; `parseTyped` and
`unmarshalCandidate` reject `null` for a pointer target the generated schema
describes as its non-null pointee; `ToolRegistry` copies the caller's `Tool` and
`Function` on `Register`, so a later mutation cannot desynchronize the advertised
name from its handler; base64 embedding responses now decode in both
`llms.Embedding` and the OpenAI-compatible wire type; and the public
`CircuitBreaker.Allow`/`RecordSuccess`/`RecordFailure` API now carries the
admission generation, so a pre-`Reset` completion cannot retire a post-`Reset`
request's in-flight mark.

| Finding | Why it does not stand |
| --- | --- |
| `llmadk/model.go` blocks because `DrainStream` is synchronous | `DrainStream` starts its own goroutine (`streaming.go:212`), so `stopped` returns without waiting on the provider channel. |
| Featherless `WithSpeechUsageHandler` has no effect | `speech.go` invokes it on the terminal `speech.audio.done` event; `TestStreamSpeech_ReportedCharacters` covers it. |
| TogetherAI image request has no `response_format`/`output_format` JSON tags | The anonymous decode struct at `image.go:67` already carries both tags; `json.Unmarshal` into it reads the marshaled body and the image tests pass. |
| `llmadk/envelope.go:118` exposes hidden thought text through `PartMetadata` | Documented design: with `IncludeThoughts=false` Anthropic requires the signed text on replay, so `c` holds it and `docs/design/llmadk.md:303` records the base64 envelope as reachable in session storage and the event stream until `WithEnvelopeKey` (AEAD sealing) lands in v0.2. |

Round 49 (2026-10-10) reported 11 errors. Fixed: `GenerateTyped` rejects a
`null` response for a concrete scalar/slice/array/struct target and recognizes
fields promoted through embedded structs (validation now walks the same schema
promotion the decoder uses); a bare scalar embedded in prose is recovered when
trailing punctuation (`true.`) is not a JSON delimiter; `TokenEstimator`
detects a typed-nil `Tokenizer` and falls back to the heuristic; and
`openaicompat.NewBaseProvider(nil, ...)` providers return
`llms.ErrClientNotConfigured` from `GenerateContent`, `Stream`, and `Embed`
instead of panicking.

| Finding | Why it does not stand |
| --- | --- |
| `errors.go:370` leaks a credential for malformed userinfo like `https://user:se?cret@host` | `url.Parse` rejects those forms with `invalid port ":se"`, so `sanitizeErrorURL` takes the hand-cut path; the probe and `TestAPIError_ErrorRedactsAwkwardCredentialForms` show the userinfo stripped. |
| `.github/workflows/release.yml:22` uses an unavailable `actions/checkout@v7` | `v7` is published for checkout, setup-go, and upload-artifact (verified against the GitHub tags API); the refs resolve. |
| `pkg/observability/metrics_test.go` fixtures must populate `callResp` | `MetricsMiddleware.Call` delegates to `GenerateContent` (the LLM interface has no Call), so the fixture's `genResp` is the response the middleware reads; `TestMetricsMiddleware_Call_Success` and `TestMetricsMiddleware_ContentRecording` pass. |
| Featherless `WithSpeechUsageHandler` is not wired | `speech.go:82` invokes `c.options.SpeechUsageHandler` on the terminal `speech.audio.done` event; `TestStreamSpeech_ReportedCharacters` passes. |
| `llmadk/model.go:177` blocks because `DrainStream` is synchronous | `DrainStream` starts its own goroutine (`streaming.go:212`), so `stopped` returns without waiting on the provider channel. |

Round 50 (2026-10-10) reported 10 errors. Fixed: `GenerateTyped` now enforces
the required list its schema advertises, rejecting a partial response at the top
level and inside nested objects and object slices (previously a missing field
decoded to a zero value and never drove the repair turn); a `*map`/`*any`
target, whose schema strips the pointer to an unconstrained `{}`, accepts `null`
again; and the OpenRouter live usage-accounting test checks `Usage` before
dereferencing its cost. The public `CircuitBreaker.Allow` doc now states that
completions are matched to grants in FIFO order.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/codeql.yml:44` uses an unavailable `actions/checkout@v7` | `v7` is published for checkout (verified against the GitHub tags API); the ref resolves. Same false positive as earlier rounds. |
| `.github/workflows/lint.yml:24` requests an unavailable Go 1.26 | `go1.26.0` through `go1.26.9` are published on go.dev/dl; `go.mod` already declares `go 1.26.0`. |
| `llmadk/config.go:94` silently drops request labels | Documented deliberate classification: `Labels` are Google billing metadata ADK sets per agent, so they are ignored rather than rejected (`llmadk/fields.go`). |
| `llmadk/config.go:182` treats the zero thinking level as unspecified | Intended contract: `ThinkingLevelUnspecified` alone maps to medium, an explicit `ThinkingLevel` beats a budget, and only a truly empty config defers to the budget; `request_test.go` pins all three. |
| `llmadk/model.go:177` blocks because `DrainStream` is synchronous | `DrainStream` starts its own goroutine (`streaming.go:212`); `stopped` returns without waiting on the provider channel. |
| Featherless `WithSpeechUsageHandler` is not wired | `speech.go` invokes `c.options.SpeechUsageHandler` on the terminal `speech.audio.done` event; `TestStreamSpeech_ReportedCharacters` passes. |
| `pkg/middleware/resilience/resilience_circuit_breaker.go:363` public completions can consume the wrong generation permit | The token-less `Allow() bool` pair cannot retroactively re-tag an outstanding grant after a concurrent `Reset`; completions are matched FIFO, which is correct for the documented sequential use. The middleware path (which threads a generation-tagged permit) is unaffected. The doc comment now states the FIFO contract. |

Round 51 (2026-10-10) reported 6 errors. Fixed: `WrapHTTPError` now keeps an
outer `context.Canceled`/`DeadlineExceeded` alongside a nested API error so
retry classification cannot retry canceled work; `anthropicapi.Client.ListModels`
normalizes a non-2xx response with `WrapError` (and the provider no longer
double-wraps); and the resilience retry loop drains its timer non-blockingly on
cancellation instead of risking a hang.

| Finding | Why it does not stand |
| --- | --- |
| `errors.go:373` leaks credentials when a password contains `?` or `#` | `url.Parse` rejects those forms with `invalid port ":se"`, so `sanitizeErrorURL` takes the hand-cut path; the probe shows every form rendered as `https://host/v1`. |
| `pkg/observability/metrics_test.go:151` expects an unset mock response | `MetricsMiddleware.Call` delegates to `GenerateContent` (the LLM interface has no `Call`), so the fixture's `genResp` is the response the middleware reads; `TestMetricsMiddleware_Call_Success` passes. |
| Featherless `WithSpeechUsageHandler` is not connected | `speech.go` invokes `c.options.SpeechUsageHandler` on the terminal `speech.audio.done` event; `TestStreamSpeech_ReportedCharacters` passes. |

Round 52 (2026-10-10) reported 7 errors. Fixed: `GenerateTyped` validates
required fields through nested slices and accepts `null` for a `json.RawMessage`
target; `anthropicapi.Client.GetModel` normalizes its error (the provider no
longer double-wraps); the Langfuse span takes the maximum streamed tool-call
count instead of the last chunk's; a half-open probe admitted before `Reset` is
dropped rather than reopening the fresh breaker; and the MCP stdio writer bounds
a single stdin write so a child that stops draining its pipe cannot park the
writer goroutine forever.

| Finding | Why it does not stand |
| --- | --- |
| Featherless `WithSpeechUsageHandler` is never wired into the client | `speech.go` reads `c.options.SpeechUsageHandler` on the terminal `speech.audio.done` event; `TestStreamSpeech_ReportedCharacters` passes. The reviewer saw the options file alone. |

Round 53 (2026-10-10) reported 8 errors. Fixed: a nested `null` for a
non-nullable field (`{"age":null}` into an `int`) is rejected like the top-level
and extracted-scalar paths, so it drives the repair turn instead of decoding to a
zero value (pointer fields still accept `null`); a transient failure opening a
stream (429/5xx or a transport error, before any chunk is emitted) is retried
like `GenerateContent` rather than surfaced immediately; a cross-origin 307/308
redirect is refused when it would replay the request body to another origin;
and the tools example rejects a non-finite calculator result instead of emitting
`+Inf`/`NaN` as invalid JSON.

| Finding | Why it does not stand |
| --- | --- |
| `errors.go:373` leaves malformed credentials in the sanitized host (`https://user:se#cret@host`) | `url.Parse` rejects that form with `invalid port ":se"`, so `sanitizeErrorURL` takes the hand-cut path and renders `https://host/v1`; the probe and `TestAPIError_ErrorRedactsAwkwardCredentialForms` cover it. |
| `.github/workflows/ci.yml:26` uses an unavailable `actions/checkout@v7` | `v7` is published for checkout (verified against the GitHub tags API); the ref resolves. Same false positive as earlier rounds. |
| `pkg/observability/metrics_test.go:133` must configure `callResp` | `MetricsMiddleware.Call` delegates to `GenerateContent` (the LLM interface has no `Call`), so the fixture's `genResp` is the response the middleware reads; the test passes. |
| `pkg/providers/togetherai/image.go:67` needs JSON tags for the snake_case response fields | The anonymous decode struct already carries the `response_format`/`output_format` tags; `json.Unmarshal` reads the marshaled body and the image tests pass. |

Round 54 (2026-10-10) reported 9 errors. Fixed: `tryAcquire` refuses an oversized
non-blocking token charge instead of clamping it to the burst (clamping admitted
the call while charging only the burst, so repeated oversized requests exceeded
the configured token rate); a success admitted before `Reset` no longer clears
the fresh breaker's failure count (failures stay asymmetric on purpose, still
counting as provider-health evidence); a video result reports no usage at all
for a size with no published unit instead of a nonzero quantity with an empty
unit; the RunPod generic registration forwards `embedding_model`; `HandleAll`
returns an error for a nil `*Response` instead of panicking; and `GenerateTyped`
rejects a property the struct cannot populate, matching the advertised
`additionalProperties: false`.

| Finding | Why it does not stand |
| --- | --- |
| `internal/httpclient/security_wiring_test.go:197` redirects to an `ftp` URL that bypasses `CheckRedirect` | The probe shows the request fails with `redirect blocked: unsupported URL scheme: ftp`; `net/http` does invoke `CheckRedirect`, whose `validateURL` rejects the scheme, and the test passes. |
| `llmadk/envelope.go:118` exposes hidden thought text through `PartMetadata` | Documented design: with `IncludeThoughts=false` Anthropic requires the signed text on replay, so the base64 envelope is reachable in session storage and the ADK event stream until `WithEnvelopeKey` (AEAD sealing) lands in v0.2 (`docs/design/llmadk.md:303`). Recurring false positive. |
| `pkg/providers/anthropic/models_test.go:16` omits Sonnet 4 from the expected pricing | `AssertKnownModelTokenPricingReconcilesWithDefaultPricing` iterates the expected map and the known map independently; it does not require set equality, so the test passes without the entry. |

Round 55 (2026-10-10) reported 9 errors. Fixed: an SSE line of exactly
`maxSSEEventSize` bytes is accepted instead of rejected (the check ran after the
terminating byte, so an exact-max line was wrongly flagged); recursive
validation picks the same field `encoding/json` does when an outer field shadows
an embedded one, so a valid response is no longer rejected; a struct whose decode
is governed by a promoted `UnmarshalJSON` emits an unconstrained schema and skips
required-field checks (the custom decoder consumes the whole object and never
populates the structural fields); the OpenRouter generic registration forwards
`embedding_model`; and the two OpenAI integration handlers report failures with
`t.Errorf` instead of `t.Fatal` on the server goroutine.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/codeql.yml:67` job-level blocks omit `security-events: write` | The gosec and trivy jobs declare no job-level `permissions` block, so the workflow-level `security-events: write` applies and the SARIF upload is authorized. Only `analyze` declares its own block, and it includes `security-events: write`. |
| `llmadk/config.go:94` silently drops `Labels` | Documented deliberate classification: `Labels` are Google billing metadata ADK sets per agent, so they are ignored explicitly (`llmadk/fields.go:79`), not dropped silently. Same false positive as round 50. |
| `pkg/providers/gemini/gemini.go:122` panics on missing `usageMetadata` | `Response.Usage` is a value (`llms.Usage`), not a pointer, so `result.Usage.TotalTokens` cannot panic; the probe with a usage-less response returns estimated usage instead. |

Round 56 (2026-10-10) reported 13 errors. Fixed: the poll backoff saturates at
its limit instead of overflowing a float-to-duration conversion to a negative
value (which fired the timer immediately and could spin);
`WeightedFallbackChain` overrides `AddClient`/`RemoveClient` so the weight list
stays aligned with the embedded chain's entries and `Weights` takes the chain
lock; a zero-value `CircuitBreaker` applies the same failure/reset/half-open
defaults as `NewCircuitBreaker` instead of opening on its first failure; the
OTel middleware drains the provider stream after an error chunk rather than
abandoning its sender; the Groq registry factory forwards `embedding_model` from
`llms.Config`; `GenerateTyped` stops applying its generated required-field and
`additionalProperties` checks when the caller supplied their own JSON Schema;
and required-field presence no longer collapses two fields whose JSON names
differ only by case. The non-blocking token path now finances an oversized
charge as burst plus debt like the blocking path; round 54 had changed it to
reject the whole call, which made a limiter whose burst sits below the estimate
reject every request forever.

| Finding | Why it does not stand |
| --- | --- |
| `errors.go:373` leaks `user:pa` for `https://user:pa?ss@host/path` | `url.Parse` returns an error for that malformed userinfo, so the sanitizer takes the hand-cut path and strips through the last `@`, yielding `https://host/v1`. Same false positive as rounds 49, 51, and 53. |
| `.github/workflows/ci.yml:14` requests an unavailable Go 1.26 | Go 1.26.x is published and `go.mod` declares `go 1.26.0`; the shared version is intentional. Recurring false positive. |
| `.github/workflows/release.yml:22` references unpublished action majors | `actions/checkout@v7`, `actions/setup-go@v7`, `actions/upload-artifact@v7`, and `actions/download-artifact@v8` are published. Recurring false positive. |
| `docs/guides/resilience.md:374` is a broken example that ignores the constructor error | The tip names `NewWeightedFallbackChain` and `MustNewWeightedFallbackChain` in prose; it is not a code snippet, assigns no result, and nothing compiles. |
| `pkg/providers/featherless/featherless-options.go:107` never invokes `SpeechUsageHandler` | `speech.go` reads the handler on the terminal `speech.audio.done` event, and `TestStreamSpeech_ReportedCharacters` covers it. Recurring false positive. |

Round 57 (2026-10-10) reported 5 errors. Fixed: the OTel, Langfuse, metrics,
and logging stream wrappers now drain the provider stream after an error chunk,
not just on a send failure (the provider sender could otherwise stay parked);
`JSONLogger` and `SlogLogger` report a nil `*LogEntry` through the sink instead
of dereferencing it into a panic; `GenerateTyped` no longer applies the generated
non-null shape to a response when the caller supplied a schema that permits
null; and `settleTokenDebt` checks the wait context between installments, so a
high token rate cannot charge a huge debt in one uninterruptible burst before
the caller's cancellation is observed.

| Finding | Why it does not stand |
| --- | --- |
| `pkg/providers/elevenlabs/speech_test.go:20` has 29 names but requires 28 | The fixture names 28 formats and `supportedFormats` has 28; `TestOutputFormat` passes. |
| `pkg/providers/elevenlabs/transcribe.go:107` never connects a speech usage handler | ElevenLabs has no `WithSpeechUsageHandler` option at all; the finding describes Featherless's option, which `speech.go` does invoke on the terminal usage event. |

Round 58 (2026-10-10) reported 7 errors. Fixed: the api baseline is regenerated
for the exported surface this branch added; a stream that delivered its terminal
chunk is no longer logged or recorded as canceled when the caller's context is
canceled by the time the wrapper goroutine drains (OTel, Langfuse, metrics, and
logging); and a struct whose decode is governed by its own `json.Unmarshaler`
now emits an unconstrained schema and skips the reflected required-field checks,
matching the promoted-embedding case.

| Finding | Why it does not stand |
| --- | --- |
| `Makefile:159` passes apidiff arguments in the wrong order | `apidiff -m` accepts a file of export data as OLD and a module path as NEW; `apidiff -m api/v6.txt github.com/nocturnium/llm-go-sdk/v6` returns the intended diff, and `make apidiff` fails only because the baseline was stale. |
| `internal/httpclient/multipart_test.go:81` uses a valid method token | `net/http` rejects `"bad method"` at `http.NewRequestWithContext` (the space is not a token character), so `DoMultipart` returns the error the test asserts. `TestClient_DoMultipart_Errors` passes. |
| `pkg/providers/featherless/featherless-options.go:107` never invokes the handler | `speech.go` calls `c.options.SpeechUsageHandler` on the terminal event; recurring false positive (rounds 50, 52–56). |
| `pkg/providers/mistral/transcription.go:74` ignores `TranscribeOptions.ResponseFormat` | `llms.TranscribeOptions` has no `ResponseFormat` field; a caller can only set it through `Extra`, which the code reads into `format` and rejects for non-JSON. |
| `structured.go:453` nested pointer fields accept null despite a non-null schema | Pointer fields accept null by design (round 53, documented in the code and `docs/nitpick-adjudication.md`): a nil pointer is the field's natural zero, and the generated schema advertises the pointee because OpenAI strict mode rejects nullable unions. |

Round 59 (2026-10-10) reported 7 errors. Fixed: `CostTracker.RecordMode`
computes its cost and applies its usage under one write lock, so a concurrent
`Reset` can no longer land between the two and then be undone by the pending
record.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/release.yml:22` references unpublished action majors | Same recurring false positive as rounds 54–58: `checkout@v7`, `setup-go@v7`, `upload-artifact@v7`, `download-artifact@v8`, and `goreleaser-action@v7` are published. |
| `internal/httpclient/multipart.go:83` panics on a typed nil target | The `target == nil` guard already runs before the assignment in both cases (the targets are re-checked inside the type switch). |
| `pkg/observability/terminal_chunk_test.go:38` closes a shared `drained` channel twice | The fixture has no `drained` field; the stale comment was removed when the test was written. `TestStreamWrappers_DrainAfterErrorChunk` passes. |
| `pkg/providers/featherless/featherless-options.go:107` never invokes the handler | `speech.go` calls it on the terminal event; recurring false positive (rounds 50–58). |
| `structured.go:181` returns the first quoted prose value for a string target | Inherently ambiguous: when the content is not valid JSON and contains several quoted strings, no rule can know which is intended. The documented behavior takes the first candidate that decodes into `T`, and `TestParseTyped_TriesEveryScalarCandidate` pins the useful case (a non-string scalar before the intended string). |

Round 60 (2026-10-10) reported 8 errors. Fixed: a struct with a custom
`json.Unmarshaler` (its own or promoted) now emits an unconstrained schema *and*
accepts JSON null, because `{}` places no type restriction — the null check had
contradicted the advertised schema.

| Finding | Why it does not stand |
| --- | --- |
| `.goreleaser.yml:72` escapes `!` in a way Go's regexp rejects | Go's regexp accepts `\!` (it is a literal `!`); `goreleaser check` validates the file, and the pattern parses. |
| `internal/httpclient/client.go:221` custom `DialContext` bypasses the SSRF hook | The wrapper around a caller-provided `DialContext` calls `validateConnRemoteAddr` on the returned connection, which rejects a private peer — the custom dialer cannot land on a private address. `security_wiring_test.go` covers it. |
| `internal/httpclient/retry_replay_test.go:118` reads `attempts` unsynchronized | The client's `DoWithRetry` returns only after the handler has written its response, so the read happens after the write; `go test -race -count=20` on the file passes. |
| `llmadk/Makefile:3` uses the wrong module path | The nested module's `go.mod` declares `github.com/nocturnium/llm-go-sdk/llmadk`, and `apidiff` compares that path; `make -C llmadk apidiff` passes. |
| `resilience_circuit_breaker.go:402` a stale public permit discards a post-reset outcome | Documented FIFO contract for the token-less `Allow()`/`Record*` pair (round 52 adjudication); the middleware path threads a generation-tagged permit and is unaffected. |
| `pkg/providers/groq/options_coverage_test.go:334` relies on the environment key | The test passes `APIKey: "test-key"` explicitly, so a clean CI environment runs it. |
| `structured.go:444` nested pointer fields accept null despite a non-null schema | Pointer fields accept null by design (rounds 53 and 58); a nil pointer is the field's natural zero and the schema advertises the pointee because OpenAI strict mode rejects nullable unions. |

Round 61 (2026-10-10) reported 6 errors. Fixed: Azure `ListModels` now calls the
account-level `/openai/models` route instead of appending `/models` to the
deployment-scoped base URL, and `settleTokenDebt` reserves from the token bucket
before it deducts the installment from `tokenDebt` (under the same lock), so a
concurrent refund in `RecordTokens` can no longer see debt that was already
removed but not yet charged.

| Finding | Why it does not stand |
| --- | --- |
| `imagegen_test.go:36` `t.Fatal` is taken for a valid generator | `AsImageGenerator` returns a non-nil interface with ok=true for the stub, so `(g == nil) == want` is `false == true` and the branch is not taken; `TestImageCapabilities` passes. |
| `pkg/observability/metrics_test.go:136` mock `Call` ignores `genResp` | `NewMetricsMiddleware`'s `Call` goes through `GenerateContent`, which returns `genResp`; `TestMetricsMiddleware_Call_Success` passes. |
| `pkg/providers/featherless/featherless-options.go:107` never invokes the handler | `speech.go` calls it on the terminal event; recurring false positive (rounds 50–60). |
| `structured.go:323` matches property names case-insensitively | Deliberate: `json.Unmarshal` matches field names case-insensitively, so the validator mirrors the decoder (`TestParseTyped_CaseInsensitiveFieldMatch`). |

Round 62 (2026-10-10) reported 10 errors. Fixed: section 6.4 of
`docs/design/llmadk.md` now says the reasoning envelope lives in
`PartMetadata`, pointing at the section 14 note that supersedes it.

| Finding | Why it does not stand |
| --- | --- |
| `Makefile:23` builds a missing `./cmd` package | `cmd/main.go` exists and `make build` uses it; the reviewer saw a partial tree. |
| `llmadk/config.go:182` empty `ThinkingConfig{}` skips the medium default | Deliberate: only an explicit UNSPECIFIED level means medium; a zero-value config leaves reasoning unset, pinned by the "zero-value level alone leaves reasoning unset" case in `request_test.go`. |
| `llmadk/fields.go:163` `FunctionCallingConfig.Mode` should be rejected | It is translated and VALIDATED is rejected in `config.go` (`FieldValidatedFunctionCalling`); the llmadk tests pass. |
| `pkg/mcp/client.go:495` cleanup drops queued progress | `dispatchNotification` captures the per-call handler into the queued closure before cleanup can run, so deleting the map entry does not affect it. |
| `pkg/mcp/stdio_respond_test.go:28` writer cannot honor the context | `respond` bounds the wait on the writer by the context; `TestStdioRespond_HonorsContext` passes in 50 ms. |
| `pkg/providers/featherless/featherless-options.go:107` never invokes the handler | `speech.go` calls it on the terminal event; recurring false positive. |
| `pkg/providers/groq/options_coverage_test.go:333` lacks an API key | The test passes `APIKey: "test-key"` in `llms.Config`. |
| `structured.go:323` case-insensitive property match | Mirrors `json.Unmarshal` by design (rounds 55, 56, 61). |
| `structured.go:450` nested pointers accept null | Pointer fields accept null by design (round 53). |

Round 63 (2026-10-10) reported 6 errors. Fixed: `APIError.Is` returns false for
a nil target (an unclassified error used to match `errors.Is(err, nil)`); a
self-referential embedded struct (`type Node struct{ *Node; Value string }`) no
longer adds a phantom required `Node` property to the schema, matching how
`encoding/json` skips a cyclic embed; and the structured Langfuse output omits
`usage` when the response carried none instead of reporting zeros.

| Finding | Why it does not stand |
| --- | --- |
| `ratelimit.go:574` refund can exceed what was charged for an oversized estimate | With burst 100, estimate 5000 and actual 10, the 4900 of debt is cancelled first, leaving a refund of 90 against the 100 charged to the bucket, so the net charge is 10. `TestRecordTokens_RefundCappedAtWhatWasCharged` covers it. |
| `pkg/observability/metrics_test.go:136` fixture lacks `callResp` | Recurring false positive (round 61); the test passes. |
| `pkg/providers/featherless/featherless-options.go:107` never invokes the handler | Recurring false positive; `speech.go` calls it on the terminal event. |

Round 64 (2026-10-10) reported 12 errors. Fixed: `settleTokenDebt` removes
partially-applied credit from the debt along with the reserved installment (the
credit used to be spent but the debt it covered stayed on the books, billing it
twice); the Anthropic tool-result decoder trims leading whitespace before
choosing the string or array form; the llmadk tool bridge passes any
string-keyed map result through as its fields instead of wrapping it in
`{"result": ...}`; and a Gemini model test reports an unexpected path with
`t.Errorf` instead of `t.Fatalf` from the handler goroutine.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/release.yml:22` action majors are unpublished | Recurring false positive; those majors are published. |
| `Makefile:155` `make ci` lacks the API baseline | `api/v6.txt` is tracked and `make apidiff` passes (round 58). |
| `llmadk/config.go:94` labels silently discarded | Deliberate and listed in the field table as `ignored` (`fields.go`): they are billing metadata ADK sets on every agent, so rejecting them would break ADK. |
| `llmadk/config.go:182` empty `ThinkingConfig` skips the medium default | Recurring (round 62); a zero-value config leaves reasoning unset by design. |
| `resilience_circuit_breaker.go:402` FIFO public permits misattribute completions | Documented contract for the token-less `Allow()`/`Record*` pair (rounds 52, 61). |
| `featherless.go:71` speech usage handler discarded | Recurring false positive; `speech.go` reads `c.options.SpeechUsageHandler` directly. |
| `structured.go:450` pointer fields accept null | By design (round 53). |
| `tokens.go:58` `isNilInterface` undefined | Defined in `llms.go:120`; the package builds. |

Round 65 (2026-10-10) reported 6 errors. Fixed: the direct-call snippet in
`docs/guides/mcp.md` no longer declares an unused variable; the MCP handshake
decodes server capabilities into a scratch value so a malformed map cannot leave
a half-populated view; and `RunTools` returns an error for a nil registry
instead of panicking.

| Finding | Why it does not stand |
| --- | --- |
| `pkg/providers/featherless/featherless-options.go:107` never invokes the handler | Recurring false positive; `speech.go` reads `c.options.SpeechUsageHandler` directly. |
| `structured.go:323` case-variant properties | Mirrors `json.Unmarshal` by design (rounds 55, 56, 61, 62). |
| `structured.go:450` pointer fields accept null | By design (round 53). |

Round 66 (2026-10-10) reported 6 errors. Fixed: the CodeQL, gosec and trivy
jobs skip fork pull requests, whose read-only token cannot upload SARIF;
`MetricsMiddleware.Call` and `GenerateContent` return `ErrIncompleteResponse`
for a `(nil, nil)` provider result instead of panicking; and the Anthropic HTTP
test's failure message names the model it actually compares against.

| Finding | Why it does not stand |
| --- | --- |
| `pkg/providers/anthropic/client_http_test.go:41` asserts the wrong model | The assertion compares against `testClaudeSonnet` itself, so the test passed; only the message text was stale. |
| `llmadk/config.go:182` empty `ThinkingConfig` skips the medium default | Recurring (rounds 62, 64); a zero-value config leaves reasoning unset by design. |
| `pkg/providers/featherless/featherless-options.go:107` never invokes the handler | Recurring false positive; `speech.go` reads `c.options.SpeechUsageHandler` directly. |
| `structured.go:330` case-insensitive property match | Mirrors `json.Unmarshal` by design (rounds 55, 56, 61, 62, 65). |

Round 67 (2026-10-10) reported 11 errors. Fixed: `GetErrorDetails` follows
`Unwrap() []error` (the `%w: %w` wrapper `WrapHTTPError` builds for a canceled
request) to the innermost cause instead of reporting the wrapper itself.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/ci.yml:26` action majors unpublished | Recurring false positive; `checkout@v7`, `setup-go@v7` and `upload-artifact@v7` are published. |
| `internal/httpclient/poll.go:56` success loses to cancellation | The documented precedence covers the policy's own deadline. Caller cancellation winning over a late success is deliberate, as the comment in the code says. |
| `internal/httpclient/security.go:181` hostnames resolving to private IPs accepted | `ValidateURL` is the literal-host layer; the transport's dial `Control` hook rejects private addresses at connect time,, which avoids the DNS rebinding gap. |
| `llms_test.go:679` capability test always fails | The final check calls the `Supports*` helpers, which test interface implementation, not the flag; the test passes. |
| `modes.go:144` batch cache rates should be derived from input ratios | Halving the published standard cache rate is the documented stacking of the batch discount on the cache price; cards whose cache rates differ from the 0.1x and 1.25x defaults (for example `claude-fable-5-1`) keep their own published multipliers. |
| `videogen.go:237` cancellation can land between the check and `ResultFn` | Any check-then-call has that window; the guarantee is that a call already canceled when `Wait` returns does not invoke `ResultFn`. |
| `featherless-options.go:107`, `structured.go:323`, `structured.go:450`, `tokens.go:58` | Recurring false positives: handler is wired in `speech.go`; case-insensitive and null-pointer matching mirror `encoding/json`; `isNilInterface` is defined in `llms.go`. |

Round 68 (2026-10-10) reported 7 errors. Fixed (documentation): `CostTracker`
states that it must be built with `NewCostTracker`, and `RecordTokens` states
that it settles requests admitted with the configured estimate, since the limiter
keeps no per-request charge for a `WaitN` call with a different explicit count.

| Finding | Why it does not stand |
| --- | --- |
| `turn.go:20` returns the system message index for [user, assistant, system] | The loop stops at the system message with `i == 3`, so the function returns 3 (`len(messages)`), as documented. |
| `llmadk/testdata/fixtures/zai/tool_loop.json:4` fixture model disagrees with responses | The field is the model the scenario requested (`glm-4.7`, as in the live-gate record); the provider answered with `glm-5.3-flash`. The harness does not key anything off the response model. |
| `llmadk/config.go:94` labels silently discarded | Deliberate and listed as `ignored` in the field table (round 64). |
| `featherless-options.go:107`, `structured.go:450` | Recurring false positives (handler wired in `speech.go`; null pointers mirror `encoding/json`, round 53). |

Round 69 (2026-10-10) reported 6 errors. Fixed: `llmadk/README.md` told users to
`go get github.com/nocturnium/llm-go-sdk/v6/llmadk`, but the nested module is
declared as `github.com/nocturnium/llm-go-sdk/llmadk` in `llmadk/go.mod`.

| Finding | Why it does not stand |
| --- | --- |
| `errors.go:377` `https://user:secret?x@example.com/path` keeps the credential | Probed: `sanitizeErrorURL` returns `https://example.com/path` for that URL, for the `#` variant, and for a `?` inside the password. |
| `llmadk/Makefile:3` should use the v6 module path | The nested module is `github.com/nocturnium/llm-go-sdk/llmadk` (`llmadk/go.mod`), so the Makefile path is correct; the README was the stale one. |
| `llmadk/config.go:182` empty `ThinkingConfig` skips the medium default | Recurring (rounds 62, 64, 66); a zero-value config leaves reasoning unset by design. |
| `pkg/mcp/stdio.go:262` response retains bufio's buffer | `readBoundedLine` does `append(line, fragment...)` onto a nil slice, which copies the fragment, so no slice aliases the reader's buffer. |
| `structured.go:323`, `structured.go:450` | Recurring: case-insensitive matching and null pointers mirror `encoding/json` (rounds 53, 55). |

Round 70 (2026-10-10) reported 7 errors. Fixed: `ProviderFromError` searches
every branch of a multi-wrapped error (`%w: %w`, `errors.Join`) instead of
stopping at the first chain; bare scalar extraction in `GenerateTyped` requires
word boundaries, so `falsehood`, `trueish` and `v5` no longer parse as
`false`, `true` and `5`; and the release workflow's tag cleanup no longer
swallows a failed `git push` of the tag deletion, so a leftover baseline tag
fails the job visibly.

| Finding | Why it does not stand |
| --- | --- |
| `llmadk/config.go:57` `strings.EqualFold` on `genai.Modality` does not compile | `genai.Modality` is a string type and the call converts it; `go build ./...` succeeds in `llmadk`. |
| `llmadk/config.go:182` empty `ThinkingConfig` skips the medium default | Recurring (rounds 62, 64, 66, 69); a zero-value config leaves reasoning unset by design. |
| `pkg/providers/groq/options_coverage_test.go:334` lacks an API key | The test passes `APIKey` in `llms.Config`; it passes with no Groq key in the environment. |
| `featherless-options.go:107`, `structured.go:450` | Recurring false positives (handler wired in `speech.go`; null pointers mirror `encoding/json`). |

Round 71 (2026-10-10) reported 6 errors, all false positives; no code change.
`TestClient_Transcribe_NonStringResponseFormatRejected` pins the OpenRouter case.

| Finding | Why it does not stand |
| --- | --- |
| `llmadk/fields.go:163` VALIDATED function calling classified as supported | Recurring (round 62): `Mode` is translated, and `config.go` rejects VALIDATED through `FieldValidatedFunctionCalling`. |
| `pkg/providers/featherless/featherless-options.go:107` never invokes the handler | Recurring; `speech.go` calls `c.options.SpeechUsageHandler` on the terminal event. |
| `pkg/providers/openrouter/transcription.go:49` non-string `response_format` bypasses validation | `format, _ = value.(string)` yields `""` for a non-string value, and `""` is neither `json` nor `verbose_json`, so the request returns `ErrInvalidParameters` before any field is serialized. |
| `scripts/release-decide.sh:74` jq program contains an uninterpolated `$1` | The program is in double quotes, so the shell substitutes `$1` before jq runs; the single-quoted strings are the arguments to `count`. |
| `structured.go:181` returns the first quoted prose value | Inherently ambiguous with several quoted strings (round 59 entry). |
| `structured.go:450` pointer fields accept null | By design (round 53). |

Round 72 (2026-10-10) reported 10 errors. Fixed: Groq transcription refuses
word timestamps combined with `response_format` `text` or `json` (Groq only
honors timestamp granularities with `verbose_json`), and `RunTools` returns an
error when a provider returns a nil response with no error, instead of
panicking on `resp.Content`.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/ci.yml:14` Go 1.26 is not an available toolchain | `go.mod` requires go 1.26.6; the module builds and tests with it locally. |
| `.github/workflows/ci.yml:26` action majors unpublished | Recurring false positive; the `@v7` majors are published. |
| `llmadk/config.go:94` labels silently dropped | Deliberate and listed as `ignored` in the field table (rounds 64, 68). |
| `options.go:439` mismatched `toolTypeFunction` comparison does not compile | The package builds and its tests pass; the comparison is between values of the same type. |
| `pkg/providers/togetherai/image.go:81` `ResponseFormat` never decoded | The field is tagged `json:"response_format"`, and the image tests pass with the default `base64`. |
| `pkg/providers/togetherai/media_transport.go:32` produces `/v1/v2/videos` | `video.go` strips the trailing `/v1` segment from the base path before joining `/v2/videos`; the proxy-prefix video test covers it. |
| `scripts/release-decide.sh:74` uninterpolated `$1` | Recurring (round 71); the jq program is double-quoted, so the shell expands `$1`. |
| `structured.go:450` nested pointers accept null | By design (round 53). |

Round 73 (2026-10-10) reported 6 errors. Fixed: `releasePermit` ignores a
half-open probe from before a `Reset` instead of decrementing the new
generation's probe count; `RetryPolicy.GetDelay` clamps to `MaxDelay` after the
float conversion, so a `MaxDelay` just below `MaxInt64` is never exceeded by
rounding; and `RunTools` returns an error for a nil `LLM`.

| Finding | Why it does not stand |
| --- | --- |
| `pkg/providers/synthetic/models_test.go:153` catalog has eight code models, test wants nine | `TestListModels` passes; the catalog has ten `ModelTypeCode` entries. |
| `structured.go:171` scalar extraction returns the first quoted prose value | Inherently ambiguous with several quoted strings (round 59 entry); no rule can know which is intended. |
| `featherless-options.go:107` never invokes the handler | Recurring; `speech.go` calls it on the terminal event. |

Round 74 (2026-10-10) reported 8 errors. Fixed: `LoggingMiddleware` now applies
the call options to each `LogEntry` (`PopulateFromCallOptions`) for `Call`,
`GenerateContent` and `Stream`, so trace overrides and explicit request
parameters appear in request, response and error entries; the method existed
and was tested but was never called.

| Finding | Why it does not stand |
| --- | --- |
| `internal/httpclient/ssrf_dial_test.go:197` redirect test expects `CheckRedirect` for an FTP target | The test passes; the assertion accepts the unsupported-scheme error `net/http` returns before `CheckRedirect` runs. |
| `pkg/mcp/stdio_respond_test.go:28` writer cannot honor the context | Recurring (round 62); `respond` bounds the wait, and `TestStdioRespond_HonorsContext` passes in 50 ms. |
| `pkg/observability/metrics_test.go:133`, `:610` mock `Call` returns unset `callResp` | Recurring (rounds 61, 63); `MetricsMiddleware.Call` goes through `GenerateContent`, which returns `genResp`. Both tests pass. |
| `pkg/providers/featherless/featherless.go:71` handler ignored | Recurring; `speech.go` reads `c.options.SpeechUsageHandler` directly. |
| `structured.go:450` pointer fields accept null | By design (round 53). |
| `structured_test.go:938` `reflect.StructOf` rejects an unnamed anonymous field | The test passes: Go accepts an anonymous field of an unnamed struct type when the type has no methods to promote. |

Round 75 (2026-10-10) reported 7 errors. Fixed: the logging, metrics and
Langfuse OTel middlewares return `ErrIncompleteResponse` when a provider's
`Stream` returns a nil channel with no error (the plain OTel middleware already
did), instead of returning a channel that never closes.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/codeql.yml:45` unpublished action majors | Recurring false positive (rounds 50–72); `checkout@v7`, `setup-go@v7` and `codeql-action@v4` are published. |
| `pkg/middleware/resilience/ratelimit_refund_test.go:23` test never records the charge | The test passes. Draining the bucket with `ReserveN` is deliberate: it leaves the bucket empty, so the refund clamp under test is the charged-amount clamp, not the full-bucket clamp (the comment says so). |
| `structured.go:450`, `featherless.go:71` and similar | Recurring false positives listed in earlier rounds. |

Round 76 (2026-10-10) reported 6 errors, all false positives; no code change.

| Finding | Why it does not stand |
| --- | --- |
| `options.go:439` mismatched types compared with `ToolTypeFunction` | Recurring (rounds 72, 74); the package builds and its tests pass. |
| `pkg/middleware/resilience/ratelimit.go:561` `RecordTokens` ignores a refused `ReserveN` | `ReserveN` waits without a horizon (`InfDuration`), so it refuses only a count above the burst, and every installment is `min(extra, burst)`. There is no delay-based refusal to lose debt to. |
| `pkg/observability/logging.go:579` incomplete stream logged as a response | `EnsureTerminal` delivers a clean `Done` to the consumer for a source that closes without a terminal chunk, so the consumer sees success and the log matches it. The OTel, metrics and Langfuse wrappers do the same. |
| `featherless-options.go:107` handler never wired | Recurring; `speech.go` calls it on the terminal event. |
| `pkg/providers/togetherai/media_transport.go:32` `/v1/v2/videos` | Recurring (round 72); `TestGenerateVideo` in `media_review_test.go` covers the `/v1` suffix and expects `/v2/videos`. |
| `structured.go:450` pointer fields accept null | By design (round 53). |

Round 77 (2026-10-10) reported 5 errors, all false positives; no code change.

| Finding | Why it does not stand |
| --- | --- |
| `llmadk/config.go:182` empty `ThinkingConfig` skips the medium default | Recurring (rounds 62, 64, 66, 69, 70); a zero-value config leaves reasoning unset by design. |
| `featherless-options.go:107` handler never wired | Recurring; `speech.go` calls it on the terminal event. |
| `pkg/providers/openrouter/video.go:222` `input_references` never serialized | The typed `InputReferences` field is tagged `json:"input_references,omitempty"`; the reserved-key switch only stops `ExtraBody` from overriding it. The video test asserts the field reaches the wire. |
| `pkg/providers/togetherai/media_transport.go:32` `/v1/v2/...` | Recurring (rounds 72, 76); `video.go` strips the `/v1` suffix before joining the v2 route. |
| `scripts/release-decide.sh:69` range parsed as an unexpected argument | In git-cliff `--context` takes no value and the range is a positional argument, so the call is valid. Not run locally: `git-cliff` is not installed in this environment, and CI installs it. |

Round 78 (2026-10-10) reported 3 errors, all false positives; no code change.

| Finding | Why it does not stand |
| --- | --- |
| `featherless-options.go:107` handler never invoked | Recurring; `speech.go` calls `c.options.SpeechUsageHandler` on the terminal event (`TestStreamSpeech_ReportedCharacters`). |
| `reasoning.go:185` `reflect.Value.Pointer` panics on a map | `Pointer` supports `Map` kinds. Probed: `cloneMetadataValue` clones nested `map[string]any`, a named map type and a map inside a slice without panicking. |
| `structured.go:771` `SchemaFrom[Node]` with `Next *Node` recurses forever | Probed: it returns `{"next":{},"v":{"type":"string"}}`. The pointer-field path goes through `schemaForType`, which sets `seen` and degrades the cycle to an unconstrained `{}`. |

Round 79 (2026-10-10) reported 7 errors, all false positives; no code change.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/llmadk.yml:121` passes a file to `apidiff` as a package | `apidiff` takes a baseline export file as its first argument as well as a package path, and `api.txt` is that export; `make -C llmadk apidiff` prints "Public API matches api.txt". |
| `llmadk/request_test.go:176` synthesized IDs are random | `synthesizeID` hashes a seed of message index, part index and name through `NineCharToolCallID`, so re-translating the same history gives the same IDs (the function's comment says so). `NewToolCallID` is only used for responses. |
| `llmadk/testdata/fixtures/zai/tool_loop.json:4` fixture model differs | Recurring (round 68); the field is the requested model, not the one the provider answered with. |
| `llms_test.go:396` `toolTypeFunction` undeclared | Recurring (rounds 72, 74, 76); the package builds and its tests pass. |
| `resilience_circuit_breaker.go:506` Reset leaves stale public permits | Documented FIFO contract for the token-less `Allow()`/`Record*` pair (rounds 52, 61, 64). |
| `featherless.go:62` handler never wired | Recurring; `speech.go` calls it on the terminal event. |
| `pkg/providers/togetherai/image.go:67` fields untagged | Recurring (round 72); the fields are tagged and the image tests pass. |

Round 80 (2026-10-10) reported 6 errors. Fixed (documentation): the comment on
`maxSSEEventSize` now says the limit counts wire bytes including field names
such as `data: `, so a payload of exactly 4 MiB does not fit.

| Finding | Why it does not stand |
| --- | --- |
| `Makefile:96` `gofmt -l .` cannot take a directory | `gofmt` walks a directory argument recursively; the same command is a required gate in this repository and prints nothing. |
| `batch.go:368` in-flight cancellation returned as success | Deliberate and commented: requests that started record their own context error in `Results`, and only a short `Results` signals unstarted work. `TestConcurrentBatcher_ContextCancellation` passes. |
| `structured.go:868` tagged unexported embedded struct advertised but never decoded | Probed: `encoding/json` populates `struct{ inner \`json:"in"\` }` from `{"in":{"a":"x"}}`, so the schema matches the decoder. |
| `featherless-options.go:107` handler never wired | Recurring; `speech.go` calls it on the terminal event. |
| `structured.go:450` pointer fields accept null | By design (round 53). |

Round 81 (2026-10-10) reported 5 errors. Fixed: `RateLimiter.Reset` now advances
a generation, and a `Wait` admitted before the reset drops its late debt and
credit adjustments instead of writing them into the fresh state;
`settleTokenDebt` stops when the limiter it holds has been replaced.

| Finding | Why it does not stand |
| --- | --- |
| `internal/httpclient/security.go:267` IPv4-mapped multicast bypasses the block | Probed: `::ffff:224.0.0.1` is rejected as link-local, `::ffff:239.255.255.250` as multicast; `net.IP.IsMulticast` unwraps mapped addresses. |
| `pkg/middleware/resilience/ratelimit.go:658` lock ordering in `TokensRemaining` | The order is always `rl.mu` then the limiter's own lock, and the limiter never calls back into `rl.mu`, so the two cannot deadlock. |
| `featherless-options.go:107`, `structured.go:450` | Recurring false positives (handler wired in `speech.go`; null pointers by design, round 53). |

Round 82 (2026-10-10) reported 10 errors. Fixed: the Langfuse OTel middleware
records token metrics and span attributes when a provider reports prompt or
completion tokens but leaves `TotalTokens` at zero (it gated on the total
alone); and the `httpclient.Poll` doc comment now says what the code does, that
the policy timeout loses to a successful callback but the caller's own
cancellation does not.

| Finding | Why it does not stand |
| --- | --- |
| `.github/workflows/codeql.yml:45` unpublished action majors | Recurring false positive; the `@v7` and `@v4` majors are published. |
| `docs/design/llmadk.md:254` `WithWebSearch` permits unsupported search | Deliberate and documented in that row: the option is the caller's statement that the provider searches, and rejection without it is the default (10x-7). |
| `internal/httpclient/poll.go:56` success rejected after the caller's deadline | Deliberate: the code comment says an abandoned operation must not look successful. Only the policy timeout yields to a late success, which the doc now states. |
| `llmadk/testdata/fixtures/anthropic/fallback_into_openai.json:12` holds only an Anthropic exchange | The scenario runs the Anthropic leg of a fallback and the OpenAI leg comes from the `openai-responses` fixture; `scenarios_test.go` lists both families. |
| `pkg/providers/mistral/transcription.go:63` keyterms sent as file parts | A `MultipartFile` with no `Filename` is emitted as a plain form field (`multipart.go`: "empty emits a plain form field"); `media_review_test.go` asserts `context_bias[]` arrives as repeated values. |
| `pkg/providers/runpod/models.go:113` Qwen 2.5 7B context 131072 | Qwen's model card lists 131,072 tokens for Qwen2.5-7B-Instruct (YaRN-scaled, 32,768 by default). The catalog records the model's maximum; whether a given RunPod worker enables YaRN is not something this table can know, and the entry's description says 131K. |
| `pkg/providers/togetherai/media_transport.go:32` `/v1/v2/videos` | Recurring (rounds 72, 76, 77); `video.go` strips the `/v1` suffix. |
| `reasoning.go:201` cyclic struct clone recurses forever | Probed: cloning `n := &node{}; n.Next = n` returns; the pointer is memoized before its target is copied. |
| `structured.go:450` nested pointers accept null | By design (round 53). |
