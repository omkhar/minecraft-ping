# Releasing

This document is the maintainer checklist for cutting a public release.

## Before You Tag

1. Confirm the repository is on the latest stable Go patch release you intend to support.
   Check the official release history at `https://go.dev/doc/devel/release`.
2. Update docs, changelog entries, and any user-visible examples that changed.
3. Run the local checks appropriate for the scope:

```bash
make verify
make coverage
make integration
```

If the release path changed, also build snapshot artifacts and run the artifact checks:

```bash
goreleaser release --snapshot --clean --skip=sign
make release-archive-smoke
make release-repro
make package-smoke ARCH=amd64
```

## Cut The Release

1. Merge the release candidate to `main`.
2. Wait for `Main Verify` to complete successfully on that exact commit.
3. Create a GitHub-verified signed, annotated `vX.Y.Z` tag at the current `main` head.
4. Push the tag to trigger the release workflow.

## After Publication

1. Inspect the GitHub release and confirm the expected archives, packages, checksums, SBOMs, and provenance assets are present.
2. Spot-check at least one published artifact using the steps in [docs/release-verification.md](release-verification.md).
3. Confirm the release notes and changelog are aligned.
4. If the public support or security process changed, make sure the repository landing pages still point to the right contact path.
5. Confirm GitHub private vulnerability reporting is enabled so the public security workflow matches [SECURITY.md](../SECURITY.md).
