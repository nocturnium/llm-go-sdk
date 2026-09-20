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
