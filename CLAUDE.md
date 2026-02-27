# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

Go CLI tool that measures latency to a Minecraft Java Edition server (protocol version 109+) using the native handshake → status → ping/pong sequence. Supports SRV record lookup and private address filtering.

## Commands

```bash
# Build
go build -v ./...

# Run
./minecraft-ping -server mc.example.com [-port 25565] [-timeout 5s] [-allow-private]

# Unit tests
go test -v ./...

# Fuzz tests (run locally; CI runs each for 30s)
go test ./... -run=^$ -fuzz=FuzzReadVarIntFromBytes -fuzztime=30s
go test ./... -run=^$ -fuzz=FuzzReadStringFromBytes -fuzztime=30s
go test ./... -run=^$ -fuzz=FuzzReadPacket -fuzztime=30s
```

## Architecture

- `minecraft-ping.go` — CLI entry point; flags: `-server`, `-port`, `-timeout`, `-allow-private`
- `ping_client.go` — core protocol logic (~500 lines): TCP connection, SRV lookup, VarInt encode/decode, packet serialization/deserialization, private address CIDR filtering
- `minecraft-ping_test.go` — unit tests with mock TCP server
- `ping_fuzz_test.go` — three fuzz targets for binary parser robustness

**CI** (`.github/workflows/go.yml`): build → unit tests → `govulncheck` → `gosec` → fuzz (15 min timeout).

## Code Review Notes (Feb 2026)

- **Maintainability**: Magic packet IDs (`0x00`, `0x01`) should be named constants — e.g. `packetIDHandshake`, `packetIDPing`, `nextStateStatus`
- **Maintainability**: No debug logging — consider a `-debug` flag for protocol-level tracing
- **Idiomatic**: Output is human-only (`"Ping time is 123"`) — optionally support `-format=json` for monitoring integration
