# Governance

This document uses ASD-STE100 Simplified Technical English.

One maintainer manages `minecraft-ping`.

## Maintainer

- The repository owner serves as the default maintainer and release manager.
- The maintainer sets the roadmap, reviews changes, publishes releases, coordinates security work, and maintains repository policy.

## Decision Making

- The maintainer merges changes when they improve the project and preserve its design constraints.
- The maintainer may reject changes that increase protocol ambiguity, add misleading ICMP-like output, or add maintenance cost disproportionate to user value.
- For a substantial refactor or behavior change, open an issue before you change the code.
- This issue lets the maintainer and contributors agree on the direction.

## Contributions

- The maintainer assesses contributions by technical merit, scope, test coverage, and documentation quality.
- Prefer small, focused pull requests to broad, mixed-purpose patches.
- Contributors must follow the repository's [Code of Conduct](CODE_OF_CONDUCT.md), [Contributing](CONTRIBUTING.md), and [Security](SECURITY.md) guidance.

## Releases

- The maintainer cuts releases from `main`.
- GitHub Actions builds and publishes release artifacts from signed, annotated tags.
- The maintainer may delay a release if validation, provenance, or artifact quality checks are not satisfactory.

## Security

- The maintainer first handles security reports privately.
- The project discloses a report after a fix exists or after the participants agree on coordinated disclosure.
- The project develops security fixes on `main`.
- It does not maintain long-lived patch branches for older tags.

## Compatibility

- Treat the command-line interface and user-visible behavior as the primary compatibility surface.
- The project uses explicit Java and Bedrock behavior instead of auto-detection or speculative protocol abstraction.
- The maintainer may change internal scripts, test harnesses, and private implementation details to improve correctness or maintainability.
