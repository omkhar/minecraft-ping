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

agent_cli_manifest="${repo_root}/tools/agent-clis/package.json"
agent_cli_lockfile="${repo_root}/tools/agent-clis/package-lock.json"
if [[ ! -f "${agent_cli_manifest}" || ! -f "${agent_cli_lockfile}" ]]; then
  echo "agent CLI bootstrap must keep package.json and package-lock.json checked in" >&2
  exit 1
fi
if ! git -C "${repo_root}" ls-files --error-unmatch -- \
  tools/agent-clis/package.json \
  tools/agent-clis/package-lock.json >/dev/null; then
  echo "agent CLI bootstrap files must be tracked by git" >&2
  exit 1
fi
if git -C "${repo_root}" ls-files -- .github/agent-clis | grep -q .; then
  echo "agent CLI bootstrap files must live outside .github so Dependabot can update them" >&2
  exit 1
fi

if grep -E 'npm[[:space:]]+install[[:space:]].*-g|CODEX_CLI_VERSION|CLAUDE_CODE_VERSION|GEMINI_CLI_VERSION' \
  "${repo_root}/scripts/install_agent_clis.sh" \
  "${repo_root}/.github/workflows/go.yml" \
  "${repo_root}/.github/workflows/pr-fast.yml" >/dev/null; then
  echo "agent CLI bootstrap must use the checked-in lockfile instead of global npm installs or workflow CLI version env pins" >&2
  exit 1
fi

if grep -E '^[[:space:]]*npm[[:space:]]' "${repo_root}/scripts/install_agent_clis.sh" >/dev/null; then
  echo "agent CLI npm commands must run through the provider-secret-scrubbing wrapper" >&2
  exit 1
fi

if ! grep -q 'run_npm_without_provider_secrets ci --include=dev --ignore-scripts' "${repo_root}/scripts/install_agent_clis.sh" ||
  ! grep -q 'run_npm_without_provider_secrets rebuild @anthropic-ai/claude-code --foreground-scripts --ignore-scripts=false' "${repo_root}/scripts/install_agent_clis.sh"; then
  echo "agent CLI bootstrap must run npm ci with scripts disabled, then rebuild only Claude Code" >&2
  exit 1
fi

"${repo_root}/scripts/verify_agent_runtime_smoke.sh"
