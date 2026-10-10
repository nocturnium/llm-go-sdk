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
