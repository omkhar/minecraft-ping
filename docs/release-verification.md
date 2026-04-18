# Release Verification

`minecraft-ping` publishes signed release artifacts, signed SPDX SBOMs, checksums, and GitHub provenance data from the repository release workflow.

## Recommended Trust Check

For most consumers, the primary verification path is:

1. download the artifact you care about
2. verify it with `gh attestation verify`
3. optionally compare the published checksum and inspect the SBOM or provenance bundle for additional audit context

If `gh attestation verify` succeeds against the repository release workflow and tag, you have verified that the artifact matches a GitHub-published attestation for that release.

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

### Platform Filename Map

Use the asset name that matches your platform:

- release archives follow `minecraft-ping_X.Y.Z_<OS>_<ARCH>`
- macOS, Linux, and source archives use `.tar.gz`
- Windows archives use `.zip`
- Linux packages use `minecraft-ping_X.Y.Z_linux_<arch>.<format>`

Examples:

- `minecraft-ping_X.Y.Z_Darwin_arm64.tar.gz`
- `minecraft-ping_X.Y.Z_Linux_amd64.tar.gz`
- `minecraft-ping_X.Y.Z_Windows_amd64.zip`
- `minecraft-ping_X.Y.Z_source.tar.gz`
- `minecraft-ping_X.Y.Z_linux_arm64.deb`

The attestation flow is the same for each published artifact. Replace the filename in the example command with the asset you downloaded.

## Checksums

`checksums.txt` is a convenience index of release-asset digests.
It is useful when you are mirroring assets, verifying multiple downloads at once, or comparing a local file against the published digest list.

Typical flow:

1. download `checksums.txt`
2. find the expected line for your artifact
3. compute the local SHA-256 digest
4. confirm the values match

Example:

```bash
grep '<artifact>$' release-assets/checksums.txt
```

Then compute the local digest with the tool that matches your platform:

- macOS: `shasum -a 256 release-assets/<artifact>`
- Linux: `sha256sum release-assets/<artifact>`
- PowerShell: `Get-FileHash release-assets/<artifact> -Algorithm SHA256`

The checksum file is supplementary. The primary trust decision should still come from `gh attestation verify` or the corresponding provenance bundle.

## Optional Cosign Bundle Verification

Release artifacts and SBOMs are also accompanied by Sigstore bundles.
Use these when you want an offline-style blob verification path instead of the GitHub attestation flow.

To verify a specific artifact bundle directly with `cosign`:

```bash
cosign verify-blob \
  --bundle release-assets/minecraft-ping_X.Y.Z_Linux_amd64.tar.gz.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/omkhar/minecraft-ping/\.github/workflows/release\.yml@refs/tags/v.+$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  release-assets/minecraft-ping_X.Y.Z_Linux_amd64.tar.gz
```

To verify a generated SBOM the same way, point `cosign verify-blob` at the `.spdx.json` file and its `.spdx.json.sigstore.json` bundle.

## Provenance Bundles

Public releases also include `.provenance.jsonl` assets.
These are downloadable copies of the GitHub artifact attestations for each published release artifact.

- To download one directly:

```bash
gh release download vX.Y.Z \
  --repo omkhar/minecraft-ping \
  --pattern 'minecraft-ping_X.Y.Z_Linux_amd64.tar.gz.provenance.jsonl' \
  --dir release-assets
```

- If you use `gh attestation verify`, you do not need the provenance bundles separately.
- If you need an auditable offline artifact trail, keep the provenance bundle alongside the release asset it describes.
- The supported verification command remains `gh attestation verify`; the provenance bundle is the archived copy of the attestation that command checks against.

## SBOM Assets

Each published artifact also has:

- an SPDX SBOM file: `.spdx.json`
- a Sigstore bundle for that SBOM: `.spdx.json.sigstore.json`

The SBOMs describe artifact contents. They are useful for inventory, dependency review, and downstream compliance checks.
They supplement artifact verification rather than replace it.

## What Releases Contain

Published release assets include:

- platform archives for macOS, Linux, and Windows
- a source archive
- Linux packages where supported by the release pipeline
- `checksums.txt`
- SPDX SBOM files and their Sigstore bundles
- GitHub provenance bundles for the published release artifacts
