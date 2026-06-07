# Contributing to llm-go-sdk

Thanks for your interest in improving **llm-go-sdk** — a unified Go SDK for many
LLM providers, built on plain `net/http` with zero external LLM dependencies.
Contributions of all kinds are welcome: bug fixes, new providers, docs, tests,
and ideas.

This project is licensed under **Apache-2.0** (see [`LICENSE`](./LICENSE) and
[`NOTICE`](./NOTICE)). By contributing, you agree that your contributions are
licensed under the same terms.

- **Code of Conduct:** Be respectful and constructive. We follow the
  [Contributor Covenant](https://www.contributor-covenant.org/version/2/1/code_of_conduct/).
  Report unacceptable behavior to **hello@nocturnium.ai**.
- **Security:** Please do **not** open public issues for vulnerabilities. See
  [`SECURITY.md`](./SECURITY.md) and email **hello@nocturnium.ai** instead.

---

## Prerequisites

- **Go 1.25+** (the module targets `go 1.25.0`; see [`go.mod`](./go.mod)).
- Standard Go tooling: `gofmt`, [`goimports`](https://pkg.go.dev/golang.org/x/tools/cmd/goimports),
  and [`golangci-lint`](https://golangci-lint.run/).
- Optional but recommended: `govulncheck`, `staticcheck`, and
  [`git-cliff`](https://git-cliff.org/) (for changelog generation).

Install all dev tools in one step:

```bash
make install-tools
```

This installs `golangci-lint`, `goimports`, `govulncheck`, `staticcheck`,
`goreleaser`, and `git-cliff`.

---

## Getting Started

```bash
# 1. Fork on GitHub, then clone your fork
git clone https://github.com/<your-username>/llm-go-sdk.git
cd llm-go-sdk

# 2. Build everything (library + CLI packages)
go build ./...

# 3. Run the tests
go test ./...
```

The repo ships a real CLI (`llms-cli`) under `cmd/`. Build it with:

```bash
make build        # produces ./llms-cli with version metadata
```

### Useful Makefile targets

These targets all exist in the [`Makefile`](./Makefile) (run `make help` for the
full list):

| Target | What it does |
|--------|--------------|
| `make build` | Build the `llms-cli` binary |
| `make test` | `go test -race` with coverage |
| `make test-short` | Run short tests only |
| `make test-integration` | Run integration tests (build tag `integration`) |
| `make bench` | Run benchmarks |
| `make coverage` | Generate and open an HTML coverage report |
| `make fmt` | Format with `gofmt` + `goimports` (local prefix applied) |
| `make fmt-check` | Fail if any file is not formatted |
| `make vet` | Run `go vet ./...` |
| `make lint` | Run `golangci-lint run ./...` |
| `make vulncheck` | Run `govulncheck ./...` |
| `make check` | `fmt-check` + `vet` + `lint` + `test` |
| `make ci` | `check` plus `vulncheck` (mirrors CI) |
| `make tidy` | `go mod tidy` |
| `make changelog` | Regenerate `CHANGELOG.md` via `git-cliff` |
| `make setup-hooks` | Install the repo's git hooks |

Running `make ci` locally before pushing is the fastest way to catch what GitHub
Actions will flag.

---

## Development Workflow

### Branching

Create a topic branch off `main`. Use a short, descriptive, kebab-case name with
a type prefix, for example:

```
feat/add-cohere-provider
fix/anthropic-stream-tool-calls
docs/contributing-guide
test/openai-model-listing
```

### Commit messages

This repo uses **[Conventional Commits](https://www.conventionalcommits.org/)**
and generates its changelog with **git-cliff** (config in [`cliff.toml`](./cliff.toml)).
Use the standard types so your change lands in the right changelog section:

```
feat(anthropic): add prompt-caching support
fix(gemini): handle empty candidates in stream
docs: clarify embeddings usage
refactor: extract shared option resolution
test(openai): cover model pagination
chore: bump dependencies
```

Recognized types include `feat`, `fix`, `docs`, `perf`, `refactor`, `style`,
`test`, `build`, `ci`, and `chore`. Append `!` (or a `BREAKING CHANGE:` footer)
for breaking changes. A scope (the provider or area you touched) is encouraged.

### Before opening a PR

Run the local checks — ideally just `make ci` — and make sure they pass:

```bash
make fmt          # or: gofmt -w . && goimports -w -local github.com/nocturnium/llm-go-sdk .
make vet
make lint
make test         # go test -race ./...
```

CI additionally runs `gofmt`, `goimports`, `go mod tidy`, `go vet`,
`golangci-lint`, `staticcheck`, `govulncheck`, and CodeQL — so format your
imports with the `-local github.com/nocturnium/llm-go-sdk` prefix and keep
`go.mod`/`go.sum` tidy.

---

## Project Layout (orientation)

The real implementation of the **core types and interfaces lives in the root
package**, imported as `llms`:

```go
import llms "github.com/nocturnium/llm-go-sdk"
```

The `pkg/types`, `pkg/options`, `pkg/errors`, and `pkg/streaming` packages are
thin **alias packages** that re-export the root — they exist for ergonomics and
clarity, not as separate sources of truth.

**Providers** live under their canonical path:

```go
import "github.com/nocturnium/llm-go-sdk/pkg/providers/openai"
```

> The top-level `providers/<name>` packages are **deprecated backwards-compat
> shims**. New code (and any new provider you add) must use
> `pkg/providers/<name>`.

---

## Adding a New Provider

[`AGENTS.md`](./AGENTS.md) is the **deep reference** for provider work — it
documents the required interfaces, error-handling rules, thread-safety,
observability, and the full step-by-step guide. Read it before starting. The
short version:

1. **Pick an implementation strategy.** If the provider has an OpenAI-compatible
   API, build on `openaicompat.BaseProvider` (see
   [`pkg/openaicompat`](./pkg/openaicompat)) — do **not** hand-roll
   `Call`/`GenerateContent`/`Stream`. Native implementations (like Anthropic and
   Gemini) require explicit justification in the package doc.
2. **Create the package** at `pkg/providers/<name>/` with the standard files:
   `<name>.go`, `<name>-options.go`, `models.go`, plus `_test.go` files and an
   `integration_test.go` (build tag `integration`).
3. **Resolve the API key** with the precedence: explicit `WithAPIKey` →
   provider-specific env var (`<PROVIDER>_API_KEY`) → generic `LLM_API_KEY`.
4. **Register the provider constant** in the root package and implement the
   required interfaces (`llms.LLM`, `llms.CapableProvider`, `llms.ModelLister`,
   and `llms.Embedder` if supported). Add compile-time checks:
   `var _ llms.LLM = (*Client)(nil)`.
5. **Document and test.** Add a package doc comment, cover options/interface/model
   listing (including concurrency), and update the README provider table.

Match the conventions of an existing provider such as
[`pkg/providers/openai`](./pkg/providers/openai) when in doubt.

---

## Pull Request Checklist

Before requesting review, confirm:

- [ ] `go build ./...` succeeds (library and CLI).
- [ ] `go vet ./...` is clean.
- [ ] `go test -race ./...` passes (and new tests cover your change, including
      edge cases and concurrent access for shared data).
- [ ] `golangci-lint run ./...` and `gofmt`/`goimports` are clean
      (`make check` / `make ci`).
- [ ] Exported symbols have godoc comments; package-level docs are present for new
      packages/providers.
- [ ] New providers use the `pkg/providers/<name>` canonical path and the
      `BaseProvider` pattern (or document why a native implementation is needed).
- [ ] Examples and the README are updated when user-facing behavior changes.
- [ ] No secrets, API keys, or tokens in code, tests, logs, or fixtures.
- [ ] Commits follow Conventional Commits so the changelog generates correctly.

Fill in the [pull request template](./.github/PULL_REQUEST_TEMPLATE.md) when you
open the PR.

---

## Reporting Bugs & Requesting Features

Use the GitHub issue templates:

- **Bug report:** [`.github/ISSUE_TEMPLATE/bug_report.md`](./.github/ISSUE_TEMPLATE/bug_report.md)
- **Feature request:** [`.github/ISSUE_TEMPLATE/feature_request.md`](./.github/ISSUE_TEMPLATE/feature_request.md)

For open-ended questions and ideas, prefer **GitHub Discussions** over issues.
Again, for anything security-sensitive, do not file a public issue — email
**hello@nocturnium.ai**.

Thank you for contributing!
