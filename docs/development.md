# Development

This document is the maintainer and contributor on-ramp for local validation.

## Basic Workflow

Useful commands during day-to-day development:

```bash
go test ./...
go test -race ./...
go run . -h
go run . -V
```

## Local Preflight

Before opening a pull request, run the checks that are reasonable to execute locally:

```bash
go test ./...
go test -race ./...
go vet ./...
```

If you have the tools installed locally, it is also worth running the same classes of checks enforced by CI:

- `actionlint`
- `gofmt`
- `goimports`
- `staticcheck`
- `ineffassign`
- `gocritic`
- `govulncheck`
- `gosec`
- `gitleaks`

CI remains the source of truth for exact versions and matrix coverage.

## Networking, Packaging, And Release-Path Changes

If your change touches networking behavior, packaging, release automation, or integration harnesses, also validate the release path locally:

```bash
scripts/run_release_integration.sh
```

The main branch CI additionally covers:

- cross-platform `go test ./...`
- GoReleaser snapshot builds
- Linux package smoke tests
- release-archive integration against the staging backend or staging container

## Release Automation

Releases are built from signed tags at the current `main` head.

- Signed release artifacts are always published.
- Signed SPDX SBOM assets are always published.
- GitHub artifact attestations and provenance bundles are published automatically when repository visibility and ownership support them.
- In the current user-owned private-repository mode, GitHub artifact attestations are unavailable, so releases publish signed artifacts and signed SBOM assets only.

