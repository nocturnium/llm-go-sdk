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
| **v2.x** | `github.com/nocturnium/llm-go-sdk/v2` | **Supported** — current; all fixes and new work land here |
| v1.x | `github.com/nocturnium/llm-go-sdk` | **End of life** as of 2026-06-15 — no further releases, bug fixes, or security backports |

All bug fixes and security patches land on the current major (**v2.x**) only. If you
need a fix on an older major, upgrade to the current major — we do not backport.

> **Rationale:** the library and all of its consumers are developed in lockstep by
> a single team. A single supported line keeps development focused and avoids the
> cost (and the security exposure) of maintaining parallel release branches. v1.x
> remains *installable* from the module proxy, but it is frozen and unsupported.

## Upgrading

The current major is **v2**. Install it with:

```bash
go get github.com/nocturnium/llm-go-sdk/v2@latest
```

See **[docs/migration-guide.md](docs/migration-guide.md)** for the full v1 → v2
guide: the import-path change plus three small breaking API changes (tool handlers
take a `context.Context`, sampling penalties are `*float64`, and the panic-prone
`MustParseToolArguments` was removed in favor of the error-returning
`ParseToolArguments`).

## Reporting problems

- **Security vulnerabilities:** follow [SECURITY.md](SECURITY.md) — do not open a
  public issue. Contact: hello@nocturnium.ai.
- **Bugs and feature requests:** open a GitHub issue.
- **Questions:** open a GitHub discussion.
