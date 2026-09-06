# Runtime Versions

This document uses ASD-STE100 Simplified Technical English.

## Shipped Program

The project uses Go 1.27.1.
GoReleaser sets `CGO_ENABLED=0` for release builds.
Thus, a release binary does not require a separate Go installation.

The staging container uses `golang:1.27.1-bookworm`.
The Dockerfile pins the image digest.

The Linux package smoke tests use these container releases:

- Alpine 3.24
- Debian 13
- Fedora 44

The package smoke script pins each container image digest.

## CI Runners

CI uses these fixed GitHub runner labels:

- `ubuntu-24.04`
- `ubuntu-24.04-arm`
- `macos-26`
- `windows-2025`
- `windows-11-arm`

The fixed labels prevent an automatic operating system change.
The Arm jobs use the native Arm runner labels.

## Actions And Tools

All external GitHub Actions use a full commit SHA.
The comment after each SHA gives the release tag.
The workflows also pin the versions of build, test, security, and release tools.

The main tool versions are:

- GoReleaser `v2.18.1`
- Cosign `v3.1.3`
- Syft `v1.51.1`
- actionlint `v1.7.12`
- deadcode and goimports from `golang.org/x/tools` `v0.49.0`
- govulncheck `v1.7.0`
- gosec `v2.29.0`
- gocritic `v0.15.0`
- Staticcheck `v0.8.1`
- gitleaks `v8.30.1`
- go-mutesting `v0.0.0-20251226130216-48d0401f00fb`

The machine-readable source is [`docs/runtime-versions.json`](runtime-versions.json).
Its tests check the Go version, staging image, package-smoke images, runner
labels, action SHAs, action tags, and tool versions.
CI fails if these values do not agree with the repository.

## Update Procedure

1. Get the current stable release from the upstream project.
2. Verify each tag and commit in the upstream repository.
3. Change the runtime file and its use in the same commit.
4. Run the local validation gates in [Development](development.md).
5. Run the network and release gates if the change affects those paths.
