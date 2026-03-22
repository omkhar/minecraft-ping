# Development

This document is the maintainer and contributor on-ramp for local validation and release-path work.

## Prerequisites

- Use the Go toolchain version declared in `go.mod`.
- For the container-backed integration path, install Docker or Podman.
- For full local parity checks, use an environment with working IPv4 and IPv6 loopback networking.

## Day-To-Day Workflow

Useful commands during normal development:

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

CI remains the source of truth for exact tool versions and matrix coverage.

## Networking, Packaging, And Release-Path Changes

If your change touches networking behavior, release automation, or integration harnesses, also validate the release path locally:

```bash
scripts/run_release_integration.sh
```

By default, the script validates the native staging backend. To exercise the Linux container-backed path locally:

```bash
CONTAINER_CLI="${CONTAINER_CLI:-docker}"
"$CONTAINER_CLI" build -f docker/staging-minecraft.Dockerfile -t minecraft-staging-image:ci .
MINECRAFT_RELEASE_INTEGRATION_BACKEND=container scripts/run_release_integration.sh
```

Useful environment overrides:

- `CONTAINER_CLI`: container runtime to use, for example `docker` or `podman`
- `MINECRAFT_STAGING_IMAGE`: staging image tag
- `MINECRAFT_STAGING_IMAGE_ARCHIVE`: compressed image archive to load before running
- `MINECRAFT_RELEASE_INTEGRATION_CONTAINER_NAME`: explicit container name

The helper script is a Bash entry point. On Windows, use Git Bash or WSL. If you need a native PowerShell equivalent for the binary backend, build the probe binary and staging server first, then run:

```powershell
$work = Join-Path $env:TEMP "minecraft-ping-dev"
New-Item -ItemType Directory -Force -Path $work | Out-Null
go build -o (Join-Path $work 'minecraft-ping.exe') .
go build -o (Join-Path $work 'minecraft-staging-server.exe') ./cmd/staging-server
go run ./cmd/release-integration `
  -binary (Join-Path $work 'minecraft-ping.exe') `
  -backend binary `
  -server-binary (Join-Path $work 'minecraft-staging-server.exe')
```

## Linux Package Smoke Reproduction

If your change touches packaging, reproduce the Linux package smoke path locally when your environment supports it.

Build snapshot release artifacts:

```bash
goreleaser release --snapshot --clean --skip=sign
```

Then run the smoke test for a Linux architecture your container runtime can execute:

```bash
scripts/release_linux_package_smoke.sh dist amd64
```

or:

```bash
scripts/release_linux_package_smoke.sh dist arm64
```

This path validates installability and basic execution of the generated `.deb`, `.rpm`, and `.apk` packages.

## CI Coverage

`Main Verify` currently covers:

- `go test -race ./...`
- shuffled `go test ./...`
- cross-platform `go test ./...` on macOS, Linux, and Windows for `amd64` and `arm64`
- lint, security, and workflow policy checks
- GoReleaser snapshot builds
- Linux package smoke tests
- release-archive integration against the staging backend or staging container

Release-archive integration probes both Java and Bedrock over IPv4 and IPv6.

## Release Automation

Releases are built from signed, annotated tags at the current `main` head.

Maintainer flow:

1. Merge the release candidate to `main`.
2. Wait for `Main Verify` to complete successfully on that commit.
3. Create a signed, annotated `vX.Y.Z` tag at that exact `main` head.
4. Push the tag to trigger the release workflow.

Release outputs:

- Signed release artifacts are always published.
- The current release workflow is configured to publish signed SPDX SBOM assets for new releases.
- In the current user-owned private-repository mode, GitHub artifact attestations are unavailable.
- The current private-repository release workflow is configured to publish signed artifacts and signed SBOM assets without GitHub attestations.
- GitHub artifact attestations and provenance bundles are published automatically when repository visibility and ownership support them.
