# minecraft-ping

## Overview
Go service and CLI for pinging Minecraft servers and reporting latency/status.

## Local Development
```bash
go test ./...
go run . -server 127.0.0.1 -port 25565 -allow-private
```

## CI and Security
- `CI`: security scan, build, tests, pinned-container staging smoke, mutation (PR), no production deploy.
- `Security Baseline`: secret scan (`gitleaks`).
- `Dependency Review`: PR-only dependency diff policy check.
- `Mutation Nightly`: scheduled and PR-scoped mutation checks.

## Operations
- Integration smoke (container-backed): `scripts/staging_smoke.sh`
- Staging container definition: `docker/staging-minecraft.Dockerfile` (`itzg/minecraft-server` pinned by digest)
- CI verification helper: `scripts/ci_wait_for_checks.sh`

## Security Notes
- Private target scanning requires explicit opt-in with `-allow-private`.
