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
