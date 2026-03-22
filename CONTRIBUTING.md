# Contributing

Thanks for considering a contribution.

## Before You Start

- Keep changes focused. Small, reviewable pull requests merge faster.
- Open an issue before large refactors or behavior changes so the direction can be aligned early.
- Do not use public issues for security reports. See [SECURITY.md](SECURITY.md).

## Development Setup

This project uses the Go toolchain version declared in `go.mod`.

Common commands:

```bash
go test ./...
go test -race ./...
go run . -h
go run . -V
```

For maintainer-oriented validation and release-path details, see [docs/development.md](docs/development.md).

## Change Expectations

- Add or update tests for behavior changes.
- Update documentation when flags, output, packaging, protocol behavior, release behavior, or the man page changes.
- Preserve the project's bias toward simple transport paths and honest protocol behavior.
- Keep Java and Bedrock behavior explicit. Do not introduce protocol auto-detection or fake ICMP semantics.
- Avoid unrelated cleanup in the same change unless it is necessary to keep the patch coherent.

## Validation

Before opening a pull request, run the basic local checks:

```bash
go test ./...
go test -race ./...
go vet ./...
```

If your environment already has the tools installed, it is also useful to run the same classes of checks enforced by CI:

- `actionlint`
- `gofmt`
- `goimports`
- `staticcheck`
- `ineffassign`
- `gocritic`
- `govulncheck`
- `gosec`
- `gitleaks`

For networking, workflow, or release-path changes, also run:

```bash
scripts/run_release_integration.sh
```

The release integration script uses the native staging backend by default. To exercise the Linux container-backed path locally, set `MINECRAFT_RELEASE_INTEGRATION_BACKEND=container`.
Build the staging image first or point the script at an existing image with `MINECRAFT_STAGING_IMAGE`.

For packaging changes, also run `scripts/release_archive_smoke.sh dist` after a snapshot build and reproduce the Linux package smoke path described in [docs/development.md](docs/development.md) when your environment supports it.

## Pull Requests

- Describe the user-visible change and the reason for it.
- Call out protocol, packaging, workflow, or release implications explicitly.
- Include the validation you ran locally.
- Keep examples and docs aligned with the current help output and actual runtime behavior.

## Licensing

This project uses the Apache 2.0 inbound-equals-outbound model.

By submitting a contribution, you agree that your work will be licensed under the Apache License, Version 2.0, under the same terms as the rest of the repository.
