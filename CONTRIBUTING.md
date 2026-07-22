# Contributing

This document uses ASD-STE100 Simplified Technical English.

Thanks for considering a contribution.

## Before You Start

- Keep changes focused. Use a small pull request when possible.
- Open an issue before a large refactor or behavior change. Get agreement on
  the design before you start the change.
- Use GitHub issues for bugs and feature work.
- Use `minecraft-ping@omkhar.net` for support or usage questions that are not actionable bugs.
- Do not use public issues for security reports. See [SECURITY.md](SECURITY.md).

## Development Setup

This project uses the Go toolchain version declared in `go.mod`.

Canonical local commands:

```bash
make verify
make coverage
make clean-repo
go run . -h
go run . -V
```

`make verify` runs the standard pre-PR built-in checks, including agent-surface drift and public-repo hygiene checks.
`make coverage` runs package coverage across the module.

For maintainer-oriented validation and release-path details, see [docs/development.md](docs/development.md) and [docs/releasing.md](docs/releasing.md).

## Change Expectations

- Add or update tests for behavior changes.
- Update documentation when flags, output, packaging, protocol behavior, release behavior, or the man page changes.
- Preserve the project's bias toward simple transport paths and honest protocol behavior.
- Keep Java and Bedrock behavior explicit. Do not introduce protocol auto-detection or fake ICMP semantics.
- Avoid unrelated cleanup in the same change unless it is necessary to keep the patch coherent.

## Validation

Before opening a pull request, run the basic local checks:

```bash
make verify
make coverage
make clean-repo
```

If your environment has these tools, run the same types of checks that CI runs:

- `actionlint`
- `gofmt`
- `goimports`
- `staticcheck`
- `ineffassign`
- `gocritic`
- `govulncheck`
- `gosec`
- `gitleaks`
- `deadcode -test ./...`
- `go fix -diff ./...`

For networking changes, run:

```bash
make integration
```

`make integration` validates locally built binaries against the staging backends.
To exercise the Linux container-backed path locally, set `MINECRAFT_RELEASE_INTEGRATION_BACKEND=container`.
Build the staging image first or point the script at an existing image with `MINECRAFT_STAGING_IMAGE`.

For release artifact changes, also run the relevant smoke checks after a snapshot build:

```bash
make release-archive-smoke
make release-repro
make package-smoke ARCH=amd64
```

If you have `go-mutesting`, `make mutation` runs the supported non-`main`
package mutation suite. Run it for a change that has much decision logic.

## Portable Agent Instructions

This repository keeps one canonical set of agent instructions in [AGENTS.md](AGENTS.md) and [.agents/skills](.agents/skills).

If you edit either of those canonical files, regenerate and verify the agent-native mirrors before opening a pull request:

```bash
make agents-sync
make agents-verify
```

## Labels

This repository uses a small label set to keep triage clear:

- `good first issue` and `help wanted` mark contributor-friendly work.
- `area/*` labels identify the primary subsystem, such as docs, CI, release, Java, or Bedrock.
- Dependency automation uses the existing `dependencies`, `docker`, and `github_actions` labels.

## Pull Requests

- Describe the user-visible change and the reason for it.
- Call out protocol, packaging, workflow, security, or release implications explicitly.
- Include the validation you ran locally.
- Keep examples and docs aligned with the current help output and actual runtime behavior.
- Update the changelog, man page, or supporting docs when user-visible behavior changes.

## Licensing

This project uses the Apache 2.0 inbound-equals-outbound model.

When you submit a contribution, you agree to license it under the Apache License, Version 2.0.
The same terms apply to the rest of the repository.
