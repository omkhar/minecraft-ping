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

## Quick Start

```text
minecraft-ping [options] destination
```

At a glance:

- `--bedrock` or `--edition bedrock`: switch from Java to Bedrock
- `-c count`: stop after a fixed number of probes
- `-j`: emit one JSON probe for scripts
- `-4` or `-6`: force one address family
- `--allow-private`: permit local or private IP targets

Common examples:

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

## Key Behavior

- Java and Bedrock stay explicit. The CLI does not auto-detect editions.
- By default, the CLI rejects loopback, RFC1918, ULA, link-local, and documentation-only IP addresses. Pass `--allow-private` only when you intentionally want to probe a local or private host.
- Java probing uses the Minecraft status and ping/pong handshake over TCP and only performs SRV lookup when the target is a hostname with no explicit port.
- Bedrock probing uses RakNet unconnected ping/pong over UDP.
- The CLI intentionally does not fake ICMP-only fields such as `ttl`, byte counts, or `icmp_seq`.

For the full flag reference, destination rules, exit status contract, and protocol notes, see [CLI Reference](docs/cli-reference.md).

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
- Bug reports: [open a bug report](https://github.com/omkhar/minecraft-ping/issues/new?template=bug_report.yml)
- Feature requests: [open a feature request](https://github.com/omkhar/minecraft-ping/issues/new?template=feature_request.yml)
- Security reports: [open a private security advisory](https://github.com/omkhar/minecraft-ping/security/advisories/new) or follow [SECURITY.md](SECURITY.md)

## Project Documentation

Users:

- [CLI Reference](docs/cli-reference.md)
- [Release Verification](docs/release-verification.md)
- [Support](SUPPORT.md)
- [Man Page Source](man/minecraft-ping.1)

Contributors:

- [Contributing](CONTRIBUTING.md)
- [Development](docs/development.md)
- [Architecture](docs/architecture.md)

Maintainer And Policy:

- [Releasing](docs/releasing.md)
- [Security](SECURITY.md)
- [Governance](GOVERNANCE.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Changelog](CHANGELOG.md)

Automation:

- [Agent Instructions](AGENTS.md)

## License

This project is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
