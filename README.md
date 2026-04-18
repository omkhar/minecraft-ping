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

# Local or private target
minecraft-ping --allow-private 127.0.0.1:25565

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
- `--allow-private`: allow loopback, private, link-local, and documentation-only IP targets
- `-V`, `--version`: print version and exit
- `-h`, `--help`: print help and exit
- `--edition java|bedrock`: select the Minecraft edition
- `--java`: alias for `--edition java`
- `--bedrock`: alias for `--edition bedrock`

Notes:

- `-i`, `-w`, and `-W` accept positive seconds and may be fractional, for example `0.5`.
- `-W` must be less than or equal to `30` seconds.
- By default, the CLI rejects loopback, RFC1918, ULA, link-local, and documentation-only IP addresses. Pass `--allow-private` only when you intentionally want to probe a local or private host.
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

## Release Artifacts

Releases are built from GitHub Actions on GitHub-verified signed, annotated tags at the current `main` head.

- Signed release artifacts are published for macOS, Linux, and Windows.
- Release archives and packages are built with deterministic paths and commit-based mtimes so the artifact bytes can be reproduced from the same source tree and toolchain.
- The release workflow publishes signed SPDX SBOM assets for each release.
- GitHub artifact attestations and downloadable provenance bundles are published with public releases.

For quick consumer verification instructions, see [Release Verification](docs/release-verification.md).

## Support

- Usage questions, operational questions, and maintainer contact: `minecraft-ping@omkhar.net`
- Bugs and feature requests: open a GitHub issue using the repository templates
- Security reports: follow [SECURITY.md](SECURITY.md) and do not file a public issue

## Project Documentation

- [Architecture](docs/architecture.md)
- [Development](docs/development.md)
- [Release Verification](docs/release-verification.md)
- [Releasing](docs/releasing.md)
- [Agent Instructions](AGENTS.md)
- [Contributing](CONTRIBUTING.md)
- [Support](SUPPORT.md)
- [Governance](GOVERNANCE.md)
- [Security](SECURITY.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Changelog](CHANGELOG.md)
- [Man Page Source](man/minecraft-ping.1)

## Development

For the common local loop:

```bash
make verify
make coverage
make clean-repo
```

For networking changes, `scripts/run_release_integration.sh` validates locally built binaries against the staging backends.
For release artifact changes, use the archive smoke, reproducibility, and package smoke checks documented in [docs/development.md](docs/development.md).
For maintainer release steps, see [docs/releasing.md](docs/releasing.md).

## License

This project is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
