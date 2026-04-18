<!-- GENERATED FROM AGENTS.md BY scripts/sync_agent_surfaces.sh. DO NOT EDIT DIRECTLY. -->

The canonical repository contract lives in `AGENTS.md`. This file is a generated mirror for Claude-native discovery.

# Repository Working Agreement

Use `.agents/skills/minecraft-ping-change-gate/SKILL.md` for every change in this repository.

Also load these repo-local skills when they apply:

- `.agents/skills/minecraft-ping-network-release-gate/SKILL.md` for networking, staging backend, release, packaging, workflow, or integration-harness changes.
- `.agents/skills/minecraft-ping-review-gate/SKILL.md` for review-only requests.
- `.agents/skills/minecraft-ping-release-gate/SKILL.md` for release preparation, tagging, or post-release verification.

## Priorities

1. Simplicity
2. Correctness
3. Linting and formatting
4. Appropriate test coverage
5. Security
6. Performance
7. Idiomatic Go
8. Dead-code and duplicate-test removal
9. Public-repo cleanliness

## Mandatory Rules

- Keep Java and Bedrock behavior explicit. Do not add protocol auto-detection or fake ICMP-only fields.
- Prefer small changes and existing repo commands over new wrappers, helper layers, or internal-only build paths.
- Before adding tooling or scripts, inspect `Makefile`, `docs/development.md`, and `.github/workflows/`.
- When behavior changes, update the tests, docs, man page, and changelog that describe it.
- Remove dead code, duplicate tests, stale docs, and internal-only references when they are adjacent to the work you are already touching.
- Do not commit machine-specific files, private infrastructure details, local-only build host instructions, generated release detritus, or temporary validation output.
- The canonical agent sources are `AGENTS.md` and `.agents/skills/`. Agent-native mirrors are generated. Update the canonical files, then run `make agents-sync` and `make agents-verify`.
- For Go-version claims, agent-surface behavior claims, or release-process claims, verify the current state from official docs or the local toolchain instead of guessing.

## Required Local Workflow

- Standard change loop:
  - `make verify`
  - `make coverage`
- When you edit `AGENTS.md` or `.agents/skills/`:
  - `make agents-sync`
  - `make agents-verify`
  - `make agents-verify` always checks generated-surface drift. It also runs a structured live Codex, Claude, and Gemini contract smoke when those CLIs are installed locally. Trusted PR runs and `Main Verify` require that live smoke once the repository secrets are configured; fork PRs keep the live smoke optional so secrets are not exposed.
- Logic-heavy parser, CLI, or protocol changes:
  - `make deadcode` when `deadcode` is installed locally
  - `make mutation` when `go-mutesting` is installed locally
- Networking, staging, packaging, workflow, or release-path changes:
  - `make integration`
- Release-archive or packaging changes:
  - `goreleaser release --snapshot --clean --skip=sign`
  - `make release-archive-smoke`
  - `make release-repro`
  - `make package-smoke ARCH=amd64`
- Before handoff or commit:
  - `make clean-repo`

## Portability

- `AGENTS.md` is the canonical repo instruction file.
- `CLAUDE.md` and `GEMINI.md` are generated mirrors of `AGENTS.md`.
- `.agents/skills/` is the canonical portable skill source.
- Codex and Gemini consume `.agents/skills/` directly.
- `.claude/skills/` is the generated mirror for Claude-native skill discovery.

