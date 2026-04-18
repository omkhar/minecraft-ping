#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
common_prompt="Read the repository instructions for this workspace and reply with exactly one line in this format, with no extra text: <instruction-file>|<always-on-skill-path>|<first-standard-change-loop-command>|<second-standard-change-loop-command>|<claude-skill-mirror-dir>|<first-prohibited-item-from-the-do-not-commit-rule>. Use the exact repo-specific values from the canonical instructions."
expected_contract="AGENTS.md|.agents/skills/minecraft-ping-change-gate/SKILL.md|make verify|make coverage|.claude/skills|machine-specific files"

missing_binary_is_error() {
  local binary="$1"

  if [[ "${AGENT_SMOKE_REQUIRE_ALL:-0}" == "1" ]]; then
    return 0
  fi
  case "${binary}" in
    codex)
      [[ "${AGENT_SMOKE_REQUIRE_CODEX:-0}" == "1" ]]
      ;;
    claude)
      [[ "${AGENT_SMOKE_REQUIRE_CLAUDE:-0}" == "1" ]]
      ;;
    gemini)
      [[ "${AGENT_SMOKE_REQUIRE_GEMINI:-0}" == "1" ]]
      ;;
    *)
      return 1
      ;;
  esac
}

run_if_available() {
  local name="$1"
  local binary="$2"
  shift
  shift

  if ! command -v "${binary}" >/dev/null 2>&1; then
    if missing_binary_is_error "${binary}"; then
      echo "missing required ${name} smoke binary: ${binary}" >&2
      exit 1
    fi
    echo "skipping ${name} smoke: ${binary} not installed"
    return 0
  fi

  "$@"
}

extract_contract_line() {
  local output="$1"

  printf '%s\n' "${output}" |
    awk -F'|' 'NF == 6 {print $0}' |
    tail -n 1
}

check_contract() {
  local name="$1"
  local output="$2"
  local contract
  local normalized

  contract="$(extract_contract_line "${output}")"
  if [[ -z "${contract}" ]]; then
    echo "${name} smoke output did not contain a contract line" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi
  normalized="${contract//\`/}"
  normalized="${normalized//|.claude\/skills\/|/|.claude\/skills|}"
  if [[ "${normalized}" != "${expected_contract}" ]]; then
    echo "${name} smoke output did not match the expected repo contract" >&2
    printf 'expected: %s\nactual:   %s\n' "${expected_contract}" "${contract}" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi
}

run_codex_smoke() {
  local output
  output="$(cd "${repo_root}" && codex exec --ephemeral -s read-only "${common_prompt}" 2>&1)"
  check_contract "Codex" "${output}"
}

run_claude_smoke() {
  local output
  output="$(cd "${repo_root}" && claude --permission-mode plan -p "/minecraft-ping-change-gate ${common_prompt}" 2>&1)"
  check_contract "Claude" "${output}"
}

run_gemini_smoke() {
  local output
  output="$(cd "${repo_root}" && gemini --approval-mode plan -p "${common_prompt}" 2>&1)"
  check_contract "Gemini" "${output}"
}

run_if_available "Codex" "codex" run_codex_smoke
run_if_available "Claude" "claude" run_claude_smoke
run_if_available "Gemini" "gemini" run_gemini_smoke
