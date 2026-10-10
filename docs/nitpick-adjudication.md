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
