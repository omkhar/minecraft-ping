# `minecraft-ping`

`minecraft-ping` is a Linux-`ping`-style CLI for Minecraft servers.

- Java Edition is the default.
- Bedrock Edition is available with `--bedrock` or `--edition bedrock`.
- Text mode runs continuously by default and prints a `ping`-shaped session transcript.
- JSON mode is single-probe only and is intended for scripting.

## Install

```bash
go install github.com/omkhar/minecraft-ping@latest
```

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

# Bedrock, default IPv4 port 19132
minecraft-ping --bedrock play.example.com

# Bedrock, default IPv6 port 19133
minecraft-ping --bedrock -6 2001:db8::20

# Explicit port
minecraft-ping --bedrock play.example.com:19133

# Single JSON probe
minecraft-ping -j mc.example.com
```

Supported options:

- `-4`: IPv4 only.
- `-6`: IPv6 only.
- `-c count`: Stop after `count` probes.
- `-i interval`: Wait `interval` seconds between probes.
- `-w deadline`: Stop after `deadline` seconds.
- `-W timeout`: Wait `timeout` seconds for each probe.
- `-q`: Quiet mode.
- `-D`: Prefix live reply lines with a Unix timestamp.
- `-n`: Numeric output only.
- `-j`: JSON output for a single probe.
- `-V`: Print version and exit.
- `-h`: Print help.
- `--edition java|bedrock`: Select the Minecraft edition.
- `--java`: Alias for `--edition java`.
- `--bedrock`: Alias for `--edition bedrock`.

Invalid argv prints the help screen and exits with status `2`.

## Destination Rules

The destination is positional and may be one of:

- `host`
- `host:port`
- `[ipv6]:port`
- bare IPv6 literal like `2001:db8::20`

Edition-specific default ports:

- Java: `25565`
- Bedrock IPv4: `19132`
- Bedrock IPv6: `19133`

If a port is present in the destination, it always wins.

## Output Modes

Text mode is the default. It prints a banner, one line per successful reply, and a final summary on completion or `Ctrl-C`.

JSON mode prints:

```json
{"server":"mc.example.com","latency_ms":12}
```

## Notes

- Java probing uses the Minecraft status and ping/pong handshake over TCP, including SRV lookup for default-port hostnames.
- Bedrock probing uses RakNet unconnected ping/pong over UDP.
- Bedrock wire-format validation in this repo is based on live `itzg/minecraft-bedrock-server` captures. The Microsoft Script API docs are not the network probe specification.

## Development

Run tests:

```bash
go test ./...
```

Release integration harness:

- Staging backend source: `cmd/staging-server`
- Release harness: `cmd/release-integration`

Releases are published only by the GitHub tag-triggered release workflow. That workflow is the canonical build provenance source and emits signed SBOM assets plus GitHub artifact attestations for the published binaries and packages.
