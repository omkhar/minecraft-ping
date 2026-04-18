---
name: minecraft-ping-change-gate
description: "Use for every change in minecraft-ping. Enforces the repo's change gate: simplicity first, correctness, linting, appropriate tests, security, performance, idiomatic Go, dead-code cleanup, and public-repo hygiene."
---

# Minecraft Ping Change Gate

Use this skill for every change in this repository.

## Priorities

1. Prefer the simplest change that preserves behavior.
2. Preserve correctness first.
3. Keep formatting, lint, and workflow checks green.
4. Add or update the smallest useful tests for the behavior you changed.
5. Avoid security and performance regressions.
6. Keep the code current with supported Go idioms.
7. Remove dead code, duplicate tests, stale docs, and other detritus while you are in the area.
8. Keep the repo ready for public consumption at all times.

## Repo Invariants

- The supported Go version comes from `go.mod`. Verify any "latest stable Go" claim against the official Go release history before changing it.
- Java and Bedrock transport paths stay explicit and separate.
- The CLI must not fake ICMP-only fields or protocol-detect by guessing.
- Public docs and tracked files must not contain internal-only build hosts, local machine paths, or stale pre-public caveats.
- `AGENTS.md` and `.agents/skills/` are canonical. Agent-native mirrors are generated.

## Required Workflow

- Inspect `Makefile`, `docs/development.md`, and `.github/workflows/` before adding new commands or abstractions.
- Add or update focused tests for every behavior change.
- Update docs, the man page, and the changelog when user-visible behavior changes.
- Run checks in this order:
  - `make verify`
  - `make coverage`
  - `make deadcode` when `deadcode` is installed locally
  - `make mutation` when `go-mutesting` is installed locally and the change touches a supported non-`main` package
- For `package main` code, strengthen focused unit, mutation-killer, and integration tests instead of pretending the mutation tool covers it.
- Run `go fix ./...` when it simplifies the code or applies current Go idioms without changing behavior.
- Run `make clean-repo` before handoff or commit.
- Get peer review on changed files and fix real findings in code instead of layering on explanation.
