# Function reference

This document uses ASD-STE100 Simplified Technical English.

ASD-STE100 permits technical names. Function names, file names, commands, and
data types are technical names.

This reference separates supported user functions from implementation
functions.
The implementation catalog covers each named function in production Go files
and shipped shell scripts.
It does not cover test-only helpers or anonymous functions.

## Supported user functions

The installed `minecraft-ping` command is the only supported user runtime.
The project also supports the distribution functions in this section.

### Command options

| Option | Supported function |
| --- | --- |
| `-4` | It uses IPv4 only. |
| `-6` | It uses IPv6 only. |
| `-c count` | It sets a positive maximum probe count. |
| `-i interval` | It sets the minimum probe start-to-start interval. |
| `-w deadline` | It stops new probes after the session deadline. |
| `-W timeout` | It sets the socket input and output timeout after connection. |
| `-q` | It hides successful reply lines. It keeps the banner and summary. |
| `-D` | It adds a Unix timestamp to each successful reply line. |
| `-n` | It requests numeric address output. |
| `-j` | It runs one probe. After a successful probe, it writes one JSON object. |
| `--allow-private` | It permits targets in non-public address ranges. |
| `-V`, `--version` | It writes the version and exits. |
| `-h`, `--help` | It writes command help and exits. |
| `--edition java|bedrock` | It selects Java Edition or Bedrock Edition. |
| `--java` | It selects Java Edition. |
| `--bedrock` | It selects Bedrock Edition. |

### Other supported surfaces

| Surface | Contract |
| --- | --- |
| `command.options` | The command-options table lists every flag that `cli.go` accepts. |
| `command.no-subcommands` | The command has no subcommands. One process accepts one destination. |
| `command.destination` | The destination accepts a host, a host and port, bracketed IPv6 with or without a port, or bare IPv6. |
| `command.defaults` | Java and the automatic address family are defaults. The interval is one second. The timeout is five seconds. By default, there is no count. By default, there is no deadline. |
| `command.precedence` | Without `--`, options can occur before or after the destination. After `--`, the destination must be the final argument. Explicit ports win. Do not use `-4` and `-6` together. Use only one of `--edition`, `--java`, and `--bedrock`. Do not combine `-j` with `-c`, `-i`, `-w`, `-q`, or `-D`. Conflicts return status `2`. |
| `command.text-mode` | Text mode writes a banner and a final summary. Unless quiet, it writes successful replies. |
| `command.json-mode` | JSON mode runs one probe. After a successful probe, it writes one object. |
| `command.help` | Help writes usage to standard output and returns status `0`. |
| `command.version` | Version writes the build version to standard output and returns status `0`. |
| `command.output` | Normal results use standard output. Errors use standard error. The command ignores output-stream write errors. |
| `command.exit-status` | Status `0` means success. Status `1` means a JSON probe failure, no text replies, or too few replies when count and deadline are both set. Status `2` means an argument or preparation failure. |
| `network.java` | Java uses a status handshake and ping and pong over TCP. Its default port is `25565`. It uses SRV only for an implicit port. |
| `network.bedrock` | Bedrock uses RakNet ping and pong over UDP. Its default IPv4 port is `19132`. Its default IPv6 port is `19133`. It does not use SRV. |
| `network.address-selection` | The command resolves host names, filters non-public addresses, and applies automatic, IPv4, or IPv6 selection. |
| `distribution.source-install` | Users can install the command with `go install`. |
| `distribution.archives` | Releases contain macOS Arm64, Linux AMD64, Linux Arm64, Windows AMD64, and Windows Arm64 archives. macOS and Linux use `tar.gz`. Windows uses ZIP. |
| `distribution.linux-packages` | Linux packages use DEB, RPM, and APK formats. |
| `distribution.source-archive` | Releases include a `tar.gz` source archive. |
| `distribution.verification-assets` | Releases include `checksums.txt`, Sigstore bundles, and signed SPDX SBOM files. Public releases also include GitHub provenance bundles. |
| `runtime.pinned` | The runtime inventory records pinned tools, actions, runners, and container images. |
| `deployment.none` | The project does not publish a production server or container. The staging server and container support validation only. |
| `library.none` | The root is a `main` package. It does not expose a supported Go library API. |

## Implementation function catalog

These functions implement the command and repository validation.
They are not a supported library API.

### Command and option functions

| Function | Input, result, and failure behavior |
| --- | --- |
| `main.main` | It gives process arguments and streams to `run`. It exits with the returned status. |
| `main.run` | It creates the normal runtime and calls `runWithRuntime`. |
| `main.runWithRuntime` | It parses the command, prepares one probe, and runs JSON mode or text mode. It returns status `0`, `1`, or `2`. |
| `main.defaultCLIRuntime` | It creates the signal context, probe function, clock, and sleep function for the command. |
| `main.usageText` | It returns the complete command help text. |
| `main.parseCLIConfig` | It scans the arguments and returns a normalized command configuration. |
| `main.scanArgv` | It reads options before or after the destination. It permits only one destination. After `--`, it requires the destination as the final argument. It rejects unknown options and empty arguments. |
| `main.consumeLongFlag` | It reads one long option and its value. It rejects an unsupported form. |
| `main.consumeShortFlags` | It reads one short-option group and any required value. It rejects an unsupported option. |
| `main.normalizeCLIConfig` | It applies defaults and checks option conflicts, values, the edition, and the destination. |
| `main.parseSecondsDuration` | It converts seconds from one nanosecond through the `int64` limit. It accepts exponent notation. It rejects nonfinite, zero, and negative values. |
| `main.durationToLatencyMs` | It converts a duration to whole milliseconds. It returns at least `1`. |
| `main.edition.String` | It returns `java` or `bedrock`. |
| `main.parseEdition` | It reads an empty value or `java` as Java. It reads `bedrock` as Bedrock and rejects other values. |
| `main.newTargetSpec` | It creates a normalized destination and records whether the port is explicit. |
| `main.targetSpec.String` | It returns the host, or it returns the host and explicit port. |
| `main.targetSpec.validate` | It requires a valid host. It checks the port only when the destination has an explicit port. |
| `main.targetSpec.literalIP` | It returns a normalized IP address when the host is an IP literal. |
| `main.targetSpec.defaultPort` | It selects the explicit port or the edition and address-family default. |
| `main.targetSpec.portForAddr` | It selects the explicit port or the edition default for one resolved address. |
| `main.targetSpec.fallbackEndpoint` | It creates the endpoint to use when Java SRV lookup does not give a route. |
| `main.parseDestination` | It reads a host, host and port, bracketed IPv6 value, or bare IPv6 value. |
| `main.versionLine` | It returns the program name and the build version. |

### Session functions

| Function | Input, result, and failure behavior |
| --- | --- |
| `main.sessionRuntime.withDefaults` | It supplies the normal clock and sleep function when either function is absent. |
| `main.defaultSessionRuntime` | It creates the normal clock and a context-aware timer. |
| `main.rttStats.add` | It adds one latency sample and updates minimum, maximum, mean, and variance data. |
| `main.rttStats.mdev` | It returns the population standard deviation for the latency samples. |
| `main.runTextSession` | It runs probes until the count, deadline, or context stops the session. It writes output and applies text-mode exit rules. |
| `main.packetLossPercent` | It calculates the percentage of sent probes that did not get a reply. |
| `main.writeSessionLine` | It writes one output line and can add a Unix timestamp. |
| `main.formatAddrPort` | It returns an address and port, or `unknown` for an invalid address. |

### Address and connection functions

| Function | Input, result, and failure behavior |
| --- | --- |
| `main.addressFamily.validate` | It accepts the automatic, IPv4, and IPv6 family values. |
| `main.addressFamily.resolverNetwork` | It returns `ip`, `ip4`, or `ip6` for the resolver. |
| `main.addressFamily.matches` | It reports if an address is in the selected family. |
| `main.addressFamily.forcedFlag` | It returns `-4`, `-6`, or an empty value. |
| `main.addressFamily.String` | It returns `IP`, `IPv4`, or `IPv6`. |
| `main.addressFamilyForAddr` | It returns the family of one IP address. |
| `main.newEndpoint` | It creates an endpoint with a normalized host. |
| `main.normalizeHost` | It removes outer white space and valid IPv6 brackets. |
| `main.unbracketIPv6Literal` | It removes brackets only from a valid IPv6 literal. |
| `main.endpoint.uint16Port` | It converts the endpoint port to an unsigned 16-bit value. |
| `main.endpoint.literalIP` | It returns the normalized address when the endpoint host is an IP literal. |
| `main.validateServerAddress` | It checks the 253-byte limit, brackets, controls, and colon use in a server name. |
| `main.toUint16` | It converts an integer from `0` through `65,535`. It rejects other values. |
| `main.mustParsePrefix` | It parses one built-in network prefix. It stops the program if the source constant is invalid. |
| `main.isNonPublicAddr` | It reports if an address is invalid or in a blocked non-public range. |
| `main.dialCandidate.Network` | It returns `tcp4` or `tcp6`. |
| `main.dialCandidate.UDPNetwork` | It returns `udp4` or `udp6`. |
| `main.dialCandidate.String` | It returns the candidate address and port. |
| `main.dialCandidateForLiteralIP` | It checks family and public-address policy for one literal target. |
| `main.dialCandidatesForResolvedIPs` | It makes candidates that use one common port. |
| `main.dialCandidatesForResolvedIPsByAddr` | It filters, de-duplicates, and orders resolved addresses. It rejects an empty usable result. |
| `main.buildDialCandidatesWithPortFunc` | It groups IPv4 and IPv6 addresses and alternates the two groups. |
| `main.interleaveDialCandidates` | It alternates candidates from a primary list and a secondary list. |
| `main.newPingClient` | It creates a client with the system resolver, dialer, token source, clock, and 250-millisecond fallback delay. |
| `main.pingClient.withDefaults` | It replaces absent client dependencies with normal dependencies. |
| `main.defaultDialContext` | It opens a network connection with the supplied context. |
| `main.pingClient.resolveDialCandidates` | It validates a literal target or resolves a host and makes candidates. |
| `main.pingClient.dialCandidates` | It uses the configured delay between address attempts. It returns the first successful connection. |
| `main.pingClient.dialCandidateAfterDelay` | It waits for its start delay and reports one connection result. |

### Java probe and protocol functions

| Function | Input, result, and failure behavior |
| --- | --- |
| `main.prepareProbe` | It selects the Java or Bedrock prepared-probe implementation. |
| `main.prepareJavaProbe` | It resolves the Java route and dial candidates before the session starts. |
| `main.javaPreparedProbe.banner` | It returns the Java session banner. Numeric mode can keep the host name when multiple candidates exist. |
| `main.javaPreparedProbe.summaryLabel` | It returns the host or the numeric address for the summary. |
| `main.javaPreparedProbe.observeSample` | It saves a valid remote address from a successful probe. |
| `main.javaPreparedProbe.probe` | It runs one prepared Java exchange. |
| `main.pingClient.resolveJavaRouteContext` | It checks SRV only for an implicit-port host name. It inspects only the first result. It uses that result only when the target, after removal of one trailing dot, is nonempty and the port is nonzero. When the lookup fails or returns no records, the function returns `ctx.Err()` when it is nonnil. Otherwise, it uses the default route. |
| `main.pingClient.pingJavaPreparedContext` | It connects and then applies the socket deadline. It validates status and measures the ping and pong exchange. |
| `main.remoteAddrPort` | It converts a network address to `netip.AddrPort`. It returns an invalid value on failure. |
| `main.generatePingToken` | It creates a cryptographically random 64-bit ping token. |
| `main.sendHandshakePacket` | It writes a Java status handshake for the target. |
| `main.sendStatusRequestPacket` | It writes a Java status request. |
| `main.sendPingPacket` | It writes a Java ping with one 64-bit token. |
| `main.readStatusResponse` | It reads one Java status packet and requires one complete JSON object. |
| `main.readPongPacket` | It reads one Java pong and requires the expected token. |
| `main.writePacket` | It writes one nonempty Java packet that is not larger than 2 MiB. |
| `main.readPacket` | It reads one positive packet length and its full payload within the supplied limit. |
| `main.readVarInt` | It reads a Java VarInt of at most five bytes. |
| `main.writeVarInt` | It writes one Java VarInt. |
| `main.readVarIntFromBytes` | It reads a Java VarInt from a byte slice and returns the consumed length. |
| `main.writeString` | It writes one length-prefixed string within the supplied byte limit. |
| `main.validateStringByteLength` | It rejects a string that exceeds the supplied limit or the signed 32-bit limit. |
| `main.readStringFromBytes` | It reads one length-prefixed UTF-8 string within the supplied byte limit. |

### Bedrock probe functions

| Function | Input, result, and failure behavior |
| --- | --- |
| `main.prepareBedrockProbe` | It resolves Bedrock candidates before the session starts. |
| `main.bedrockPreparedProbe.banner` | It returns the Bedrock session banner. Numeric mode can keep the host name when multiple candidates exist. |
| `main.bedrockPreparedProbe.summaryLabel` | It returns the host or the numeric address for the summary. |
| `main.bedrockPreparedProbe.observeSample` | It saves a valid remote address from a successful probe. |
| `main.bedrockPreparedProbe.probe` | It runs one prepared Bedrock exchange. |
| `main.pingClient.resolveBedrockCandidates` | It validates the target and makes UDP candidates with the IPv4 or IPv6 default port. |
| `main.pingBedrockCandidates` | It tries UDP candidates in order and returns the first successful sample. |
| `main.pingBedrockCandidate` | It connects and then applies the socket deadline. It sends one RakNet ping and validates a pong of at most 2,048 bytes. |
| `main.buildBedrockStatusRequest` | It makes a RakNet unconnected-ping request with a random client identifier. |
| `main.buildBedrockStatusRequestWith` | It makes the request with an injected random-byte reader. |
| `main.randomUint64With` | It gives an eight-byte zeroed buffer to the supplied callback. It ignores the returned byte count. It returns zero and wraps a callback error. Otherwise, it converts the full buffer to a big-endian 64-bit value. |
| `main.parseBedrockStatusResponse` | It checks the packet ID, timestamp, magic, exact length, UTF-8 data, and status text. |
| `main.parseBedrockStatusText` | It parses the required Bedrock status fields and available optional fields. |

### Staging-server functions

| Function | Input, result, and failure behavior |
| --- | --- |
| `cmd/staging-server.bindFlags` | It defines the Java, Bedrock, status, and deadline options for the staging server. |
| `cmd/staging-server.main` | It starts the staging server and exits with status `1` after a serve error. |
| `internal/stagingserver.DefaultStatusJSON` | It returns the default Java status object. |
| `internal/stagingserver.DefaultBedrockStatus` | It returns the default Bedrock status text for the supplied ports. |
| `internal/stagingserver.Serve` | It validates the configuration, opens enabled listeners, and serves until cancellation or error. |
| `internal/stagingserver.Config.withDefaults` | It supplies the default Java status and ten-second connection deadline. |
| `internal/stagingserver.validateStatusJSON` | It requires valid Java status JSON of at most 1 MiB. |
| `internal/stagingserver.validateBedrockStatus` | It requires valid UTF-8 Bedrock status text that fits a 16-bit length. |
| `internal/stagingserver.configuredPort` | It returns a port from a listen address or a fallback port. |
| `internal/stagingserver.defaultBedrockStatusForListeners` | It builds status text from the active IPv4 and IPv6 packet listeners. |
| `internal/stagingserver.closeListeners` | It closes all Java TCP listeners. |
| `internal/stagingserver.closePacketListeners` | It closes all Bedrock UDP listeners. |
| `internal/stagingserver.waitForServeExit` | It waits for cancellation or one serve error and then closes all listeners. |
| `internal/stagingserver.serveListener` | It accepts Java connections until cancellation or a listener error. |
| `internal/stagingserver.handleConn` | It performs one Java status and ping/pong exchange. |
| `internal/stagingserver.expectStatusHandshake` | It reads and validates one Java status handshake and request. |
| `internal/stagingserver.sendStatusJSON` | It writes one Java status response. |
| `internal/stagingserver.expectPingToken` | It reads one Java ping and returns its token. |
| `internal/stagingserver.sendPong` | It writes one Java pong with the supplied token. |
| `internal/stagingserver.writePacket` | It writes one staging Java packet within the 2 MiB limit. |
| `internal/stagingserver.readPacket` | It reads one staging Java packet within the supplied limit. |
| `internal/stagingserver.readVarInt` | It reads a staging Java VarInt of at most five bytes. |
| `internal/stagingserver.readVarIntFromBytes` | It reads a VarInt from bytes and returns the consumed length. |
| `internal/stagingserver.readStringFromBytes` | It reads one length-prefixed UTF-8 string within the supplied limit. |
| `internal/stagingserver.writeVarInt` | It writes one Java VarInt. |
| `internal/stagingserver.writeString` | It writes one length-prefixed string within the supplied limit. |
| `internal/stagingserver.serveBedrockListener` | It reads Bedrock pings and writes pongs until cancellation or error. |
| `internal/stagingserver.buildBedrockStatusResponse` | It reads a Bedrock ping and builds the matching pong. |
| `internal/stagingserver.parseBedrockUnconnectedPing` | It validates a RakNet unconnected ping and returns its timestamp. |
| `internal/stagingserver.encodeBedrockUnconnectedPong` | It writes a RakNet pong with the supplied timestamp and status. |
| `internal/stagingserver.Probe` | It runs one Java probe against a staging listener. |
| `internal/stagingserver.probeConn` | It performs the Java staging probe on an open connection. |
| `internal/stagingserver.ProbeBedrock` | It runs one Bedrock probe against a staging listener. |
| `internal/stagingserver.parseBedrockPong` | It validates the staging Bedrock pong and expected timestamp. |

### Release-integration functions

These functions support repository validation. They are not part of the
installed `minecraft-ping` command.

| Function | Input, result, and failure behavior |
| --- | --- |
| `cmd/release-integration.main` | It gives process arguments to `mainWithArgs` and exits with the returned status. |
| `cmd/release-integration.mainWithArgs` | It removes the program name and calls `runCLI`. |
| `cmd/release-integration.runCLI` | It parses and validates options, creates a signal context, and runs the harness. It returns status `0`, `1`, or `2`. |
| `cmd/release-integration.run` | It selects the binary, starts the backend, and runs all Java and Bedrock probes. |
| `cmd/release-integration.bindFlags` | It defines all release-integration options. |
| `cmd/release-integration.validatePort` | It requires a port from `1` through `65,535`. |
| `cmd/release-integration.validateConfig` | It checks the binary source, timeout, ports, and selected backend. |
| `cmd/release-integration.probeSpecs` | It returns the Java and Bedrock IPv4 and IPv6 probe cases. |
| `cmd/release-integration.versionFromArchiveName` | It reads the expected version from a release archive name. |
| `cmd/release-integration.assertVersion` | It runs the binary version option and requires the expected version. |
| `cmd/release-integration.versionLine` | It adds the program name to an expected version. |
| `cmd/release-integration.startBackend` | It starts the selected native or container staging backend. |
| `cmd/release-integration.startBinaryBackend` | It starts the native staging-server binary and waits for all listeners. |
| `cmd/release-integration.startContainerBackend` | It loads an optional image archive and starts the container backend. |
| `cmd/release-integration.resolveSingleFile` | It requires a glob to match exactly one path. |
| `cmd/release-integration.extractBinary` | It extracts one named binary from a ZIP or tar archive into a temporary directory. |
| `cmd/release-integration.extractZipBinary` | It extracts one safe regular ZIP entry within the size limit. |
| `cmd/release-integration.extractTarGzBinary` | It extracts one safe regular tar entry within the size limit. |
| `cmd/release-integration.copyWithLimit` | It copies data and rejects input larger than the supplied limit. |
| `cmd/release-integration.openReadOnlyFile` | It opens one path through an operating-system directory root without write access. |
| `cmd/release-integration.loadImage` | It gives a compressed image archive to the selected container CLI. |
| `cmd/release-integration.removeContainer` | It removes one container and ignores a not-found result. |
| `cmd/release-integration.isContainerNotFoundError` | It recognizes supported container not-found messages. |
| `cmd/release-integration.startContainer` | It starts the staging container with the configured ports. |
| `cmd/release-integration.waitForJava` | It waits for one Java listener to accept a valid probe. |
| `cmd/release-integration.waitForBedrock` | It waits for one Bedrock listener to accept a valid probe. |
| `cmd/release-integration.waitForListener` | It retries a listener probe until success, timeout, or cancellation. |
| `cmd/release-integration.runProbe` | It runs one JSON probe and decodes the result. |
| `cmd/release-integration.formatProbeTimeout` | It converts the harness timeout to decimal seconds for the command. |
| `cmd/release-integration.setIPv6Only` | It enables IPv6-only listener behavior on Unix. It returns an unsupported error on Windows. |
| `cmd/release-integration.ipv6OnlyControl` | It returns a network control function that applies IPv6-only behavior. |
| `cmd/release-integration.newIPv6Relay` | It opens an IPv6 TCP relay to an IPv4 target. |
| `cmd/release-integration.ipv6Relay.serve` | It accepts relay connections and forwards each connection. |
| `cmd/release-integration.proxyConns` | It copies data in both directions until both copy operations stop. |
| `cmd/release-integration.ipv6Relay.close` | It closes the TCP relay listener. |
| `cmd/release-integration.newUDPIPv6Relay` | It opens an IPv6 UDP relay to an IPv4 target. |
| `cmd/release-integration.udpIPv6Relay.serve` | It reads client datagrams and forwards each datagram. |
| `cmd/release-integration.udpIPv6Relay.forwardPacket` | It sends one datagram to the target and returns the response to the client. |
| `cmd/release-integration.udpIPv6Relay.close` | It closes the UDP relay socket. |
| `cmd/release-integration.runCleanup` | It runs cleanup functions in reverse order. |
| `cmd/release-integration.setDeadlineFromNow` | It sets a connection deadline from the current time. |
| `cmd/release-integration.setUDPRelayDeadline` | It sets the fixed UDP relay deadline. |

### Script functions

| Function | Input, result, and failure behavior |
| --- | --- |
| `scripts/release_archive_smoke.sh:require_single_archive` | It requires one release archive that matches a pattern. |
| `scripts/release_archive_smoke.sh:require_no_archive` | It requires that no release archive matches a pattern. |
| `scripts/release_archive_smoke.sh:check_tar_archive` | It checks required files in one tar archive. |
| `scripts/release_archive_smoke.sh:check_zip_archive` | It checks required files in one ZIP archive. |
| `scripts/release_archive_smoke.sh:check_source_archive` | It checks required files in the source archive. |
| `scripts/release_linux_package_smoke.sh:find_package` | It requires one package that matches the accepted name patterns. |
| `scripts/release_linux_package_smoke.sh:run_container_smoke` | It runs one package installation and command test in a pinned container. |
| `scripts/release_reproducibility.sh:resolve_goreleaser` | It finds the configured or installed GoReleaser binary. |
| `scripts/release_reproducibility.sh:cleanup` | It removes the temporary detached worktree. |
| `scripts/run_mutation_pr.sh:restore_package` | It restores the source package after PR mutation tests. |
| `scripts/run_mutation_supported.sh:restore_package` | It restores the source package after the supported mutation tests. |
| `scripts/sync_agent_surfaces.sh:write_context_mirror` | It writes one generated agent-instruction mirror. |
| `scripts/sync_agent_surfaces.sh:sync_skill_mirror` | It copies one canonical skill to a generated skill path. |
| `scripts/verify_agent_surfaces.sh:compare_path` | It compares one generated path with its expected canonical content. |
