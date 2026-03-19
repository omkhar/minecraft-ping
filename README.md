# minecraft-ping

[![PR Auto-Merge](https://github.com/omkhar/minecraft-ping/actions/workflows/dependabot-auto-merge.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/dependabot-auto-merge.yml)
[![Dependency Review](https://github.com/omkhar/minecraft-ping/workflows/Dependency%20Review/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/dependency-review.yml)
[![CI](https://github.com/omkhar/minecraft-ping/workflows/CI/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/go.yml)
[![Mutation Nightly](https://github.com/omkhar/minecraft-ping/workflows/Mutation%20Nightly/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/mutation-nightly.yml)
[![Release](https://github.com/omkhar/minecraft-ping/actions/workflows/release.yml/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/release.yml)
[![Security Baseline](https://github.com/omkhar/minecraft-ping/workflows/Security%20Baseline/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/security-baseline.yml)
[![Semgrep](https://github.com/omkhar/minecraft-ping/workflows/Semgrep/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/semgrep.yml)
[![zizmor](https://github.com/omkhar/minecraft-ping/workflows/zizmor/badge.svg)](https://github.com/omkhar/minecraft-ping/actions/workflows/zizmor.yml)

## Overview
Go service and CLI for pinging Minecraft servers and reporting latency/status.

## Local Development
```bash
go test ./...
go run . -server 127.0.0.1 -port 25565 -allow-private
```

## CI and Security
- `CI`: security scan, build, tests, pinned-container staging smoke, mutation (PR), no production deploy.
- `Release`: builds cross-platform archives on tags, publishes GitHub release assets, and signs every release artifact with keyless `cosign`.
- `Security Baseline`: secret scan (`gitleaks`).
- `Dependency Review`: PR-only dependency diff policy check.
- `Mutation Nightly`: scheduled and PR-scoped mutation checks.

## Operations
- Integration smoke (container-backed): `scripts/staging_smoke.sh`
- Staging container definition: `docker/staging-minecraft.Dockerfile` (`itzg/minecraft-server` pinned by digest)
- CI verification helper: `scripts/ci_wait_for_checks.sh`

## Releases
- Tag `vX.Y.Z` on `main` to publish a GitHub release with signed `darwin`, `linux`, and `windows` archives for `amd64` and `arm64`.
- Each uploaded release asset includes a matching `.sigstore.json` bundle produced by `cosign` keyless signing in GitHub Actions.
- `checksums.txt` is also signed and published so consumers can verify the full release manifest.

Example verification:

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/omkhar/minecraft-ping/.github/workflows/release.yml@refs/tags/v.*$' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  checksums.txt
```

## Security Notes
- Private target scanning requires explicit opt-in with `-allow-private`.
