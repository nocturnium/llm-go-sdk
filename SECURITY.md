# Security Policy

Thanks for helping keep `llm-go-sdk` and its users safe. This document explains
which versions receive security fixes, how to report a vulnerability privately,
and how the project's security posture is maintained.

## Supported Versions

`llm-go-sdk` follows semantic versioning. Security fixes land on the latest
minor release line; older lines are no longer maintained. Always update to the
most recent patch release before reporting an issue, as it may already be fixed.

| Version | Supported          |
| ------- | ------------------ |
| 1.2.x   | :white_check_mark: |
| 1.1.x   | :x:                |
| 0.0.x   | :x:                |

The current latest release is `v1.2.0`. We recommend pinning to a released tag
and keeping your dependency current with `go get -u` so you receive fixes
promptly.

## Reporting a Vulnerability

**Please do not open a public GitHub issue, pull request, or discussion for a
security vulnerability.** Public reports expose users before a fix is available.

Report privately through either of the following channels:

- **Email:** [hello@nocturnium.ai](mailto:hello@nocturnium.ai) — preferred for a
  direct, written report.
- **GitHub private security advisories:** use the **Security → Report a
  vulnerability** ("Privately report a vulnerability") flow on the repository to
  open a confidential advisory that we can coordinate within GitHub.

To help us triage quickly, please include where practical:

- A description of the issue and its potential impact.
- The affected version(s) or commit, and the provider(s) involved if relevant.
- Step-by-step reproduction details or a minimal proof of concept.
- Any suggested remediation you have in mind.

### What to expect

- **Acknowledgement:** we aim to acknowledge your report within **3 business
  days**.
- **Assessment:** we will work with you to confirm the issue, determine its
  severity and scope, and agree on a remediation and disclosure timeline.
- **Resolution:** once a fix is available we will publish a patched release on
  the supported minor line and credit reporters who wish to be acknowledged.

Please give us reasonable time to investigate and ship a fix before any public
disclosure. We are committed to working with reporters in good faith.

### Credential-handling reports are especially welcome

This SDK handles API keys and other provider credentials on behalf of the
calling application (see `apikey.go`'s `ResolveAPIKey`/`RequireAPIKey`). Reports
about credential handling — for example keys leaking into logs, error messages,
traces/telemetry, panics, or being transmitted to an unintended endpoint — are
high priority. If you find anything in this category, please report it through
the private channels above.

## Scope

`llm-go-sdk` is a **client SDK**: it makes outbound HTTPS calls to third-party
LLM providers using the standard library `net/http`. A few notes on scope:

- **Secrets belong to the user.** API keys are supplied by you, the integrator,
  typically via provider-specific environment variables (for example
  `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`) or the `LLM_API_KEY`
  fallback. The SDK reads these; it does not store, persist, or transmit them
  anywhere other than the provider's API endpoint.
- **Never commit real keys.** Keep credentials out of source control. Use
  environment variables or your platform's secret manager, and base any local
  configuration on a non-secret template such as `.env.example` rather than
  checking in a populated `.env` file.
- **SSRF protection is on by default.** Outbound requests are validated and the SDK
  refuses connections to private, loopback, link-local, and cloud-metadata
  (`169.254.169.254`) addresses, requires HTTPS, and re-validates every redirect
  hop. Self-hosted/local endpoints (and the `ollama`, `llamacpp`, and `infinity`
  providers, which default to `http://localhost`) are reached by passing
  `WithAllowPrivateIPs()` — used by default only for those local providers.
- **Provider-side issues are out of scope** here. Vulnerabilities in a third-party
  provider's API or in upstream Go dependencies should be reported to the
  respective maintainers, though we welcome a heads-up so we can update or pin
  affected dependencies.

If you are unsure whether something is in scope, report it privately and we will
help triage.

## Security Posture and Tooling

Security is enforced continuously in CI (see `.github/workflows/`):

- **CodeQL** — GitHub's semantic code analysis with the `security-extended` and
  `security-and-quality` query packs, run on pushes, pull requests, and on a
  weekly schedule.
- **gosec** — Go-specific static security analysis, with results uploaded as
  SARIF to GitHub code scanning.
- **Trivy** — filesystem vulnerability scanning for `CRITICAL` and `HIGH`
  severity findings, also surfaced via SARIF.
- **govulncheck** — the official Go vulnerability scanner, run against the module
  to catch known vulnerabilities in dependencies and standard-library usage.

These tools complement, and do not replace, responsible disclosure. If you find
something the automated tooling missed, please tell us.

---

This project is licensed under the Apache License 2.0. See `LICENSE` and
`NOTICE`. Copyright 2026 Nocturnium, Inc.
