# Changelog

This document uses ASD-STE100 Simplified Technical English.

This file documents all notable project changes.

The format is intentionally simple and release-oriented.
Earlier tags exist in git history, but the changelog starts with the current public release line.

## Unreleased

- Made the release workflow delete stale same-tag draft releases before it stages assets. This prevents the publication of a leftover draft with old artifacts.

## v2.0.8 - 2026-09-06

- Removed the unused legacy programmatic ping API and simplified internal helpers. CLI behavior did not change.
- Bumped the Go toolchain to 1.27.1 (go.mod and the staging container image) and refreshed pinned CI tool versions.
- Updated Go, the staging image, CI tools, GitHub Actions, and fixed runner labels.
- Added a machine-checked runtime reference.
- Added complete function and limitation references.
- Added tests that keep the documentation surface consistent with the code.
- Fixed Windows CRLF handling and a port-reuse race in the test suites.

## v2.0.7 - 2026-07-01

- Bumped Go toolchain to `1.26.4` (`go.mod` and the staging container image) to stay on the latest stable patch release.
- Pinned the staging container's golang base image by multi-arch index digest (`golang:1.26.4-bookworm@sha256:b305420…`), clearing the OpenSSF Scorecard `Pinned-Dependencies` (`containerImage`) finding on `docker/staging-minecraft.Dockerfile`.
- Removed live LLM CLI smoke tests, provider-key CI plumbing, checked-in agent CLI npm dependencies, and old agent CLI ignore entries.
  Removed unused benchmarks, stale release gates, and opaque mutation suppressions. Agent-surface verification now checks structure only.
- Applied Go 1.21-1.26 idiomatic upgrades across the module.
  Used range-over-int, `errors.AsType`, `slices.Backward`, and `sync.WaitGroup.Go`.
  Removed obsolete loop-variable captures. Replaced `sort.Strings` with `slices.Sort`.
  Used `slices.Equal` or `bytes.Equal` for byte-slice comparisons. Behavior and public APIs did not change.
- Bumped pinned GitHub Actions (`github/codeql-action` 4.35.5 → 4.36.2, `release-drafter/release-drafter` 7.3.0 → 7.4.0).
- Removed Intel macOS (`amd64`) from the release matrix, release validation, and published support surface.
  `v2.0.6` remains the final release with a `Darwin_amd64` archive.
- Hardened release publication by keeping GoReleaser assets in a draft release until archive, provenance, and SBOM validation pass.

## v2.0.6 - 2026-04-18

- Added launch-readiness documentation, governance, support routing, release verification guidance, contributor entrypoints, and portable agent guidance for ongoing repository maintenance.
- Tightened release-path validation, workflow coverage, public-repo hygiene checks, and Go-idiomatic cleanup ahead of public launch.
- Fixed staging server shutdown ordering so listener cleanup happens before accept-loop waits on early serve errors.

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
