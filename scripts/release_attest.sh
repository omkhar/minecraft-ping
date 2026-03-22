#!/usr/bin/env bash
set -euo pipefail

dist_dir="${1:-dist}"
sbom_dir="${dist_dir}/sbom"
attest_dir="${dist_dir}/attestation"
workflow_path="${RELEASE_WORKFLOW_PATH:-.github/workflows/release.yml}"
server_url="${GITHUB_SERVER_URL:-https://github.com}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required env: ${name}" >&2
    exit 1
  fi
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi

  shasum -a 256 "$1" | awk '{print $1}'
}

require_cmd cosign
require_cmd jq

if [[ ! -d "${sbom_dir}" ]]; then
  echo "SBOM directory not found: ${sbom_dir}" >&2
  exit 1
fi

mapfile -t subjects < <(
  find "${dist_dir}" -maxdepth 1 -type f -name 'minecraft-ping_*' ! -name '*.sigstore.json' | sort
)
if [[ "${#subjects[@]}" -eq 0 ]]; then
  echo "No release artifacts found in ${dist_dir}" >&2
  exit 1
fi

repo="${GITHUB_REPOSITORY:-local/local}"
ref="${GITHUB_REF:-refs/tags/${GITHUB_REF_NAME:-local}}"
tag="${GITHUB_REF_NAME:-local}"
sha="${GITHUB_SHA:-unknown}"
run_id="${GITHUB_RUN_ID:-local}"
run_attempt="${GITHUB_RUN_ATTEMPT:-1}"
workflow_name="${GITHUB_WORKFLOW:-Release}"
event_name="${GITHUB_EVENT_NAME:-workflow_dispatch}"
actor="${GITHUB_ACTOR:-unknown}"
repository_id="${GITHUB_REPOSITORY_ID:-unknown}"
repository_owner_id="${GITHUB_REPOSITORY_OWNER_ID:-unknown}"
repository_visibility="${GITHUB_REPOSITORY_VISIBILITY:-private}"
runner_environment="${RUNNER_ENVIRONMENT:-unknown}"

mkdir -p "${attest_dir}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

sign_args=(--yes)
verify_args=()

if [[ -n "${COSIGN_ATTEST_KEY:-}" ]]; then
  require_env COSIGN_ATTEST_VERIFY_KEY
  sign_args+=(--key "${COSIGN_ATTEST_KEY}")
  verify_args=(--key "${COSIGN_ATTEST_VERIFY_KEY}")
else
  require_env GITHUB_REPOSITORY
  require_env GITHUB_REF
  require_env GITHUB_REF_NAME
  require_env GITHUB_SHA
  require_env GITHUB_RUN_ATTEMPT
  require_env GITHUB_RUN_ID
  require_env GITHUB_WORKFLOW
  require_env GITHUB_EVENT_NAME

  cert_identity="${COSIGN_ATTEST_IDENTITY:-${server_url}/${repo}/${workflow_path}@${ref}}"
  verify_args=(
    --certificate-identity "${cert_identity}"
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com"
    --certificate-github-workflow-name "${workflow_name}"
    --certificate-github-workflow-ref "${ref}"
    --certificate-github-workflow-repository "${repo}"
    --certificate-github-workflow-sha "${sha}"
    --certificate-github-workflow-trigger "${event_name}"
  )
fi

generated=0

for subject in "${subjects[@]}"; do
  base="$(basename "${subject}")"
  sbom="${sbom_dir}/${base}.spdx.json"
  if [[ ! -f "${sbom}" ]]; then
    echo "Missing SBOM for ${base}: ${sbom}" >&2
    exit 1
  fi

  subject_sha256="$(sha256_file "${subject}")"
  provenance_predicate="${tmpdir}/${base}.provenance-predicate.json"

  jq -n \
    --arg actor "${actor}" \
    --arg asset "${base}" \
    --arg event_name "${event_name}" \
    --arg owner_id "${repository_owner_id}" \
    --arg ref "${ref}" \
    --arg repo "${repo}" \
    --arg repository_id "${repository_id}" \
    --arg run_attempt "${run_attempt}" \
    --arg run_id "${run_id}" \
    --arg runner_environment "${runner_environment}" \
    --arg server_url "${server_url}" \
    --arg sha "${sha}" \
    --arg subject_sha256 "${subject_sha256}" \
    --arg tag "${tag}" \
    --arg visibility "${repository_visibility}" \
    --arg workflow_name "${workflow_name}" \
    --arg workflow_path "${workflow_path}" \
    '{
      buildDefinition: {
        buildType: "https://actions.github.io/buildtypes/workflow/v1",
        externalParameters: {
          workflow: {
            repository: ($server_url + "/" + $repo),
            path: $workflow_path,
            ref: $ref,
            name: $workflow_name
          },
          release: {
            tag: $tag,
            asset: $asset
          }
        },
        internalParameters: {
          github: {
            actor: $actor,
            event_name: $event_name,
            repository_id: $repository_id,
            repository_owner_id: $owner_id,
            repository_visibility: $visibility,
            runner_environment: $runner_environment
          }
        },
        resolvedDependencies: [
          {
            uri: ("git+" + $server_url + "/" + $repo + "@" + $ref),
            digest: {
              gitCommit: $sha
            }
          },
          {
            uri: ("file:" + $asset),
            digest: {
              sha256: $subject_sha256
            }
          }
        ]
      },
      runDetails: {
        builder: {
          id: ($server_url + "/" + $repo + "/" + $workflow_path + "@" + $ref)
        },
        metadata: {
          invocationId: ($server_url + "/" + $repo + "/actions/runs/" + $run_id + "/attempts/" + $run_attempt)
        }
      }
    }' > "${provenance_predicate}"

  provenance_bundle="${attest_dir}/${base}.provenance.sigstore.json"
  sbom_bundle="${attest_dir}/${base}.sbom.sigstore.json"

  cosign attest-blob "${sign_args[@]}" \
    --type slsaprovenance1 \
    --predicate "${provenance_predicate}" \
    --bundle "${provenance_bundle}" \
    "${subject}"

  cosign attest-blob "${sign_args[@]}" \
    --type spdxjson \
    --predicate "${sbom}" \
    --bundle "${sbom_bundle}" \
    "${subject}"

  cosign verify-blob-attestation "${verify_args[@]}" \
    --type slsaprovenance1 \
    --bundle "${provenance_bundle}" \
    "${subject}" >/dev/null

  cosign verify-blob-attestation "${verify_args[@]}" \
    --type spdxjson \
    --bundle "${sbom_bundle}" \
    "${subject}" >/dev/null

  generated=$((generated + 2))
done

printf 'generated %d attestation bundles in %s\n' "${generated}" "${attest_dir}"
