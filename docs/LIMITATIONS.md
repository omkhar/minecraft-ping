# Limits and failure behavior

This document uses ASD-STE100 Simplified Technical English.

ASD-STE100 permits technical names. Command names, file names, protocol names,
and data types are technical names.

## Product scope

- The program measures one Minecraft protocol exchange. It does not use ICMP.
- The program does not detect the Minecraft edition. Java is the default. You
  must select Bedrock.
- One process accepts one destination.
- The program does not log in to the server.
- The program does not report the server description, player list, or game
  state.
- A successful probe does not prove that a player can join the server.
- The program does not supply a proxy option.
- The program does not do a reverse DNS lookup.
- The program does not save session history or metrics.

## Command limits

- The default interval is one second. The default probe timeout is five
  seconds.
- A count must be a positive integer.
- A count must fit the Go `int` type. All current release targets use a 64-bit
  `int`.
- An interval, deadline, or timeout must use decimal seconds. The minimum is
  one nanosecond.
- The largest duration is `9,223,372,036.854775807` seconds. This value is
  `9,223,372,036,854,775,807` nanoseconds, which is the `int64` limit.
- The parser rejects a nonfinite value, an exponent form, zero, and a negative
  value.
- The maximum probe timeout is 30 seconds.
- Do not use `-4` and `-6` together.
- Use only one of `--edition`, `--java`, and `--bedrock`.
- `--edition` accepts only `java` or `bedrock`. Edition names are not case
  sensitive.
- JSON mode makes one probe. Do not combine it with `-c`, `-i`, `-w`, `-q`, or
  `-D`.
- `--` can occur before the destination. No argument can occur after that
  destination.
- Help returns status `0`. Invalid arguments print help to standard error and
  return status `2`.
- Version output does not require a destination.

## Destination and address limits

- A server name can contain no more than 253 bytes.
- A server name cannot contain control characters or square brackets.
- A destination port must be from 1 through 65,535.
- Use brackets when an IPv6 destination has an explicit port. A bare IPv6
  address is valid only without an explicit port.
- Java uses port `25565` when the destination has no port.
- Bedrock uses port `19132` for IPv4 and port `19133` for IPv6 when the
  destination has no port.
- By default, the program rejects these IPv4 prefixes: `0.0.0.0/8`,
  `10.0.0.0/8`, `100.64.0.0/10`, `127.0.0.0/8`, `169.254.0.0/16`,
  `172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.168.0.0/16`,
  `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`, `224.0.0.0/4`, and
  `240.0.0.0/4`.
- By default, the program rejects these IPv6 prefixes: `::/128`, `::1/128`,
  `100::/64`, `2001:db8::/32`, `fc00::/7`, `fe80::/10`, and `ff00::/8`.
- `--allow-private` disables this address filter. Use this option only for a
  target that you trust.
- An address-family option applies to literal targets and resolved addresses.
  The program fails when no address in the selected family is usable.
- The resolver result order selects the primary address family. The program
  alternates primary-family and secondary-family addresses and removes
  duplicate addresses.
- The program resolves the destination once before a text session. It does not
  refresh DNS or SRV data during that session.

## Java limits

- Java probing sends a status handshake and reads one status response before
  it sends the measured ping.
- The reported Java latency measures only the ping and pong exchange. It does
  not include DNS, TCP connection, handshake, or status-response time.
- Java uses SRV only for a host name without an explicit port.
- The Java path uses only the first SRV record that the resolver returns. It
  uses the default route when that record is absent or invalid.
- The handshake keeps the original host name. When SRV changes the port, the
  handshake uses the SRV port.
- Java address attempts start 250 milliseconds apart. The first successful TCP
  connection wins.
- The status response must contain one JSON object. The program validates the
  object but does not display its fields.
- The maximum Java packet is 2 MiB.
- The maximum status JSON value is 1 MiB.
- The maximum handshake host value is 255 bytes.
- A Java VarInt can contain no more than five bytes.
- The pong must contain the exact random 64-bit token from the ping.

## Bedrock limits

- Bedrock probing uses RakNet unconnected ping and pong over UDP.
- Bedrock does not use SRV.
- Bedrock tries resolved addresses in sequence. A slow address can delay the
  next address.
- The program reads at most 2,048 bytes for one pong datagram.
- The pong must have the expected packet ID, timestamp, magic value, exact
  length, and valid UTF-8 status text.
- The status text must contain at least the six required `MCPE` fields.
- The protocol version, online-player count, and maximum-player count must be
  decimal integers.
- The program validates available Bedrock status fields but does not display
  them.

## Session and output limits

- Text mode continues until a count, deadline, signal, or context cancellation
  stops the session.
- The interval is the minimum time from one probe start to the next probe
  start. A slow probe can use the full interval, so no additional wait occurs.
- The probe timeout starts after a TCP or UDP connection succeeds. It limits
  socket input and output.
- The probe timeout does not limit DNS, SRV lookup, or TCP or UDP connection
  setup. Those operations can use more time than `-W` specifies.
- The session deadline does not interrupt preparation, connection setup, or an
  active probe. The program checks the deadline between probes.
- The session deadline can shorten socket input and output time for the last
  probe after its connection succeeds.
- A signal cancels DNS and connection setup through their contexts. An active
  socket operation can continue until its socket deadline.
- Text mode does not print one error for each failed probe. The final summary
  reports packet loss.
- Quiet mode hides successful reply lines. It still prints the banner and
  summary.
- If a count and a deadline are both set, text mode returns status `1` when
  replies are fewer than the count. This rule also applies when the count stops
  transmission before the deadline.
- Text mode returns status `1` when it gets no replies.
- JSON mode returns status `2` for a prepare error and status `1` for a probe
  error.
- JSON output contains the destination host, not the resolved address.
- Numeric mode can put the host name in the first banner when DNS gives more
  than one usable address. A successful probe supplies a numeric summary name.
- JSON latency is an integer number of milliseconds. A value below one
  millisecond becomes `1`.
- The text summary uses population standard deviation for `mdev`.
- The program ignores output-stream write errors.

## Staging and integration limits

- `cmd/staging-server` is a test backend. It is not a Minecraft game server.
- The staging server needs at least one Java or Bedrock listen address.
- Its default per-connection deadline is 10 seconds.
- Its Java status value must be valid JSON of at most 1 MiB.
- Its Bedrock status value must start with `MCPE;`. It must use valid UTF-8 and
  fit in a 16-bit wire length.
- The staging Bedrock listener reads at most 2,048 bytes. It accepts only an
  exact 33-byte unconnected ping.
- The release harness waits at most 2 minutes for a listener.
- The timeout for a listener check is 2 seconds.
- The check repeats every 500 milliseconds.
- The release harness uses a default probe timeout of 12 seconds.
- The release harness gives the version command 30 seconds. It gives each
  released-binary probe 2 minutes.
- The release harness gives a container image-load command 2 minutes.
- The deadline for a UDP relay operation is 5 seconds.
- The release harness extracts one exact entry name to a new regular file.
- The maximum extracted binary size is 64 MiB.
- The container integration backend publishes only the Java IPv4 and Bedrock
  IPv4 ports. The harness uses local relays for other required paths.

## Release limits

- Current releases include macOS Arm64, Linux AMD64 and Arm64, and Windows
  AMD64 and Arm64.
- Current releases do not include Intel macOS. `v2.0.6` is the final release
  with an Intel macOS archive.
- Linux packages use `.deb`, `.rpm`, and `.apk` formats.
- Release archives include the license, README, and man-page source.
- Linux packages install the command, license, README, and man page.
- Release builds set `CGO_ENABLED=0`.
- The release workflow accepts only a GitHub-verified signed, annotated tag at
  the current `main` commit.
- The release workflow publishes a draft only after archive, signature, SBOM,
  and applicable provenance checks succeed.
- GitHub artifact attestations are not available for a private repository that
  belongs to a personal account. In that case, the workflow still publishes
  signed artifacts and signed SBOM files.

## Documentation and runtime contract limits

- [`docs/runtime-versions.json`](runtime-versions.json) is the machine-readable
  runtime inventory.
- The runtime tests read a restricted GitHub Actions YAML form. They reject
  aliases, anchors, merge keys, tags, flow mappings, and unsupported runner
  expressions.
- The package-smoke runtime test reads the three-line
  `run_container_smoke` call form in the shipped shell script.
- The function-reference test covers named functions in non-test Go files and
  shipped shell scripts. It does not cover anonymous functions or test-only
  helpers.
