# Contributing

Thanks for considering a contribution.

## Before You Start

- Keep changes focused. Small, reviewable pull requests merge faster.
- Open an issue before large refactors or behavior changes so the direction can be aligned early.
- Do not use public issues for security reports. See [SECURITY.md](SECURITY.md).

## Development Setup

This project uses the Go toolchain version declared in `go.mod`.

Useful commands:

```bash
go test ./...
go test -race ./...
go run . -h
go run . -V
scripts/run_release_integration.sh
```

For the maintainer-oriented validation flow, see [docs/development.md](docs/development.md).

## Change Expectations

- Add or update tests for behavior changes.
- Update documentation when flags, output, packaging, or protocol behavior changes.
- Preserve the project's bias toward simple transport paths and honest protocol behavior.
- Avoid unrelated cleanup in the same change unless it is necessary to keep the patch coherent.

## Before Opening A Pull Request

Run the basic local checks:

```bash
go test ./...
go test -race ./...
go vet ./...
```

If your environment already has the tools installed, it is also useful to run the same classes of checks exercised by CI: `actionlint`, `goimports`, `staticcheck`, `ineffassign`, `gocritic`, `govulncheck`, `gosec`, and `gitleaks`.

For networking, packaging, workflow, or release-path changes, also run:

```bash
scripts/run_release_integration.sh
```

## Pull Requests

- Describe the user-visible change and the reason for it.
- Call out protocol, packaging, workflow, or release implications explicitly.
- Include the validation you ran locally.

## Licensing

This project uses the Apache 2.0 inbound-equals-outbound model.

By submitting a contribution, you agree that your work will be licensed under the Apache License, Version 2.0, under the same terms as the rest of the repository.
