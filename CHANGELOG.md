# Changelog

All notable changes to this project will be documented in this file.

The format is intentionally simple and release-oriented.
Earlier tags exist in git history, but the changelog starts with the current public release line.

## Unreleased

## v2.0.7 - 2026-07-01

- Bumped Go toolchain to `1.26.4` (`go.mod` and the staging container image) to stay on the latest stable patch release.
- Pinned the staging container's golang base image by multi-arch index digest (`golang:1.26.4-bookworm@sha256:b305420…`), clearing the OpenSSF Scorecard `Pinned-Dependencies` (`containerImage`) finding on `docker/staging-minecraft.Dockerfile`.
- Removed live LLM CLI smoke tests, provider-key CI plumbing, checked-in agent CLI npm dependencies, old agent CLI ignore entries, unused benchmark scaffolding, stale release check gating, and opaque mutation blacklist suppressions. Agent-surface verification is now structural only.
- Applied Go 1.21–1.26 idiomatic upgrades across the module (range-over-int, `errors.AsType`, `slices.Backward`, `sync.WaitGroup.Go`, removed obsolete loop-variable captures, `sort.Strings` → `slices.Sort`, byte-slice comparisons via `slices.Equal`/`bytes.Equal`); behavior and public API unchanged.
- Bumped pinned GitHub Actions (`github/codeql-action` 4.35.5 → 4.36.2, `release-drafter/release-drafter` 7.3.0 → 7.4.0).
- Removed Intel macOS (`amd64`) from the release matrix, release validation, and published support surface; `v2.0.6` remains the final release with a `Darwin_amd64` archive.
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
