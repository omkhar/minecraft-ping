# minecraft-ping

## Actions

Current GitHub Actions workflow status:

- [CI](https://github.com/omkhar/minecraft-ping/actions/workflows/go.yml): [![CI](https://github.com/omkhar/minecraft-ping/actions/workflows/go.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/go.yml)
- [Dependabot Updates](https://github.com/omkhar/minecraft-ping/actions/workflows/dynamic/dependabot/dependabot-updates): [![Dependabot Updates](https://github.com/omkhar/minecraft-ping/actions/workflows/dynamic/dependabot/dependabot-updates/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/dynamic/dependabot/dependabot-updates)
- [Dependency Graph](https://github.com/omkhar/minecraft-ping/actions/workflows/dynamic/dependabot/update-graph): [![Dependency Graph](https://github.com/omkhar/minecraft-ping/actions/workflows/dynamic/dependabot/update-graph/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/dynamic/dependabot/update-graph)
- [Dependency Review](https://github.com/omkhar/minecraft-ping/actions/workflows/dependency-review.yml): [![Dependency Review](https://github.com/omkhar/minecraft-ping/actions/workflows/dependency-review.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/dependency-review.yml)
- [Mutation Nightly](https://github.com/omkhar/minecraft-ping/actions/workflows/mutation-nightly.yml): [![Mutation Nightly](https://github.com/omkhar/minecraft-ping/actions/workflows/mutation-nightly.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/mutation-nightly.yml)
- [OSV Scanner](https://github.com/omkhar/minecraft-ping/actions/workflows/osv-scanner.yml): [![OSV Scanner](https://github.com/omkhar/minecraft-ping/actions/workflows/osv-scanner.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/osv-scanner.yml)
- [PR Auto-Merge](https://github.com/omkhar/minecraft-ping/actions/workflows/dependabot-auto-merge.yml): [![PR Auto-Merge](https://github.com/omkhar/minecraft-ping/actions/workflows/dependabot-auto-merge.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/dependabot-auto-merge.yml)
- [Release](https://github.com/omkhar/minecraft-ping/actions/workflows/release.yml): [![Release](https://github.com/omkhar/minecraft-ping/actions/workflows/release.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/release.yml)
- [Security Baseline](https://github.com/omkhar/minecraft-ping/actions/workflows/security-baseline.yml): [![Security Baseline](https://github.com/omkhar/minecraft-ping/actions/workflows/security-baseline.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/security-baseline.yml)
- [Semgrep](https://github.com/omkhar/minecraft-ping/actions/workflows/semgrep.yml): [![Semgrep](https://github.com/omkhar/minecraft-ping/actions/workflows/semgrep.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/semgrep.yml)
- [zizmor](https://github.com/omkhar/minecraft-ping/actions/workflows/zizmor.yml): [![zizmor](https://github.com/omkhar/minecraft-ping/actions/workflows/zizmor.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/zizmor.yml)

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

- `CI`: runs `govulncheck`, `gosec`, `go build`, native `go test` coverage on Linux, macOS, and Windows for `amd64` and `arm64`, a release-snapshot packaging build, Linux package install smoke tests on Debian, Ubuntu, Fedora, and Alpine for `amd64` and `arm64`, and a final dual-stack integration matrix that executes every release binary against the staging Minecraft target with both `-4` and `-6`; it also runs diff-scoped mutation testing on pull requests.
- `Release`: builds cross-platform archives plus Linux distro packages with GoReleaser, injects the release version used by `-version`, publishes GitHub release assets, and signs all artifacts with keyless `cosign`.
- `OSV Scanner`: checks dependency advisories against OSV.
- `Security Baseline`: runs `gitleaks`.
- `Dependency Review`: enforces PR dependency policy checks.
- `Mutation Nightly`: runs scheduled mutation testing.
- `zizmor`: lints GitHub Actions workflow security.

## Operations

- Final integration helper: `scripts/run_release_integration.sh`
- Staging backend source: `cmd/staging-server`
- Staging image definition: `docker/staging-minecraft.Dockerfile`
- CI helper: `scripts/ci_wait_for_checks.sh`

The final integration gate uses a small first-party staging backend that speaks the real Minecraft status and ping/pong protocol. Every release archive for Linux, macOS, and Windows on `amd64` and `arm64` is executed natively against that target with both `-4` and `-6`. Linux runners also build the staging container and run the Linux release binaries against it, with the IPv6 probe exposed through a local `::1` relay backed by the same container so the test remains portable even when the Docker runtime does not publish IPv6 loopback ports directly.

## Releases

- Tag `vX.Y.Z` on `main` to publish a release.
- Release assets include signed archives for `darwin`, `linux`, and `windows` on `amd64` and `arm64`.
- Linux release assets also include signed `.deb`, `.rpm`, and `.apk` packages for `amd64` and `arm64`.
- Every uploaded asset includes a matching `.sigstore.json` bundle.
- `checksums.txt` is also signed and published.

Example verification:

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/omkhar/minecraft-ping/.github/workflows/release.yml@refs/tags/v.*$' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  checksums.txt
```

## Security Notes

- Private target scanning requires explicit opt-in with `-allow-private`.
- Literal IPs and resolved hostnames are filtered against non-public IPv4 and IPv6 ranges unless `-allow-private` is set.
