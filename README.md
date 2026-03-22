# minecraft-ping

`minecraft-ping` is a small Go CLI that measures Minecraft server latency with Unix-`ping` ergonomics.

- Java Edition is the default.
- Bedrock Edition is available with `--bedrock` or `--edition bedrock`.
- Text mode behaves like a `ping` session.
- JSON mode is single-probe and intended for scripts and automation.

## Install

While this repository remains private, build from source from an authorized checkout:

```bash
go build ./...
```

Once the repository is public, the canonical install path will be:

```bash
go install github.com/omkhar/minecraft-ping@latest
```

## Quick Start

```text
minecraft-ping [options] destination
```

```bash
# Java, default port 25565, continuous until Ctrl-C
minecraft-ping mc.example.com

# Java, 3 probes
minecraft-ping -c 3 mc.example.com

# Bedrock, default IPv4 port 19132
minecraft-ping --bedrock play.example.com

# Bedrock, default IPv6 port 19133
minecraft-ping --bedrock -6 2001:db8::20

# Explicit port
minecraft-ping --bedrock play.example.com:19133

# Single JSON probe
minecraft-ping -j mc.example.com
```

## Supported Options

- `-4`: IPv4 only
- `-6`: IPv6 only
- `-c count`: stop after `count` probes
- `-i interval`: wait `interval` seconds between probes
- `-w deadline`: stop after `deadline` seconds
- `-W timeout`: wait `timeout` seconds for each probe
- `-q`: quiet mode
- `-D`: prefix live reply lines with a Unix timestamp
- `-n`: numeric output only
- `-j`: JSON output for a single probe
- `-V`: print version and exit
- `-h`: print help
- `--edition java|bedrock`: select the Minecraft edition
- `--java`: alias for `--edition java`
- `--bedrock`: alias for `--edition bedrock`

Invalid argv prints the help screen and exits with status `2`.

## Destinations And Ports

The destination is positional and may be:

- `host`
- `host:port`
- `[ipv6]:port`
- bare IPv6 literal such as `2001:db8::20`

Default ports:

- Java: `25565`
- Bedrock IPv4: `19132`
- Bedrock IPv6: `19133`

If a port is present in the destination, it always wins.

## Protocol Notes

- Java probing uses the Minecraft status and ping/pong handshake over TCP, including SRV lookup for default-port hostnames.
- Bedrock probing uses RakNet unconnected ping/pong over UDP.
- Bedrock wire-format validation in this repository is based on live `itzg/minecraft-bedrock-server` captures. The Microsoft Script API docs are not the network probe specification.

## Project Documentation

- [Architecture](docs/architecture.md)
- [Development](docs/development.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)

## Developing

For a quick local validation loop:

```bash
go test ./...
go test -race ./...
```

For packaging, workflow, and release-path changes, also run:

```bash
scripts/run_release_integration.sh
```

## Release Artifacts

Releases are built from GitHub Actions on signed tags at the current `main` head.

- Signed release artifacts are published for macOS, Linux, and Windows.
- Signed SPDX SBOM assets are published with each release.
- In the current user-owned private-repository mode, releases publish signed artifacts and signed SBOM assets only.
- GitHub artifact attestations and downloaded provenance bundles are enabled automatically when repository visibility and ownership support them.

## License

This project is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
