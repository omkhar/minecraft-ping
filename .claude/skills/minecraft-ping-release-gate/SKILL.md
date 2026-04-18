---
name: minecraft-ping-release-gate
description: "Use for release preparation, tag creation, and post-release verification in minecraft-ping. Enforces the public release checklist, Go-version recheck, and verification workflow."
---

# Minecraft Ping Release Gate

Use this skill for release preparation, release recovery, tag validation, or post-release verification.

## Release Rules

- Recheck the latest stable Go patch release from the official Go release history before cutting a release. Do not guess.
- The release candidate must already be merged to `main`, and the release tag must point at the exact `main` head that passed `Main Verify`.
- Release docs, changelog entries, README examples, and artifact-verification instructions must match the workflow that will publish the release.
- Private vulnerability reporting should remain enabled for the public repository.

## Required Workflow

1. Run `make verify`, `make coverage`, and `make integration`.
2. If the release path changed, run:
   - `goreleaser release --snapshot --clean --skip=sign`
   - `make release-archive-smoke`
   - `make release-repro`
   - `make package-smoke ARCH=amd64`
3. Verify the release instructions in `docs/releasing.md` and consumer verification steps in `docs/release-verification.md`.
4. After publication, confirm the GitHub release has the expected archives, packages, checksums, SBOMs, and provenance bundles.
