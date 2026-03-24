# minecraft-ping

`minecraft-ping` measures Minecraft server latency with `ping`-style ergonomics while staying honest about the underlying protocols.

- Java Edition is the default.
- Bedrock Edition is available with `--bedrock` or `--edition bedrock`.
- Text mode behaves like a `ping` session and runs continuously by default.
- JSON mode is a single probe intended for scripts and automation.

## Installation

Install the public module:

```bash
go install github.com/omkhar/minecraft-ping/v2@latest
```

Install from a checkout:

```bash
go install .
```

If you want a local binary in the repository root instead:

```bash
go build -o minecraft-ping .
```

Signed release archives and Linux packages are published from tagged releases on [GitHub Releases](https://github.com/omkhar/minecraft-ping/releases).
When the repository is private, use an authorized checkout or the corresponding authorized release artifacts.
Linux packages install the `minecraft-ping(1)` man page, and release archives ship the same source at `man/minecraft-ping.1`.

## Usage

```text
minecraft-ping [options] destination
```

Examples:

```bash
# Java, default port 25565, continuous until Ctrl-C
minecraft-ping mc.example.com

# Java, 3 probes
minecraft-ping -c 3 mc.example.com

# Bedrock, default port selected from the resolved address family
minecraft-ping --bedrock play.example.com

# Bedrock, default IPv6 port 19133
minecraft-ping --bedrock -6 2001:db8::20

# Explicit port
minecraft-ping --bedrock play.example.com:19133

# Single JSON probe
minecraft-ping -j mc.example.com
```

## Options

- `-4`: use IPv4 only
- `-6`: use IPv6 only
- `-c count`: stop after `count` probes
- `-i interval`: wait `interval` seconds between probes
- `-w deadline`: stop after `deadline` seconds
- `-W timeout`: wait `timeout` seconds for each probe
- `-q`: quiet mode
- `-D`: prefix live reply lines with a Unix timestamp
- `-n`: numeric output only
- `-j`: emit a single JSON probe result
- `-V`, `--version`: print version and exit
- `-h`, `--help`: print help and exit
- `--edition java|bedrock`: select the Minecraft edition
- `--java`: alias for `--edition java`
- `--bedrock`: alias for `--edition bedrock`

Notes:

- `-i`, `-w`, and `-W` accept positive seconds and may be fractional, for example `0.5`.
- `-W` must be less than or equal to `30` seconds.
- `-j` is incompatible with `-c`, `-i`, `-w`, `-q`, and `-D`.
- Invalid argv prints the help screen and exits with status `2`.

## Destinations And Default Ports

The destination is positional and may be:

- `host`
- `host:port`
- `[ipv6]:port`
- bare IPv6 literal such as `2001:db8::20`

Default ports:

- Java: `25565`
- Bedrock over IPv4: `19132`
- Bedrock over IPv6: `19133`

If a port is present in the destination, it always wins.

## Exit Status

- `0`: at least one reply was received, or a JSON probe succeeded
- `1`: no replies were received, or a JSON probe reached the probe step and failed
- `2`: invalid argv or a setup/runtime error occurred before a meaningful probe completed, including JSON prepare/setup failures

## Protocol Behavior

- Java probing uses the Minecraft status and ping/pong handshake over TCP.
- Java performs an SRV lookup only when probing Java Edition with a hostname and no explicit port.
- Bedrock probing uses RakNet unconnected ping/pong over UDP.
- The CLI intentionally does not fake ICMP-only fields such as `ttl`, byte counts, or `icmp_seq`.
- Bedrock wire-format validation in this repository is based on live `itzg/minecraft-bedrock-server` captures. The Microsoft Script API documentation is not the network probe specification.

## Supported Release Targets

Release artifacts are built for:

- macOS `amd64` and `arm64`
- Linux `amd64` and `arm64`
- Windows `amd64` and `arm64`

## Project Documentation

- [Architecture](docs/architecture.md)
- [Development](docs/development.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Man Page Source](man/minecraft-ping.1)

## Development

For a quick local validation loop:

```bash
go test ./...
go test -race ./...
```

For networking, workflow, or release-path changes, also run:

```bash
scripts/run_release_integration.sh
```

The release integration script uses the native staging backend by default. To exercise the Linux container-backed path locally, set `MINECRAFT_RELEASE_INTEGRATION_BACKEND=container` and make sure Docker or Podman is available.

The container-backed path also requires a staging image. Build it first with:

```bash
CONTAINER_CLI="${CONTAINER_CLI:-docker}"
"$CONTAINER_CLI" build -f docker/staging-minecraft.Dockerfile -t minecraft-staging-image:ci .
```

Then run the integration script. Set `CONTAINER_CLI=podman` if you are using Podman, or point the script at an existing image with `MINECRAFT_STAGING_IMAGE`.

For packaging changes, also run `scripts/release_archive_smoke.sh dist` after a snapshot build and reproduce the Linux package smoke path described in [docs/development.md](docs/development.md) when your environment supports it.
If you are changing the release path itself, also run `scripts/release_reproducibility.sh dist` to confirm the snapshot artifacts rebuild identically from a second checkout path.

For heavyweight batch validation on a shared Linux builder, use `scripts/run_shared_builder_checks.sh` instead of running the release-path commands directly in a shared checkout. The maintainer guide in [docs/development.md](docs/development.md) documents the required Docker-compatible socket setup and the isolation model.

## Release Artifacts

Releases are built from GitHub Actions on signed, annotated tags at the current `main` head.

- Signed release artifacts are published for macOS, Linux, and Windows.
- Release archives and packages are built with deterministic paths and commit-based mtimes so the artifact bytes can be reproduced from the same source tree and toolchain.
- The release workflow is configured to publish signed SPDX SBOM assets for each release.
- GitHub artifact attestations and downloaded provenance bundles are published automatically when repository visibility and ownership support them.
- Until GitHub supports first-party attestations for the active repository configuration, releases continue to publish signed artifacts and signed SBOM assets.

## License

This project is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
