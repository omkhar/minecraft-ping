# CLI Reference

This document summarizes the user-visible CLI contract in a GitHub-readable format.

For the highest-fidelity behavior reference, use `minecraft-ping -h` and the shipped `minecraft-ping(1)` man page.
Use `minecraft-ping -V` when you need the build/version stamp for bug reports or release identification.

For installation, quick-start examples, release trust guidance, or support paths, go back to the top-level [README](../README.md) and [SUPPORT.md](../SUPPORT.md).

## Usage

```text
minecraft-ping [options] destination
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

## Important Notes

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
