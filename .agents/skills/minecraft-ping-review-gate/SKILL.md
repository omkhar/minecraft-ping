---
name: minecraft-ping-review-gate
description: "Use for code review in minecraft-ping. Enforces a findings-first review focused on correctness, regressions, test gaps, security, performance, simplicity, and public-repo hygiene."
---

# Minecraft Ping Review Gate

Use this skill for review-only requests in this repository.

## Review Priorities

1. Correctness bugs and behavioral regressions
2. Missing or misleading tests
3. Security issues
4. Performance regressions
5. Complexity that can be removed without changing behavior
6. Stale docs, dead code, duplicate tests, or internal-only details that should not survive in a public repo

## Review Rules

- Lead with actionable findings, ordered by severity.
- Include file and line references for each finding.
- Treat missing tests as findings when a change adds meaningful behavior or risk.
- Prefer pointing out simpler designs when they reduce maintenance without hiding protocol behavior.
- If no findings remain, say so explicitly and mention any residual risk or validation gaps.
