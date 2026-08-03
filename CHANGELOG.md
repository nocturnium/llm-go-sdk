# Changelog

All notable changes to this project will be documented in this file.

## [6.2.0] - 2026-08-03

### Features

- **mcp:** Serve server-initiated requests with bounded, non-blocking dispatch ([#52](https://github.com/nocturnium/llm-go-sdk/issues/52))

## [6.1.0] - 2026-08-03

### Features

- Request-mode pricing for batch, flex and fast lanes ([#49](https://github.com/nocturnium/llm-go-sdk/issues/49))

## [6.0.1] - 2026-08-03

### Bug Fixes

- **mcp:** Route server-initiated requests instead of misdelivering them as responses ([#48](https://github.com/nocturnium/llm-go-sdk/issues/48))

## [6.0.0] - 2026-08-03

### Features

- V6.0.0 — long-context pricing tiers + current-generation model refresh ([#46](https://github.com/nocturnium/llm-go-sdk/issues/46)) [**BREAKING**]

## [5.1.0] - 2026-07-13

### Documentation

- V5.0.0 changelog + migration-guide fast-follow ([#43](https://github.com/nocturnium/llm-go-sdk/issues/43))

### Features

- **mcp:** Decouple WithAllowHTTP from WithAllowPrivateIPs ([#44](https://github.com/nocturnium/llm-go-sdk/issues/44))

## [5.0.0] - 2026-07-13

### Features

- V5.0.0 — breaking API cleanup + /v4→/v5 module path ([#41](https://github.com/nocturnium/llm-go-sdk/issues/41)) [**BREAKING**]

## [4.2.0] - 2026-07-12

### CI/CD

- Auto-release on merge via conventional commits ([#37](https://github.com/nocturnium/llm-go-sdk/issues/37))
- Run integration tests under -race ([#38](https://github.com/nocturnium/llm-go-sdk/issues/38))

### Features

- V4.2.0 — correctness, resilience & transport-security hardening sweep ([#39](https://github.com/nocturnium/llm-go-sdk/issues/39))

## [4.1.1] - 2026-06-22

### Bug Fixes

- V4.1.1 hardening — close 5 HIGH deficiencies from CTO review ([#35](https://github.com/nocturnium/llm-go-sdk/issues/35))

## [4.1.0] - 2026-06-21

### Features

- V4.1.0 — observability fast-follow (MCP drop-count, CB panic hook) + cleanups ([#33](https://github.com/nocturnium/llm-go-sdk/issues/33))

## [4.0.0] - 2026-06-21

### Features

- V4.0.0 — security/correctness hardening sweep + breaking API cleanups ([#31](https://github.com/nocturnium/llm-go-sdk/issues/31)) [**BREAKING**]

## [3.1.0] - 2026-06-21

### Bug Fixes

- Harden post-v3 work per dual expert review, 8 findings ([#25](https://github.com/nocturnium/llm-go-sdk/issues/25))
- **observability:** Sanitize CR/LF in logged error strings, CWE-117 ([#28](https://github.com/nocturnium/llm-go-sdk/issues/28))

### Documentation

- **changelog:** Unreleased section for post-v3.0.0 additive changes ([#22](https://github.com/nocturnium/llm-go-sdk/issues/22))
- Nocturnium theme — dark-first re-skin, hero landing, go logo ([#23](https://github.com/nocturnium/llm-go-sdk/issues/23))

### Features

- **huggingface:** Chat/text-generation for Inference Endpoints, TGI ([#24](https://github.com/nocturnium/llm-go-sdk/issues/24))
- **models:** Refresh model mappings to current flagships, June 2026 ([#26](https://github.com/nocturnium/llm-go-sdk/issues/26))

### Testing

- **ollama:** Fix integration-tag build break + gate it on PRs ([#27](https://github.com/nocturnium/llm-go-sdk/issues/27))

## [3.0.0] - 2026-06-19

### Features

- V3.0.0 — extract observability + resilience middleware out of root (sheds OTel) ([#19](https://github.com/nocturnium/llm-go-sdk/issues/19)) [**BREAKING**]

## [2.1.0] - 2026-06-19

### Documentation

- Add Support & Versioning policy (single-version; v1.x EOL) ([#10](https://github.com/nocturnium/llm-go-sdk/issues/10))

### Features

- **openai:** OpenAI Responses API support (non-streaming) — roadmap Track A PR1 ([#12](https://github.com/nocturnium/llm-go-sdk/issues/12))
- **openai:** Responses API statefulness ergonomics — Track A PR2 ([#13](https://github.com/nocturnium/llm-go-sdk/issues/13))
- **openai:** Stream the Responses API (Track A PR3) ([#14](https://github.com/nocturnium/llm-go-sdk/issues/14))
- **mcp:** Inspectable error mapping (RPCError + ToolError) — Track B PR1 ([#15](https://github.com/nocturnium/llm-go-sdk/issues/15))
- **huggingface:** Add embeddings provider for HF Inference Endpoints ([#16](https://github.com/nocturnium/llm-go-sdk/issues/16))
- **zai:** Refresh model list from live API (8 GLM models incl. GLM-5 series) ([#17](https://github.com/nocturnium/llm-go-sdk/issues/17))

## [2.0.0] - 2026-06-16

### Bug Fixes

- **response:** Repopulate deprecated Thinking alias on JSON unmarshal
- **metrics:** End stream span before decrementing active requests
- **gemini:** Surface empty SAFETY/RECITATION stream finish as an error

### Documentation

- **construction:** Clarify the three construction paths and dual WithModel
- **release:** Add v2.0.0 CHANGELOG, bump version pointers, note runpod error change

### Features

- **options:** Make FrequencyPenalty/PresencePenalty *float64 [**BREAKING**]
- **tools:** Thread context through ToolHandler; remove MustParseToolArguments [**BREAKING**]
- **streaming:** Add CollectStream/StreamText drain helpers; fix error-dropping docs
- **capabilities:** Complete As*/Supports* symmetry + ToCapabilities drift guard
- **registry:** Explicit, validated required provider config via RequireExtra
- **cost:** Add web-verified pricing for groq/fireworks/perplexity/zai
- Migrate module path to /v2 for the v2.0.0 release [**BREAKING**]

### Refactoring

- **validation:** Move provider quirks out of core ValidateMessages
- **openaicompat:** Alias ModelPricing to llms.ModelPricing

### Testing

- **openaicompat:** Use context.Background() not nil in D7 validator tests

## [1.2.1] - 2026-06-15

### Bug Fixes

- **release:** Remove invalid GOWORK=off prefix from GoReleaser hooks

## [1.2.0] - 2026-06-15

### Bug Fixes

- Post-v1.1 hardening — correctness, security, resilience, docs

### Documentation

- Bump version pointers to v1.2.0 for release

### Security

- Add .dockerignore to keep .env out of images

## [1.1.0] - 2026-06-09

### Bug Fixes

- **security:** Resolve all open code-scanning findings
- **mcp:** Annotate intentional subprocess launch (gosec G204)
- V1.1.0 pre-release hardening (security, correctness, resilience)

### CI/CD

- Fix public-repo CI — integration test compile, SARIF perms, CodeQL v4, Pages deploy
- **release:** Open a PR for CHANGELOG instead of pushing to main
- **codeql:** Use examples/** glob so the Go extractor excludes examples

### Documentation

- Update CHANGELOG.md for v1.0.0
- Comprehensive MkDocs Material documentation site

### Features

- V1.1.0 — reasoning, prompt caching, and MCP client

## [1.0.0] - 2026-06-08

<!-- generated by git-cliff -->
