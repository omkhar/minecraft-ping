# Release Verification

`minecraft-ping` publishes signed release artifacts, signed SPDX SBOMs, and GitHub provenance data from the repository release workflow.

## Quick Verification With GitHub Attestations

Download the release assets you want to inspect:

```bash
gh release download vX.Y.Z \
  --repo omkhar/minecraft-ping \
  --pattern 'minecraft-ping_*' \
  --dir release-assets
```

Verify a downloaded artifact against the repository release workflow:

```bash
gh attestation verify release-assets/minecraft-ping_X.Y.Z_Linux_amd64.tar.gz \
  --repo omkhar/minecraft-ping \
  --signer-workflow omkhar/minecraft-ping/.github/workflows/release.yml \
  --source-ref refs/tags/vX.Y.Z \
  --deny-self-hosted-runners
```

Repeat that command for each artifact you care about.

## Optional Cosign Bundle Verification

Release artifacts and SBOMs are also accompanied by Sigstore bundles.
To verify a specific artifact bundle directly with `cosign`:

```bash
cosign verify-blob \
  --bundle release-assets/minecraft-ping_X.Y.Z_Linux_amd64.tar.gz.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/omkhar/minecraft-ping/\.github/workflows/release\.yml@refs/tags/v.+$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  release-assets/minecraft-ping_X.Y.Z_Linux_amd64.tar.gz
```

## What Releases Contain

Published release assets include:

- platform archives for macOS, Linux, and Windows
- Linux packages where supported by the release pipeline
- `checksums.txt`
- SPDX SBOM files and their Sigstore bundles
- GitHub provenance bundles for the published release artifacts
