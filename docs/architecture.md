# Architecture

`minecraft-ping` is a small Go CLI that measures Minecraft server latency with `ping`-style ergonomics while staying honest about the underlying protocols.

## Design Goals

- Keep the CLI familiar to people who use Unix `ping`.
- Keep Java and Bedrock transport logic separate.
- Prefer simple data flow over layered abstractions.
- Verify behavior with real release artifacts, not only unit tests.

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
- `cmd/staging-server`: portable staging backend for integration validation
- `cmd/release-integration`: release-artifact integration harness

## Probe Flow

### Java Edition

1. Parse the positional destination.
2. Apply Java default port behavior and SRV lookup when appropriate.
3. Resolve and dial the TCP target.
4. Perform handshake, status, and ping/pong exchange.
5. Report latency and session statistics.

### Bedrock Edition

1. Parse the positional destination.
2. Apply Bedrock default port behavior.
3. Resolve and dial the UDP target.
4. Send a RakNet unconnected ping.
5. Validate the unconnected pong and report latency.

## Testing And Release Validation

- Unit and CLI tests cover parsing, protocol behavior, and exit paths.
- Fuzz targets exercise parser robustness.
- Release validation runs against built release archives, not only `go run`.
- Linux integration validates container-backed networking; macOS and Windows use the native staging backend.

