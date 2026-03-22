# minecraft-ping

## Actions

Current GitHub Actions workflow status:

- [PR Fast](https://github.com/omkhar/minecraft-ping/actions/workflows/pr-fast.yml): [![PR Fast](https://github.com/omkhar/minecraft-ping/actions/workflows/pr-fast.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/pr-fast.yml)
- [PR Network](https://github.com/omkhar/minecraft-ping/actions/workflows/pr-network.yml): [![PR Network](https://github.com/omkhar/minecraft-ping/actions/workflows/pr-network.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/pr-network.yml)
- [Main Verify](https://github.com/omkhar/minecraft-ping/actions/workflows/go.yml): [![Main Verify](https://github.com/omkhar/minecraft-ping/actions/workflows/go.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/go.yml)
- [Deep Validation](https://github.com/omkhar/minecraft-ping/actions/workflows/deep-validation.yml): [![Deep Validation](https://github.com/omkhar/minecraft-ping/actions/workflows/deep-validation.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/deep-validation.yml)
- [PR Auto-Merge](https://github.com/omkhar/minecraft-ping/actions/workflows/dependabot-auto-merge.yml): [![PR Auto-Merge](https://github.com/omkhar/minecraft-ping/actions/workflows/dependabot-auto-merge.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/dependabot-auto-merge.yml)
- [Release](https://github.com/omkhar/minecraft-ping/actions/workflows/release.yml): [![Release](https://github.com/omkhar/minecraft-ping/actions/workflows/release.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/release.yml)

## Overview

`minecraft-ping` is a Go CLI for measuring latency to a Minecraft Java Edition server using the native handshake -> status -> ping/pong flow. It supports:

- SRV lookup for default-port hostnames
- explicit IPv4 or IPv6 forcing with `-4` and `-6`
- private-address blocking by default with `-allow-private` opt-in
- text or JSON output
- build-time version reporting with `-version`

## Quick Start

```bash
# Build
go build -v ./...

# Show flags
./minecraft-ping -help

# Show embedded version
./minecraft-ping -version

# Ping a public hostname
./minecraft-ping -server mc.example.com

# Emit JSON for monitoring integrations
./minecraft-ping -server mc.example.com -format json

# Force IPv4 or IPv6 explicitly
./minecraft-ping -server 127.0.0.1 -allow-private -4
./minecraft-ping -server ::1 -allow-private -6
```

## CLI Flags

- `-server`: Minecraft server hostname or IP literal. Defaults to `mc.hypixel.net`.
- `-port`: TCP port. Defaults to `25565`.
- `-timeout`: Connection deadline for the whole request. Defaults to `5s`.
- `-allow-private`: Allows RFC1918, loopback, link-local, and other non-public targets.
- `-format`: `text` or `json`. Defaults to `text`.
- `-4`: Restrict DNS resolution and dialing to IPv4.
- `-6`: Restrict DNS resolution and dialing to IPv6.
- `-version`: Print the embedded build version and exit.

`-4` and `-6` are mutually exclusive.

## Output

Text output:

```text
Ping time is 12 ms
```

JSON output:

```json
{"server":"mc.example.com","latency_ms":12}
```

Local development builds print `minecraft-ping dev` for `-version`. Tagged release builds embed the release version at build time.

## Architecture

- `minecraft-ping.go`: program entry point
- `cli.go`: CLI parsing, help/version handling, output rendering, and exit codes
- `version.go`: embedded build version string used by `-version`
- `client.go`: request validation, SRV resolution, dialing, and end-to-end ping execution
- `endpoint.go`: endpoint normalization, address-family selection, and host validation
- `address.go`: non-public address filtering and family-aware dial candidate ordering
- `protocol.go`: VarInt, packet, handshake, status, and ping/pong protocol framing
- `minecraft-ping_test.go`: unit tests with fake servers and dial/resolver stubs
- `minecraft_ping_cli_test.go`: CLI and process-level behavior tests
- `ping_fuzz_test.go`: fuzz targets for parser robustness

## CI And Security

- `PR Fast`: runs `go mod verify`, `actionlint`, `gofmt`, `goimports`, `go vet`, `staticcheck`, `ineffassign`, `gocritic`, `zizmor`, GitHub dependency review, `govulncheck`, `gosec`, `gitleaks`, Linux `go test -race ./...`, Linux `go test -shuffle=on -count=1 ./...`, and diff-scoped mutation testing for changed Go packages.
- `PR Network`: runs a Linux dual-stack integration gate on pull requests that touch Go, Docker, workflow, or release-integration paths. It builds the staging Minecraft container, executes the real CLI against it, and requires both `-4` and `-6` to succeed.
- `Main Verify`: runs the same lint, dependency, security, and Linux test gates on `main`, then runs native `go test ./...` on Linux, macOS, and Windows for `amd64` and `arm64`, a GoReleaser snapshot build, Linux package install smoke tests on Debian, Fedora, and Alpine for `amd64` and `arm64`, and a final dual-stack release-archive integration matrix that executes every shipped binary with both `-4` and `-6`.
- `Deep Validation`: runs weekly or manually and covers the parser fuzz targets plus the full mutation suite.
- `Release`: waits for the exact tagged `main` commit to pass `Main Verify`, then builds cross-platform archives plus Linux distro packages with GoReleaser, injects the release version used by `-version`, signs the release artifacts with keyless `cosign`, publishes signed SPDX SBOMs, and uploads verified keyless Sigstore provenance and SBOM attestation bundles for every shipped artifact.

## Operations

- Final integration helper: `scripts/run_release_integration.sh`
- Staging backend source: `cmd/staging-server`
- Staging image definition: `docker/staging-minecraft.Dockerfile`
- CI helper: `scripts/ci_wait_for_checks.sh`

The final integration gate uses a small first-party staging backend that speaks the real Minecraft status and ping/pong protocol. Every release archive for Linux, macOS, and Windows on `amd64` and `arm64` is executed against that target with both `-4` and `-6`. Linux runners use the live staging container, while macOS and Windows runners use the native staging backend because GitHub-hosted runners do not offer the same portable dual-stack container path. `scripts/run_release_integration.sh` is a developer helper for source builds; the release gates validate actual GoReleaser output.

## Releases

- Create an annotated, signed `vX.Y.Z` tag at the current `main` head to publish a release.
- Release assets include signed archives for `darwin`, `linux`, and `windows` on `amd64` and `arm64`.
- Linux release assets also include signed `.deb`, `.rpm`, and `.apk` packages for `amd64` and `arm64`.
- Release assets also include signed SPDX SBOM files for every published archive and package.
- Every uploaded asset includes a matching `.sigstore.json` bundle.
- Every published archive and package also includes a `*.provenance.sigstore.json` build provenance attestation and a `*.sbom.sigstore.json` SBOM attestation bundle.
- `checksums.txt` is also signed and published.
- This repository publishes portable Sigstore attestation bundles instead of depending on GitHub's artifact attestation API, which does not support user-owned private repositories.

Example verification:

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/omkhar/minecraft-ping/.github/workflows/release.yml@refs/tags/v.*$' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  checksums.txt
```

Example provenance verification:

```bash
cosign verify-blob-attestation \
  --bundle minecraft-ping_1.1.11_Linux_amd64.tar.gz.provenance.sigstore.json \
  --certificate-identity-regexp '^https://github.com/omkhar/minecraft-ping/.github/workflows/release.yml@refs/tags/v.*$' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  --type slsaprovenance1 \
  minecraft-ping_1.1.11_Linux_amd64.tar.gz
```

## Security Notes

- Private target scanning requires explicit opt-in with `-allow-private`.
- Literal IPs and resolved hostnames are filtered against non-public IPv4 and IPv6 ranges unless `-allow-private` is set.
