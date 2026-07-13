# Support & Versioning Policy

## Versioning

`llm-go-sdk` follows [Semantic Versioning](https://semver.org/) and Go's
[semantic import versioning](https://go.dev/ref/mod#major-version-suffixes).
Breaking changes ship only in a new **major** version, which carries a module-path
suffix (for example, `/v2`). Minor and patch releases within a major are always
backward compatible.

## Supported versions

We maintain a **single-version support policy: only the latest major is supported.**
There is no long-term support for previous majors.

| Version | Module path | Status |
|---------|-------------|--------|
| **v5.x** | `github.com/nocturnium/llm-go-sdk/v5` | **Supported** — current; all fixes and new work land here |
| v4.x | `github.com/nocturnium/llm-go-sdk/v4` | **End of life** as of the v5.0.0 release — no further releases, bug fixes, or security backports |
| v3.x | `github.com/nocturnium/llm-go-sdk/v3` | **End of life** as of 2026-06-21 — no further releases, bug fixes, or security backports |
| v2.x | `github.com/nocturnium/llm-go-sdk/v2` | **End of life** as of 2026-06-19 — no further releases, bug fixes, or security backports |
| v1.x | `github.com/nocturnium/llm-go-sdk` | **End of life** as of 2026-06-15 — no further releases, bug fixes, or security backports |

All bug fixes and security patches land on the current major (**v5.x**) only. If you
need a fix on an older major, upgrade to the current major — we do not backport.

> **Rationale:** the library and all of its consumers are developed in lockstep by
> a single team. A single supported line keeps development focused and avoids the
> cost (and the security exposure) of maintaining parallel release branches. Older
> majors remain *installable* from the module proxy, but they are frozen and unsupported.

## Upgrading

The current major is **v5**. Install it with:

```bash
go get github.com/nocturnium/llm-go-sdk/v5@latest
```

See **[docs/migration-guide.md](docs/migration-guide.md)** for the full migration
guides. v4 → v5 is mostly a mechanical import-path bump to `/v5`, plus a handful of
renames the compiler points you straight at (unified `Pricing`, `ToolChoice{Mode, Tool}`,
`WithModelLimit`/`WithModelCursor`, and the removal of the deprecated `Thinking` API).

## Reporting problems

- **Security vulnerabilities:** follow [SECURITY.md](SECURITY.md) — do not open a
  public issue. Contact: hello@nocturnium.ai.
- **Bugs and feature requests:** open a GitHub issue.
- **Questions:** open a GitHub discussion.
