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

If `mandoc` is available locally, it is also worth checking the man page source:

```bash
mandoc -Tlint man/minecraft-ping.1
```

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

Validate the generated release archives:

```bash
scripts/release_archive_smoke.sh dist
```

Validate that the snapshot artifacts are reproducible across checkout paths:

```bash
scripts/release_reproducibility.sh dist
```

Then run the smoke test for a Linux architecture your container runtime can execute:

```bash
scripts/release_linux_package_smoke.sh dist amd64
```

or:

```bash
scripts/release_linux_package_smoke.sh dist arm64
```

If you use Podman instead of Docker, set the runtime explicitly:

```bash
CONTAINER_CLI=podman scripts/release_linux_package_smoke.sh dist amd64
```

This path validates installability and basic execution of the generated `.deb`, `.rpm`, and `.apk` packages.
It also verifies that the packaged `minecraft-ping(1)` man page is installed.
The package smoke script also asserts that the shipped binary reports the expected stamped version.

## Shared Builder Hosts

If you want to offload the heavyweight release-path checks to a strong shared Linux box such as `builder@bewear`, use the isolated shared-builder wrapper instead of running the release scripts directly in a shared checkout:

```bash
scripts/run_shared_builder_checks.sh
```

What the wrapper does:

- clones the current clean checkout into a unique per-run root under `~/codex-runs/minecraft-ping`
- builds a dedicated runner image from `docker/build-runner.Dockerfile`
- runs the heavy validation inside that runner with its own Go caches and tool installs
- mounts a Docker-compatible socket into the runner so package smoke and container-backed integration still work
- assigns a unique staging image tag, integration container name, and integration port set for the run

This wrapper is intentionally Linux-host oriented because the runner uses host networking so the inner release integration process can probe the ports published by the daemon-backed staging container.

Useful overrides:

- `SHARED_BUILDER_HOST_CLI`: outer container runtime used to launch the runner, for example `docker` or `podman`
- `SHARED_BUILDER_SOCKET`: Docker-compatible socket path to mount into the runner
- `SHARED_BUILDER_PACKAGE_ARCHES`: comma-separated package smoke architectures, for example `amd64` or `amd64,arm64`
- `SHARED_BUILDER_KEEP_RUN_ROOT=0`: remove the isolated checkout after a successful run

Example with a rootless Podman socket:

```bash
SHARED_BUILDER_HOST_CLI=podman \
SHARED_BUILDER_SOCKET="$XDG_RUNTIME_DIR/podman/podman.sock" \
scripts/run_shared_builder_checks.sh
```

The wrapper is meant for batch validation, not for the tight local edit loop and not for final release publication. GitHub Actions remains the only supported path for signed release publishing, provenance, and SBOM uploads.

## CI Coverage

`Main Verify` currently covers:

- `go test -race ./...`
- shuffled `go test ./...`
- cross-platform `go test ./...` on macOS, Linux, and Windows for `amd64` and `arm64`
- lint, security, and workflow policy checks
- GoReleaser snapshot builds
- release archive smoke tests
- release reproducibility smoke tests against a second detached worktree
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
- The release workflow is configured to publish signed SPDX SBOM assets for each release.
- GitHub artifact attestations and provenance bundles are published automatically when repository visibility and ownership support them.
- Until GitHub supports first-party attestations for the active repository configuration, releases continue to publish signed artifacts and signed SBOM assets.
