# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

Go CLI tool that measures latency to Minecraft Java and Bedrock servers with Linux-`ping` ergonomics. Java uses the native handshake -> status -> ping/pong sequence over TCP, and Bedrock uses RakNet unconnected ping/pong over UDP.

## Commands

```bash
# Build
go build -v ./...

# Show help
go run . -h

# Show embedded build version
go run . -V

# Run Java continuously until Ctrl-C
./minecraft-ping mc.example.com

# Run Bedrock
./minecraft-ping --bedrock play.example.com

# Single JSON probe
./minecraft-ping -j mc.example.com

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
- `cli.go` — Linux-style argv parsing, help/version handling, JSON mode, and process exit behavior
- `version.go` — build-time version string used by `-V`
- `client.go` — shared request validation, resolution, dialing, and legacy direct-probe helpers
- `editions.go` — edition selection, destination parsing, and edition-aware default ports
- `endpoint.go` — endpoint normalization plus address-family and host validation
- `address.go` — family-aware dial candidate ordering
- `java.go` — Java route resolution plus Java session probing
- `bedrock.go` — Bedrock UDP/RakNet probing and strict pong parsing
- `probe.go` — edition dispatch for prepared probes
- `session.go` — ping-style text session loop and summary rendering
- `protocol.go` — Java packet serialization and parsing for handshake, status, and ping/pong
- `minecraft-ping_test.go` — end-to-end protocol, resolution, and dialing tests with fakes
- `minecraft_ping_cli_test.go` — CLI behavior tests for help, version, output, and exit paths
- `ping_fuzz_test.go` — fuzz targets for parser robustness
- `cmd/staging-server` — portable staging backend that implements the Minecraft status and ping/pong protocol used by the final integration gate
- `cmd/release-integration` — archive-aware integration harness for executing release binaries against either the native staging backend or the staging container

## CLI Behavior

- Public CLI is `minecraft-ping [options] destination`
- Java is the default edition; Bedrock is selected with `--bedrock` or `--edition bedrock`
- Text mode is `ping`-style and continuous by default; `-c` makes it finite
- `-j` emits `{"server":"...","latency_ms":N}` for a single probe
- `-4` forces IPv4 resolution and dialing
- `-6` forces IPv6 resolution and dialing
- `-V` prints the embedded build version and exits
- Invalid argv prints the help screen and exits with status `2`

## CI

`PR Fast` (`.github/workflows/pr-fast.yml`) runs `go mod verify`, `actionlint`, `gofmt`, `goimports`, `go vet`, `staticcheck`, `ineffassign`, `gocritic`, `zizmor`, dependency review, `govulncheck`, `gosec`, `gitleaks`, Linux race tests, Linux shuffled tests, and diff-scoped mutation testing.

`PR Network` (`.github/workflows/pr-network.yml`) runs a Linux dual-stack container integration gate when pull requests touch networking, packaging, workflow, or integration paths. It requires both `-4` and `-6` to succeed against the live staging container.

`Main Verify` (`.github/workflows/go.yml`) runs the same lint, dependency, security, and Linux unit-test gates on `main`, then adds native `go test ./...` coverage on Linux, macOS, and Windows for `amd64` and `arm64`, a GoReleaser snapshot build, Linux package install smoke on Debian, Fedora, and Alpine for `amd64` and `arm64`, and the full release-archive dual-stack integration matrix.

The final integration harness executes real release artifacts everywhere. Linux validates the live staging container path; macOS and Windows validate the native staging backend because GitHub-hosted runners do not provide an equivalent portable dual-stack container runtime.

`Deep Validation` (`.github/workflows/deep-validation.yml`) runs weekly or manually and covers the parser fuzz targets plus the full mutation suite.

`Release` (`.github/workflows/release.yml`) only publishes from the exact signed tag at the current `main` head after `Main Verify` passes. It is tag-triggered only, builds on GitHub-hosted runners, publishes GitHub artifact attestations as the release provenance source of truth, injects the release version for `-version`, and uploads signed SPDX SBOM assets plus downloaded provenance bundles for offline verification.

## Notes

- Packet IDs are named constants in `protocol.go`; preserve that style.
- Keep the transport path simple: validation -> destination parsing -> edition-aware resolution -> dial -> protocol exchange.
- Prefer tests that exercise real CLI behavior and fake network boundaries over broad mocking of internal implementation details.
