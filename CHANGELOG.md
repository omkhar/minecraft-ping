# Changelog

All notable changes to this project will be documented in this file.

The format is intentionally simple and release-oriented.
Earlier tags exist in git history, but the changelog starts with the current public release line.

## Unreleased

- Added launch-readiness documentation, governance, support routing, release verification guidance, and contributor entrypoints.
- Tightened release-path validation, workflow coverage, and Go-idiomatic cleanup ahead of public launch.

## v2.0.5 - 2026-04-17

- Hardened network and release validation paths for the `v2.0.5` release.
- Updated pinned GitHub Actions dependencies and the Go toolchain to `1.26.2`.

## v2.0.4 - 2026-03-23

- Fixed the Syft certificate identity verification regex in release automation.
- Hardened input-surface coverage and deep validation fuzz scope.

## v2.0.3 - 2026-03-22

- Tightened open source release hygiene and aligned the module path.
- Improved PR mutation scope, reproducibility validation, Linux package smoke checks, and release pipeline hardening.

## v2.0.2 - 2026-03-22

- Enforced a stricter CLI parser contract.

## v2.0.1 - 2026-03-22

- Added the `minecraft-ping(1)` man page source and release smoke checks.
- Aligned documentation with the shipped CLI behavior.

## v2.0.0 - 2026-03-22

- Redesigned the CLI around explicit Java and Bedrock probing.
- Added release attestations and tightened release-package validation for the `v2` line.
