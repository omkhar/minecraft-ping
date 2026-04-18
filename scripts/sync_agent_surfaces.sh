#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
dest_root="${AGENT_SYNC_DEST_ROOT:-${repo_root}}"
canonical_context="${repo_root}/AGENTS.md"
canonical_skills="${repo_root}/.agents/skills"

if [[ ! -f "${canonical_context}" ]]; then
  echo "missing canonical context file: ${canonical_context}" >&2
  exit 1
fi

if [[ ! -d "${canonical_skills}" ]]; then
  echo "missing canonical skills directory: ${canonical_skills}" >&2
  exit 1
fi

write_context_mirror() {
  local target_path="$1"
  local agent_name="$2"

  mkdir -p "$(dirname "${target_path}")"
  {
    printf '<!-- GENERATED FROM AGENTS.md BY scripts/sync_agent_surfaces.sh. DO NOT EDIT DIRECTLY. -->\n\n'
    printf 'The canonical repository contract lives in `AGENTS.md`. This file is a generated mirror for %s-native discovery.\n\n' "${agent_name}"
    cat "${canonical_context}"
    printf '\n'
  } > "${target_path}"
}

sync_skill_mirror() {
  local mirror_dir="$1"
  local target_dir="${dest_root}/${mirror_dir}"

  rm -rf "${target_dir}"
  mkdir -p "${target_dir}"

  local skill_dir
  for skill_dir in "${canonical_skills}"/*; do
    [[ -d "${skill_dir}" ]] || continue
    cp -R "${skill_dir}" "${target_dir}/"
  done
}

write_context_mirror "${dest_root}/CLAUDE.md" "Claude"
write_context_mirror "${dest_root}/GEMINI.md" "Gemini"

rm -rf "${dest_root}/.codex/skills" "${dest_root}/.gemini/skills"
rmdir "${dest_root}/.codex" "${dest_root}/.gemini" 2>/dev/null || true

sync_skill_mirror ".claude/skills"
