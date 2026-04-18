#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

canonical_skills="${repo_root}/.agents/skills"

if [[ ! -d "${canonical_skills}" ]]; then
  echo "missing canonical skills directory: ${canonical_skills}" >&2
  exit 1
fi

found_skill=0
for skill_dir in "${canonical_skills}"/*; do
  [[ -d "${skill_dir}" ]] || continue
  found_skill=1
  if [[ ! -f "${skill_dir}/SKILL.md" ]]; then
    echo "canonical skill is missing SKILL.md: ${skill_dir}" >&2
    exit 1
  fi
done

if [[ "${found_skill}" -ne 1 ]]; then
  echo "no canonical skills found under ${canonical_skills}" >&2
  exit 1
fi

AGENT_SYNC_DEST_ROOT="${tmp_root}" "${repo_root}/scripts/sync_agent_surfaces.sh"

compare_path() {
  local relative_path="$1"
  local expected_path="${tmp_root}/${relative_path}"
  local actual_path="${repo_root}/${relative_path}"

  if ! git diff --no-index --exit-code -- "${expected_path}" "${actual_path}" >/dev/null; then
    echo "generated agent surface drift detected for ${relative_path}" >&2
    git diff --no-index -- "${expected_path}" "${actual_path}" || true
    exit 1
  fi
}

compare_path "CLAUDE.md"
compare_path "GEMINI.md"
compare_path ".claude/skills"

"${repo_root}/scripts/verify_agent_runtime_smoke.sh"
