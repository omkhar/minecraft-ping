# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

Go CLI tool that measures latency to a Minecraft Java Edition server using the native handshake -> status -> ping/pong sequence. It supports SRV lookup, explicit IPv4 or IPv6 forcing, and private-address filtering.

## Commands

```bash
# Build
go build -v ./...

# Show flags
go run . -help

# Show embedded build version
go run . -version

# Run
./minecraft-ping -server mc.example.com \
  [-port 25565] \
  [-timeout 5s] \
  [-allow-private] \
  [-format text|json] \
  [-4|-6]

# Unit tests
go test -v ./...

# Fuzz tests (run locally; CI runs them from the unit-test workflow)
go test ./... -run=^$ -fuzz=FuzzReadVarIntFromBytes -fuzztime=30s
go test ./... -run=^$ -fuzz=FuzzReadStringFromBytes -fuzztime=30s
go test ./... -run=^$ -fuzz=FuzzReadPacket -fuzztime=30s

# Final release-binary integration
scripts/run_release_integration.sh
```

## Architecture

- `minecraft-ping.go` — program entry point
- `cli.go` — CLI parsing, help/version handling, output rendering, and process exit behavior
- `version.go` — build-time version string used by `-version`
- `client.go` — request validation, SRV handling, address resolution, dialing, and ping execution
- `endpoint.go` — endpoint normalization plus address-family and host validation
- `address.go` — non-public address detection and family-aware dial candidate ordering
- `protocol.go` — packet serialization and parsing for handshake, status, and ping/pong
- `minecraft-ping_test.go` — end-to-end protocol, resolution, and dialing tests with fakes
- `minecraft_ping_cli_test.go` — CLI behavior tests for help, version, output, and exit paths
- `ping_fuzz_test.go` — fuzz targets for parser robustness
- `cmd/staging-server` — portable staging backend that implements the Minecraft status and ping/pong protocol used by the final integration gate
- `cmd/release-integration` — archive-aware integration harness for executing release binaries against either the native staging backend or the staging container

## CLI Behavior

- Default output is human-readable text: `Ping time is N ms`
- `-format=json` emits `{"server":"...","latency_ms":N}`
- `-4` forces IPv4 resolution and dialing
- `-6` forces IPv6 resolution and dialing
- `-version` prints the embedded build version and exits
- Private and loopback targets are rejected unless `-allow-private` is set

## CI

`PR Fast` (`.github/workflows/pr-fast.yml`) runs `go mod verify`, `actionlint`, `gofmt`, `goimports`, `go vet`, `staticcheck`, `ineffassign`, `gocritic`, `zizmor`, dependency review, `govulncheck`, `gosec`, `gitleaks`, Linux race tests, Linux shuffled tests, and diff-scoped mutation testing.

`PR Network` (`.github/workflows/pr-network.yml`) runs a Linux dual-stack container integration gate when pull requests touch networking, packaging, workflow, or integration paths. It requires both `-4` and `-6` to succeed against the live staging container.

`Main Verify` (`.github/workflows/go.yml`) runs the same lint, dependency, security, and Linux unit-test gates on `main`, then adds native `go test ./...` coverage on Linux, macOS, and Windows for `amd64` and `arm64`, a GoReleaser snapshot build, Linux package install smoke on Debian, Fedora, and Alpine for `amd64` and `arm64`, and the full release-archive dual-stack integration matrix.

The final integration harness executes real release artifacts everywhere. Linux validates the live staging container path; macOS and Windows validate the native staging backend because GitHub-hosted runners do not provide an equivalent portable dual-stack container runtime.

`Deep Validation` (`.github/workflows/deep-validation.yml`) runs weekly or manually and covers the parser fuzz targets plus the full mutation suite.

`Release` (`.github/workflows/release.yml`) only publishes from the exact signed tag at the current `main` head after `Main Verify` passes. It uses GoReleaser to build signed archives for macOS, Linux, and Windows, build signed Linux `.deb`, `.rpm`, and `.apk` packages, inject the release version for `-version`, publish signed SPDX SBOMs, and upload GitHub build provenance attestations for the release artifacts and SBOMs.

## Notes

- Packet IDs are named constants in `protocol.go`; preserve that style.
- Keep the transport path simple: validation -> SRV resolution -> family-aware resolution -> dial -> protocol exchange.
- Prefer tests that exercise real CLI behavior and fake network boundaries over broad mocking of internal implementation details.
