# CLI Reference

This document uses ASD-STE100 Simplified Technical English.

This document gives the user-visible command contract.

For the exact option text, use `minecraft-ping -h` and the shipped
`minecraft-ping(1)` man page.
Use `minecraft-ping -V` when you need the build/version stamp for bug reports or release identification.

For installation, examples, release verification, and support, read the
top-level [README](../README.md) and [SUPPORT.md](../SUPPORT.md).
For the complete boundaries, read [Limits and Failure Behavior](LIMITATIONS.md).

## Usage

```text
minecraft-ping [options] destination
```

## Options

- `-4`: use IPv4 only
- `-6`: use IPv6 only
- `-c count`: stop after `count` probes
- `-i interval`: set the minimum start-to-start probe interval in seconds
- `-w deadline`: stop starting probes after `deadline` seconds
- `-W timeout`: set the socket input/output timeout after each connection
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

- `-i`, `-w`, and `-W` accept decimal seconds from one nanosecond through `9,223,372,036.854775807` seconds.
- The duration parser accepts exponent notation. For example, `1e1` is 10 seconds.
- The default interval is one second. The default probe timeout is five seconds.
- `-W` must be less than or equal to `30` seconds.
- `-W` does not limit DNS work or connection setup. It limits socket input and output after a connection succeeds.
- `-w` does not cancel DNS work, connection setup, or an active probe. The program checks it between probes.
- The interval measures one probe start to the next probe start. The program does not add a wait when a probe uses the full interval.
- `-n` can show the host name in the first banner when DNS gives more than one usable address.
- By default, the CLI rejects loopback, RFC1918, ULA, link-local, and documentation-only IP addresses. Pass `--allow-private` only when you intentionally want to probe a local or private host.
- `-j` is incompatible with `-c`, `-i`, `-w`, `-q`, and `-D`.
- Invalid argv prints the help screen and exits with status `2`.
- One process accepts one destination.
- A server name can contain no more than 253 bytes.
- Use only one of `--edition`, `--java`, and `--bedrock`.

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

- `0`: text mode got a reply and did not meet the special count-and-deadline failure condition, or a JSON probe succeeded
- `1`: text mode got no reply, or count and deadline were both set but replies were fewer than count, or a JSON probe failed
- `2`: argv was invalid, or setup failed before a probe, including JSON preparation failures

## Protocol Behavior

- Java probing uses the Minecraft status and ping/pong handshake over TCP.
- Java performs an SRV lookup only when probing Java Edition with a hostname and no explicit port.
- Bedrock probing uses RakNet unconnected ping/pong over UDP.
- The CLI intentionally does not fake ICMP-only fields such as `ttl`, byte counts, or `icmp_seq`.
- Live `itzg/minecraft-bedrock-server` captures supply the Bedrock wire-format evidence. The Microsoft Script API does not specify this network probe.
