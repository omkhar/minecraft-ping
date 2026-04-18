# Development

This document is the maintainer and contributor on-ramp for local validation and release-path work.

## Prerequisites

- Use the Go toolchain version declared in `go.mod`.
- For the container-backed integration path, install Docker or Podman.
- For full local parity checks, use an environment with working IPv4 and IPv6 loopback networking.

## Day-To-Day Workflow

Useful commands during normal development:

```bash
make test
make verify
make coverage
make clean-repo
go run . -h
go run . -V
```

## Local Preflight

Before opening a pull request, run the checks that are reasonable to execute locally:

```bash
make verify
make coverage
make clean-repo
```

If you edit [AGENTS.md](../AGENTS.md) or anything under `.agents/skills/`, regenerate and verify the agent-native mirrors in the same change:

```bash
make agents-sync
make agents-verify
```

`make agents-verify` always checks the generated agent files against the canonical sources. When the `codex`, `claude`, and `gemini` CLIs are installed locally, it also runs the same structured smoke prompt against each one and requires them to return the same repo contract. Trusted PR runs and `Main Verify` enforce that live smoke in GitHub Actions once `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, and `GEMINI_API_KEY` are configured for the repository. Those workflow-only secrets stay scoped to the dedicated `agent-smoke` environment. Fork PR workflows keep the live smoke structural-only so secrets are not exposed to untrusted code.

If you have `deadcode` installed locally, run it before large refactors or cleanup-heavy changes:

```bash
make deadcode
```

If you have `go-mutesting` installed locally, run mutation checks before large refactors or protocol changes:

```bash
make mutation
```

That mutation entrypoint covers the supported non-`main` packages. The root CLI and command packages stay guarded by their focused unit tests, mutation-killer tests, and integration coverage.

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
make integration
```

`make integration` and `scripts/run_release_integration.sh` build local binaries in a temporary directory, then run the integration harness against those locally built artifacts. They do not validate the release archives under `dist/`.

By default, the integration script validates the native staging backend. To exercise the Linux container-backed path locally:

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

That step is the start of the release-artifact path. The commands below validate the generated archives and packages in `dist/`, not the locally built binaries used by `make integration`.

Validate the generated release archives:

```bash
make release-archive-smoke
```

Validate that the snapshot artifacts are reproducible across checkout paths:

```bash
make release-repro
```

Then run the smoke test for a Linux architecture your container runtime can execute:

```bash
make package-smoke ARCH=amd64
```

or:

```bash
make package-smoke ARCH=arm64
```

If you use Podman instead of Docker, set the runtime explicitly:

```bash
CONTAINER_CLI=podman scripts/release_linux_package_smoke.sh dist amd64
```

This path validates installability and basic execution of the generated `.deb`, `.rpm`, and `.apk` packages.
It also verifies that the packaged `minecraft-ping(1)` man page is installed.
The package smoke script also asserts that the shipped binary reports the expected stamped version.

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

Releases are built from GitHub-verified signed, annotated tags at the current `main` head.

Maintainer flow:

1. Merge the release candidate to `main`.
2. Wait for `Main Verify` to complete successfully on that commit.
3. Create a GitHub-verified signed, annotated `vX.Y.Z` tag at that exact `main` head.
4. Push the tag to trigger the release workflow.

Release outputs:

- Signed release artifacts are always published.
- The release workflow is configured to publish signed SPDX SBOM assets for each release.
- GitHub artifact attestations and provenance bundles are published for public releases.
