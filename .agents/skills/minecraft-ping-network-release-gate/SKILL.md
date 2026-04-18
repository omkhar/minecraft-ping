---
name: minecraft-ping-network-release-gate
description: "Use for networking, staging backend, release, packaging, workflow, or integration-harness changes in minecraft-ping. Adds the extra release-path and dual-stack validation gates."
---

# Minecraft Ping Network And Release Gate

Use this skill when a change touches Java or Bedrock networking, address resolution, the staging backend, release integration, packaging, or GitHub workflows.

## Extra Invariants

- The native staging backend and the container-backed path must keep matching the real release path.
- Release docs, workflow behavior, and artifact-verification instructions must describe the same process.
- Public release docs should assume a public GitHub repository and must not keep private-launch caveats.

## Required Workflow

- Run the normal change gate first.
- Run `make integration`.
- If the change affects packaging, archives, signing, provenance, SBOMs, or release automation, also run:
  - `goreleaser release --snapshot --clean --skip=sign`
  - `make release-archive-smoke`
  - `make release-repro`
  - `make package-smoke ARCH=amd64`
- When touching workflow policy, keep `README.md`, `docs/development.md`, `docs/releasing.md`, and `docs/release-verification.md` aligned with the workflow files.
- Prefer deleting internal-only release helpers over documenting them.
