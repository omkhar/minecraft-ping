# Architecture

`minecraft-ping` is a small Go CLI that measures Minecraft server latency with `ping`-style ergonomics while staying honest about the underlying Minecraft protocols.

## Design Principles

- Keep the CLI familiar to people who already use Unix `ping`.
- Keep Java and Bedrock transport logic separate and explicit.
- Prefer simple data flow over layered abstractions.
- Validate behavior with real release artifacts, not only `go run`.

## Runtime Model

The shipped binary has two top-level execution paths:

- text mode: continuous by default, session-oriented, summary on exit
- JSON mode: single probe, machine-readable output, intended for scripts

Java Edition is the default. Bedrock Edition is selected explicitly with `--bedrock` or `--edition bedrock`. The CLI does not auto-detect editions.

## Repository Layout

- `minecraft-ping.go`: program entry point
- `cli.go`: argv parsing, help/version handling, JSON mode, and exit behavior
- `version.go`: build-time version string used by `-V`
- `editions.go`: edition parsing plus destination and default-port handling
- `client.go`: shared validation, resolution, and dialing helpers
- `address.go`: family-aware dial candidate ordering
- `endpoint.go`: endpoint normalization and validation
- `java.go`: Java status and ping/pong probing over TCP
- `bedrock.go`: Bedrock RakNet unconnected ping/pong probing over UDP
- `probe.go`: edition dispatch for prepared probes
- `session.go`: text-mode session loop and summary rendering
- `protocol.go`: Java packet serialization and parsing
- `cmd/staging-server`: portable staging backend used only for integration validation
- `cmd/release-integration`: release-artifact integration harness

## Probe Flow

### Java Edition

1. Parse the positional destination.
2. Apply Java default-port behavior.
3. Perform an SRV lookup only when the target is a hostname with no explicit port.
4. Resolve and dial the TCP target.
5. Perform the Minecraft handshake, status request, and ping/pong exchange.
6. Report latency and session statistics.

### Bedrock Edition

1. Parse the positional destination.
2. Apply Bedrock default-port behavior, including `19133` for IPv6 when no port is explicit.
3. Resolve and dial the UDP target.
4. Send a RakNet unconnected ping.
5. Validate the unconnected pong.
6. Report latency and session statistics.

## Validation Model

- Unit and CLI tests cover parsing, protocol behavior, and exit paths.
- Fuzz targets exercise Java packet parsing robustness.
- `Main Verify` builds release archives and validates shipped binaries, not only source trees.
- Release integration probes both Java and Bedrock over IPv4 and IPv6.
- Linux release integration validates the container-backed path; macOS and Windows validate against the native staging backend.
