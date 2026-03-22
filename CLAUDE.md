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

`CI` (`.github/workflows/go.yml`) runs:

- `govulncheck`
- `gosec`
- `go build`
- native `go test` coverage on Linux, macOS, and Windows for `amd64` and `arm64`
- a GoReleaser snapshot build that exercises release archives and Linux packages before tagging
- Linux package install smoke tests on Debian, Ubuntu, Fedora, and Alpine for `amd64` and `arm64`
- a final dual-stack release integration matrix that runs every released binary against the staging target with both `-4` and `-6`
- diff-scoped mutation testing on pull requests

The final integration harness runs a native staging backend everywhere so every OS and architecture is exercised by the real released binary. On Linux it also validates the containerized form of that same backend, and the IPv6 path is exposed through a local `::1` relay to avoid Docker runtime differences in direct IPv6 loopback publishing.

`Release` (`.github/workflows/release.yml`) uses GoReleaser to build signed archives for macOS, Linux, and Windows, build signed Linux `.deb`, `.rpm`, and `.apk` packages, inject the release version for `-version`, and publish the release assets on GitHub.

## Notes

- Packet IDs are named constants in `protocol.go`; preserve that style.
- Keep the transport path simple: validation -> SRV resolution -> family-aware resolution -> dial -> protocol exchange.
- Prefer tests that exercise real CLI behavior and fake network boundaries over broad mocking of internal implementation details.
