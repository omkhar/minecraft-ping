# Governance

`minecraft-ping` is currently a single-maintainer project.

## Maintainer

- The repository owner is the default maintainer and release manager.
- The maintainer is responsible for roadmap decisions, review, release publication, security coordination, and repository policy.

## Decision Making

- Changes merge when they improve the project and preserve its design constraints.
- The maintainer may reject changes that increase protocol ambiguity, add misleading ICMP-like output, or add maintenance cost disproportionate to user value.
- For substantial refactors or behavior changes, open an issue before implementation so direction can be aligned first.

## Contributions

- Contributions are reviewed on technical merit, scope, test coverage, and documentation quality.
- Small, focused pull requests are preferred over broad mixed-purpose patches.
- Contributors are expected to follow the repository's [Code of Conduct](CODE_OF_CONDUCT.md), [Contributing](CONTRIBUTING.md), and [Security](SECURITY.md) guidance.

## Releases

- Releases are cut from `main`.
- Release artifacts are built and published from GitHub Actions using signed, annotated tags.
- The maintainer may delay a release if validation, provenance, or artifact quality checks are not satisfactory.

## Security

- Security reports are handled privately first.
- Public disclosure happens after a fix is available or when coordinated disclosure has been agreed.
- Security fixes are developed on `main`; this repository does not maintain long-lived patch branches for older tags.

## Compatibility

- The command-line interface and user-visible behavior are treated as the primary compatibility surface.
- The project prefers explicit Java and Bedrock behavior over auto-detection or speculative protocol abstraction.
- Internal scripts, test harnesses, and private implementation details may change when that improves correctness or maintainability.
